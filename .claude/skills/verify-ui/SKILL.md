---
name: verify-ui
description: Drive the Chores frontend in a real browser and capture screenshots, at phone and desktop sizes, in light and dark mode. Use to verify any change under web/ — the Go test suite covers none of the UI, and node --check only catches syntax errors.
---

# Verifying frontend changes in a browser

Nothing in `go test ./...` exercises `web/`, and `node --check` only parses.
A rendering or layout change is unverified until it has been looked at.

## Prerequisites

Start the app first — see the `run-local` skill — and remember that `web/` is
embedded at build time, so **restart the server after every frontend edit** or
you will screenshot the old UI.

`lib.js` finds the browser and Playwright on its own, so no path flags are
needed. It tries, in order:

- browser: `--chrome` / `$CHORES_CHROME`, then the managed Claude Code image's
  `/opt/pw-browsers/chromium-*/chrome-linux/chrome`, then
  `/usr/bin/google-chrome`, `/usr/bin/chromium`, `/usr/bin/chromium-browser`
- library: `--playwright` / `$CHORES_PLAYWRIGHT`, then the managed image's
  `/opt/node22/lib/node_modules/playwright`, then a private copy in
  `~/.cache/chores-verify-ui`, then whatever `require()` finds

Don't run `playwright install` in the managed image. Anywhere else, if
neither library is found, it prints the one-time install command (no browser
download, it drives the system Chrome):

```bash
npm install --prefix ~/.cache/chores-verify-ui playwright-core
```

`seed-demo-data.js` needs neither — it's plain `fetch()`, no browser involved.

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

Unlike `screenshot.js`, this one doesn't touch Chromium or Playwright at
all — it's plain `fetch()` calls straight against the Connect API. Logging in
still needs a session cookie (everything's gated behind one — see
internal/auth/auth.go), but devauth's identity picker is plain HTML with an
`identity=` query param shortcut, so the whole handshake is three GETs with
their redirects followed by hand, no browser required. It reuses whatever
family the login already has if one exists, and prints the created ids as
JSON. Flags: `--family`, `--children` (comma-separated), `--task`,
`--price-cents`, `--url`. Completion dates are computed relative to *today*,
not hardcoded, so the year-boundary behavior stays exercised no matter when
this runs.

## Capture

```bash
node .claude/skills/verify-ui/screenshot.js            # --theme hacker, --lang nb, --out, --url
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

**Don't write a browser script from scratch.** `lib.js` already does the
launch, the devauth login, theme/language presets, error collection
(page errors, console errors, 4xx/5xx, and any third-party request) and
element screenshots, so a one-off check is a few lines:

```js
const { open } = require("/abs/path/to/.claude/skills/verify-ui/lib.js");
(async () => {
  const s = await open({ theme: "hacker", lang: "nb" });   // also device, viewport, colorScheme, path
  await s.login();                                         // devauth "Test Parent"
  await s.tab("tasks");
  await s.page.click(".fab");
  await s.page.fill("#task-icon-search", "trash");
  await s.shot("icon-search", "#task-icon-results");      // element; omit the selector for the page
  await s.done();                                          // prints errors, sets exit code
})();
```

Write it to `/tmp` and run it with `node`. For a second viewport on the same
login, pass `storageState: await s.storageState()` to another `open()`.
`screenshot.js` is the longer example.

## Putting screenshots in a PR

```bash
.claude/skills/verify-ui/pr-shots.sh <topic> /tmp/shots/a.png /tmp/shots/b.png
```

pushes them to a `pr-assets/<topic>` orphan branch (adding to it if it
exists) and prints the `![](…)` lines to paste into the PR body.

## Regenerating the welcome page screenshots

`web/screenshots/` holds the shots the logged-out welcome page shows
(`today-en.webp`, `today-nb.webp`, …). They're real captures, so regenerate
them whenever Today, Tasks or Balance changes:

```bash
scripts/dev-up.sh --fresh
node .claude/skills/verify-ui/welcome-shots.js
```

It seeds one demo family per language (task titles are data, not UI
strings), captures at 390x720 and 2x, re-encodes to WebP through Chromium's
canvas (there's no image tooling in the sandbox), and deletes each family
again. Edit the `LANGS` table in it to change what's shown, then read the
files back before committing.

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
