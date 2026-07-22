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

Create a Spotify application and add this redirect URI in its dashboard:

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

## Search

Search tracks with one Spotify query. The default output includes album and
artist IDs as relationship breadcrumbs:

```sh
sptfy search track 'artist:"Björk"' --max 10
```

```text
ID | TRACK | ARTIST_IDS | ARTISTS | ALBUM_ID | ALBUM | DURATION
```

Spotify development-mode search is limited to 10 results per page. When more
results exist, `sptfy` writes an opaque `--next-page-token` hint to stderr;
table rows remain on stdout. `--id` emits one track ID per line and overrides
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

## License

MIT
