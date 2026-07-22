# spotify-cli

`sptfy` is a command-line interface for Spotify.

The project is at the initial repository stage. Authentication and Spotify
commands are not implemented yet.

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
