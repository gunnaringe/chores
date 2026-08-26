# Chores — notes for coding agents

Start with `README.md`. It covers what the app does, config layering, Auth0
setup, the kiosk dashboard, and the project layout.

If you change the UI, check whether the README's description of it is still
true and update it in the same PR — its feature list is prose, so nothing
catches it going stale.

This file is only the delta: the things that have actually cost someone time
in this repo and that reading the code doesn't make obvious.

## Toolchain

**`mise` is not installed in agent sandboxes, and every Makefile target shells
through `mise exec --`.** So `make run`, `make dev` and `make devauth` all fail
with `mise: not found` (exit 2). Run the underlying `go run` commands directly
— see the `run-local` skill.

`go` is on `PATH`. `buf` is not: `gen/` is checked in, so you only need buf if
you edit `proto/chores/v1/chores.proto`.

## Verifying a change

```bash
go build ./...
go vet ./...
gofmt -l . | grep -v '^gen/'    # should print nothing
go test ./...
node --check web/app.js && node --check web/i18n.js && node --check web/sw.js
```

There is no linter beyond `go vet` and `gofmt`, no JS build step, and no
frontend test runner. `node --check` catches syntax errors and nothing else,
so **any frontend change needs to be exercised in a real browser** — see the
`verify-ui` skill. Rendering bugs here are invisible to the Go test suite.

## The frontend

`web/` is vanilla HTML/CSS/JS with no framework and no build step. `web/app.js`
is a single file: a `state` object plus `render()`, which tears down and
rebuilds the entire DOM on every change.

**Assets are compiled into the binary** via `//go:embed` in `web/assets.go`.
Editing anything under `web/` has no effect on an already-running server —
you must restart it. This is the most common way to make a correct change,
see no difference, and conclude the change was wrong.

**`el()` returns only the first element.** It builds a `<template>` and returns
`firstElementChild`, so passing it sibling nodes silently drops everything
after the first:

```js
el(`<h1>Title</h1><p>Subtitle</p>`)   // the <p> is discarded, no error
el(`<div><h1>Title</h1><p>Subtitle</p></div>`)  // correct
```

This has caused real bugs twice. Wrap multi-element markup.

**Everything user-supplied goes through `escapeHtml()`.** The whole UI is built
from template strings, so an un-escaped name or task title is an XSS hole.
Material Symbols are the one deliberate exception-ish case: they render as
ligature *text content*, so `escapeHtml` already makes them safe, and
`materialIconName()` additionally whitelists the character set.

**A full re-render steals focus** from whatever the user is typing in. Two
existing mechanisms handle this, and new code should reuse them rather than
reinvent: `rerenderPreservingFocus()` (restores focus and selection by element
id — this is what makes the History search box usable) and `isEditingSomething()`
(makes the background auto-refresh skip a tick while a field has focus).

**Transient UI state lives in module-level `let`s, not in `state`.** Which row
is expanded, which delete is awaiting confirmation, which task sheet is open —
these are deliberately not persisted or reacted to elsewhere.
`resetTransientUiState()` clears them on navigation; add new ones there too.

**Bump `CACHE_NAME` in `web/sw.js`** whenever you change a precached shell asset
(`app.js`, `app.css`, `i18n.js`, `login.html`, the manifest, the icons). The
service worker is cache-first, so without a bump returning users get one stale
load of the old UI.

## Data conventions

- **Money is integer cents everywhere** (`priceCents`, `amountCents`,
  `balanceCents`). `money()` formats for display. Never store or compare
  floats.
- **Dates are `"YYYY-MM-DD"` strings.** Never `new Date(dateStr)` — that parses
  as UTC midnight and renders as the *previous day* in any timezone behind UTC.
  Build from the Y/M/D components instead; `formatDateStr()`, `dayBeforeStr()`
  and `mondayOfWeekStr()` all do this and exist for exactly this reason.
- **Cron day-of-week is the standard 0=Sunday..6=Saturday**, which is what
  `internal/scheduling` expects. The UI displays Monday-first purely as
  presentation — `DOW()` reorders the display without touching the stored
  numbering.
- An occurrence has no id of its own; `occurrenceKey()` (task + child + date)
  is its identity, mirroring the server's `completionKey`.

## i18n

Every user-facing string is a `t("some.key")` lookup resolved in `web/i18n.js`,
which holds two complete blocks: `en` and `nb` (Norwegian Bokmål).

**Add every new key to both blocks.** A key missing from `nb` silently falls
back to English rather than erroring, so a half-translated UI looks fine in
testing and ships broken. The app's own name, "Chores", is a brand and stays
untranslated.

## CSS

`web/app.css` is a mobile-first app shell — sticky app bar, bottom tab bar,
scrolling content between them — that widens into a centred desktop page above
760px. A few rules worth knowing before editing it:

- **Use the design tokens** (`--bg`, `--surface`, `--text`, `--muted`,
  `--border`, `--accent`, `--r-lg`, `--shadow-sm`, …). Every token has a dark
  variant under `prefers-color-scheme: dark`; a hardcoded hex will look wrong
  in one of the two themes.
- **Form text must stay at 16px.** Below that, iOS Safari zooms the viewport
  when a field is focused, which reads as the page lurching sideways mid-typing.
- **Tap targets are 44px minimum.**
- **Any fixed-size icon container must set `overflow: hidden`.** Material
  Symbols render as ligature text, so until the font loads (or if it never
  does) each glyph is its literal name — `dishwasher_gen` etc. — which will
  overflow its box and intercept clicks meant for its neighbours.
- Respect `env(safe-area-inset-*)` on anything pinned to a screen edge.

## Pull requests

**A PR that changes anything visible includes screenshots of the change.**
Reviewers can't run the branch, and a UI diff is unreadable as text — a
screenshot is the only way the change is actually reviewable. Capture them
with the `verify-ui` skill, at phone width, and include:

- the screen you changed, before and after where the difference is subtle;
- both languages if you touched or added any user-facing string, since `nb`
  is the one that silently falls back;
- dark mode or a 320px screen when the change involves layout.

GitHub has no API for attaching an image to a PR body, so push the PNGs to a
throwaway orphan branch and embed them by raw URL — that keeps binaries out
of `main` and out of the PR's own diff:

```bash
git worktree add --detach /tmp/prshots
cd /tmp/prshots
git checkout --orphan pr-assets/<topic>
git rm -rf . >/dev/null
cp /tmp/shots/*.png .
git add . && git commit -m "Screenshots for <topic>"
git push -u origin pr-assets/<topic>
cd - && git worktree remove /tmp/prshots
```

Then reference them as
`![](https://raw.githubusercontent.com/<owner>/<repo>/pr-assets/<topic>/<file>.png)`
and say in the PR that the branch can be deleted once merged.

## House style

Comments here explain *why*, not *what* — usually the constraint or the bug
that forced the code into its current shape. Match that: a comment restating
the code is noise, but the reason a value is what it is, is worth keeping.
