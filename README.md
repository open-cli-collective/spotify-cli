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
ID | ARTIST | GENRES
```

Spotify development-mode search is limited to 10 results per page. When more
results exist, `sptfy` writes an opaque `--next-page-token` hint to stderr;
table rows remain on stdout. `--id` emits one resource ID per line and overrides
all other shape flags. `--extended` widens the default columns, `--fields`
replaces the selection, and `--include-artwork` adds Spotify-hosted image
dimensions and URLs. Resource search intentionally has no JSON mode.

To keep every result on one line, carriage returns, newlines, and the reserved
` | ` separator sequence inside Spotify text are replaced with one space.

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
SPOTIFY_CLIENT_ID=your_client_id \
make live-smoke
```

The harness is interactive because Spotify authorization opens a browser. It
exercises setup, identity, refresh, search/pagination shapes, replacement,
clear, and re-initialization without exporting the stored OAuth credential.

## License

MIT
