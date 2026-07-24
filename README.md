# spotify-cli

`sptfy` is a command-line interface for Spotify.

## Install

The executable is `sptfy`; package-manager listings use `spotify-cli`.

```sh
brew install --cask open-cli-collective/tap/spotify-cli
winget install OpenCLICollective.spotify-cli
choco install spotify-cli
```

After configuring the Open CLI Collective
[APT or RPM repository](https://github.com/open-cli-collective/linux-packages#installation),
install package `spotify-cli`; both formats provide `/usr/bin/sptfy`.

```sh
sudo apt install spotify-cli
sudo dnf install spotify-cli
```

## Setup

Create a Spotify Development Mode application in the Spotify developer
dashboard. The app owner must have Spotify Premium, and every dedicated test
account must be added under the app's Users Management page. Note the client
ID and add this exact redirect URI to the app's allowlist:

```text
http://127.0.0.1/callback
```

Then authorize the CLI with the application's client ID. `sptfy` uses
Authorization Code with PKCE; it does not accept a client secret.

```sh
sptfy init --client-id YOUR_CLIENT_ID
sptfy me
```

`init` opens the authorization page when possible and stores the resulting
OAuth token in the configured credential backend. Setup messages and the
authorization URL go to stderr. `me` writes the authenticated identity and
granted scopes to stdout.

Authorization requests `playlist-modify-private`, `playlist-modify-public`,
`playlist-read-collaborative`, `playlist-read-private`, `user-library-modify`,
`user-library-read`, and `user-read-private`. Replace older credentials with
`sptfy init --overwrite`.

For a prompt-free setup, supply `--non-interactive`. Use `--no-browser` to
open the printed URL yourself, or `--auth-code-stdin` to paste the complete
redirected URL back into the command.

The default credential backend is the native store for the current OS. Select
one explicitly with the global `--backend` flag: `keychain` (macOS), `wincred`
(Windows), `secret-service` (Linux), `file`, `pass`, `op`, `op-connect`, or
`op-desktop`. Selection precedence is flag, `SPOTIFY_CLI_KEYRING_BACKEND`,
config, then OS default. The encrypted `file` backend prompts for a passphrase
on a TTY; automation can set `SPOTIFY_CLI_KEYRING_PASSPHRASE` without placing
the OAuth credential itself in configuration or a runtime environment value.

## Search

Search tracks, albums, or artists with one Spotify query. Track and album
output includes relationship IDs as breadcrumbs:

```sh
sptfy search track 'artist:"Björk"' --max 10
sptfy search album 'artist:"Björk"' --max 10
sptfy search artist 'Björk' --max 10
```

```text
ID | TRACK | ARTIST_IDS | ARTISTS | ALBUM_ID | ALBUM | DURATION
ID | ALBUM | ARTIST_IDS | ARTISTS | RELEASE_DATE | TOTAL_TRACKS
ID | ARTIST
```

Spotify development-mode search is limited to 10 results per page. When more
results exist, `sptfy` writes an opaque `--next-page-token` hint to stderr;
table rows remain on stdout. `--id` emits one resource ID per line and overrides
all other shape flags. `--extended` widens the default columns, `--fields`
replaces the selection, and `--include-artwork` adds Spotify-hosted image
dimensions and URLs. Resource search intentionally has no JSON mode.

To keep every result on one line, carriage returns, newlines, and the reserved
` | ` separator sequence inside Spotify text are replaced with one space.

## Catalog

Get one track, album, or artist from a raw Spotify ID, matching Spotify URI, or
canonical `open.spotify.com` URL:

```sh
sptfy tracks get 11dFghVXANMlKmJXsNCbNl
sptfy albums get spotify:album:4aawyAB9vmqN3uQ7FjRGTy
sptfy artists get https://open.spotify.com/artist/0TnOYISbd1XYRBk9myaseg
sptfy albums tracks list spotify:album:4aawyAB9vmqN3uQ7FjRGTy
sptfy artists albums list https://open.spotify.com/artist/0TnOYISbd1XYRBk9myaseg
```

```text
11dFghVXANMlKmJXsNCbNl  Cut To The Feeling
Artist IDs: 6sFIWsNpZYqfjUpaCgueju   Artists: Carly Rae Jepsen
Album ID: 0tGPJ0bkWOUmH7MEOR77qc   Album: Cut To The Feeling
Duration: 3:27
```

Catalog reads are text-only. `--id` emits only the fetched ID and overrides
`--extended`, `--fields`, and `--include-artwork`. Selected identity fields
remain in the stable identity header instead of being duplicated below it.

Relationship lists accept the same parent references. Normal output prints the
parent ID once before the child table; `--id` prints only child IDs.
Album-track pages support 1–50 results and expose no album or artwork columns.
Artist-album pages support 1–10 results and can include album artwork metadata.
Both default to 10 and write opaque continuation hints to stderr.

## Playlists

List the current user's playlists or get one playlist from a raw Spotify ID,
matching URI, or canonical URL:

```sh
sptfy playlists list --max 10
sptfy playlists get spotify:playlist:YOUR_OWN_OR_COLLABORATIVE_PLAYLIST_ID
sptfy playlists items list spotify:playlist:YOUR_OWN_OR_COLLABORATIVE_PLAYLIST_ID
sptfy playlists items add spotify:playlist:YOUR_OWN_OR_COLLABORATIVE_PLAYLIST_ID TRACK_ID --position 1
sptfy playlists items remove spotify:playlist:YOUR_OWN_OR_COLLABORATIVE_PLAYLIST_ID 1
sptfy playlists items update spotify:playlist:YOUR_OWN_OR_COLLABORATIVE_PLAYLIST_ID 1 --item REPLACEMENT_TRACK_ID
```

Lists default to 10 and allow 1–50 results. Default fields are
`ID | PLAYLIST | OWNER_ID | OWNER | ITEM_COUNT | PUBLIC | COLLABORATIVE`.
`--extended` adds `URI | URL | SNAPSHOT_ID | DESCRIPTION`, and
`--include-artwork` adds Spotify-hosted artwork metadata. `--fields`, `--id`,
text sanitization, detail identity headers, and opaque stderr continuation
hints follow the resource-output rules above.

Playlist-item lists preserve Spotify order and print the playlist ID once,
followed by `POSITION | TYPE | ID | ITEM | ARTIST_IDS | ARTISTS | ALBUM_ID |
ALBUM | DURATION`. Positions are absolute and zero-based across continuation
pages. Track, episode, local, unavailable, and future item shapes remain
distinct; `--id` emits only entries with Spotify IDs. Spotify currently
exposes playlist details and items only when the current user owns or
collaborates on the playlist.

Playlist add accepts one or more raw track IDs, track URIs, or canonical track
URLs. It preserves argument order and duplicates, appends by default, and
accepts `--position` from zero through the current item count. Batches larger
than 100 use ordered requests. Remove accepts one absolute zero-based position;
it refuses non-track items and duplicate track URIs because Spotify's current
removal operation is URI-based rather than position-based. Update replaces one
unique track at an exact position by adding the replacement before removing the
original. It refuses unsafe non-track, duplicate, same-item, and stale states.
All three commands
require the two read and two modify scopes and emit reversible, headerless tab
records for applied mutations:

```text
added<TAB>playlist-id<TAB>start-position<TAB>count<TAB>snapshot-id
removed<TAB>playlist-id<TAB>position<TAB>track-id<TAB>snapshot-id
updated<TAB>playlist-id<TAB>position<TAB>old-track-id<TAB>new-track-id<TAB>snapshot-id
```

The add record retains the position and count needed for an inverse operation.
Repeated guarded remove at `start-position` is directly usable only while each
inserted track ID is unique in the playlist; intentional or preexisting
duplicates must first be made unique by another Spotify client. The remove
record can be inverted by adding `track-id` at `position`. If a later add chunk
is definitely rejected, the confirmed-prefix record is emitted and the command
exits nonzero; a first-chunk definite rejection emits no record. When transport,
an ambiguous provider `5xx`, or a successful but invalid response makes an add
or remove outcome uncertain, the command emits no stdout record and its nonzero
error says to inspect and reconcile before retrying. The uncertain change may
have applied. Remove emits a stdout record only after confirmed success.

The update record is inverted by updating the reported position with
`old-track-id`. Update emits no record unless both steps are confirmed. A
definite provider `4xx` removal rejection after a successful add reports only
the confirmed facts: the add succeeded at the included snapshot, and this
command's removal was rejected without applying. It directs inspection of the
current playlist before recovery or retry because collaborators may have moved
or removed either item. An uncertain add reports the playlist, position, and
old/new IDs but has no confirmed post-write snapshot. An uncertain removal
additionally reports the confirmed add snapshot. Provider `5xx` responses are
uncertain; provider details remain hidden, so inspect before retrying because
either write may have applied.
Before removing the original, update verifies the confirmed add snapshot and
item count around a complete identity-and-order read. A mismatch or read failure
leaves the original untouched, emits no record, and reports the confirmed add
snapshot for reconciliation. The final delete identifies the original's exact
post-add position under that snapshot, so a concurrently added duplicate is not
also removed.

## Saved tracks

```sh
sptfy library tracks list
sptfy library tracks check 11dFghVXANMlKmJXsNCbNl spotify:track:0VjIjW4GlUZAMYd2vXMi3b
sptfy library tracks add https://open.spotify.com/track/11dFghVXANMlKmJXsNCbNl
sptfy library tracks remove 11dFghVXANMlKmJXsNCbNl
```

Lists default to 10 and allow 1–50 results. Normal output begins with
`ADDED_AT`; `--id` emits only track IDs. Checks emit
`REFERENCE | ID | SAVED` in first-seen unique order. Mutations validate and
deduplicate the complete batch before making requests, then print only
`added<TAB>N` or `removed<TAB>N` after every chunk succeeds.

## Saved albums

```sh
sptfy library albums list
sptfy library albums check 4aawyAB9vmqN3uQ7FjRGTy spotify:album:2up3OPMp9Tb4dAKM2erWXQ
sptfy library albums add https://open.spotify.com/album/4aawyAB9vmqN3uQ7FjRGTy
sptfy library albums remove 4aawyAB9vmqN3uQ7FjRGTy
```

Lists default to 10 and allow 1–50 results. Normal output begins with
`ADDED_AT` and preserves every credited artist ID and name; `--id` emits only
album IDs. Album checks and mutations follow the same complete-batch
validation, first-seen deduplication, compact output, and 40-item request
chunking as saved tracks.

## Development

Requires Go 1.26 or newer.

```sh
make check
make snapshot
go run ./cmd/sptfy --help
```

Shared CLI behavior and repository conventions come from
[`open-cli-collective/cli-common`](https://github.com/open-cli-collective/cli-common).

### Live verification

The live smoke uses a dedicated Spotify app/account and a temporary encrypted
file store. It pins all supported OS state-directory variables under one
temporary root and removes that root on exit; it never uses normal CLI state or
an OS keychain. It is opt-in and is not part of ordinary CI:

```sh
SPOTIFY_CLI_LIVE=1 \
SPOTIFY_CLI_LIVE_DEDICATED_ACCOUNT=1 \
SPOTIFY_CLI_LIVE_PLAYLIST_ID=your_playlist_id \
SPOTIFY_CLI_LIVE_PLAYLIST_ITEM_IDS=first_item_id,second_item_id,third_item_id \
SPOTIFY_CLI_LIVE_PLAYLIST_MUTATION_TRACK_ID=a_distinct_track_id \
SPOTIFY_CLI_LIVE_PLAYLIST_MUTATION_SEARCH_QUERY='isrc:a_unique_isrc' \
SPOTIFY_CLIENT_ID=your_client_id \
make live-smoke
```

`SPOTIFY_CLI_LIVE_PLAYLIST_ITEM_IDS` is a known ordered three-ID prefix, not the
entire playlist. Before mutating, the harness captures one complete runtime page
of up to 50 IDs, requires that prefix and an absent mutation track, and uses the
captured sequence for exact restoration.
`SPOTIFY_CLI_LIVE_PLAYLIST_MUTATION_SEARCH_QUERY` must return that mutation
track as its first ID-only result; the harness verifies the match before any
playlist write.

The harness is interactive because Spotify authorization opens a browser. It
exercises setup, identity, refresh, search/pagination shapes, catalog gets,
relationship traversals, playlist list/get, ordered playlist-item pagination,
replacement, clear, and re-initialization without exporting the stored OAuth
credential.

The real-only provider-contract probe temporarily inserts the distinct mutation
track twice, waits through Spotify's bounded write-settling window, demonstrates
that one URI removal removes both occurrences, and polls for exact baseline
restoration. That build-tagged probe owns best-effort cleanup for test failures;
abrupt process termination during the probe is outside its guarantee. The shell
trap separately protects the command round trip on failure or interruption.

## License

MIT
