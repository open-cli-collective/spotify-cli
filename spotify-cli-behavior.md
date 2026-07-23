# `sptfy` CLI Behavioral Specification

## Authority and release scope

This document defines Spotify-specific behavior. The policies in
[`cli-common`](https://github.com/open-cli-collective/cli-common/tree/main/docs)
are authoritative for command naming, state, secrets, output, scriptability,
CI, release, and distribution. When this document differs, `cli-common` wins
and this document must be corrected.

The currently implemented surface contains exactly these commands:

- `sptfy init`
- `sptfy set-credential`
- `sptfy config show|path|clear`
- `sptfy me`
- `sptfy search track <query>`
- `sptfy search album <query>`
- `sptfy search artist <query>`

The remaining catalog and library ideas under [Future roadmap](#future-roadmap)
are directional and non-normative until promoted into the command sections.

## Authentication and configuration

`sptfy` uses Spotify Authorization Code with PKCE for user authorization. The
user supplies a Spotify client ID; the CLI never accepts or stores a client
secret. The CLI currently requests only `user-read-private`; catalog search
does not require an additional scope. Later commands may require
reauthorization for additional scopes.

OAuth access and refresh material is stored only in a `cli-common/credstore`
backend under the configured credential reference. It is never stored in the
configuration file, read from a runtime Spotify-token environment variable, or
included in output and errors.

### `sptfy init`

`init` is the interactive and scriptable PKCE setup flow. It collects
non-secret configuration, completes authorization, writes the OAuth credential,
and verifies the identity unless verification is explicitly disabled.

The setup flow follows the shared wizard contract:

- The redirect URI defaults to `http://127.0.0.1/callback` and must be
  allowlisted in the Spotify application. A configured redirect URI may use
  `127.0.0.1` with an explicit port. In ordinary callback mode, a portless URI
  binds an available loopback port under Spotify's dynamic-port exception.
  `--auth-code-stdin` starts no listener and keeps the configured URI. The exact
  URI resolved for authorization is reused for token exchange. `localhost` and
  non-loopback HTTP redirects are rejected.
- Every interactive input has an equivalent flag.
- `--non-interactive` or non-TTY stdin disables prompts.
- `--no-browser` suppresses browser opening but still prints the authorization
  URL to stderr.
- `--auth-code-stdin` reads the complete redirected URL from stdin and implies
  `--no-browser`. A raw authorization code is rejected because it cannot prove
  the returned OAuth state.
- `--overwrite` permits replacement of an existing credential and has no other
  meaning.
- `--no-verify` skips the post-authorization identity check.

### `sptfy set-credential`

`set-credential` is the low-level one-secret ingress path. It supports the
shared `--ref`, `--key oauth_token`, exactly one of `--stdin` or `--from-env`,
`--overwrite`, and `--json` surface. It validates the OAuth token envelope but
does not call Spotify or echo secret material.

### `sptfy config`

- `config show` reports non-secret configuration, active credential reference,
  backend selection/source, and OAuth credential presence.
- `config path` reports the resolved state paths.
- `config clear` removes credentials for the active reference while retaining
  reusable non-secret configuration.
- `config clear --all` additionally removes configuration and owned cache
  state. It supports `--dry-run` and never touches other credential references.

`config show`, `config path`, and `config clear` support `--json` because they
are control-plane operations.

### `sptfy me`

`me` is the canonical health check. It exits successfully only when
configuration is complete, Spotify is reachable, the credential is usable or
refreshable, and the current-user request succeeds.

Text output contains the stable Spotify account ID, display name when present,
Spotify user ID and URI when present, and the granted OAuth scopes. `me --json`
is allowed because `me` is a control-plane diagnostic. Failures are concise,
actionable, secret-free messages on stderr.

## Output contract

Resource commands emit token-efficient text, never JSON.

- Primary data goes to stdout.
- Prompts, progress, warnings, retry notices, continuation hints, and errors go
  to stderr.
- Lists use stable `ALL_CAPS` headers and ` | ` separators.
- Empty values render as `-`.
- Embedded newlines, carriage returns, and the structural ` | ` sequence are
  replaced inside cells so one resource remains one line.
- Production resource output has no color.

JSON is supported only by these initial control-plane operations:

- `set-credential`
- `config show`
- `config path`
- `config clear`
- `me`

### Output-shape flags

Resource commands register only meaningful shared flags:

- `--id` emits one primary Spotify ID per line and overrides every other output
  shape.
- `--extended` adds less-frequent metadata such as Spotify URIs, URLs, disc and
  track positions, explicit status, and restrictions.
- `--fields <csv>` replaces the selected columns. Matching is
  case-insensitive; an unknown field fails with the valid field list.
- `--include-artwork` adds Spotify-provided image URL and dimensions without
  downloading image data.

`--fulltext` is not registered for track search because that result has no
prose field requiring truncation control.

### Relationship breadcrumbs

Spotify catalog relationships are a graph, not a single parent tree. Outputs
therefore expose explicit identifiers instead of a generic `parent` object.

- Track rows include the associated album ID and every credited artist ID.
- Album rows include every credited artist ID.
- Parent-scoped future commands render the parent identity once above the child
  table instead of repeating it in every row.
- Membership and mutation commands do not make extra metadata calls solely to
  manufacture breadcrumbs.

These identifiers are part of the contextually rich default output so an agent
can traverse track → album and track/album → artists without first widening the
result.

## Track search

### `sptfy search track <query>`

Search accepts exactly one non-empty textual query. Spotify query syntax such
as `artist:`, `album:`, `track:`, `year:`, `genre:`, and `isrc:` is passed
through unchanged.

Flags:

- `-m, --max <count>` sets the page size. The default and maximum are 10 because
  Spotify Development Mode caps each search request at 10 results.
- `--next-page-token <token>` resumes the next page. The token is opaque to
  callers, owned by `sptfy`, bound to track search, and validated before any
  network request.
- `--id`, `--extended`, `--fields`, and `--include-artwork` follow the shared
  output rules above.

Default output:

```text
ID | TRACK | ARTIST_IDS | ARTISTS | ALBUM_ID | ALBUM | DURATION
```

Multiple credited artists are comma-separated in the corresponding cells. The
duration uses `m:ss`, or `h:mm:ss` for tracks at least one hour long. Artwork is
omitted unless explicitly requested.

When another page exists, the command writes this shape to stderr:

```text
More results available (next: <opaque-token>)
```

An empty result is successful and emits no fabricated row. Invalid queries,
page sizes, page tokens, and field selections fail before the Spotify request.

## Album search

### `sptfy search album <query>`

Album search follows the track-search query, pagination, validation, and output
shape rules. Its continuation tokens are bound to album search.

Default output:

```text
ID | ALBUM | ARTIST_IDS | ARTISTS | RELEASE_DATE | TOTAL_TRACKS
```

`--extended` adds `URI`, `URL`, `ALBUM_TYPE`,
`RELEASE_DATE_PRECISION`, and `RESTRICTION`. `--include-artwork` adds
`ARTWORK`. Every credited artist ID and name is emitted in its corresponding
comma-separated cell.

## Artist search

### `sptfy search artist <query>`

Artist search follows the track-search query, pagination, validation, and
output shape rules. Its continuation tokens are bound to artist search.

Default output:

```text
ID | ARTIST | GENRES
```

`--extended` adds `URI` and `URL`. `--include-artwork` adds `ARTWORK`.
Popularity and follower data are not requested or rendered.

## Request behavior

- Requests use fixed Spotify account/API origins; continuation data never
  supplies a URL to follow.
- OAuth access tokens refresh when possible and the refreshed credential is
  persisted back to the same allowed key.
- `429` responses honor `Retry-After`; retryable `502`, `503`, and `504`
  responses use conservative retries bounded to three total attempts.
- Retries stop on context cancellation and never apply to unrelated client
  errors.
- Response/error bodies are size-limited before decoding or reporting.
- Authorization headers, access tokens, refresh tokens, authorization codes,
  and PKCE verifier/state material never appear in logs or errors.

## Acceptance suite

The initial suite proves only the shipped surface.

### Control plane

- Interactive and non-interactive setup have flag parity.
- Browser, no-browser, redirected-URL stdin, denial, timeout, overwrite,
  verification, and rollback paths behave deterministically.
- Every production credential backend can be selected; memory is test-only.
- `set-credential` accepts only the declared token key and secret ingress
  channels.
- Configuration output reports non-secret values, credential presence, and
  backend metadata, but never credential contents or other secret values.
- `config clear`, `config clear --dry-run`, and `config clear --all` remain
  scoped to the active credential reference.
- `me` succeeds for valid/refreshable credentials and fails for missing,
  revoked, insufficient, or unreachable authorization.

### Track search

Exercise ordinary, Spotify-qualified, quoted, Unicode, and guaranteed-no-match
queries. Cover page sizes 1 and 10, continuation-token resume, ID-only output,
explicit fields, extended fields, artwork inclusion, and exact stdout/stderr
routing.

Invalid empty queries, non-positive or over-10 page sizes, malformed or
wrong-surface page tokens, and unknown fields must fail before network I/O.
Tests also cover token refresh, rate limiting, bounded transient retries,
cancellation, bounded bodies, and cell sanitization.

### Album and artist search

Exercise ordinary, qualified, quoted, and Unicode queries. Cover exact default,
ID-only, selected-field, extended, and artwork output; empty pages; page sizes
1 and 10; and continuation resume for both resource types.

Invalid queries, page sizes, fields, malformed tokens, and tokens issued for a
different search resource fail before network I/O. Fixture tests verify fixed
origin requests, response validation, and that Spotify pagination URLs are
never followed.

Live tests are opt-in and use a dedicated Spotify application/account plus a
hermetic state directory and an explicitly selected encrypted-file credential
backend rooted there. They never run in ordinary CI or mutate a developer's
normal Spotify configuration or OS keychain.

## Future roadmap

The following ideas preserve product direction but are not commitments for the
initial release:

- Resolve Spotify IDs, URIs, and URLs for track, album, and artist metadata.
- Traverse album → tracks and artist → albums.
- List, check, add, and remove saved tracks and albums.
- Validate and deduplicate complete mutation batches before making changes.
- Chunk generic Spotify library requests at the upstream maximum while
  preserving input/result association.

Future paginated commands use `-m/--max`, `--next-page-token`, and stderr
continuation hints. Future resource reads remain text-only and carry the
relationship breadcrumbs defined above. Playlist management, playback,
recommendations, podcasts, audiobooks, raw HTTP access, and local media-library
synchronization remain out of scope until separately specified.
