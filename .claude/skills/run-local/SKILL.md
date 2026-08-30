---
name: run-local
description: Start the Chores app locally with the devauth test identity provider and log in as a test parent or child. Use when you need the app actually running — to exercise a change by hand, reproduce a bug, drive it in a browser, or hit the Connect API. Covers the Makefile/mise trap and the go:embed restart rule.
---

# Running Chores locally

## Do not use `make`

Every Makefile target shells through `mise exec --`, and `mise` is not
installed in agent sandboxes. `make run`, `make dev` and `make devauth` all
fail with `mise: not found`. Use `scripts/dev-up.sh` / `scripts/dev-down.sh`
instead (below) — plain shell, no mise involved.

## Start it

```bash
scripts/dev-up.sh          # or --fresh to wipe the dev database first
```

That starts both processes `make dev` would (devauth on :9999, chores on
:8080 pointed at it — the app refuses to boot without Auth0 config, so local
runs point `AUTH0_DOMAIN` at `cmd/devauth`, a self-contained OAuth2 provider
that mimics Auth0's endpoint shape), each in its own process group with a
pidfile under `/tmp`, and polls until `:8080` answers before returning —
however long the first build takes. Re-running it is safe: an already-running
instance is left alone rather than double-started. See "Stopping it cleanly"
below for why it's structured this way instead of two plain `go run`s.

The database lives at `/tmp/chores-dev.db`, out of the repo so a test run
never leaves a stray `chores.db` in the working tree.

## Logging in

Open `http://localhost:8080/`, click **Log in**, and devauth serves a plain
page listing canned identities. Two exist by default:

| Identity | Email |
|---|---|
| Test Parent | parent@example.com |
| Test Child  | child@example.com  |

Two exist because testing an invite needs two distinct logins — a parent
invites, then the child accepts with their *own* identity. Pass
`-identity="sub|name|email"` one or more times to replace the defaults.

A fresh database drops you on onboarding: create a family, then add members
from Settings.

## Restart after every `web/` edit

`web/` is compiled into the binary by `//go:embed` (see `web/assets.go`).
A running server keeps serving the assets it started with, so editing
`app.js` / `app.css` / `i18n.js` and reloading the page shows **no change**.
Restart after any frontend edit:

```bash
scripts/dev-down.sh && scripts/dev-up.sh
```

Confirm the running server actually has your change rather than trusting the
reload — this is faster than re-debugging a change that was already correct:

```bash
curl -sS --noproxy '*' http://localhost:8080/app.css | grep -c 'some-new-class'
```

## Stopping it cleanly

```bash
scripts/dev-down.sh
```

This used to be the single most time-wasting thing in this repo, with three
separate traps stacking on top of each other — worth knowing even now that
the script handles it, since it explains why the script is structured the
way it is (and what to do if you ever have to do this by hand):

1. **`pkill -f 'chores -addr=:8080'` kills your own shell.** The pattern
   matches the shell's own `/proc` command line, because it contains the
   string you just typed. The shell dies, and it is not obvious why.
2. **Killing the `go run` pid leaves the server running.** `go run` compiles
   to `/root/.cache/go-build/<hash>/exe/chores` and execs that as a *child*.
   Kill the wrapper and the child keeps holding `:8080`, so your next start
   fails with `address already in use` — while the old binary, with the old
   embedded assets, quietly keeps serving your browser.
3. **Matching on the binary path misses it**, because that cache path
   contains neither `chores` nor `cmd/chores` in the part you would match.

Moving the matcher into a script file only fixes trap 1 *if nothing else on
the invoking command line repeats the pattern* — which is easy to get wrong.

The fix `scripts/dev-up.sh` / `dev-down.sh` use: a process group and a
pidfile, no `/proc` scanning at all. `dev-up.sh` starts each process with
`setsid ... &` and records `$!` (the wrapper's pid, which `setsid` also makes
the process group leader) to a pidfile; `dev-down.sh` reads it back and runs
`kill -- "-$pid"` — the leading `-` targets the whole group, wrapper and
exec'd binary together, so nothing is left holding the port.

If you've lost a pidfile and must scan by hand, match the **listen flag**
(`-addr=:8080`), do it from a standalone script file, and invoke that script
as the *only* thing on the command line.

## Talking to the API directly

The frontend calls Connect's unary JSON encoding with no generated client, so
`curl` works the same way it does:

```bash
curl -sS --noproxy '*' -X POST \
  -H 'Content-Type: application/json' \
  --data '{"familyId":"..."}' \
  http://localhost:8080/chores.v1.ChoresService/ListTasks
```

Every RPC except the kiosk dashboard ones needs the session cookie from a
completed login.
