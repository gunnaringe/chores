#!/bin/bash
# Fetches the Material Symbols variable font for offline use by screenshot.js.
#
#   .claude/skills/verify-ui/fetch-icon-font.sh [dest-dir]   # default /tmp
#
# Writes ms.css + ms.woff2. Safe to re-run; skips the download if both exist.
set -euo pipefail

DEST="${1:-/tmp}"
mkdir -p "$DEST"

if [ -s "$DEST/ms.css" ] && [ -s "$DEST/ms.woff2" ]; then
  echo "icon font already present in $DEST"
  exit 0
fi

# The full UA matters. Google content-negotiates on it: anything it doesn't
# recognise as a modern browser gets legacy per-weight .ttf faces instead of
# the single variable .woff2, and the woff2 lookup below then finds nothing.
UA='Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36'
API='https://fonts.googleapis.com/css2?family=Material+Symbols+Outlined:opsz,wght,FILL,GRAD@20..48,100..700,0..1,0&display=block'

curl -sS -A "$UA" "$API" -o "$DEST/ms.css"

FONT_URL="$(grep -o 'https://[^)]*\.woff2' "$DEST/ms.css" | head -1)"
if [ -z "$FONT_URL" ]; then
  echo "ERROR: no .woff2 in the returned CSS — Google served legacy faces," >&2
  echo "which means the User-Agent above was not accepted. First lines:" >&2
  head -8 "$DEST/ms.css" >&2
  rm -f "$DEST/ms.css"
  exit 1
fi

curl -sS "$FONT_URL" -o "$DEST/ms.woff2"

if [ ! -s "$DEST/ms.woff2" ]; then
  echo "ERROR: font download produced an empty file" >&2
  exit 1
fi

echo "icon font -> $DEST/ms.css + $DEST/ms.woff2 ($(wc -c < "$DEST/ms.woff2") bytes)"
