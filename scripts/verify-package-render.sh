#!/usr/bin/env bash
# shellcheck disable=SC2016 # Assert literal template expressions.
set -euo pipefail

dist_dir="${1:-dist}"

fail() {
  echo "package render check failed: $*" >&2
  exit 1
}

require_file() {
  [ -f "$1" ] || fail "missing file: $1"
}

require_text() {
  grep -Fq "$1" "$2" || fail "$2 missing: $1"
}

require_file "$dist_dir/metadata.json"
require_file "$dist_dir/artifacts.json"
version="$(jq -r '.version' "$dist_dir/metadata.json")"
[ -n "$version" ] && [ "$version" != "null" ] || fail "metadata version missing"

for os in darwin linux; do
  for arch in amd64 arm64; do
    require_file "$dist_dir/sptfy_v${version}_${os}_${arch}.tar.gz"
  done
done
for arch in amd64 arm64; do
  require_file "$dist_dir/sptfy_v${version}_windows_${arch}.zip"
  for kind in deb rpm; do
    require_file "$dist_dir/spotify-cli_${version}_linux_${arch}.${kind}"
    jq -e --arg kind "$kind" --arg dotted ".$kind" --arg arch "$arch" '
      .[] | select(
        .type == "Linux Package" and
        .goarch == $arch and
        (.extra.Ext == $kind or .extra.Ext == $dotted) and
        .extra.ID == "spotify-cli"
      )
    ' "$dist_dir/artifacts.json" >/dev/null || fail "missing spotify-cli linux package artifact: $kind/$arch"
  done
done

cask="$dist_dir/homebrew/Casks/spotify-cli.rb"
require_file "$cask"
require_text 'cask "spotify-cli"' "$cask"
require_text 'binary "sptfy"' "$cask"
require_text 'open-cli-collective/spotify-cli/releases/download/v' "$cask"
require_text 'sptfy_v#{version}_darwin_amd64.tar.gz' "$cask"
require_text 'sptfy_v#{version}_darwin_arm64.tar.gz' "$cask"

require_text "winget: { id: OpenCLICollective.spotify-cli, bootstrap: true }" packaging/identity.yml
require_text "chocolatey: { id: spotify-cli }" packaging/identity.yml

for manifest in \
  packaging/winget/OpenCLICollective.spotify-cli.yaml \
  packaging/winget/OpenCLICollective.spotify-cli.installer.yaml \
  packaging/winget/OpenCLICollective.spotify-cli.locale.en-US.yaml; do
  require_file "$manifest"
  require_text "PackageIdentifier: OpenCLICollective.spotify-cli" "$manifest"
done
require_text "PortableCommandAlias: sptfy" packaging/winget/OpenCLICollective.spotify-cli.installer.yaml
require_text "sptfy_v0.0.0_windows_amd64.zip" packaging/winget/OpenCLICollective.spotify-cli.installer.yaml
require_text "sptfy_v0.0.0_windows_arm64.zip" packaging/winget/OpenCLICollective.spotify-cli.installer.yaml

require_file packaging/chocolatey/spotify-cli.nuspec
require_file packaging/chocolatey/tools/chocolateyInstall.ps1
require_text "<id>spotify-cli</id>" packaging/chocolatey/spotify-cli.nuspec
require_text 'releases/download/v${version}' packaging/chocolatey/tools/chocolateyInstall.ps1
require_text 'sptfy_v${version}_windows_${arch}.zip' packaging/chocolatey/tools/chocolateyInstall.ps1

require_text 'homebrew-tap-token: ${{ secrets.TAP_GITHUB_TOKEN }}' .github/workflows/release.yml
require_text 'chocolatey-api-key: ${{ secrets.CHOCOLATEY_API_KEY }}' .github/workflows/release.yml
require_text 'winget-token: ${{ secrets.WINGET_GITHUB_TOKEN }}' .github/workflows/release.yml
require_text 'linux-dispatch-token: ${{ secrets.LINUX_PACKAGES_DISPATCH_TOKEN }}' .github/workflows/release.yml

echo "package render check OK"
