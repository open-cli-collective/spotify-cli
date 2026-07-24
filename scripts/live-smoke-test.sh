#!/usr/bin/env bash
set -euo pipefail

test_root=$(mktemp -d "${TMPDIR:-/tmp}/spotify-cli-live-test.XXXXXX")
cleanup() { rm -rf -- "$test_root"; }
trap cleanup EXIT

fake="$test_root/sptfy"
cat >"$fake" <<'FAKE'
#!/usr/bin/env bash
set -euo pipefail
args=" $* "
root=${SPOTIFY_CLI_LIVE_ROOT:?}
[[ ${SPOTIFY_CLI_KEYRING_BACKEND:-} == file ]]
[[ $HOME == "$root/home" ]]
[[ $USERPROFILE == "$root/home" ]]
[[ $AppData == "$root/appdata" ]]
[[ $LocalAppData == "$root/localappdata" ]]
[[ $XDG_CONFIG_HOME == "$root/xdgconfig" ]]
[[ $XDG_CACHE_HOME == "$root/xdgcache" ]]
[[ $XDG_DATA_HOME == "$root/xdgdata" ]]
[[ $XDG_STATE_HOME == "$root/xdgstate" ]]
marker="$XDG_DATA_HOME/cleared"
if [[ $args == *" config clear "* ]]; then
  touch "$marker"
elif [[ $args == *" init "* ]]; then
  [[ $args == *" --backend file "* && $args == *" --non-interactive "* ]] || exit 8
  if [[ ${SPOTIFY_CLI_LIVE_FAKE_INITIAL_SAVED:-0} == 1 && ! -f $XDG_DATA_HOME/library-initialized ]]; then
    touch "$XDG_DATA_HOME/library-saved" "$XDG_DATA_HOME/library-initialized"
  fi
  if [[ ${SPOTIFY_CLI_LIVE_FAKE_INITIAL_ALBUM_SAVED:-0} == 1 && ! -f $XDG_DATA_HOME/library-album-initialized ]]; then
    touch "$XDG_DATA_HOME/library-album-saved" "$XDG_DATA_HOME/library-album-initialized"
  fi
  rm -f "$marker"
elif [[ $args == *" me "* ]]; then
  [[ ! -f $marker ]] || exit 4
  printf 'account_id\ttest\nscopes\tplaylist-modify-private,playlist-modify-public,playlist-read-collaborative,playlist-read-private,user-library-modify,user-library-read,user-read-private\n'
elif [[ $args == *" playlists list "* ]]; then
  if [[ $args == *" --id "* ]]; then
    if [[ $args == *" --max 50 "* ]]; then
      touch "$XDG_DATA_HOME/playlist-fixture-listed"
    fi
    printf '%s\n' "${SPOTIFY_CLI_LIVE_PLAYLIST_ID:?}"
  else
    printf 'ID | PLAYLIST | OWNER_ID | OWNER | ITEM_COUNT | PUBLIC | COLLABORATIVE\n'
    printf '%s | Mix | owner-1 | Ada | 3 | true | false\n' "${SPOTIFY_CLI_LIVE_PLAYLIST_ID:?}"
  fi
  if [[ $args == *" --max 1 "* && $args != *" --next-page-token "* ]]; then
    printf 'More results available (next: playlist-token)\n' >&2
  fi
elif [[ $args == *" playlists get "* ]]; then
	[[ -f $XDG_DATA_HOME/playlist-fixture-listed ]] || exit 17
	[[ $args == *" playlists get ${SPOTIFY_CLI_LIVE_PLAYLIST_ID:?} --id "* ]] || exit 16
	printf '%s\n' "$SPOTIFY_CLI_LIVE_PLAYLIST_ID"
