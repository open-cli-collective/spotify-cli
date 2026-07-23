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
library_track_id=
library_original_saved=
library_restore_needed=0
cleanup() {
  live_status=$?
  trap - EXIT HUP INT TERM
  if [[ $library_restore_needed == 1 ]]; then
    if [[ $library_original_saved == true ]]; then
      if ! "$SPTFY" --backend file library tracks add "$library_track_id" >/dev/null 2>&1; then
        printf 'warning: failed to restore original saved-track membership for %s\n' "$library_track_id" >&2
        live_status=1
      fi
    else
      if ! "$SPTFY" --backend file library tracks remove "$library_track_id" >/dev/null 2>&1; then
        printf 'warning: failed to restore original saved-track membership for %s\n' "$library_track_id" >&2
        live_status=1
      fi
    fi
  fi
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
grep -Fxq $'scopes\tuser-library-modify,user-library-read,user-read-private' <<<"$me_out"
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

album_out=$("$SPTFY" --backend file search album 'artist:"Björk"' --max 1)
[[ $(sed -n '1p' <<<"$album_out") == 'ID | ALBUM | ARTIST_IDS | ARTISTS | RELEASE_DATE | TOTAL_TRACKS' ]] || { printf '%s\n' 'album search returned an unexpected shape' >&2; exit 1; }
[[ $(wc -l <<<"$album_out") -eq 2 ]] || { printf '%s\n' 'album search did not return exactly one row' >&2; exit 1; }
"$SPTFY" --backend file search album 'artist:"Björk"' --id --max 1
"$SPTFY" --backend file search album 'artist:"Björk"' --fields ALBUM,ARTIST_IDS,ARTWORK --max 1
"$SPTFY" --backend file search album 'artist:"Björk"' --extended --max 1

artist_out=$("$SPTFY" --backend file search artist 'Björk' --max 1)
[[ $(sed -n '1p' <<<"$artist_out") == 'ID | ARTIST' ]] || { printf '%s\n' 'artist search returned an unexpected shape' >&2; exit 1; }
[[ $(wc -l <<<"$artist_out") -eq 2 ]] || { printf '%s\n' 'artist search did not return exactly one row' >&2; exit 1; }
"$SPTFY" --backend file search artist 'Björk' --id --max 1
"$SPTFY" --backend file search artist 'Björk' --fields ARTIST,ARTWORK --max 1
"$SPTFY" --backend file search artist 'Björk' --extended --max 1

track_id=11dFghVXANMlKmJXsNCbNl
album_id=4aawyAB9vmqN3uQ7FjRGTy
artist_id=0TnOYISbd1XYRBk9myaseg
library_track_id=$track_id
library_out=$("$SPTFY" --backend file library tracks list --max 1)
[[ $(sed -n '1p' <<<"$library_out") == 'ADDED_AT | ID | TRACK | ARTIST_IDS | ARTISTS | ALBUM_ID | ALBUM | DURATION' ]] || { printf '%s\n' 'saved tracks returned an unexpected shape' >&2; exit 1; }
library_original_saved=$("$SPTFY" --backend file library tracks check "$library_track_id" | awk -F ' \\| ' 'NR == 2 {print $3}')
[[ $library_original_saved == true || $library_original_saved == false ]] || { printf '%s\n' 'saved-track check returned an unexpected value' >&2; exit 1; }
library_restore_needed=1
if [[ $library_original_saved == true ]]; then
  [[ $("$SPTFY" --backend file library tracks remove "$library_track_id") == $'removed\t1' ]]
  [[ $("$SPTFY" --backend file library tracks check "$library_track_id" | awk -F ' \\| ' 'NR == 2 {print $3}') == false ]]
  [[ $("$SPTFY" --backend file library tracks add "$library_track_id") == $'added\t1' ]]
else
  [[ $("$SPTFY" --backend file library tracks add "$library_track_id") == $'added\t1' ]]
  [[ $("$SPTFY" --backend file library tracks check "$library_track_id" | awk -F ' \\| ' 'NR == 2 {print $3}') == true ]]
  [[ $("$SPTFY" --backend file library tracks remove "$library_track_id") == $'removed\t1' ]]
fi
[[ $("$SPTFY" --backend file library tracks check "$library_track_id" | awk -F ' \\| ' 'NR == 2 {print $3}') == "$library_original_saved" ]] || { printf '%s\n' 'saved-track membership was not restored' >&2; exit 1; }
library_restore_needed=0
[[ $("$SPTFY" --backend file tracks get "$track_id" --id) == "$track_id" ]] || { printf '%s\n' 'track get returned an unexpected ID' >&2; exit 1; }
[[ $("$SPTFY" --backend file albums get "spotify:album:$album_id" --id) == "$album_id" ]] || { printf '%s\n' 'album get returned an unexpected ID' >&2; exit 1; }
[[ $("$SPTFY" --backend file artists get "https://open.spotify.com/artist/$artist_id" --id) == "$artist_id" ]] || { printf '%s\n' 'artist get returned an unexpected ID' >&2; exit 1; }

album_tracks_out=$("$SPTFY" --backend file albums tracks list "spotify:album:$album_id" --max 1)
[[ $(sed -n '1p' <<<"$album_tracks_out") == "Album ID: $album_id" ]] || { printf '%s\n' 'album tracks returned an unexpected parent' >&2; exit 1; }
[[ $(sed -n '2p' <<<"$album_tracks_out") == 'ID | TRACK | ARTIST_IDS | ARTISTS | DURATION' ]] || { printf '%s\n' 'album tracks returned an unexpected shape' >&2; exit 1; }
[[ $(wc -l <<<"$album_tracks_out") -eq 3 ]] || { printf '%s\n' 'album tracks did not return exactly one row' >&2; exit 1; }

artist_albums_out=$("$SPTFY" --backend file artists albums list "https://open.spotify.com/artist/$artist_id" --max 1)
[[ $(sed -n '1p' <<<"$artist_albums_out") == "Artist ID: $artist_id" ]] || { printf '%s\n' 'artist albums returned an unexpected parent' >&2; exit 1; }
[[ $(sed -n '2p' <<<"$artist_albums_out") == 'ID | ALBUM | ARTIST_IDS | ARTISTS | RELEASE_DATE | TOTAL_TRACKS' ]] || { printf '%s\n' 'artist albums returned an unexpected shape' >&2; exit 1; }
[[ $(wc -l <<<"$artist_albums_out") -eq 3 ]] || { printf '%s\n' 'artist albums did not return exactly one row' >&2; exit 1; }

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
