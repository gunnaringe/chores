# Chores

A family allowance and chore tracker. Single Go binary, embedded web UI,
SQLite storage, Buf-generated Connect API.

(The Go module is `github.com/gunnaringe/chores`, and the proto package,
Connect service (`chores.v1.ChoresService`), and generated code all match.)

Font Awesome and Google's Material Symbols are the two external
dependencies the app pulls in at runtime — Font Awesome from cdnjs (with a
Subresource Integrity hash pinned in `web/index.html`), Material Symbols
from Google Fonts — everything else is embedded in the binary. That's an
acceptable tradeoff here since the app already needs a live connection to
its own backend for essentially everything; there's no offline mode to
preserve.

The layout is fluid rather than tied to fixed device breakpoints: a list
row's title and its action buttons share one flex line and drop to their
own line only once they no longer both fit, so the same CSS rule adapts
correctly from a phone up through a desktop window instead of snapping at
a couple of hardcoded widths. Fields that are deliberately narrow on
desktop (price, cron expression, etc.) go full-width below 480px, where
that space would otherwise sit unused.

- Parents create tasks with a price, an assignment to one or more children
  (with a "select all" shortcut), and an optional icon shown next to the
  title everywhere the task appears — any emoji, a
  [Font Awesome](https://fontawesome.com) Free Solid icon, or a
  [Material Symbols](https://fonts.google.com/icons) icon, each with a row
  of quick-pick suggestions plus a free-text field. A task only shows up
  for the children it's assigned to, can be edited in place at any time,
  and can be paused (and later resumed) instead of deleted.
- A task's repeat rule is one of three modes: **does not repeat** (due once,
  on a specific date, then never again), **weekly** (day-of-week checkboxes,
  shown Monday-first, plus "every N weeks" — 1 for every week, higher for
  every other week and beyond, counted from the date the task was created),
  or **cron** (a raw 5-field cron expression, e.g. `0 0 1 * *` for the 1st of
  every month) for anything the other two can't express.
- A parent gets four tabs, in this order: **Today**, **History**, **Tasks**,
  **Balance**. Today is a daily dashboard: for each child, today's tasks and
  their completion status, what they've earned today, and their outstanding
  balance — all at a glance. History is a browsable log of every completion,
  grouped into Today / Yesterday / Earlier this week / Later — the "Later"
  group loads a page at a time as you ask for more, rather than pulling a
  family's entire history up front — plus a search box that matches by task
  title or child name across the whole history. Every entry there can be
  deleted (undoing that completion, e.g. one logged for the wrong child) via
  a two-step inline confirm — no browser popup, just a "Confirm delete"
  button that appears in place of the delete button itself. Tasks manages
  task definitions (add/edit/pause/delete). Balance handles payouts and
  balance history. Switching to a specific child's own restricted view (via
  "Switch user") is a separate action, mainly useful for previewing what a
  kid sees, or for a parent marking a chore done on behalf of a child who
  doesn't have their own login — that switcher only ever offers children and
  yourself, never another parent, so one parent's login can't casually end
  up "being" a co-parent.
- A child gets a single page, no tabs: their own checklist for today, with
  what they've earned today, this week, and their current balance shown
  right above it. There's no separate balance/payout page or family-member
  list for a child to get lost in — those are parent-only concerns, tucked
  into the Balance tab and the Settings page respectively.
- Balance tracks earnings in the last 7 days and the outstanding balance
  (total earned minus total paid out).
- Parents can pay out the full balance or a partial amount.
- A child can belong to more than one family at once (e.g. a child who
  splits time between two households, each running its own independent
  chores/allowance) — when a login is bound to more than one family, it's
  asked which household to open, with a "Switch household" control to
  change later. A parent's login, by contrast, is always scoped to exactly
  one family.
- The page auto-refreshes every 5 minutes (so a completion made from another
  device or family member's session shows up without a manual reload),
  pausing automatically while a form on the page has focus so it never wipes
  out something you're mid-typing. This, Web Push notifications (sent to
  every other subscribed device in the family whenever a task is completed),
  and — for a parent — managing family members and invitations, all live on
  the Settings page (the button next to "Switch user" in the top bar), since
  none of them are things you look at often enough to deserve their own
  always-visible tab.

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

When Auth0 is enabled, a login is bound to a specific family member: the
first parent to log in creates the family and is bound to it automatically,
and anyone else — another parent, or a child old enough to have their own
account — joins via a one-time invite link. Once bound, a login only ever
sees its own family, and the server enforces role restrictions regardless
of what the UI shows: only parents can manage tasks, family members, and
payouts, and a bound child's login can only ever act on their own tasks and
accounting, never a sibling's. Children don't have to log in individually,
though — a bound parent's session can still act as any unbound child in
their family via the in-app picker (handing the device to a kid to mark a
chore done, for example).

## Running

```bash
go run ./cmd/chores -addr=:8080 -db=chores.db
```

Then open http://localhost:8080. By default (see Authentication below) this
runs with no login required, so local testing needs no extra setup.