elif [[ $args == *" playlists items list "* ]]; then
  IFS=, read -r -a item_ids <<<"${SPOTIFY_CLI_LIVE_PLAYLIST_ITEM_IDS:?}"
  extra_ids=()
  extra_count=0
  if [[ -n ${SPOTIFY_CLI_LIVE_FAKE_PLAYLIST_EXTRA_IDS:-} ]]; then
    IFS=, read -r -a extra_ids <<<"$SPOTIFY_CLI_LIVE_FAKE_PLAYLIST_EXTRA_IDS"
    extra_count=${#extra_ids[@]}
  fi
  playlist_items_base=" --backend file playlists items list ${SPOTIFY_CLI_LIVE_PLAYLIST_ID:?} "
  if [[ $args == "${playlist_items_base}--id --max 50 " ]]; then
    delayed_baseline=0
    if [[ -f $XDG_DATA_HOME/playlist-pending ]]; then
      rm -f "$XDG_DATA_HOME/playlist-pending"
      touch "$XDG_DATA_HOME/playlist-mutated"
      delayed_baseline=1
      [[ -z ${SPOTIFY_CLI_LIVE_FAKE_EVENT_LOG:-} ]] || printf '%s\n' delayed-baseline >>"$SPOTIFY_CLI_LIVE_FAKE_EVENT_LOG"
    fi
    if [[ -f $XDG_DATA_HOME/playlist-mutated && $delayed_baseline == 0 ]]; then
      if [[ ${SPOTIFY_CLI_LIVE_FAKE_DELAY_PLAYLIST_ADD_VISIBILITY:-0} == 1 && ! -f $XDG_DATA_HOME/playlist-visible-logged ]]; then
        touch "$XDG_DATA_HOME/playlist-visible-logged"
        [[ -z ${SPOTIFY_CLI_LIVE_FAKE_EVENT_LOG:-} ]] || printf '%s\n' visible >>"$SPOTIFY_CLI_LIVE_FAKE_EVENT_LOG"
      fi
      printf '%s\n' "${item_ids[0]}" "${SPOTIFY_CLI_LIVE_PLAYLIST_MUTATION_TRACK_ID:?}" "${item_ids[1]}" "${item_ids[2]}"
    else
      printf '%s\n' "${item_ids[0]}" "${item_ids[1]}" "${item_ids[2]}"
    fi
    if [[ $extra_count -gt 0 ]]; then
      printf '%s\n' "${extra_ids[@]}"
    fi
  elif [[ $args == "${playlist_items_base}--id --max 3 " ]]; then
    printf '%s\n' "${item_ids[0]}" "${item_ids[1]}" "${item_ids[2]}"
  elif [[ $args == "${playlist_items_base}--max 2 --next-page-token playlist-items-token " ]]; then
    printf 'Playlist ID: %s\n' "${SPOTIFY_CLI_LIVE_PLAYLIST_ID:?}"
    printf 'POSITION | TYPE | ID | ITEM | ARTIST_IDS | ARTISTS | ALBUM_ID | ALBUM | DURATION\n'
    printf '2 | track | %s | Three | artist-1 | Artist | album-1 | Album | 1:02\n' "${item_ids[2]}"
    if [[ $extra_count -gt 0 ]]; then
      printf '3 | track | %s | Four | artist-1 | Artist | album-1 | Album | 1:03\n' "${extra_ids[0]}"
    fi
    if [[ $extra_count -gt 1 ]]; then
      printf 'More results available (next: playlist-items-more-token)\n' >&2
    elif [[ ${SPOTIFY_CLI_LIVE_FAKE_SPURIOUS_ITEM_NEXT:-0} == 1 ]]; then
      printf 'More results available (next: bogus-final-token)\n' >&2
    fi
  elif [[ $args == "${playlist_items_base}--max 2 " ]]; then
    printf 'Playlist ID: %s\n' "${SPOTIFY_CLI_LIVE_PLAYLIST_ID:?}"
    printf 'POSITION | TYPE | ID | ITEM | ARTIST_IDS | ARTISTS | ALBUM_ID | ALBUM | DURATION\n'
    printf '0 | track | %s | One | artist-1 | Artist | album-1 | Album | 1:00\n' "${item_ids[0]}"
    printf '1 | track | %s | Two | artist-1 | Artist | album-1 | Album | 1:01\n' "${item_ids[1]}"
    printf 'More results available (next: playlist-items-token)\n' >&2
  else
    exit 18
  fi
elif [[ $args == *" playlists items add "* ]]; then
  [[ $args == " --backend file playlists items add ${SPOTIFY_CLI_LIVE_PLAYLIST_ID:?} ${SPOTIFY_CLI_LIVE_PLAYLIST_MUTATION_TRACK_ID:?} --position 1 " ]] || exit 19
  if [[ ${SPOTIFY_CLI_LIVE_FAKE_DELAY_PLAYLIST_ADD_VISIBILITY:-0} == 1 ]]; then
    touch "$XDG_DATA_HOME/playlist-pending"
  else
    touch "$XDG_DATA_HOME/playlist-mutated"
  fi
  [[ -z ${SPOTIFY_CLI_LIVE_FAKE_EVENT_LOG:-} ]] || printf '%s\n' add >>"$SPOTIFY_CLI_LIVE_FAKE_EVENT_LOG"
  if [[ ${SPOTIFY_CLI_LIVE_FAKE_INTERRUPT_PLAYLIST_ADD:-0} == 1 ]]; then
    kill -TERM "$PPID"
    exit 143
  fi
  [[ ${SPOTIFY_CLI_LIVE_FAKE_FAIL_PLAYLIST_ADD:-0} != 1 ]] || exit 20
  printf 'added\t%s\t1\t1\tsnapshot-add\n' "$SPOTIFY_CLI_LIVE_PLAYLIST_ID"
elif [[ $args == *" playlists items remove "* ]]; then
  [[ $args == " --backend file playlists items remove ${SPOTIFY_CLI_LIVE_PLAYLIST_ID:?} 1 " ]] || exit 21
  [[ ${SPOTIFY_CLI_LIVE_FAKE_FAIL_PLAYLIST_RESTORE:-0} != 1 ]] || exit 22
  rm -f "$XDG_DATA_HOME/playlist-mutated" "$XDG_DATA_HOME/playlist-pending"
  [[ -z ${SPOTIFY_CLI_LIVE_FAKE_EVENT_LOG:-} ]] || printf '%s\n' remove >>"$SPOTIFY_CLI_LIVE_FAKE_EVENT_LOG"
  printf 'removed\t%s\t1\t%s\tsnapshot-remove\n' "$SPOTIFY_CLI_LIVE_PLAYLIST_ID" "$SPOTIFY_CLI_LIVE_PLAYLIST_MUTATION_TRACK_ID"
elif [[ $args == *" library tracks list "* ]]; then
  printf 'ADDED_AT | ID | TRACK | ARTIST_IDS | ARTISTS | ALBUM_ID | ALBUM | DURATION\n'
  printf '2026-07-23T12:00:00Z | 11dFghVXANMlKmJXsNCbNl | Song | artist-1 | Artist | album-1 | Album | 1:00\n'
elif [[ $args == *" library tracks check "* ]]; then
  printf 'REFERENCE | ID | SAVED\n'
  if [[ -f $XDG_DATA_HOME/library-saved ]]; then
    printf '11dFghVXANMlKmJXsNCbNl | 11dFghVXANMlKmJXsNCbNl | true\n'
  else
    printf '11dFghVXANMlKmJXsNCbNl | 11dFghVXANMlKmJXsNCbNl | false\n'
  fi
elif [[ $args == *" library tracks add "* ]]; then
  if [[ ${SPOTIFY_CLI_LIVE_FAKE_FAIL_RESTORE:-0} == 1 && ${SPOTIFY_CLI_LIVE_FAKE_INITIAL_SAVED:-0} == 1 ]]; then
    exit 14
  fi
  touch "$XDG_DATA_HOME/library-saved"
  printf 'added\t1\n'
elif [[ $args == *" library tracks remove "* ]]; then
  if [[ ${SPOTIFY_CLI_LIVE_FAKE_FAIL_RESTORE:-0} == 1 && ${SPOTIFY_CLI_LIVE_FAKE_INITIAL_SAVED:-0} == 0 ]]; then
    exit 15
  fi
  rm -f "$XDG_DATA_HOME/library-saved"
  printf 'removed\t1\n'
elif [[ $args == *" library albums list "* ]]; then
  if [[ $args == *" --id "* ]]; then
    printf '2up3OPMp9Tb4dAKM2erWXQ\n'
  else
    printf 'ADDED_AT | ID | ALBUM | ARTIST_IDS | ARTISTS | RELEASE_DATE | TOTAL_TRACKS\n'
    printf '2026-07-23T12:00:00Z | 4aawyAB9vmqN3uQ7FjRGTy | Album | artist-1,artist-2 | First,Second | 2026 | 2\n'
  fi
  if [[ $args == *" --max 1 "* && $args != *" --next-page-token "* ]]; then
    printf 'More results available (next: album-token)\n' >&2
  fi
elif [[ $args == *" library albums check "* ]]; then
  printf 'REFERENCE | ID | SAVED\n'
  if [[ -f $XDG_DATA_HOME/library-album-saved ]]; then
    printf '4aawyAB9vmqN3uQ7FjRGTy | 4aawyAB9vmqN3uQ7FjRGTy | true\n'
  else
    printf '4aawyAB9vmqN3uQ7FjRGTy | 4aawyAB9vmqN3uQ7FjRGTy | false\n'
  fi
elif [[ $args == *" library albums add "* ]]; then
  touch "$XDG_DATA_HOME/library-album-saved"
  printf 'added\t1\n'
elif [[ $args == *" library albums remove "* ]]; then
  rm -f "$XDG_DATA_HOME/library-album-saved"
  printf 'removed\t1\n'
elif [[ $args == *" search track "* ]]; then
  printf 'ID | TRACK | ARTIST_IDS | ARTISTS | ALBUM_ID | ALBUM | DURATION\n'
  if [[ $args != *" track:\"sptfy-"* ]]; then
    if [[ $args == *" --id "* ]]; then
      printf 'track-1\n'
    else
      printf 'track-1 | Song | artist-1 | Artist | album-1 | Album | 1:00\n'
    fi
  fi
  if [[ $args == *" --max 1 "* && $args != *" --next-page-token "* ]]; then
    printf 'More results available (next: token)\n' >&2
  fi
elif [[ $args == *" search album "* ]]; then
  if [[ $args == *" --id "* ]]; then
    printf 'album-1\n'
  elif [[ $args == *" --fields "* ]]; then
    printf 'ALBUM | ARTIST_IDS | ARTWORK\nDebut | artist-1 | 640x640 https://image\n'
  elif [[ $args == *" --extended "* ]]; then
    printf 'ID | ALBUM | ARTIST_IDS | ARTISTS | RELEASE_DATE | TOTAL_TRACKS | URI | URL | ALBUM_TYPE | RELEASE_DATE_PRECISION | RESTRICTION\n'
    printf 'album-1 | Debut | artist-1 | Björk | 1993 | 12 | spotify:album:album-1 | https://open.spotify.com/album/album-1 | album | year | -\n'
  else
    printf 'ID | ALBUM | ARTIST_IDS | ARTISTS | RELEASE_DATE | TOTAL_TRACKS\n'
    printf 'album-1 | Debut | artist-1 | Björk | 1993 | 12\n'
  fi
elif [[ $args == *" search artist "* ]]; then
  if [[ $args == *" --id "* ]]; then
    printf 'artist-1\n'
  elif [[ $args == *" --fields "* ]]; then
    printf 'ARTIST | ARTWORK\nBjörk | 320x320 https://image\n'
  elif [[ $args == *" --extended "* ]]; then
    printf 'ID | ARTIST | URI | URL\n'
    printf 'artist-1 | Björk | spotify:artist:artist-1 | https://open.spotify.com/artist/artist-1\n'
  else
    printf 'ID | ARTIST\nartist-1 | Björk\n'
  fi
elif [[ $args == *" tracks get "* ]]; then
  [[ $args == *" tracks get 11dFghVXANMlKmJXsNCbNl --id "* ]] || exit 9
  printf '11dFghVXANMlKmJXsNCbNl\n'
elif [[ $args == *" albums get "* ]]; then
  [[ $args == *" albums get spotify:album:4aawyAB9vmqN3uQ7FjRGTy --id "* ]] || exit 10
  printf '4aawyAB9vmqN3uQ7FjRGTy\n'
elif [[ $args == *" artists get "* ]]; then
  [[ $args == *" artists get https://open.spotify.com/artist/0TnOYISbd1XYRBk9myaseg --id "* ]] || exit 11
  printf '0TnOYISbd1XYRBk9myaseg\n'
elif [[ $args == *" albums tracks list "* ]]; then
  [[ $args == *" albums tracks list spotify:album:4aawyAB9vmqN3uQ7FjRGTy --max 1 "* ]] || exit 12
  printf 'Album ID: 4aawyAB9vmqN3uQ7FjRGTy\n'
  printf 'ID | TRACK | ARTIST_IDS | ARTISTS | DURATION\n'
  printf 'track-1 | Song | artist-1 | Artist | 1:00\n'
elif [[ $args == *" artists albums list "* ]]; then
  [[ $args == *" artists albums list https://open.spotify.com/artist/0TnOYISbd1XYRBk9myaseg --max 1 "* ]] || exit 13
  printf 'Artist ID: 0TnOYISbd1XYRBk9myaseg\n'
  printf 'ID | ALBUM | ARTIST_IDS | ARTISTS | RELEASE_DATE | TOTAL_TRACKS\n'
  printf 'album-1 | Album | artist-1 | Artist | 2026 | 1\n'
fi
FAKE
chmod +x "$fake"

expect_guard_failure() {
  if "$@" >/dev/null 2>&1; then
    printf '%s\n' 'live harness guard unexpectedly succeeded' >&2
    exit 1
  fi
}

expect_guard_failure env -u SPOTIFY_CLI_LIVE SPOTIFY_CLI_LIVE_DEDICATED_ACCOUNT=1 SPOTIFY_CLI_LIVE_DRY_RUN=1 SPOTIFY_CLI_LIVE_BINARY="$fake" SPOTIFY_CLIENT_ID=test ./scripts/live-smoke.sh
expect_guard_failure env -u SPOTIFY_CLI_LIVE_DEDICATED_ACCOUNT SPOTIFY_CLI_LIVE=1 SPOTIFY_CLI_LIVE_DRY_RUN=1 SPOTIFY_CLI_LIVE_BINARY="$fake" SPOTIFY_CLIENT_ID=test ./scripts/live-smoke.sh
expect_guard_failure env -u SPOTIFY_CLIENT_ID SPOTIFY_CLI_LIVE=1 SPOTIFY_CLI_LIVE_DEDICATED_ACCOUNT=1 SPOTIFY_CLI_LIVE_DRY_RUN=1 SPOTIFY_CLI_LIVE_BINARY="$fake" ./scripts/live-smoke.sh
expect_guard_failure env -u SPOTIFY_CLI_LIVE_PLAYLIST_ID SPOTIFY_CLI_LIVE=1 SPOTIFY_CLI_LIVE_DEDICATED_ACCOUNT=1 SPOTIFY_CLI_LIVE_DRY_RUN=1 SPOTIFY_CLI_LIVE_BINARY="$fake" SPOTIFY_CLIENT_ID=test ./scripts/live-smoke.sh
expect_guard_failure env SPOTIFY_CLI_LIVE=1 SPOTIFY_CLI_LIVE_DEDICATED_ACCOUNT=1 SPOTIFY_CLI_LIVE_DRY_RUN=1 SPOTIFY_CLI_LIVE_BINARY="$fake" SPOTIFY_CLI_LIVE_PLAYLIST_ID=bad SPOTIFY_CLIENT_ID=test ./scripts/live-smoke.sh
expect_guard_failure env -u SPOTIFY_CLI_LIVE_PLAYLIST_ITEM_IDS SPOTIFY_CLI_LIVE=1 SPOTIFY_CLI_LIVE_DEDICATED_ACCOUNT=1 SPOTIFY_CLI_LIVE_DRY_RUN=1 SPOTIFY_CLI_LIVE_BINARY="$fake" SPOTIFY_CLI_LIVE_PLAYLIST_ID=0123456789ABCDEFGHIJKL SPOTIFY_CLIENT_ID=test ./scripts/live-smoke.sh
expect_guard_failure env SPOTIFY_CLI_LIVE=1 SPOTIFY_CLI_LIVE_DEDICATED_ACCOUNT=1 SPOTIFY_CLI_LIVE_DRY_RUN=1 SPOTIFY_CLI_LIVE_BINARY="$fake" SPOTIFY_CLI_LIVE_PLAYLIST_ID=0123456789ABCDEFGHIJKL SPOTIFY_CLI_LIVE_PLAYLIST_ITEM_IDS=abcdefghijklmnopqrstuv SPOTIFY_CLIENT_ID=test ./scripts/live-smoke.sh
expect_guard_failure env SPOTIFY_CLI_LIVE=1 SPOTIFY_CLI_LIVE_DEDICATED_ACCOUNT=1 SPOTIFY_CLI_LIVE_DRY_RUN=1 SPOTIFY_CLI_LIVE_BINARY="$fake" SPOTIFY_CLI_LIVE_PLAYLIST_ID=0123456789ABCDEFGHIJKL SPOTIFY_CLI_LIVE_PLAYLIST_ITEM_IDS=abcdefghijklmnopqrstuv,ZYXWVUTSRQPONMLKJIHGFE,1111111111111111111111,2222222222222222222222 SPOTIFY_CLIENT_ID=test ./scripts/live-smoke.sh
expect_guard_failure env -u SPOTIFY_CLI_LIVE_PLAYLIST_MUTATION_TRACK_ID SPOTIFY_CLI_LIVE=1 SPOTIFY_CLI_LIVE_DEDICATED_ACCOUNT=1 SPOTIFY_CLI_LIVE_DRY_RUN=1 SPOTIFY_CLI_LIVE_BINARY="$fake" SPOTIFY_CLI_LIVE_PLAYLIST_ID=0123456789ABCDEFGHIJKL SPOTIFY_CLI_LIVE_PLAYLIST_ITEM_IDS=abcdefghijklmnopqrstuv,ZYXWVUTSRQPONMLKJIHGFE,1111111111111111111111 SPOTIFY_CLIENT_ID=test ./scripts/live-smoke.sh
expect_guard_failure env SPOTIFY_CLI_LIVE=1 SPOTIFY_CLI_LIVE_DEDICATED_ACCOUNT=1 SPOTIFY_CLI_LIVE_DRY_RUN=1 SPOTIFY_CLI_LIVE_BINARY="$fake" SPOTIFY_CLI_LIVE_PLAYLIST_ID=0123456789ABCDEFGHIJKL SPOTIFY_CLI_LIVE_PLAYLIST_ITEM_IDS=abcdefghijklmnopqrstuv,ZYXWVUTSRQPONMLKJIHGFE,1111111111111111111111 SPOTIFY_CLI_LIVE_PLAYLIST_MUTATION_TRACK_ID=bad SPOTIFY_CLIENT_ID=test ./scripts/live-smoke.sh
expect_guard_failure env SPOTIFY_CLI_LIVE=1 SPOTIFY_CLI_LIVE_DEDICATED_ACCOUNT=1 SPOTIFY_CLI_LIVE_DRY_RUN=1 SPOTIFY_CLI_LIVE_BINARY="$fake" SPOTIFY_CLI_LIVE_PLAYLIST_ID=0123456789ABCDEFGHIJKL SPOTIFY_CLI_LIVE_PLAYLIST_ITEM_IDS=abcdefghijklmnopqrstuv,ZYXWVUTSRQPONMLKJIHGFE,1111111111111111111111 SPOTIFY_CLI_LIVE_PLAYLIST_MUTATION_TRACK_ID=abcdefghijklmnopqrstuv SPOTIFY_CLIENT_ID=test ./scripts/live-smoke.sh

for initially_saved in 0 1; do
  smoke_err="$test_root/smoke-$initially_saved.err"
  playlist_extra_ids=
  if [[ $initially_saved == 1 ]]; then
    playlist_extra_ids=3333333333333333333333,4444444444444444444444
  fi
  if ! TMPDIR="$test_root" \
    SPOTIFY_CLI_LIVE=1 \
    SPOTIFY_CLI_LIVE_DEDICATED_ACCOUNT=1 \
    SPOTIFY_CLI_LIVE_DRY_RUN=1 \
    SPOTIFY_CLI_LIVE_BINARY="$fake" \
    SPOTIFY_CLI_LIVE_FAKE_INITIAL_SAVED="$initially_saved" \
    SPOTIFY_CLI_LIVE_FAKE_INITIAL_ALBUM_SAVED="$initially_saved" \
    SPOTIFY_CLI_LIVE_FAKE_PLAYLIST_EXTRA_IDS="$playlist_extra_ids" \
    SPOTIFY_CLI_LIVE_PLAYLIST_ID=0123456789ABCDEFGHIJKL \
    SPOTIFY_CLI_LIVE_PLAYLIST_ITEM_IDS=abcdefghijklmnopqrstuv,ZYXWVUTSRQPONMLKJIHGFE,1111111111111111111111 \
    SPOTIFY_CLI_LIVE_PLAYLIST_MUTATION_TRACK_ID=2222222222222222222222 \
    SPOTIFY_CLIENT_ID=test \
    ./scripts/live-smoke.sh >/dev/null 2>"$smoke_err"; then
    sed -n '1,120p' "$smoke_err" >&2
    exit 1
  fi
  rm -f "$smoke_err"
done

spurious_err="$test_root/spurious-item-next.err"
if TMPDIR="$test_root" \
  SPOTIFY_CLI_LIVE=1 \
  SPOTIFY_CLI_LIVE_DEDICATED_ACCOUNT=1 \
  SPOTIFY_CLI_LIVE_DRY_RUN=1 \
  SPOTIFY_CLI_LIVE_BINARY="$fake" \
  SPOTIFY_CLI_LIVE_FAKE_SPURIOUS_ITEM_NEXT=1 \
  SPOTIFY_CLI_LIVE_PLAYLIST_ID=0123456789ABCDEFGHIJKL \
  SPOTIFY_CLI_LIVE_PLAYLIST_ITEM_IDS=abcdefghijklmnopqrstuv,ZYXWVUTSRQPONMLKJIHGFE,1111111111111111111111 \
  SPOTIFY_CLI_LIVE_PLAYLIST_MUTATION_TRACK_ID=2222222222222222222222 \
  SPOTIFY_CLIENT_ID=test \
  ./scripts/live-smoke.sh >/dev/null 2>"$spurious_err"; then
  printf '%s\n' 'live harness unexpectedly accepted a final playlist-item continuation marker' >&2
  exit 1
fi
grep -Fq 'playlist item continuation emitted unexpected stderr' "$spurious_err"
rm -f "$spurious_err"

for initially_saved in 0 1; do
  restore_err="$test_root/restore-$initially_saved.err"
  if TMPDIR="$test_root" \
    SPOTIFY_CLI_LIVE=1 \
    SPOTIFY_CLI_LIVE_DEDICATED_ACCOUNT=1 \
    SPOTIFY_CLI_LIVE_DRY_RUN=1 \
    SPOTIFY_CLI_LIVE_BINARY="$fake" \
    SPOTIFY_CLI_LIVE_FAKE_INITIAL_SAVED="$initially_saved" \
    SPOTIFY_CLI_LIVE_FAKE_FAIL_RESTORE=1 \
    SPOTIFY_CLI_LIVE_PLAYLIST_ID=0123456789ABCDEFGHIJKL \
    SPOTIFY_CLI_LIVE_PLAYLIST_ITEM_IDS=abcdefghijklmnopqrstuv,ZYXWVUTSRQPONMLKJIHGFE,1111111111111111111111 \
    SPOTIFY_CLI_LIVE_PLAYLIST_MUTATION_TRACK_ID=2222222222222222222222 \
    SPOTIFY_CLIENT_ID=test \
    ./scripts/live-smoke.sh >/dev/null 2>"$restore_err"; then
    printf '%s\n' 'live harness unexpectedly ignored a restoration failure' >&2
    exit 1
  fi
  grep -Fq 'warning: failed to restore original saved-track membership' "$restore_err"
done

for playlist_failure in FAIL INTERRUPT; do
  playlist_event_log="$test_root/playlist-$playlist_failure.events"
  playlist_failure_err="$test_root/playlist-$playlist_failure.err"
  failure_name="SPOTIFY_CLI_LIVE_FAKE_${playlist_failure}_PLAYLIST_ADD"
  if env TMPDIR="$test_root" \
    SPOTIFY_CLI_LIVE=1 \
    SPOTIFY_CLI_LIVE_DEDICATED_ACCOUNT=1 \
    SPOTIFY_CLI_LIVE_DRY_RUN=1 \
    SPOTIFY_CLI_LIVE_BINARY="$fake" \
    SPOTIFY_CLI_LIVE_FAKE_EVENT_LOG="$playlist_event_log" \
    "$failure_name"=1 \
    SPOTIFY_CLI_LIVE_PLAYLIST_ID=0123456789ABCDEFGHIJKL \
    SPOTIFY_CLI_LIVE_PLAYLIST_ITEM_IDS=abcdefghijklmnopqrstuv,ZYXWVUTSRQPONMLKJIHGFE,1111111111111111111111 \
    SPOTIFY_CLI_LIVE_PLAYLIST_MUTATION_TRACK_ID=2222222222222222222222 \
    SPOTIFY_CLIENT_ID=test \
    ./scripts/live-smoke.sh >/dev/null 2>"$playlist_failure_err"; then
    printf '%s\n' "live harness unexpectedly accepted $playlist_failure playlist add" >&2
    exit 1
  fi
  [[ $(cat "$playlist_event_log") == $'add\nremove' ]] || { printf '%s\n' "playlist $playlist_failure cleanup did not restore" >&2; exit 1; }
  rm -f "$playlist_event_log" "$playlist_failure_err"
done

delayed_playlist_event_log="$test_root/playlist-delayed.events"
delayed_playlist_err="$test_root/playlist-delayed.err"
if env TMPDIR="$test_root" \
  SPOTIFY_CLI_LIVE=1 \
  SPOTIFY_CLI_LIVE_DEDICATED_ACCOUNT=1 \
  SPOTIFY_CLI_LIVE_DRY_RUN=1 \
  SPOTIFY_CLI_LIVE_BINARY="$fake" \
  SPOTIFY_CLI_LIVE_FAKE_EVENT_LOG="$delayed_playlist_event_log" \
  SPOTIFY_CLI_LIVE_FAKE_DELAY_PLAYLIST_ADD_VISIBILITY=1 \
  SPOTIFY_CLI_LIVE_FAKE_FAIL_PLAYLIST_ADD=1 \
  SPOTIFY_CLI_LIVE_PLAYLIST_ID=0123456789ABCDEFGHIJKL \
  SPOTIFY_CLI_LIVE_PLAYLIST_ITEM_IDS=abcdefghijklmnopqrstuv,ZYXWVUTSRQPONMLKJIHGFE,1111111111111111111111 \
  SPOTIFY_CLI_LIVE_PLAYLIST_MUTATION_TRACK_ID=2222222222222222222222 \
  SPOTIFY_CLIENT_ID=test \
  ./scripts/live-smoke.sh >/dev/null 2>"$delayed_playlist_err"; then
  printf '%s\n' 'live harness unexpectedly accepted a delayed playlist add failure' >&2
  exit 1
fi
[[ $(cat "$delayed_playlist_event_log") == $'add\ndelayed-baseline\nvisible\nremove' ]] || { printf '%s\n' 'delayed playlist cleanup did not wait for visibility and restore' >&2; exit 1; }
rm -f "$delayed_playlist_event_log" "$delayed_playlist_err"

playlist_restore_err="$test_root/playlist-restore.err"
if TMPDIR="$test_root" \
  SPOTIFY_CLI_LIVE=1 \
  SPOTIFY_CLI_LIVE_DEDICATED_ACCOUNT=1 \
  SPOTIFY_CLI_LIVE_DRY_RUN=1 \
  SPOTIFY_CLI_LIVE_BINARY="$fake" \
  SPOTIFY_CLI_LIVE_FAKE_FAIL_PLAYLIST_RESTORE=1 \
  SPOTIFY_CLI_LIVE_PLAYLIST_ID=0123456789ABCDEFGHIJKL \
  SPOTIFY_CLI_LIVE_PLAYLIST_ITEM_IDS=abcdefghijklmnopqrstuv,ZYXWVUTSRQPONMLKJIHGFE,1111111111111111111111 \
  SPOTIFY_CLI_LIVE_PLAYLIST_MUTATION_TRACK_ID=2222222222222222222222 \
  SPOTIFY_CLIENT_ID=test \
  ./scripts/live-smoke.sh >/dev/null 2>"$playlist_restore_err"; then
  printf '%s\n' 'live harness unexpectedly ignored a playlist restoration failure' >&2
  exit 1
fi
grep -Fq 'warning: failed to restore the exact playlist baseline' "$playlist_restore_err"
rm -f "$playlist_restore_err"

[[ $(find "$test_root" -mindepth 1 -maxdepth 1 | wc -l) -eq 3 ]]
