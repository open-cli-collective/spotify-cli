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
- `sptfy tracks get <id-or-reference>`
- `sptfy albums get <id-or-reference>`
- `sptfy artists get <id-or-reference>`
- `sptfy albums tracks list <album-id-or-reference>`
- `sptfy artists albums list <artist-id-or-reference>`
- `sptfy library tracks list`
- `sptfy library tracks check <track-reference>...`
- `sptfy library tracks add <track-reference>...`
- `sptfy library tracks remove <track-reference>...`
- `sptfy library albums list`
- `sptfy library albums check <album-reference>...`
- `sptfy library albums add <album-reference>...`
- `sptfy library albums remove <album-reference>...`

The product exclusions under [Future boundaries](#future-boundaries) remain
directional and non-normative until promoted into command sections.

## Authentication and configuration

`sptfy` uses Spotify Authorization Code with PKCE for user authorization. The
user supplies a Spotify client ID; the CLI never accepts or stores a client
secret. The CLI requests exactly these sorted scopes: `user-library-modify`,
`user-library-read`, and `user-read-private`. Catalog reads do not require an
additional scope. A credential missing a command's library scope fails before
its Spotify resource request with a `sptfy init --overwrite` hint.

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
ID | ARTIST
```

`--extended` adds `URI` and `URL`. `--include-artwork` adds `ARTWORK`.
Popularity and follower data are not requested or rendered.

## Catalog get

### `sptfy tracks get <id-or-reference>`

### `sptfy albums get <id-or-reference>`

### `sptfy artists get <id-or-reference>`

Each command accepts exactly one 22-character ASCII alphanumeric Spotify ID,
matching `spotify:<kind>:<id>` URI, or canonical
`https://open.spotify.com/<kind>/<id>` URL. Other URL origins, userinfo, ports,
query strings, fragments, wrong resource kinds, and additional path components
are rejected before authentication is opened.

The commands perform one fixed-origin request for the named resource and never
follow the supplied URL. Output is text-only and starts with the stable
`ID  Name` identity header. Remaining attributes use `Key: Value` pairs
separated by three spaces. Track output includes album and artist relationship
IDs; album output includes artist IDs.

`--id` emits only the fetched resource ID and overrides the other shape flags.
`--extended`, `--fields`, and `--include-artwork` follow the shared output
rules. `--fields` replaces attributes below the identity header; selected
identity fields are not duplicated. Artwork is URL and dimensions metadata
only and is never downloaded.

## Catalog traversal

### `sptfy albums tracks list <album-id-or-reference>`

### `sptfy artists albums list <artist-id-or-reference>`

Both commands accept the same validated raw ID, URI, and URL forms as catalog
gets. They request one fixed-origin relationship page and never follow a
provider `next` URL. `--next-page-token` is bound to the relationship and
parsed parent ID and is validated before authentication.

Normal output starts with exactly one `Album ID: <id>` or `Artist ID: <id>`
line followed by the child table. `--id` emits only child IDs, one per line.
Album tracks default to 10 and allow `--max` values from 1 through 50. Their
fields are `ID`, `TRACK`, `ARTIST_IDS`, `ARTISTS`, and `DURATION`; `--extended`
adds `URI`, `URL`, `DISC_NUMBER`, `TRACK_NUMBER`, `EXPLICIT`, and
`RESTRICTION`. Parent album and artwork fields are unavailable. Artist albums
default to 10 and allow `--max` values from 1 through 10; they reuse album
fields, including optional artwork URL metadata.

## Saved tracks

`sptfy library tracks list` uses `GET /me/tracks`, defaults to 10, and accepts
1–50 results. Its opaque continuation token is bound to `library-tracks`;
provider response URLs are never followed. Normal output begins with
`ADDED_AT` and the established track fields. `--id` is pure IDs, while
`--fields`, `--extended`, and `--include-artwork` follow the standard
precedence.

`check`, `add`, and `remove` accept only raw 22-character track IDs, typed
track URIs, or canonical track URLs. The full batch is validated, normalized,
and deduplicated before authentication opens. The first reference and
first-seen order are retained. Check uses `GET /me/library/contains`; add uses
`PUT /me/library`; remove uses `DELETE /me/library`. Requests pass `uris` as a
query parameter in chunks of at most 40. Check output is
`REFERENCE | ID | SAVED`. Mutations emit only `added<TAB>N` or `removed<TAB>N`
after every chunk succeeds; partial failure is reported without rollback.

## Saved albums

`sptfy library albums list` uses `GET /me/albums`, defaults to 10, and accepts
1–50 results. Its opaque continuation token is bound to `library-albums`;
provider response URLs are never followed. Normal output begins with
`ADDED_AT`, followed by `ID`, `ALBUM`, every credited `ARTIST_IDS` and
`ARTISTS` value, `RELEASE_DATE`, and `TOTAL_TRACKS`. `--id` is pure IDs, while
`--fields`, `--extended`, and `--include-artwork` follow the standard
precedence. Artwork remains URL metadata only.

`check`, `add`, and `remove` accept only raw 22-character album IDs, typed
album URIs, or canonical album URLs. They use the same complete-batch
validation, first-seen deduplication, 40-URI generic-library request chunks,
ordered check output, success-only mutation output, and partial-failure
semantics as saved tracks. Album-specific membership endpoints are not used.

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

### Catalog get

Exercise raw ID, Spotify URI, and canonical Spotify URL references across
tracks, albums, and artists. Cover exact default, ID-only, selected-field,
extended, artwork, breadcrumb, and sanitized output. Malformed, wrong-kind,
and hostile URL references must fail before credential or network access.
Fixture tests verify exactly one fixed-origin single-resource request plus
inherited authorization, retry, cancellation, and bounded-body behavior.

### Catalog traversal

Exercise raw ID, Spotify URI, and canonical Spotify URL parents for album
tracks and artist albums. Cover exact default, ID-only, selected-field,
extended, artwork where available, empty-page, continuation, parent-header,
and stdout/stderr behavior. Invalid parents, endpoint-specific page sizes,
fields, and wrong-relationship or wrong-parent tokens fail before network I/O.
Fixture tests verify fixed relationship paths, page validation, inherited
transport behavior, and that provider pagination URLs are never followed.

### Saved tracks

Exercise list bounds, empty pages, continuation, shape flags, accepted
reference forms, first-seen deduplication, complete-batch validation, exact
scope-guard timing and hints, and stdout/stderr routing. Provider tests cover
fixed paths and methods, response validation, check-length mismatch, inherited
transport behavior, and 40/41/80/81 chunks including later-chunk failure.

### Saved albums

Exercise list bounds, empty pages, album-bound continuation, default and
selected fields, extended fields, artwork, pure IDs, multi-artist breadcrumbs,
accepted album reference forms, first-seen deduplication, wrong-kind rejection,
complete-batch validation, exact scope timing, and stdout/stderr routing.
Provider tests cover the fixed saved-album list path, generic membership paths,
response and check-length validation, inherited transport behavior, and
40/41/80/81 chunks including later-chunk failure.

Live tests are opt-in and use a dedicated Spotify application/account plus a
hermetic state directory and an explicitly selected encrypted-file credential
backend rooted there. They never run in ordinary CI or mutate a developer's
normal Spotify configuration or OS keychain.

## Future boundaries

Future paginated commands use `-m/--max`, `--next-page-token`, and stderr
continuation hints. Future resource reads remain text-only and carry the
relationship breadcrumbs defined above. Playlist management, playback,
recommendations, podcasts, audiobooks, raw HTTP access, and local media-library
synchronization remain out of scope until separately specified.
