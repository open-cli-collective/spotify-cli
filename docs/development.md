# spotify-cli Development Guide

Repo-local facts live here. Shared Open CLI Collective standards remain
canonical in `cli-common`.

The repository's Spotify-specific command and security contract is
[`spotify-cli-behavior.md`](../spotify-cli-behavior.md). It defines the planned
initial release; later ideas there are explicitly non-normative.

## Project

- Module: `github.com/open-cli-collective/spotify-cli`
- Binary: `sptfy`
- Main branch: `main`
- Config, cache, and credential handling use
  `github.com/open-cli-collective/cli-common`.
- Spotify API integration uses a thin typed `net/http` client plus
  `golang.org/x/oauth2`; no third-party Spotify SDK is planned.
- Distribution uses `.goreleaser.yml`, `packaging/identity.yml`, and the shared
  Open CLI Collective auto-release/release workflows. Package listings use
  `spotify-cli`; archives and the installed executable use `sptfy`.

## Commands

```sh
make build
make test
make lint
make check
make snapshot
```

`make live-smoke` is deliberately excluded from `make check` and CI. It
requires the three explicit opt-ins documented in the README and uses a
dedicated Spotify Development Mode account.

## Release prerequisites

The shared workflows expect `TAP_GITHUB_TOKEN`, `CHOCOLATEY_API_KEY`,
`WINGET_GITHUB_TOKEN`,
`LINUX_PACKAGES_DISPATCH_TOKEN`, `MACOS_CERT_P12`,
`MACOS_CERT_PASSWORD`, `MACOS_CERT_CN`, and `MACOS_CERT_LEAF_SHA` through
repository or organization secrets. macOS artifacts are self-signed and
verified according to cli-common policy; they are not notarized.

A conventional final squash on `main` triggers the shared auto-release flow,
so release-secret readiness must be confirmed before merge. Homebrew and the
deb/rpm repository are updated by their configured release channels. Winget
and Chocolatey submissions enter external community indexes and may appear
asynchronously after the canonical GitHub release.

## Shared Standards

Source of truth: https://github.com/open-cli-collective/cli-common/tree/main/docs
Local convenience copy, if present: `../../cli-common/docs`

Source of truth: https://github.com/open-cli-collective/.github
Local convenience copy, if present: `../../.github`
