#!/usr/bin/env bash
# Refreshes web/fonts/material-symbols-outlined.woff2, the self-hosted copy
# of Google's Material Symbols Outlined icon font (Apache License 2.0, see
# web/fonts/LICENSE-material-symbols.txt). Self-hosted so no page ever
# makes a request to Google — see the privacy policy (web/privacy.html).
#
#   scripts/update-icon-font.sh
#
# Only the axes the app actually uses are kept (opsz 24, GRAD 0, wght
# 400–500, FILL 0–1: see the .tabbar icon rules in web/app.css), which
# roughly halves the file compared to the full variable font. Every icon is
# kept, since the task icon picker offers all of them.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
dest="$repo_root/web/fonts"

# Google content-negotiates on the User-Agent: anything it doesn't recognise
# as a modern browser gets legacy per-weight .ttf faces instead of the
# single variable .woff2.
UA='Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36'
API='https://fonts.googleapis.com/css2?family=Material+Symbols+Outlined:opsz,wght,FILL,GRAD@24,400..500,0..1,0&display=block'

font_url="$(curl -fsS -A "$UA" "$API" | grep -o 'https://[^)]*\.woff2' | head -1)"
if [ -z "$font_url" ]; then
  echo "ERROR: no .woff2 in Google's CSS response (User-Agent not accepted?)" >&2
  exit 1
fi
curl -fsS "$font_url" -o "$dest/material-symbols-outlined.woff2"
curl -fsS https://raw.githubusercontent.com/google/material-design-icons/master/LICENSE -o "$dest/LICENSE-material-symbols.txt"
echo "Fetched $font_url" >&2
echo "Done. Bump CACHE_NAME in web/sw.js, then review and commit web/fonts/." >&2