(If you have an existing database from before the rename, pass
`-db=ukelonn.db` to keep using it, or just rename the file — the schema
itself didn't change.)

## Language

The UI is available in English and Norwegian (Bokmål), picked with the
dropdown shown on every screen (including the login page). It defaults to
the browser's language when there's no saved preference, and the choice is
then remembered in `localStorage`. Translation strings live in
`web/i18n.js`; add a new language by adding another entry to
`TRANSLATIONS` there and to `window.LANGUAGES`. Error messages coming from
the server (validation errors, permission errors) aren't localized yet —
only the UI text is.

## Authentication

Auth mode is controlled by `-auth` (or left on its default, `auto`):

- `auto` (default) — uses Auth0 if `AUTH0_DOMAIN`, `AUTH0_CLIENT_ID` and
  `AUTH0_CLIENT_SECRET` are all set; otherwise falls back to `disabled`.
- `disabled` — **local testing mode.** No login wall at all — behaves exactly
  like the app did before auth existed. This is the default whenever the
  Auth0 environment variables aren't set, so `go run ./cmd/chores` keeps
  working unchanged. You can also force it with `-auth=disabled` even if the
  Auth0 env vars happen to be set.
- `auth0` — requires an Auth0 login before the app or its API can be used.

### Setting up Auth0

1. In the Auth0 dashboard, create a **Regular Web Application** (not SPA —
   the token exchange happens server-side).
2. Set **Allowed Callback URLs** to your callback URL, e.g.
   `http://localhost:8080/auth/callback` for local testing against a real
   tenant, or your production URL.
3. Set **Allowed Logout URLs** to your app's base URL, e.g.
   `http://localhost:8080/`.
4. Run the app with:

   ```bash
   export AUTH0_DOMAIN=your-tenant.eu.auth0.com
   export AUTH0_CLIENT_ID=...
   export AUTH0_CLIENT_SECRET=...
   # optional, defaults to http://localhost<addr>/auth/callback
   export AUTH0_CALLBACK_URL=http://localhost:8080/auth/callback
   go run ./cmd/chores
   ```

   (or pass the equivalent `-auth0-domain`, `-auth0-client-id`,
   `-auth0-client-secret`, `-auth0-callback-url` flags).

Login uses the standard OAuth2 Authorization Code flow: `/auth/login`
redirects to Auth0, `/auth/callback` exchanges the code and fetches the
profile from Auth0's `/userinfo` endpoint, and a session is kept in-memory
(no JWT verification needed since tokens never leave the server). Sessions
don't survive a server restart. `/auth/logout` clears the session and signs
out of Auth0 too.

### Inviting a family member

From the Family section of the Settings page, a parent can create an invite
link for another parent (e.g. the other guardian) or for a child old enough
to have their own Auth0 account. The link is a one-time, unguessable token
(`/invite/accept?token=...`) — whoever opens it, after logging into their
own Auth0 account, is bound to that slot in the same family, with the role
the invite was created for. The token is shown once at creation time; a
parent can revoke an invite before it's accepted, which also removes the
unclaimed slot it created. Accepting an invite isn't restricted to any
particular email address — possession of the link is what grants access, so
only share it with the intended person.

A parent login can only ever be bound to one family this way — accepting a
second parent invite with a login that already belongs somewhere is
rejected. A child invite has no such limit: the same login can accept
invites into more than one family, which is exactly the "lives in two
households" case.

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
Without a `?key=`, `/dashboard` instead prompts for the key to be typed in.

A dashboard key is a bearer credential scoped to exactly one family and to
four actions: list children's daily status, list today's task occurrences,
and complete/uncomplete a task. It cannot see or touch anything else —
family membership, task definitions, payouts, other families' data, and
every other RPC in the API reject a dashboard-only request the same way
they'd reject an anonymous one. "Regenerate key" invalidates the old key
immediately (any device still using it falls back to the key prompt) and
"Disable dashboard" turns the feature off until set up again.

## Installing as an app (PWA)

The web UI is an installable Progressive Web App: `web/manifest.webmanifest`
declares its name, icons, and standalone display mode, and `web/sw.js`
precaches the static app shell (HTML/CSS/JS/icons only — never API
responses or login state, so nothing about family data or sessions is ever
cached) so the shell keeps loading offline. Both the app and the login page
register the service worker, so an install prompt can appear before or
after logging in. On a phone, use the browser's "Add to Home Screen" /
"Install app" option; on desktop Chrome/Edge, an install icon appears in
the address bar. `web/sw.js` also handles incoming Web Push events (see
Push notifications above) and focuses or opens the app when a notification
is tapped.

## Regenerating the Connect/protobuf code

After editing `proto/chores/v1/chores.proto`:

```bash
buf generate
```

## Project layout

- `proto/` — protobuf service/message definitions
- `gen/` — generated protobuf + Connect Go code (checked in, regenerate with `buf generate`)
- `internal/db` — SQLite schema and connection setup
- `internal/scheduling` — cron-expression date matching for recurring tasks
- `internal/server` — Connect service implementation (`push.go` holds VAPID key setup and Web Push sending)
- `internal/auth` — Auth0 login (or the local-testing bypass) gating the app
- `web/` — embedded static frontend (vanilla HTML/CSS/JS, calls the Connect API directly via JSON); `web/i18n.js` holds the English/Norwegian translation strings
- `cmd/chores` — main entrypoint
