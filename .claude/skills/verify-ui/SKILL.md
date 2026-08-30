---
name: verify-ui
description: Drive the Chores frontend in a real browser and capture screenshots, at phone and desktop sizes, in light and dark mode. Use to verify any change under web/ — the Go test suite covers none of the UI, and node --check only catches syntax errors. Covers the Material Symbols font stub that agent sandboxes need.
---

# Verifying frontend changes in a browser

Nothing in `go test ./...` exercises `web/`, and `node --check` only parses.
A rendering or layout change is unverified until it has been looked at.

## Prerequisites

Start the app first — see the `run-local` skill — and remember that `web/` is
embedded at build time, so **restart the server after every frontend edit** or
you will screenshot the old UI.

Chromium and Playwright are preinstalled; do not run `playwright install`.

- browser: `/opt/pw-browsers/chromium-*/chrome-linux/chrome`
- library: `/opt/node22/lib/node_modules/playwright`

## Stub the icon font first

`web/index.html` loads Material Symbols from `fonts.googleapis.com`, which the
browser cannot reach from a sandbox. This matters more than it sounds:

Material Symbols render as **ligature text**, so with the font missing every
glyph draws as its literal name — `dishwasher_gen`, `expand_more` — at full
text width. Screenshots become unreadable, *and* the overflowing text can
intercept pointer events, so clicks fail with a confusing "element intercepts
pointer events" timeout on a locator that is plainly visible.

`curl` does have proxy access, so fetch the font once to disk and let the
browser load it from there:

```bash
.claude/skills/verify-ui/fetch-icon-font.sh /tmp
```

That writes `ms.css` + `ms.woff2` and is safe to re-run. It is a script rather
than a one-line `curl` because Google content-negotiates on the User-Agent:
anything it doesn't recognise as a modern browser gets legacy per-weight
`.ttf` faces instead of the single variable `.woff2`, and the extraction then
silently finds nothing. The script pins a full UA and fails loudly if it gets
the legacy response anyway.

`screenshot.js` picks the stub up automatically.

## Seed data first

A fresh database has no family, so most tabs have nothing in them to
screenshot. `seed-demo-data.js` creates a family (if needed), one or more
children, a repeating task, and completions spread across several months —
including two year boundaries back, so anything that depends on multi-year
history (Balance's month-by-month breakdown) has something to show:

```bash
node .claude/skills/verify-ui/seed-demo-data.js \
  --url http://localhost:8080/ --children "Anna,Erik"
```

It logs in the same way `screenshot.js` does, reuses whatever family that
login already has if one exists, and prints the created ids as JSON. Flags:
`--family`, `--children` (comma-separated), `--task`, `--price-cents`, `--url`,
`--playwright`. Completion dates are computed relative to *today*, not
hardcoded, so the year-boundary behavior stays exercised no matter when this
runs.

## Capture

```bash
.claude/skills/verify-ui/fetch-icon-font.sh /tmp          # once per container
node .claude/skills/verify-ui/screenshot.js \
  --out /tmp/shots --assets /tmp --url http://localhost:8080/
```

It logs in through devauth as Test Parent and captures every tab at phone
size, plus dark mode, a 320px screen and desktop. Any page error, console
error or 4xx/5xx is printed at the end — treat a non-empty list as a failure.

Against a **fresh database** there is no family yet, so there are no tabs to
walk; the script says so and captures onboarding instead. Run
`seed-demo-data.js` first if you need the real screens.

Then actually **look at the PNGs** with the Read tool. That is the point; a
script that exits 0 has verified nothing about how the page looks.

## Writing your own flow

Copy `screenshot.js` and edit it. The parts worth keeping:

```js
const browser = await chromium.launch({
  executablePath: CHROME,                       // globbed, see the script
  args: ['--proxy-server=' + process.env.HTTPS_PROXY, '--ignore-certificate-errors'],
});
const ctx = await browser.newContext({ ...devices['iPhone 13'] });
await stubFonts(ctx);                           // always, before newPage()
```

Collect diagnostics on every page — silent console errors are how a broken
render passes review:

```js
page.on('pageerror', e => errs.push('PAGEERROR: ' + e.message));
page.on('console', m => m.type() === 'error' && errs.push('CONSOLE: ' + m.text()));
page.on('response', r => r.status() >= 400 && errs.push(r.status() + ' ' + r.url()));
```

## Regenerating the welcome page screenshots

`web/screenshots/` holds the shots the logged-out welcome page shows, one set
per language (`today-en.webp`, `today-nb.webp`, …). They are real captures of
a seeded family, so they have to be regenerated whenever the screens in them
change — and both languages, since the task titles in them are seeded data,
not UI strings.

There is **no image tooling in this sandbox** (no ImageMagick, no sharp, no
pngquant), so re-encode through Chromium itself. `canvas.toDataURL("image/webp", 0.88)`
turns a ~110 KB PNG capture into a ~47 KB WebP with no visible loss at the
size the page displays:

```js
const c = document.createElement("canvas");
c.width = img.naturalWidth; c.height = img.naturalHeight;
c.getContext("2d").drawImage(img, 0, 0);
return c.toDataURL("image/webp", 0.88);   // then base64-decode and write
```

Capture at a 390x720 viewport with `deviceScaleFactor: 2`, not `fullPage` —
the page frames them as phones.

## Reading the results

- **Move the mouse away before screenshotting** (`page.mouse.move(0, 0)`).
  Playwright leaves the pointer where it last clicked, so a hovered control
  photographs in its hover state and looks like a styling bug.
- **`fullPage: true` renders fixed elements once, mid-page.** The bottom tab
  bar shows up floating in the middle of a tall screenshot with content
  behind it. That is a screenshot artifact, not a layout bug — use
  `fullPage: false` to see the real viewport.
- **Lazy-loaded `<img loading="lazy">` elements (the welcome page's
  screenshots) can paint as blank white boxes in a `fullPage` capture**, even
  though `img.complete` and `naturalWidth` both report the fetch succeeded.
  Resizing the viewport for a full-page shot doesn't reliably fire the same
  paint/intersection path a real scroll does. Fix: scroll through the page in
  small steps with a short pause between each before capturing, rather than
  jumping straight to a fullPage screenshot:
  ```js
  await page.evaluate(async () => {
    for (let y = 0; y < document.body.scrollHeight; y += 400) {
      window.scrollTo(0, y);
      await new Promise((r) => setTimeout(r, 60));
    }
    window.scrollTo(0, 0);
  });
  ```
  Don't mistake this for a real bug — check `img.complete`/`naturalWidth` via
  `page.evaluate()` first; if those report success but the screenshot is
  blank, it's this, not broken image loading.
- Check both themes (`colorScheme: 'dark'`) and a narrow screen
  (`devices['iPhone SE']`, 320px). Most layout breaks show up at 320px first.
