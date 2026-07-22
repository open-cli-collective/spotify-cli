# spotify-cli

`sptfy` is a command-line interface for Spotify.

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

## Development

Requires Go 1.26 or newer.

```sh
make check
go run ./cmd/sptfy --help
```

Shared CLI behavior and repository conventions come from
[`open-cli-collective/cli-common`](https://github.com/open-cli-collective/cli-common).

## License

MIT
