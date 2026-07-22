#!/usr/bin/env bash
set -euo pipefail

[[ ${SPOTIFY_CLI_LIVE:-} == 1 ]] || { printf '%s\n' 'set SPOTIFY_CLI_LIVE=1 to run the live smoke' >&2; exit 2; }
[[ ${SPOTIFY_CLI_LIVE_DEDICATED_ACCOUNT:-} == 1 ]] || { printf '%s\n' 'set SPOTIFY_CLI_LIVE_DEDICATED_ACCOUNT=1 to acknowledge dedicated-account use' >&2; exit 2; }
[[ -n ${SPOTIFY_CLIENT_ID:-} ]] || { printf '%s\n' 'SPOTIFY_CLIENT_ID is required' >&2; exit 2; }
live_dry=${SPOTIFY_CLI_LIVE_DRY_RUN:-0}
if [[ $live_dry != 1 ]]; then
  [[ -t 0 ]] || { printf '%s\n' 'the live smoke requires an interactive terminal' >&2; exit 2; }
  live_gopath=$(go env GOPATH)
  live_gomodcache=$(go env GOMODCACHE)
  live_gocache=$(go env GOCACHE)
fi

umask 077
SPOTIFY_CLI_LIVE_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/spotify-cli-live.XXXXXX")
export SPOTIFY_CLI_LIVE_ROOT
cleanup() {
  live_status=$?
  trap - EXIT HUP INT TERM
  rm -rf -- "$SPOTIFY_CLI_LIVE_ROOT"
  exit "$live_status"
}
trap cleanup EXIT HUP INT TERM

export HOME="$SPOTIFY_CLI_LIVE_ROOT/home"
export USERPROFILE="$HOME"
export AppData="$SPOTIFY_CLI_LIVE_ROOT/appdata"
export LocalAppData="$SPOTIFY_CLI_LIVE_ROOT/localappdata"
export XDG_CONFIG_HOME="$SPOTIFY_CLI_LIVE_ROOT/xdgconfig"
export XDG_CACHE_HOME="$SPOTIFY_CLI_LIVE_ROOT/xdgcache"
export XDG_DATA_HOME="$SPOTIFY_CLI_LIVE_ROOT/xdgdata"
export XDG_STATE_HOME="$SPOTIFY_CLI_LIVE_ROOT/xdgstate"
mkdir -p "$HOME" "$AppData" "$LocalAppData" "$XDG_CONFIG_HOME" "$XDG_CACHE_HOME" "$XDG_DATA_HOME" "$XDG_STATE_HOME"
if [[ $live_dry != 1 ]]; then
  export GOPATH=$live_gopath GOMODCACHE=$live_gomodcache GOCACHE=$live_gocache
fi

export SPOTIFY_CLI_KEYRING_BACKEND=file
SPOTIFY_CLI_KEYRING_PASSPHRASE=$(od -An -N32 -tx1 /dev/urandom | tr -d ' \n')
export SPOTIFY_CLI_KEYRING_PASSPHRASE

if [[ $live_dry == 1 ]]; then
  [[ -n ${SPOTIFY_CLI_LIVE_BINARY:-} ]] || { printf '%s\n' 'dry run requires SPOTIFY_CLI_LIVE_BINARY' >&2; exit 2; }
  SPTFY=$SPOTIFY_CLI_LIVE_BINARY
else
  make build
  SPTFY=./bin/sptfy
fi

"$SPTFY" --backend file init --non-interactive --client-id "$SPOTIFY_CLIENT_ID"
me_out=$("$SPTFY" --backend file me)
grep -q '^account_id' <<<"$me_out"
ordinary_out=$("$SPTFY" --backend file search track a --max 10)
[[ $(wc -l <<<"$ordinary_out") -gt 1 ]] || { printf '%s\n' 'ordinary search returned no rows' >&2; exit 1; }
live_nonce=$(od -An -N12 -tx1 /dev/urandom | tr -d ' \n')
empty_out=$("$SPTFY" --backend file search track "track:\"sptfy-$live_nonce\" artist:\"sptfy-$live_nonce\"")
[[ $(wc -l <<<"$empty_out") -eq 1 ]] || { printf '%s\n' 'guaranteed-no-match search returned rows' >&2; exit 1; }

page_out="$SPOTIFY_CLI_LIVE_ROOT/page.out"
page_err="$SPOTIFY_CLI_LIVE_ROOT/page.err"
"$SPTFY" --backend file search track a --max 1 >"$page_out" 2>"$page_err"
live_token=$(sed -n 's/^More results available (next: \(.*\))$/\1/p' "$page_err")
[[ -n $live_token ]] || { printf '%s\n' 'page-size-1 search did not return a continuation token' >&2; exit 1; }
[[ $(wc -l <"$page_out") -eq 2 ]] || { printf '%s\n' 'page-size-1 search did not return exactly one row' >&2; exit 1; }
"$SPTFY" --backend file search track a --max 1 --next-page-token "$live_token"
"$SPTFY" --backend file search track a --id --max 1
"$SPTFY" --backend file search track a --fields TRACK,ALBUM_ID,ARTWORK --max 1

"$SPTFY" --backend file init --non-interactive --client-id "$SPOTIFY_CLIENT_ID" --overwrite
if [[ $live_dry != 1 ]]; then
  go test -tags=keyring_nopassage,spotify_live ./internal/livesmoke -run '^TestExpireCredential$' -count=1
fi
grep -q '^account_id' < <("$SPTFY" --backend file me)

"$SPTFY" --backend file config clear
if "$SPTFY" --backend file me; then
  printf '%s\n' 'me unexpectedly succeeded after config clear' >&2
  exit 1
fi
"$SPTFY" --backend file init --non-interactive --client-id "$SPOTIFY_CLIENT_ID"
grep -q '^account_id' < <("$SPTFY" --backend file me)

printf '%s\n' 'live smoke passed; hermetic state removed on exit' >&2
