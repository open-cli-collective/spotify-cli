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
  rm -f "$marker"
elif [[ $args == *" me "* ]]; then
  [[ ! -f $marker ]] || exit 4
  printf 'account_id\ttest\nscopes\tuser-read-private\n'
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
    printf 'ID | ARTIST | GENRES | URI | URL\n'
    printf 'artist-1 | Björk | art pop | spotify:artist:artist-1 | https://open.spotify.com/artist/artist-1\n'
  else
    printf 'ID | ARTIST | GENRES\nartist-1 | Björk | art pop\n'
  fi
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

TMPDIR="$test_root" \
SPOTIFY_CLI_LIVE=1 \
SPOTIFY_CLI_LIVE_DEDICATED_ACCOUNT=1 \
SPOTIFY_CLI_LIVE_DRY_RUN=1 \
SPOTIFY_CLI_LIVE_BINARY="$fake" \
SPOTIFY_CLIENT_ID=test \
./scripts/live-smoke.sh >/dev/null 2>&1

[[ $(find "$test_root" -mindepth 1 -maxdepth 1 | wc -l) -eq 1 ]]
