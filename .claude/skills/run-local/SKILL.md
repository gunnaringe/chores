---
name: run-local
description: Start the Chores app locally with the devauth test identity provider and log in as a test parent or child. Use when you need the app actually running — to exercise a change by hand, reproduce a bug, drive it in a browser, or hit the Connect API. Covers the Makefile/mise trap and the go:embed restart rule.
---

# Running Chores locally

## Do not use `make`

Every Makefile target shells through `mise exec --`, and `mise` is not
installed in agent sandboxes. `make run`, `make dev` and `make devauth` all
fail with `mise: not found`. Run the `go run` commands directly instead.

## Start it

The app refuses to boot without Auth0 config, so local runs point
`AUTH0_DOMAIN` at `cmd/devauth`, a self-contained OAuth2 provider that mimics
Auth0's endpoint shape. Two processes, in this order:

```bash
# 1. the test identity provider, on :9999
go run ./cmd/devauth -client-id=devclient -client-secret=devsecret

# 2. the app, on :8080, pointed at it
AUTH0_DOMAIN=http://localhost:9999 \
AUTH0_CLIENT_ID=devclient \
AUTH0_CLIENT_SECRET=devsecret \
  go run ./cmd/chores -addr=:8080 -db=/tmp/chores-dev.db
```

Keep the database out of the repo (`/tmp/...`), so a test run never leaves a
stray `chores.db` in the working tree.

Give the first start ~10s — the Go toolchain and modules may need fetching.
Wait for `Chores listening on :8080` in the log, or poll:

```bash
curl -sS --noproxy '*' -o /dev/null -w '%{http_code}\n' http://localhost:8080/app.css
```

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
Restart the app process after any frontend edit.

Confirm the running server actually has your change rather than trusting the
reload — this is faster than re-debugging a change that was already correct:

```bash
curl -sS --noproxy '*' http://localhost:8080/app.css | grep -c 'some-new-class'
```

## Stopping it cleanly

`pkill -f 'chores -addr=:8080'` **will kill your own shell**: the pattern
matches the shell's own command line, which contains the string you just
typed. This is easy to do repeatedly without realising why the session died.

Start it under `setsid` with a pidfile instead:

```bash
setsid go run ./cmd/chores ... > /tmp/chores.log 2>&1 < /dev/null &
echo $! > /tmp/chores.pid
# later:
kill "$(cat /tmp/chores.pid)"
```

If you have already lost the pid, match on something that cannot appear in
your own command line — the compiled binary path under `go-build`, not the
`go run` invocation.

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
