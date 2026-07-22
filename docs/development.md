# spotify-cli Development Guide

Repo-local facts live here. Shared Open CLI Collective standards remain
canonical in `cli-common`.

## Project

- Module: `github.com/open-cli-collective/spotify-cli`
- Binary: `sptfy`
- Main branch: `main`
- Config, cache, and credential handling will use
  `github.com/open-cli-collective/cli-common`.
- Spotify API integration is expected to use `github.com/zmb3/spotify/v2` when
  the first API command is implemented.
- Distribution is not configured yet; add GoReleaser and package identity when
  the CLI has a releasable command surface.

## Commands

```sh
make build
make test
make lint
make check
```

## Shared Standards

Source of truth: https://github.com/open-cli-collective/cli-common/tree/main/docs
Local convenience copy, if present: `../../cli-common/docs`

Source of truth: https://github.com/open-cli-collective/.github
Local convenience copy, if present: `../../.github`
