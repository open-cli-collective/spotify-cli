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
  rm -f "$marker"
elif [[ $args == *" me "* ]]; then
  [[ ! -f $marker ]] || exit 4
  printf 'account_id\ttest\nscopes\tuser-library-modify,user-library-read,user-read-private\n'
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

for initially_saved in 0 1; do
  TMPDIR="$test_root" \
  SPOTIFY_CLI_LIVE=1 \
  SPOTIFY_CLI_LIVE_DEDICATED_ACCOUNT=1 \
  SPOTIFY_CLI_LIVE_DRY_RUN=1 \
  SPOTIFY_CLI_LIVE_BINARY="$fake" \
  SPOTIFY_CLI_LIVE_FAKE_INITIAL_SAVED="$initially_saved" \
  SPOTIFY_CLIENT_ID=test \
  ./scripts/live-smoke.sh >/dev/null 2>&1
done

for initially_saved in 0 1; do
  restore_err="$test_root/restore-$initially_saved.err"
  if TMPDIR="$test_root" \
    SPOTIFY_CLI_LIVE=1 \
    SPOTIFY_CLI_LIVE_DEDICATED_ACCOUNT=1 \
    SPOTIFY_CLI_LIVE_DRY_RUN=1 \
    SPOTIFY_CLI_LIVE_BINARY="$fake" \
    SPOTIFY_CLI_LIVE_FAKE_INITIAL_SAVED="$initially_saved" \
    SPOTIFY_CLI_LIVE_FAKE_FAIL_RESTORE=1 \
    SPOTIFY_CLIENT_ID=test \
    ./scripts/live-smoke.sh >/dev/null 2>"$restore_err"; then
    printf '%s\n' 'live harness unexpectedly ignored a restoration failure' >&2
    exit 1
  fi
  grep -Fq 'warning: failed to restore original saved-track membership' "$restore_err"
done

[[ $(find "$test_root" -mindepth 1 -maxdepth 1 | wc -l) -eq 3 ]]
