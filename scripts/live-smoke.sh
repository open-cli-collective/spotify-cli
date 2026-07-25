#!/usr/bin/env bash
set -euo pipefail

[[ ${SPOTIFY_CLI_LIVE:-} == 1 ]] || { printf '%s\n' 'set SPOTIFY_CLI_LIVE=1 to run the live smoke' >&2; exit 2; }
[[ ${SPOTIFY_CLI_LIVE_DEDICATED_ACCOUNT:-} == 1 ]] || { printf '%s\n' 'set SPOTIFY_CLI_LIVE_DEDICATED_ACCOUNT=1 to acknowledge dedicated-account use' >&2; exit 2; }
[[ -n ${SPOTIFY_CLIENT_ID:-} ]] || { printf '%s\n' 'SPOTIFY_CLIENT_ID is required' >&2; exit 2; }
[[ -n ${SPOTIFY_CLI_LIVE_PLAYLIST_ID:-} ]] || { printf '%s\n' 'SPOTIFY_CLI_LIVE_PLAYLIST_ID is required' >&2; exit 2; }
[[ $SPOTIFY_CLI_LIVE_PLAYLIST_ID =~ ^[A-Za-z0-9]{22}$ ]] || { printf '%s\n' 'SPOTIFY_CLI_LIVE_PLAYLIST_ID must be a 22-character Spotify ID' >&2; exit 2; }
[[ -n ${SPOTIFY_CLI_LIVE_PLAYLIST_ITEM_IDS:-} ]] || { printf '%s\n' 'SPOTIFY_CLI_LIVE_PLAYLIST_ITEM_IDS is required' >&2; exit 2; }
IFS=, read -r -a playlist_expected_item_ids <<<"$SPOTIFY_CLI_LIVE_PLAYLIST_ITEM_IDS"
[[ ${#playlist_expected_item_ids[@]} -eq 3 ]] || { printf '%s\n' 'SPOTIFY_CLI_LIVE_PLAYLIST_ITEM_IDS requires exactly three comma-separated IDs' >&2; exit 2; }
for playlist_item_id in "${playlist_expected_item_ids[@]}"; do
  [[ $playlist_item_id =~ ^[A-Za-z0-9]{22}$ ]] || { printf '%s\n' 'SPOTIFY_CLI_LIVE_PLAYLIST_ITEM_IDS must contain only 22-character Spotify IDs' >&2; exit 2; }
done
[[ ${SPOTIFY_CLI_LIVE_PLAYLIST_MUTATION_TRACK_ID:-} =~ ^[A-Za-z0-9]{22}$ ]] || { printf '%s\n' 'SPOTIFY_CLI_LIVE_PLAYLIST_MUTATION_TRACK_ID must be a 22-character Spotify ID' >&2; exit 2; }
[[ -n ${SPOTIFY_CLI_LIVE_PLAYLIST_MUTATION_SEARCH_QUERY:-} ]] || { printf '%s\n' 'SPOTIFY_CLI_LIVE_PLAYLIST_MUTATION_SEARCH_QUERY is required' >&2; exit 2; }
for playlist_item_id in "${playlist_expected_item_ids[@]}"; do
  [[ $SPOTIFY_CLI_LIVE_PLAYLIST_MUTATION_TRACK_ID != "$playlist_item_id" ]] || { printf '%s\n' 'SPOTIFY_CLI_LIVE_PLAYLIST_MUTATION_TRACK_ID must be distinct from the configured prefix' >&2; exit 2; }
done
printf -v playlist_prefix_ids '%s\n%s\n%s' "${playlist_expected_item_ids[0]}" "${playlist_expected_item_ids[1]}" "${playlist_expected_item_ids[2]}"
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
library_album_id=
library_album_original_saved=
library_album_restore_needed=0
playlist_restore_needed=0
playlist_baseline_ids=
playlist_replaced_ids=
playlist_partial_ids=
playlist_mutation_candidate_id=
playlist_mutation_started_at=0
read_full_playlist_item_ids() {
  local first_out="$SPOTIFY_CLI_LIVE_ROOT/full-items-first.out"
  local first_err="$SPOTIFY_CLI_LIVE_ROOT/full-items-first.err"
  local next_out="$SPOTIFY_CLI_LIVE_ROOT/full-items-next.out"
  local next_err="$SPOTIFY_CLI_LIVE_ROOT/full-items-next.err"
  "$SPTFY" --backend file playlists items list "$SPOTIFY_CLI_LIVE_PLAYLIST_ID" --id --max 50 >"$first_out" 2>"$first_err" || return 1
  local next_token
  next_token=$(sed -n 's/^More results available (next: \(.*\))$/\1/p' "$first_err")
  if [[ -z $next_token ]]; then
    [[ ! -s $first_err ]] || return 1
    cat "$first_out"
    return
  fi
  "$SPTFY" --backend file playlists items list "$SPOTIFY_CLI_LIVE_PLAYLIST_ID" --id --max 50 --next-page-token "$next_token" >"$next_out" 2>"$next_err" || return 1
  [[ ! -s $next_err ]] || return 1
  cat "$first_out" "$next_out"
}
wait_for_playlist_write_settle() {
  [[ $live_dry == 1 ]] && return
  local now remaining
  now=$(date +%s)
  remaining=$((playlist_mutation_started_at + 75 - now))
  if [[ $remaining -gt 0 ]]; then
    sleep "$remaining"
  fi
}
wait_for_playlist_item_ids() {
  local expected=$1 current attempt
  for attempt in 1 2 3 4 5 6 7; do
    current=$(read_full_playlist_item_ids 2>/dev/null) || current=
    if [[ $current == "$expected" ]]; then
      printf '%s' "$current"
      return
    fi
    [[ $live_dry == 1 ]] && break
    sleep 5
  done
  return 1
}
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
  if [[ $library_album_restore_needed == 1 ]]; then
    if [[ $library_album_original_saved == true ]]; then
      if ! "$SPTFY" --backend file library albums add "$library_album_id" >/dev/null 2>&1; then
        printf 'warning: failed to restore original saved-album membership for %s\n' "$library_album_id" >&2
        live_status=1
      fi
    else
      if ! "$SPTFY" --backend file library albums remove "$library_album_id" >/dev/null 2>&1; then
        printf 'warning: failed to restore original saved-album membership for %s\n' "$library_album_id" >&2
        live_status=1
      fi
    fi
  fi
  if [[ $playlist_restore_needed == 1 ]]; then
    playlist_current_ids=$(read_full_playlist_item_ids 2>/dev/null) || playlist_current_ids=
    wait_for_playlist_write_settle
    playlist_current_ids=$(read_full_playlist_item_ids 2>/dev/null) || playlist_current_ids=
    if [[ $playlist_current_ids == "$playlist_replaced_ids" ]]; then
      playlist_mutation_started_at=$(date +%s)
      if ! "$SPTFY" --backend file playlists items update "$SPOTIFY_CLI_LIVE_PLAYLIST_ID" 1 --item "${playlist_expected_item_ids[1]}" >/dev/null 2>&1; then
        playlist_current_ids=
      else
        wait_for_playlist_write_settle
        playlist_current_ids=$(wait_for_playlist_item_ids "$playlist_baseline_ids") || playlist_current_ids=
      fi
    elif [[ $playlist_current_ids == "$playlist_partial_ids" ]]; then
      playlist_mutation_started_at=$(date +%s)
      if ! "$SPTFY" --backend file playlists items remove "$SPOTIFY_CLI_LIVE_PLAYLIST_ID" 1 >/dev/null 2>&1; then
        playlist_current_ids=
      else
        wait_for_playlist_write_settle
        playlist_current_ids=$(wait_for_playlist_item_ids "$playlist_baseline_ids") || playlist_current_ids=
      fi
    fi
    if [[ $playlist_current_ids != "$playlist_baseline_ids" ]]; then
      printf '%s\n' 'warning: failed to restore the exact playlist baseline' >&2
      live_status=1
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
grep -Fxq $'scopes\tplaylist-modify-private,playlist-modify-public,playlist-read-collaborative,playlist-read-private,user-library-modify,user-library-read,user-read-private' <<<"$me_out"
ordinary_out=$("$SPTFY" --backend file search track a --max 10)
[[ $(wc -l <<<"$ordinary_out") -gt 1 ]] || { printf '%s\n' 'ordinary search returned no rows' >&2; exit 1; }
live_nonce=$(od -An -N12 -tx1 /dev/urandom | tr -d ' \n')
empty_out=$("$SPTFY" --backend file search track "track:\"sptfy-$live_nonce\" artist:\"sptfy-$live_nonce\"")
[[ $(wc -l <<<"$empty_out") -eq 1 ]] || { printf '%s\n' 'guaranteed-no-match search returned rows' >&2; exit 1; }
playlist_mutation_candidate_id=$("$SPTFY" --backend file search track "$SPOTIFY_CLI_LIVE_PLAYLIST_MUTATION_SEARCH_QUERY" --id --max 1)
[[ $playlist_mutation_candidate_id =~ ^[A-Za-z0-9]{22}$ ]] || { printf '%s\n' 'playlist mutation search did not return exactly one track ID' >&2; exit 1; }
[[ $playlist_mutation_candidate_id == "$SPOTIFY_CLI_LIVE_PLAYLIST_MUTATION_TRACK_ID" ]] || { printf '%s\n' 'playlist mutation search result does not match the configured safe mutation fixture' >&2; exit 1; }

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

playlist_page_out="$SPOTIFY_CLI_LIVE_ROOT/playlist-page.out"
playlist_page_err="$SPOTIFY_CLI_LIVE_ROOT/playlist-page.err"
"$SPTFY" --backend file playlists list --max 1 >"$playlist_page_out" 2>"$playlist_page_err"
[[ $(sed -n '1p' "$playlist_page_out") == 'ID | PLAYLIST | OWNER_ID | OWNER | ITEM_COUNT | PUBLIC | COLLABORATIVE' ]] || { printf '%s\n' 'playlists returned an unexpected shape' >&2; exit 1; }
[[ $(wc -l <"$playlist_page_out") -eq 2 ]] || { printf '%s\n' 'playlist list did not return exactly one row' >&2; exit 1; }
playlist_token=$(sed -n 's/^More results available (next: \(.*\))$/\1/p' "$playlist_page_err")
if [[ $live_dry == 1 && -z $playlist_token ]]; then
  printf '%s\n' 'playlists did not return a continuation token' >&2
  exit 1
fi
if [[ -n $playlist_token ]]; then
  playlist_next=$("$SPTFY" --backend file playlists list --id --max 1 --next-page-token "$playlist_token")
  [[ -n $playlist_next && $(wc -l <<<"$playlist_next") -eq 1 ]] || { printf '%s\n' 'playlist continuation returned an unexpected shape' >&2; exit 1; }
fi
playlist_fixture_ids=$("$SPTFY" --backend file playlists list --id --max 50)
grep -Fxq "$SPOTIFY_CLI_LIVE_PLAYLIST_ID" <<<"$playlist_fixture_ids" || { printf '%s\n' 'configured playlist fixture was not listed' >&2; exit 1; }
[[ $("$SPTFY" --backend file playlists get "$SPOTIFY_CLI_LIVE_PLAYLIST_ID" --id) == "$SPOTIFY_CLI_LIVE_PLAYLIST_ID" ]] || { printf '%s\n' 'playlist get returned an unexpected ID' >&2; exit 1; }

playlist_baseline_out="$SPOTIFY_CLI_LIVE_ROOT/playlist-baseline.out"
playlist_baseline_err="$SPOTIFY_CLI_LIVE_ROOT/playlist-baseline.err"
"$SPTFY" --backend file playlists items list "$SPOTIFY_CLI_LIVE_PLAYLIST_ID" --id --max 50 >"$playlist_baseline_out" 2>"$playlist_baseline_err"
[[ ! -s $playlist_baseline_err ]] || { printf '%s\n' 'playlist baseline exceeds one 50-item page' >&2; exit 1; }
playlist_baseline_count=$(wc -l <"$playlist_baseline_out" | tr -d ' ')
[[ $playlist_baseline_count -ge 3 ]] || { printf '%s\n' 'playlist baseline has fewer than three items' >&2; exit 1; }
for playlist_prefix_index in 1 2 3; do
  [[ $(sed -n "${playlist_prefix_index}p" "$playlist_baseline_out") == "${playlist_expected_item_ids[playlist_prefix_index-1]}" ]] || { printf '%s\n' 'playlist baseline does not match the configured three-item prefix' >&2; exit 1; }
done
if grep -Fxq "$SPOTIFY_CLI_LIVE_PLAYLIST_MUTATION_TRACK_ID" "$playlist_baseline_out"; then
  printf '%s\n' 'playlist mutation track is already present in the runtime baseline' >&2
  exit 1
fi
playlist_baseline_ids=$(<"$playlist_baseline_out")
playlist_replaced_ids=$(sed -n '1p' "$playlist_baseline_out"; printf '%s\n' "$playlist_mutation_candidate_id"; sed -n '3,$p' "$playlist_baseline_out")
playlist_partial_ids=$(sed -n '1p' "$playlist_baseline_out"; printf '%s\n' "$playlist_mutation_candidate_id"; sed -n '2,$p' "$playlist_baseline_out")

playlist_items_page_out="$SPOTIFY_CLI_LIVE_ROOT/playlist-items-page.out"
playlist_items_page_err="$SPOTIFY_CLI_LIVE_ROOT/playlist-items-page.err"
"$SPTFY" --backend file playlists items list "$SPOTIFY_CLI_LIVE_PLAYLIST_ID" --max 2 >"$playlist_items_page_out" 2>"$playlist_items_page_err"
[[ $(wc -l <"$playlist_items_page_out") -eq 4 ]] || { printf '%s\n' 'playlist items first page did not return exactly two rows' >&2; exit 1; }
[[ $(sed -n '1p' "$playlist_items_page_out") == "Playlist ID: $SPOTIFY_CLI_LIVE_PLAYLIST_ID" ]] || { printf '%s\n' 'playlist items returned an unexpected parent' >&2; exit 1; }
[[ $(sed -n '2p' "$playlist_items_page_out") == 'POSITION | TYPE | ID | ITEM | ARTIST_IDS | ARTISTS | ALBUM_ID | ALBUM | DURATION' ]] || { printf '%s\n' 'playlist items returned an unexpected shape' >&2; exit 1; }
[[ $(awk -F ' \\| ' 'NR == 3 {print $1":"$3}' "$playlist_items_page_out") == "0:${playlist_expected_item_ids[0]}" ]] || { printf '%s\n' 'playlist item zero did not match the fixture' >&2; exit 1; }
[[ $(awk -F ' \\| ' 'NR == 4 {print $1":"$3}' "$playlist_items_page_out") == "1:${playlist_expected_item_ids[1]}" ]] || { printf '%s\n' 'playlist item one did not match the fixture' >&2; exit 1; }
playlist_items_token=$(sed -n 's/^More results available (next: \(.*\))$/\1/p' "$playlist_items_page_err")
[[ -n $playlist_items_token ]] || { printf '%s\n' 'playlist items did not return a continuation token' >&2; exit 1; }
playlist_items_next_out="$SPOTIFY_CLI_LIVE_ROOT/playlist-items-next.out"
playlist_items_next_err="$SPOTIFY_CLI_LIVE_ROOT/playlist-items-next.err"
"$SPTFY" --backend file playlists items list "$SPOTIFY_CLI_LIVE_PLAYLIST_ID" --max 2 --next-page-token "$playlist_items_token" >"$playlist_items_next_out" 2>"$playlist_items_next_err"
playlist_items_next_rows=1
if [[ $playlist_baseline_count -ge 4 ]]; then
  playlist_items_next_rows=2
fi
[[ $(wc -l <"$playlist_items_next_out") -eq $((playlist_items_next_rows + 2)) ]] || { printf '%s\n' 'playlist item continuation returned an unexpected row count' >&2; exit 1; }
[[ $(sed -n '1p' "$playlist_items_next_out") == "Playlist ID: $SPOTIFY_CLI_LIVE_PLAYLIST_ID" ]] || { printf '%s\n' 'playlist item continuation returned an unexpected parent' >&2; exit 1; }
[[ $(sed -n '2p' "$playlist_items_next_out") == 'POSITION | TYPE | ID | ITEM | ARTIST_IDS | ARTISTS | ALBUM_ID | ALBUM | DURATION' ]] || { printf '%s\n' 'playlist item continuation returned an unexpected shape' >&2; exit 1; }
[[ $(awk -F ' \\| ' 'NR == 3 {print $1":"$3}' "$playlist_items_next_out") == "2:${playlist_expected_item_ids[2]}" ]] || { printf '%s\n' 'playlist item continuation did not resume at position two' >&2; exit 1; }
if [[ $playlist_baseline_count -ge 4 ]]; then
  [[ $(awk -F ' \\| ' 'NR == 4 {print $1":"$3}' "$playlist_items_next_out") == "3:$(sed -n '4p' "$playlist_baseline_out")" ]] || { printf '%s\n' 'playlist item continuation position three did not match the baseline' >&2; exit 1; }
fi
playlist_items_next_token=$(sed -n 's/^More results available (next: \(.*\))$/\1/p' "$playlist_items_next_err")
if [[ $playlist_baseline_count -gt 4 ]]; then
  [[ -n $playlist_items_next_token ]] || { printf '%s\n' 'playlist item continuation omitted its next-page token' >&2; exit 1; }
else
  [[ ! -s $playlist_items_next_err ]] || { printf '%s\n' 'playlist item continuation emitted unexpected stderr' >&2; exit 1; }
fi
playlist_items_ids=$("$SPTFY" --backend file playlists items list "$SPOTIFY_CLI_LIVE_PLAYLIST_ID" --id --max 3)
[[ $playlist_items_ids == "$playlist_prefix_ids" ]] || { printf '%s\n' 'playlist item ID-only prefix did not match normal output' >&2; exit 1; }
if [[ $live_dry != 1 ]]; then
  go test -tags=keyring_nopassage,spotify_live ./internal/client -run '^TestPlaylistDuplicateURIRemovalContract$' -count=1
fi

playlist_restore_needed=1
playlist_mutation_started_at=$(date +%s)
playlist_update_out=$("$SPTFY" --backend file playlists items update "$SPOTIFY_CLI_LIVE_PLAYLIST_ID" 1 --item "$playlist_mutation_candidate_id")
IFS=$'\t' read -r playlist_action playlist_record_id playlist_record_position playlist_record_old playlist_record_new playlist_record_snapshot playlist_record_extra <<<"$playlist_update_out"
[[ $playlist_action == updated && $playlist_record_id == "$SPOTIFY_CLI_LIVE_PLAYLIST_ID" && $playlist_record_position == 1 && $playlist_record_old == "${playlist_expected_item_ids[1]}" && $playlist_record_new == "$playlist_mutation_candidate_id" && -n $playlist_record_snapshot && -z $playlist_record_extra ]] || { printf '%s\n' 'playlist update returned an unexpected record' >&2; exit 1; }
wait_for_playlist_write_settle
playlist_items_ids=$(wait_for_playlist_item_ids "$playlist_replaced_ids") || playlist_items_ids=
[[ $playlist_items_ids == "$playlist_replaced_ids" ]] || { printf '%s\n' 'playlist update did not replace exactly the target position' >&2; exit 1; }
playlist_mutation_started_at=$(date +%s)
playlist_update_out=$("$SPTFY" --backend file playlists items update "$SPOTIFY_CLI_LIVE_PLAYLIST_ID" 1 --item "${playlist_expected_item_ids[1]}")
IFS=$'\t' read -r playlist_action playlist_record_id playlist_record_position playlist_record_old playlist_record_new playlist_record_snapshot playlist_record_extra <<<"$playlist_update_out"
[[ $playlist_action == updated && $playlist_record_id == "$SPOTIFY_CLI_LIVE_PLAYLIST_ID" && $playlist_record_position == 1 && $playlist_record_old == "$playlist_mutation_candidate_id" && $playlist_record_new == "${playlist_expected_item_ids[1]}" && -n $playlist_record_snapshot && -z $playlist_record_extra ]] || { printf '%s\n' 'inverse playlist update returned an unexpected record' >&2; exit 1; }
wait_for_playlist_write_settle
playlist_items_ids=$(wait_for_playlist_item_ids "$playlist_baseline_ids") || playlist_items_ids=
[[ $playlist_items_ids == "$playlist_baseline_ids" ]] || { printf '%s\n' 'playlist update round trip did not restore the exact baseline' >&2; exit 1; }
playlist_restore_needed=0

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
library_album_id=$album_id
library_album_page_out="$SPOTIFY_CLI_LIVE_ROOT/library-album-page.out"
library_album_page_err="$SPOTIFY_CLI_LIVE_ROOT/library-album-page.err"
"$SPTFY" --backend file library albums list --max 1 >"$library_album_page_out" 2>"$library_album_page_err"
[[ $(sed -n '1p' "$library_album_page_out") == 'ADDED_AT | ID | ALBUM | ARTIST_IDS | ARTISTS | RELEASE_DATE | TOTAL_TRACKS' ]] || { printf '%s\n' 'saved albums returned an unexpected shape' >&2; exit 1; }
library_album_token=$(sed -n 's/^More results available (next: \(.*\))$/\1/p' "$library_album_page_err")
if [[ $live_dry == 1 && -z $library_album_token ]]; then
  printf '%s\n' 'saved albums did not return a continuation token' >&2
  exit 1
fi
if [[ -n $library_album_token ]]; then
  library_album_next=$("$SPTFY" --backend file library albums list --id --max 1 --next-page-token "$library_album_token")
  [[ -n $library_album_next && $(wc -l <<<"$library_album_next") -eq 1 ]] || { printf '%s\n' 'saved-album continuation returned an unexpected shape' >&2; exit 1; }
fi
library_album_original_saved=$("$SPTFY" --backend file library albums check "$library_album_id" | awk -F ' \\| ' 'NR == 2 {print $3}')
[[ $library_album_original_saved == true || $library_album_original_saved == false ]] || { printf '%s\n' 'saved-album check returned an unexpected value' >&2; exit 1; }
library_album_restore_needed=1
if [[ $library_album_original_saved == true ]]; then
  [[ $("$SPTFY" --backend file library albums remove "$library_album_id") == $'removed\t1' ]]
  [[ $("$SPTFY" --backend file library albums check "$library_album_id" | awk -F ' \\| ' 'NR == 2 {print $3}') == false ]]
  [[ $("$SPTFY" --backend file library albums add "$library_album_id") == $'added\t1' ]]
else
  [[ $("$SPTFY" --backend file library albums add "$library_album_id") == $'added\t1' ]]
  [[ $("$SPTFY" --backend file library albums check "$library_album_id" | awk -F ' \\| ' 'NR == 2 {print $3}') == true ]]
  [[ $("$SPTFY" --backend file library albums remove "$library_album_id") == $'removed\t1' ]]
fi
[[ $("$SPTFY" --backend file library albums check "$library_album_id" | awk -F ' \\| ' 'NR == 2 {print $3}') == "$library_album_original_saved" ]] || { printf '%s\n' 'saved-album membership was not restored' >&2; exit 1; }
library_album_restore_needed=0
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
  go test -tags=keyring_nopassage,spotify_live ./internal/credentials -run '^TestExpireCredential$' -count=1
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
