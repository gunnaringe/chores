<div align="center">

# Chores

**A family allowance and chore tracker.**

A simple family chore and allowance app

Choose between running yourself or use my hosted version for free. \
★ [link to self host] ★ \
★ [chores.apphub.casa](https://chores.apphub.casa) ★ \
★ [ukepenger.apphub.casa](https://ukepenger.apphub.casa) ★


Everything is running as a single Go binary with a SQLite database.

Available in English, Norwegian (Bokmål and Nynorsk), and Swedish

Dashboard for showing and ticking of tasks on a wall display

Installable as a Progressive Webapp (looks and feels like a app)

gRPC and REST-ish API through Buf Connect [SDK](https://buf.build/apphub/chores)

Home Assistant app (coming soon)

<images of the app here>

</div>

<div>
  The easiest way to use this is by using my hosted version, you are welcome to use that one.
  No payment required. If you prefer, you can of course run it yourself instead.  
</div>

---

After I made the app, I also found these ones that could be worth to look at:
- https://github.com/donetick/donetick
- https://github.com/liftedkilt/openchore
- https://github.com/ccpk1/choreops
- https://github.com/caspii/dinkydash

# Features

# Quick Start

## Running the app
< setup publishing the go binary in github >

## Container
< setup a container build in github >

## Build locally
Use mise for managing dependencies (list them here in addition)


# Contributing
Tell people to create issues

# Licence

# Support Chores
Issues and PRs are welcome! Consider leaving us a star or tell your neighbour about the project if you like it.

-- Claude -> The below here should be rewritten --


 Single Go binary, embedded web UI,
SQLite storage, Buf-generated Connect API.

**The easiest way to use this is the author's own instance at
[chores.apphub.casa](https://chores.apphub.casa)** (also reachable at
[ukepenger.apphub.casa](https://ukepenger.apphub.casa)) — anyone's welcome
to log in and set up their own family there; nothing about it is limited to
the author. It's still a personal hobby project run for free in spare time
(see the disclaimer on its welcome page), so treat it accordingly: no
uptime promise, and [GitHub issues](https://github.com/gunnaringe/chores/issues)
is the only support channel. See "Hosting" further down for how it's
deployed.

Prefer your own copy — your own data, your own uptime — instead of the
shared instance? See "Running" below. The code is
[Apache-2.0 licensed](LICENSE), so forking and self-hosting it is
explicitly fine.

(The Go module is `github.com/gunnaringe/chores`, and the proto package,
Connect service (`chores.v1.ChoresService`), and generated code all match.)

Google's Material Symbols font is the one external dependency the app
pulls in at runtime, from Google Fonts — everything else, including the
icon *name* list used to search it (`web/material-symbols.json`, vendored
from [`@material-symbols/metadata`](https://www.npmjs.com/package/@material-symbols/metadata),
a mirror of Google's own Material Symbols codepoints), is embedded in the
binary. That's an acceptable tradeoff here since the app already needs a
live connection to its own backend for essentially everything; there's no
offline mode to preserve.

## Contents

- [Running](#running)
- [Features](#features)
  - [Tasks](#tasks)
  - [Occurrence history and retention](#occurrence-history-and-retention)
  - [Repeat rules](#repeat-rules)
  - [The parent view](#the-parent-view)
  - [The child view](#the-child-view)
  - [Balance and payouts](#balance-and-payouts)
  - [Multiple families](#multiple-families)
  - [Auto-refresh and the Settings page](#auto-refresh-and-the-settings-page)
  - [The welcome page](#the-welcome-page)
  - [Push notifications](#push-notifications)
- [Hosting](#hosting)
- [Language](#language)
- [Theme](#theme)
- [Authentication](#authentication)
  - [Local testing without a real Auth0 tenant](#local-testing-without-a-real-auth0-tenant)
  - [Setting up Auth0](#setting-up-auth0)
- [Family membership](#family-membership)
  - [Creating or joining another family](#creating-or-joining-another-family)
  - [The family members list](#the-family-members-list)
  - [Inviting a family member](#inviting-a-family-member)
  - [Leaving, removing a child, or deleting the family](#leaving-removing-a-child-or-deleting-the-family)
- [Kiosk dashboard](#kiosk-dashboard)
- [External API access](#external-api-access)
- [Installing as an app (PWA)](#installing-as-an-app-pwa)
- [Deploying a schema change](#deploying-a-schema-change)
- [Regenerating the Connect/protobuf code](#regenerating-the-connectprotobuf-code)
- [Continuous integration](#continuous-integration)
- [Updating the Material Symbols icon list](#updating-the-material-symbols-icon-list)
- [Working on this with a coding agent](#working-on-this-with-a-coding-agent)
- [Project layout](#project-layout)

## Running

To self-host instead of using the hosted instance mentioned above, clone
this repo and run:

```bash
go run ./cmd/chores -addr=:8080 -db=chores.db
```

Then open http://localhost:8080. A login is always required (see
Authentication below) — for local testing without a real Auth0 tenant, run
`cmd/devauth` alongside it instead.

(If you have an existing database from before the rename, pass
`-db=ukelonn.db` to keep using it, or just rename the file — the schema
itself didn't change.)

Config is layered with [koanf](https://github.com/knadh/koanf), lowest to
highest priority: an optional `.env` file in the working directory, real
environment variables, then CLI flags on top. Each layer only overrides a
value the one below it actually set — an unset flag or absent env var
never clobbers what an earlier layer provided (see `loadConfig` in
`cmd/chores/main.go`). The `.env` file is entirely optional and
git-ignored; only the three `AUTH0_*` keys are read from it:

```
AUTH0_DOMAIN=your-tenant.eu.auth0.com
AUTH0_CLIENT_ID=...
AUTH0_CLIENT_SECRET=...
```

## Features

The UI is built mobile-first, as an app shell rather than a page that
merely reflows: a sticky app bar, a tab bar pinned to the bottom edge under
the thumb, and the screen scrolling between them, with safe-area padding so
nothing hides behind a notch or home indicator. Above 760px that same tab
bar becomes a row of pills under the header and the whole thing reads as an
ordinary centred desktop page.

Within a screen the layout stays fluid rather than snapping at hardcoded
widths: a list row's title and its action buttons share one flex line and
drop to their own line only once they no longer both fit, so the same rule
adapts from a phone up through a desktop window. Fields that are
deliberately narrow on desktop (price, cron expression, etc.) go full-width
below 520px, where that space would otherwise sit unused.

### Tasks

Parents create tasks with a price, an assignment to one or more children
(with a "select all" shortcut), and an optional icon shown next to the
title everywhere the task appears — a
[Material Symbols](https://fonts.google.com/icons) icon, picked by typing
a search term (matched against the official icon names, underscores
treated as spaces — no separate keyword/synonym list) and clicking one of
the matches. A task predating Material Symbols becoming the only
supported icon type (an emoji or a Font Awesome icon) keeps showing its
old icon as plain text rather than losing it, but can't be re-picked
through this UI — editing such a task starts with no icon selected. A
task only shows up for the children it's assigned to, can be edited in
place at any time, and can be paused (and later resumed) instead of
deleted. Cloning opens the same create-task sheet pre-filled from an
existing task, title suffixed `(copy)` — or `(copy 2)`, `(copy 3)`, etc.
if that title is already taken — as a starting point for a similar task
rather than building one from scratch.

Editing or deleting a task never rewrites what a child earned. An
occurrence records the amount it was worth at the moment it was recorded,
and nothing else — so repricing a chore changes what it pays from now on
and leaves last month's earnings alone. Its title and icon are read live
from the task, so correcting a name fixes it everywhere rather than
leaving older entries under the old one; the same treatment a child's own
name gets. Deleting is a soft delete: the task disappears from the Tasks
tab and stops coming due, but every occurrence it produced stays, and so
do the earnings behind them — a child's balance is never moved by
deleting a task. The hidden row is reclaimed once its occurrences have
aged out and nothing can still need its title. (Removing a *child* still
does take their history with them; see Family membership below.)

### Occurrence history and retention

Occurrence history is kept for a rolling **62 days**, which always covers
the current month plus the whole of the previous one — the widest that
span ever gets is 61 days (31 January back to 1 December). Expressed as a
rolling window rather than calendar months so history ages out a day at a
time instead of a month disappearing on the 1st. Balances are *not*
affected: when occurrences age out, their earnings are carried into a
per-child ledger in the same transaction that deletes them, so what a
child has earned and is owed stays exact forever. What's lost beyond the
window is the itemisation — which chores made up the total — not the
total. Payouts are kept indefinitely.

### Repeat rules

A task's repeat rule is one of three modes: **does not repeat** (due once,
on a specific date, then never again), **weekly** (day-of-week checkboxes,
shown Monday-first, plus "every N weeks" — 1 for every week, higher for
every other week and beyond, counted from the date the task was created),
or **cron** (a raw 5-field cron expression, e.g. `0 0 1 * *` for the 1st of
every month) for anything the other two can't express.

### The parent view

A parent gets five tabs, in this order: **Today**, **History**, **Tasks**,
**Balance**, **Settings**. Today is a daily dashboard: for each child, today's tasks and
their completion status, what they've earned today, and their outstanding
balance — all at a glance. History is a browsable log of every completion,
grouped into Today / Yesterday / Earlier this week / Later — the "Later"
group loads a page at a time as you ask for more, rather than pulling a
whole retained window up front — plus a search box that matches by task
title or child name across that window. Every entry there can be
toggled between completed and not completed via a two-step inline confirm
— no browser popup, just a confirm button that appears in place of the
toggle itself. That's how a completion logged for the wrong child gets
undone (it stays visible as a missed task rather than vanishing), and
equally how one missed at the time gets backfilled after the fact. Tasks
manages task definitions, with the editor opening as a sheet over the list
rather than a form beneath it. Balance handles payouts and balance
history. The app bar's user name is itself a dropdown for
switching to a specific child's own restricted view, mainly useful for
previewing what a kid sees, or for a parent marking a chore done on
behalf of a child who doesn't have their own login — that dropdown only
ever offers children and yourself, never another parent, so one parent's
login can't casually end up "being" a co-parent.

### The child view

A child gets two tabs, **Today** and **Settings**, and Today is the whole
app for them: their own checklist, with what they've earned today, this
week, and their current balance shown right above it. There's no separate
balance/payout page or family-member list for a child to get lost in —
those are parent-only concerns, tucked into the Balance tab and the
parent's own Settings page respectively.

### Balance and payouts

Balance tracks earnings in the last 7 days and the outstanding balance
(total earned minus total paid out), plus — per child — what they've
earned this calendar month and last, with an expandable month-by-month
table underneath going back further. That table stays available past the
62-day retention window: the retention purge compacts each month's total
into `child_monthly_earnings` before deleting the occurrence rows it was
summed from, the same way it carries a purged balance into `child_ledger`.

Parents can pay out the full balance or a partial amount.

### Multiple families

A login — parent or child — can belong to more than one family at once
(e.g. a child who splits time between two households, or a parent
co-running two, each running its own independent chores/allowance). The
family name in the top bar becomes a dropdown once there's more than one
to switch between; creating another family or joining one with an invite
code, whether or not you already belong to one, is done from the Settings
page (see Family membership below).

### Auto-refresh and the Settings page

The page auto-refreshes every 5 minutes (so a completion made from another
device or family member's session shows up without a manual reload),
pausing automatically while a form on the page has focus so it never wipes
out something you're mid-typing. This isn't configurable — there's no real
downside to it, so it's just always on. Web Push notifications (see below),
renaming the family, and — for a parent — managing family members and
invitations (see Family membership below), all live on the Settings page,
since none of them are things you look at often enough to deserve their
own always-visible tab.

### The welcome page

Logged out, `/` serves a welcome page rather than a bare login box: what the
app is, a three-step walkthrough, screenshots of the real screens (in
`web/screenshots/` — English and Bokmål each have their own set; other
languages fall back to English), and a footer pointing at the
source and where it runs. The Log in button stays at the top, above the
fold, exactly where it was before — someone who already uses the app should
never have to read or scroll past any of it. A quiet second link below it,
"Have a dashboard code?", goes straight to `/dashboard`'s key prompt (see
Kiosk dashboard below) for the kitchen-tablet crowd who were never going to
log in at all.

### Push notifications

Push notifications use the standard Web Push API: the server generates a
VAPID keypair on first run (stored in the `app_settings` table, stable for
the life of the database) and signs every push with it, so no external push
service account or API key is needed beyond what's already built into the
browser. Enabling notifications from the Settings page subscribes the
current browser via `PushManager` and registers that subscription with the
server; completing a task then pushes a notification to every *other*
subscribed device in the family (not the one that just completed it) with
the child's name and the task title. A subscription the push service
reports as gone (the browser unsubscribed, or it expired) is cleaned up
automatically on the next send attempt.

This needs a real service worker, which requires either `https://` or
`http://localhost` — it won't work over a plain HTTP LAN address.

A login is bound to a specific family member: the first parent to log in
creates the family and is bound to it automatically,
and anyone else — another parent, or a child old enough to have their own
account — joins via a one-time invite link. Once bound, a login only ever
sees its own family, and the server enforces role restrictions regardless
of what the UI shows: only parents can manage tasks, family members, and
payouts, and a bound child's login can only ever act on their own tasks and
accounting, never a sibling's. Children don't have to log in individually,
though — a bound parent's session can still act as any unbound child in
their family via the in-app picker (handing the device to a kid to mark a
chore done, for example).

## Hosting

None of this is required to run the app — `fly.toml` and the deploy setup
are specific to the author's own instance, kept here for reference rather
than as something this repo expects you to reuse. That instance is a single
Fly.io machine (`primary_region = "arn"`, i.e. Stockholm) with its SQLite
database on a persistent volume, reachable through Cloudflare DNS (DNS only
— Fly's own edge terminates TLS, Cloudflare doesn't proxy the traffic),
with login
handled by Auth0's EU region (see Authentication below). The welcome page
states the same three facts for anyone visiting the running app, not just
anyone reading this file.

## Language

The UI is available in English, Norwegian Bokmål, Norwegian Nynorsk, and
Swedish, picked with a dropdown on the welcome page and, once logged in, on
the Settings page. It defaults to the browser's language when there's no
saved preference, and the choice is then remembered in `localStorage`.
Translation strings live in `web/i18n.js`; add a new language by adding
another entry to `TRANSLATIONS` there and to `window.LANGUAGES`. Error
messages coming from the server (validation errors, permission errors)
aren't localized yet — only the UI text is.

The app's own name is part of that: it's **Chores** in English,
**Ukepenger** in Bokmål, **Vekepengar** in Nynorsk, and **Veckopeng** in
Swedish, including the name an installed PWA takes (see Installing as an
app below). Because the manifest is JSON served by Go while the UI is JS,
those names are declared twice — `app.name` in `web/i18n.js` and `appNames`
in `web/manifest.go` — and have to be kept in step.

Only English and Bokmål have their own welcome-page screenshots — Nynorsk
and Swedish fall back to the English set (see The welcome page above).

## Theme

Settings offers a choice of **Match device** (the default — follows the
OS's light/dark setting, which is all the app did before this option
existed), **Light**, **Dark**, **Retro**, **Playful**, **Hacker**, **Space
Invaders**, **High Contrast**, **Fairytale Forest**, **Pirate Map**,
**Underwater World**, **Robot Workshop**, **Cabin & Outdoors**, **Monster
Lab**, **Dinosaur Park**, **Mission Base**, **Block World**, and **Cozy
Home**. Like the language and currency choices, it's purely a
`localStorage` preference on that device and never reaches the server.
Each theme is a block of CSS custom properties in `web/app.css`
(`:root[data-theme="..."]`); add one there and an entry in `THEME_OPTIONS`
in `web/app.js` to add another.

Most of the newer themes also set `--bg-pattern`, a repeating decorative
background tile drawn as an inline SVG data URI rather than a separate
asset file — consistent with everything else here being embedded at build
time rather than fetched. Each one is an original, generic shape (a pine
tree, a compass rose, a fish, a gear, a footprint, a radar sweep, a plain
building block, and so on) chosen specifically to avoid any resemblance to
a real character, franchise, or brand. It only shows in the gaps between
cards and faintly through the translucent app bar/tab bar, never behind
text, since cards stay opaque. High Contrast, Cabin & Outdoors, and Cozy
Home deliberately set none — the first for OS-agnostic
black-on-white-with-black-borders legibility, the other two because
they're meant to read as the calmest, most adult-friendly themes on offer.

## Authentication

A login is always required — `AUTH0_DOMAIN`, `AUTH0_CLIENT_ID` and
`AUTH0_CLIENT_SECRET` (via `.env`, environment variables, or the
`-auth0-*` flags) all have to be set, or `chores` refuses to start. There's
no way to run the app open, unauthenticated.

### Local testing without a real Auth0 tenant

`cmd/devauth` is a tiny, self-contained OAuth2 identity provider (stdlib
Go, no external dependencies) that mimics Auth0's specific endpoint shape
closely enough that `AUTH0_DOMAIN` can just point at it. It exists purely
to exercise the real login code path in local dev/tests without needing a
real tenant — there's no real login UI, just a choice of which canned test
identity to log in as. By default it offers two, a test parent and a test
child, since testing an invite (a parent inviting a child, then the child
logging in with their *own* separate identity to accept it) needs two
distinct logins, not one. Run it alongside `chores`:

```bash
go run ./cmd/devauth -client-id=devclient -client-secret=devsecret
```

```bash
AUTH0_DOMAIN=http://localhost:9999 AUTH0_CLIENT_ID=devclient \
  AUTH0_CLIENT_SECRET=devsecret go run ./cmd/chores
```

(`devauth` prints this exact pair of commands, with its actual flags
substituted in, on startup.) Clicking "Log in" now lands on a plain page
listing the available identities — pick one to continue. Pass one or more
`-identity="sub|name|email"` flags to replace the two defaults with your
own set (a single `-identity` skips the picker entirely, going straight to
that one identity, same as the old single-identity behavior).

### Setting up Auth0

1. In the Auth0 dashboard, create a **Regular Web Application** (not SPA —
   the token exchange happens server-side).
2. Set **Allowed Callback URLs** to every hostname you'll actually log in
   through, e.g. `http://localhost:8080/auth/callback` for local testing
   against a real tenant, plus your production URL(s).
3. Set **Allowed Logout URLs** the same way, e.g. `http://localhost:8080/`
   plus your production URL(s).
4. Run the app with:

   ```bash
   export AUTH0_DOMAIN=your-tenant.eu.auth0.com
   export AUTH0_CLIENT_ID=...
   export AUTH0_CLIENT_SECRET=...
   go run ./cmd/chores
   ```

   (or pass the equivalent `-auth0-domain`, `-auth0-client-id`,
   `-auth0-client-secret` flags).

Login uses the standard OAuth2 Authorization Code flow: `/auth/login`
redirects to Auth0, `/auth/callback` exchanges the code and fetches the
profile from Auth0's `/userinfo` endpoint, and a session is kept in-memory
(no JWT verification needed since tokens never leave the server). Sessions
don't survive a server restart. `/auth/logout` clears the session and signs
out of Auth0 too.

The `redirect_uri` sent to Auth0 is derived from the incoming request's
own hostname rather than a fixed config value, so the same deployment
works behind any domain pointed at it (a raw `*.fly.dev` URL, a custom
domain, `localhost`, whatever) with no extra configuration — Auth0's
Allowed Callback URLs list (step 2 above) is what actually decides which
hostnames are allowed to complete a login; the app doesn't add any
restriction of its own on top of that.

## Family membership

### Creating or joining another family

"Create a new family" and "Join a family" sit at the very top of the
Settings page, above a divider that separates them from everything else
there — unlike the rest of Settings, neither is about the family currently
open, so both are available to a parent or a child alike. Creating one
founds a brand-new family with you as its first parent, the same as the
very first family you ever created or joined. Joining takes an invite code
someone shared with you (see "Inviting a family member" below) to become a
member of their family too. A login can found or accept invites into any
number of families; the only thing rejected is accepting the same family's
invite twice with a login already bound there. That's what makes both the
"lives in two households" case and a parent co-running two families work.

### The family members list

Below that divider, the rest of Settings is scoped to whichever family is
currently open — including its members list, which shows every member as a
collapsed row (parents first, then children, alphabetically within each
group — the same ordering the topbar's user-switcher dropdown uses).
Tapping a row expands it to show what can be done with it, which depends on
whose row it is:

- **Your own row** — rename yourself, and (parents only) leave the family.
- **A child's row** — remove them, which cascades away their task
  assignments, completion history, and payout history along with them.
- **A co-parent's row** — nothing. It's shown so you know who's in the
  family, but there's nothing to manage from someone else's row.

One more row, **"+ Add a family member"**, sits at the bottom of the same
list, expanding the same way: add someone directly, with no login of their
own (the usual way to add a child too young to have an account); or send
them an invite instead (see below).

### Inviting a family member

Sending an invite (from the "+ Add a family member" row) creates a
one-time invite code for another parent (e.g. the other guardian) or for
a child old enough to have their own Auth0 account.
Whoever enters that code in their own "Join a family" section — after
logging into their own Auth0 account — is bound to that slot in the same
family, with the role the invite was created for. The code is shown once,
right where it was created, next to a QR-code button that expands a
scannable code for the invite link — handing a phone the link beats reading
a code aloud; a parent can revoke an invite before it's accepted (from the
"Pending invitations" list), which also removes the unclaimed slot it
created. Accepting an invite isn't restricted to any particular email
address — possession of the code is what grants access, so only share it
with the intended person.

### Leaving, removing a child, or deleting the family

The same Family section has the other side of membership: a parent can
leave the family (only ever themselves — never on a co-parent's behalf),
remove a child (which cascades away their task assignments, completion
history, and payout history along with them), or delete the family outright
(which cascades away everything in it — every user, task, completion,
payout, invitation, and push subscription). A family always keeps at least
one parent: leaving is refused for the last one, who has to delete the
family instead if they want to be rid of it. None of these take effect on
a single click — each opens an inline "type a word to confirm" prompt
(`remove` / `leave` / `delete`) with its own Cancel, on the theory that
losing a family member's history, your own membership, or a whole family is
much harder to walk back than most things a confirm button guards.

## Kiosk dashboard

A parent can turn a shared or wall-mounted device (a tablet on the fridge,
say) into a read/complete-only view of the family's Today tab, without it
ever going through login. From the Settings page's Dashboard section, "Set
up dashboard" generates a per-family secret key and a URL
(`/dashboard?key=...`); opening that URL on the kiosk device unlocks it
immediately, and the key is then remembered in that browser's
`localStorage` so it survives reloads without needing the query string
again (which is stripped from the address bar right after first use).
Without a `?key=`, `/dashboard` instead prompts for the key to be typed in
(pasting the whole URL there works too — it pulls the key back out of it).
The URL also has its own QR-code button for scanning it straight onto the
kiosk device, generated client-side by a vendored library (`web/qrcode.js`)
rather than a hosted QR API, since the URL it encodes is a bearer
credential.

A dashboard key is a bearer credential scoped to exactly one family and to
four actions: list children's daily status, list today's task occurrences,
and complete/uncomplete a task. It cannot see or touch anything else —
family membership, task definitions, payouts, other families' data, and
every other RPC in the API reject a dashboard-only request the same way
they'd reject an anonymous one. "Regenerate key" invalidates the old key
immediately (any device still using it falls back to the key prompt) and
"Disable dashboard" turns the feature off until set up again.

The kiosk screen has its own small Settings page too (a gear icon in its
app bar, separate from the family's own Settings) — theme and language,
same device-level preferences as the main app; which children it shows at
all; and whether it shows every included child at once (the default, as
above) or one at a time with a switcher in place of that child's name, for
a screen too small to show everyone. All three are stored in that
browser's `localStorage`, same as the dashboard key itself, so they're
per-device and never touch the server.

## External API access

Two ways exist to call the Connect API from outside the browser, and they're
deliberately different shapes for deliberately different jobs.

**Personal access tokens** (Settings → API access) authenticate as a login
identity — the same access that identity's own browser session has, in
every family it belongs to, unrestricted by role. "Create token" returns
the raw bearer value exactly once; it's never shown or recoverable again
(only its SHA-256 hash is stored), so losing it means revoking it and
creating another. Send it as `Authorization: Bearer chorespat_...`; every
RPC accepts it exactly where it would accept a session cookie. A token
isn't deleted when the login it belongs to is later removed from every
family it was bound to — it simply stops being useful, the same as a
lingering session cookie for that identity would, since every permission
check still runs normally against whatever (if anything) that identity is
still bound to.

**gRPC server reflection** (`connectrpc.com/grpcreflect`, wired in
`cmd/chores/main.go`) lets a schema-aware client — `grpcurl`, `grpcui`,
Postman's gRPC mode — discover the API's methods and message shapes without
`chores.proto` handed to it separately:

```bash
grpcurl -plaintext -H "Authorization: Bearer chorespat_..." localhost:8080 list
grpcurl -plaintext -H "Authorization: Bearer chorespat_..." \
  -d '{}' localhost:8080 chores.v1.ChoresService/ListFamilies
```

It sits behind the same login gate as everything else (a personal access
token or a session cookie — never a dashboard key, since the kiosk's method
allowlist doesn't cover reflection's own RPC paths). Reflection's RPCs are
bidirectional streams, which plain HTTP/1.1 can't carry; the server enables
cleartext HTTP/2 (`http.Server.Protocols.SetUnencryptedHTTP2`) alongside
HTTP/1.1 on the same port so this works without TLS in local dev, while
every ordinary HTTP/1.1 caller — the web UI, curl, a reverse proxy that
doesn't itself speak h2c to the origin — is unaffected.

Reaching it through a reverse proxy needs every leg of the connection to
actually carry HTTP/2 through, not just terminate TLS with it — a proxy
that negotiates h2 with the client but falls back to HTTP/1.1 for its own
connection to the origin will deliver an HTTP/2 request onto a connection
that can't parse it, which surfaces as a blunt "505 HTTP Version Not
Supported" rather than anything naming the real cause. On the author's own
Fly.io instance (see Hosting above — Cloudflare there is DNS only, so
fly-proxy is the only other hop) this needed two settings in `fly.toml`'s
`[http_service]`, one per leg: `tls_options.alpn = ["h2", "http/1.1"]` so
fly-proxy's edge offers h2 to external clients at all (otherwise ALPN falls
back to http/1.1 regardless of what the app supports), and
`http_options.h2_backend = true` so fly-proxy in turn forwards to this app
over h2c instead of always using HTTP/1.1.

## Installing as an app (PWA)

The web UI is an installable Progressive Web App: `web/manifest.webmanifest`
declares its icons and standalone display mode, and `web/sw.js`
precaches the static app shell (HTML/CSS/JS/icons only — never API
responses or login state, so nothing about family data or sessions is ever
cached) so the shell keeps loading offline. Both the app and the login page
register the service worker, so an install prompt can appear before or
after logging in. The manifest is served by a handler rather than as a
static file (`web/manifest.go`), so the installed app's name follows the
chosen language — the page rewrites the `<link rel="manifest">` href to
carry it, and the service worker deliberately never caches the manifest, so
a Norwegian install doesn't get handed the English name. On a phone, use the browser's "Add to Home Screen" /
"Install app" option; on desktop Chrome/Edge, an install icon appears in
the address bar. `web/sw.js` also handles incoming Web Push events (see
Push notifications above) and focuses or opens the app when a notification
is tapped.

## Deploying a schema change

The app migrates its own database on startup (`internal/db`), so a deploy
is the migration. The database holds money owed to children and the volume
has no rollback of its own, so a migration is worth checking rather than
assuming.

Take a snapshot first — Fly's automatic dailies don't exist yet on a
newly created volume:

```bash
fly volumes snapshots create <volume-id> --app <app>
```

Pull a copy, **including the `-wal` sidecar**. SQLite keeps recent writes
there, and a database copied without it can be missing nearly everything
while still opening cleanly:

```bash
fly ssh sftp get /data/chores.db ./prod-chores.db --app <app>
```

```bash
fly ssh sftp get /data/chores.db-wal ./prod-chores.db-wal --app <app>
```

Audit the copy, migrate it, and audit it again. `cmd/dbaudit` opens the
file read-only and understands both the pre- and post-migration layouts,
so the two runs are directly comparable:

```bash
go run ./cmd/dbaudit ./prod-chores.db
```

Every per-child balance it prints has to be identical after the migration.
Those are the figures a real family sees, and the one class of bug worth
this whole procedure is the kind that quietly moves them. Running the app
once against the copy performs the migration; audit it again and diff.

## Regenerating the Connect/protobuf code

After editing `proto/chores/v1/chores.proto`:

```bash
buf generate
```

The schema is also published to the [Buf Schema
Registry](https://buf.build/apphub/chores), for anything that wants to
depend on it without cloning this repo. `buf.yaml`'s `name:` points at that
module, and pushing happens automatically on merge to `main` (see
Continuous integration below) whenever `proto/`, `buf.yaml`, or
`buf.gen.yaml` changed — no manual step needed. To push by hand anyway
(e.g. to check a change against the BSR before opening a PR):

```bash
buf push
```

## Continuous integration

Three GitHub Actions workflows run on every PR into `main`:

- **CI** (`.github/workflows/ci.yml`) — `go build ./...` and
  `go test ./...`. Also runs on pushes to `main` as a safety net.
- **Buf** (`.github/workflows/buf.yml`) — lints and format-checks
  `proto/chores/v1/chores.proto` and runs breaking-change detection against
  the PR's base branch, via the single unified `bufbuild/buf-action`. On a
  push to `main` that touches the schema, the same workflow instead runs
  `buf push` to the BSR (see above) — that leg needs the `BUF_TOKEN`
  repository secret, a BSR API token.
- **pre-commit** (`.github/workflows/pre-commit.yml`) — runs
  [`prek`](https://github.com/j178/prek) (a fast, dependency-free
  reimplementation of `pre-commit`) against `.pre-commit-config.yaml`, so a
  PR fails the same way whether or not the author had it installed
  locally.

`.pre-commit-config.yaml` itself mirrors the "Verifying a change" checklist
in `CLAUDE.md` — `gofmt`, `go vet`, `buf lint`, `node --check` on the web
JS files — rather than introducing a linter this repo doesn't otherwise
use. Install it locally with `prek install` (git hook, runs on every
commit) or run it on demand with `prek run --all-files`.

## Updating the Material Symbols icon list

`web/material-symbols.json` (the icon names the task icon picker's search
runs against — see Tasks above) is a point-in-time
snapshot; Google adds new icons occasionally. Refresh it with:

```bash
make update-material-symbols
```

which regenerates the file via `scripts/update-material-symbols.sh` — see
the script's header comment for where the data comes from. Review the
diff and commit it like any other change.

## Working on this with a coding agent

`CLAUDE.md` holds the notes an agent needs that this README doesn't cover —
the `go:embed` restart rule, the frontend's footguns, and how to verify a
change. Two skills live in `.claude/skills/`: `run-local` (start the app
against the devauth test provider) and `verify-ui` (drive it in a browser and
screenshot it). A `SessionStart` hook in `.claude/hooks/` warms the Go caches.

`scripts/dev-up.sh` and `scripts/dev-down.sh` start and stop a local
devauth + app pair for manual testing — the same two processes `make dev`
runs, but as plain shell (no `mise`, which agent sandboxes don't have) and
with each process in its own process group, so stopping them actually
releases the port. `--fresh` wipes the dev database first.
`.claude/skills/verify-ui/seed-demo-data.js` then seeds a family, children,
a task, and completions spanning several months to test against — over plain
HTTP, so it needs Node but no browser. (`make test-data` runs the same thing,
for anyone who does have `mise`.)

## Project layout

- `proto/` — protobuf service/message definitions
- `gen/` — generated protobuf + Connect Go code (checked in, regenerate with `buf generate`)
- `internal/db` — SQLite schema and connection setup
- `internal/scheduling` — cron-expression date matching for recurring tasks
- `internal/server` — Connect service implementation, split by concern: `authz.go` (membership/role checks), `tasks.go`, `occurrences.go`, `completions.go`, `accounting.go`, `monthly.go` (per-child month-by-month earnings), `payouts.go`, `families.go`, `users.go`, `invitations.go`, `retention.go`, `convert.go` (API types <-> storage), plus `push.go` for VAPID key setup and Web Push sending, `dashboard.go` for the kiosk key, and `tokens.go` for personal access tokens
- `internal/auth` — the OAuth2/OIDC login gating the app
- `web/` — embedded static frontend (vanilla HTML/CSS/JS, calls the Connect API directly via JSON); `web/i18n.js` holds the English/Norwegian translation strings, and `web/qrcode.js` is a vendored client-side QR encoder (see Kiosk dashboard)
- `cmd/chores` — main entrypoint
- `cmd/devauth` — tiny local OAuth2 test identity provider (see Authentication)
- `cmd/dbaudit` — read-only report on a database: row counts, data hazards, and per-child balances. Used to check that a migration didn't move anything (see Deploying a schema change)
- `scripts/` — development helpers, not part of the built binary: `dev-up.sh`/`dev-down.sh` (run the app locally against devauth) and `update-material-symbols.sh` (refresh the icon name list)
