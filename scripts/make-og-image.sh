#!/usr/bin/env bash
# Regenerates web/icons/og-image-<lang>.png — the 1200x630 cards Facebook,
# Slack and friends show when someone links to the app, one per UI language
# (which one a given hostname gets is decided at runtime; see web/og.go).
# The cards are laid out as HTML and screenshotted with
# headless Chrome, so it picks up the same colours as web/app.css rather than
# being drawn by hand in an image editor; if you restyle the app's palette,
# update the values below to match and re-run this. The wording comes from
# appNames in web/manifest.go and ogTaglines in web/og.go — keep all three in
# step.
#
# Needs a Chrome/Chromium on PATH (or $CHROME). Nothing else here uses one,
# so this is not part of any build — run it only when the card changes.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT

chrome="${CHROME:-$(command -v google-chrome || command -v chromium || command -v chromium-browser || true)}"
if [[ -z "$chrome" ]]; then
  echo "No Chrome/Chromium found. Install one or set CHROME=/path/to/chrome." >&2
  exit 1
fi

# The logo is referenced relative to the HTML, so copy it next to the file.
cp "$repo_root/web/icons/icon-512.png" "$work_dir/icon-512.png"

# lang|title|tagline — the localized app name and the one-line description,
# matching what the served og:title and og:description say for each language.
cards=(
  "en|Chores|Family chores and allowance, in one place."
  "nb|Ukepenger|Husarbeid og ukepenger på ett sted."
  "nn|Vekepengar|Husarbeid og vekepengar på éin stad."
  "sv|Veckopeng|Hushållssysslor och veckopeng, på ett ställe."
)

for card in "${cards[@]}"; do
  IFS="|" read -r lang title tagline <<< "$card"

  # The title sets its own size: "Chores" fits at 104px but the longer
  # Scandinavian names would crowd the logo, so they step down a little.
  title_size=104
  (( ${#title} > 8 )) && title_size=84

  cat > "$work_dir/card.html" <<HTML
<!doctype html>
<html>
<head>
<meta charset="utf-8" />
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  html, body { width: 1200px; height: 630px; }
  body {
    /* --bg and --text/--muted/--accent from web/app.css's light theme. */
    background: #f2f3f7;
    font-family: -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
    display: flex; align-items: center; justify-content: center;
  }
  .card { display: flex; align-items: center; gap: 64px; }
  img { width: 240px; height: 240px; display: block; }
  h1 { font-size: ${title_size}px; line-height: 1; font-weight: 700; color: #11141a; letter-spacing: -0.03em; }
  p { font-size: 42px; line-height: 1.3; color: #656d7b; margin-top: 24px; max-width: 640px; }
  .rule { width: 132px; height: 10px; border-radius: 5px; background: #2563eb; margin-top: 36px; }
</style>
</head>
<body>
  <div class="card">
    <img src="icon-512.png" alt="" />
    <div>
      <h1>${title}</h1>
      <p>${tagline}</p>
      <div class="rule"></div>
    </div>
  </div>
</body>
</html>
HTML

  "$chrome" --headless --disable-gpu --no-sandbox --hide-scrollbars \
    --force-device-scale-factor=1 --window-size=1200,630 \
    --screenshot="$repo_root/web/icons/og-image-$lang.png" "$work_dir/card.html" 2>/dev/null

  echo "Wrote web/icons/og-image-$lang.png" >&2
done
