# Ukelønn

A family allowance tracker. Single Go binary, embedded web UI, SQLite storage,
Buf-generated Connect API.

- Parents create tasks with a price and a cron-like recurrence (`0 0 * * 1,3,5`
  = every Monday, Wednesday, Friday). The Tasks UI offers day-of-week
  checkboxes that build this expression for you.
- Children mark tasks done for a given day.
- Accounting tracks earnings in the last 7 days and the outstanding balance
  (total earned minus total paid out).
- Parents can pay out the full balance or a partial amount.

Authentication (via Auth0) gates access to the app as a whole. It does not
yet know about individual family members — anyone who logs in can still act
as any family member via the in-app picker. Per-person login is meant to
follow later.

## Running

```bash
go run ./cmd/ukelonn -addr=:8080 -db=ukelonn.db
```

Then open http://localhost:8080. By default (see Authentication below) this
runs with no login required, so local testing needs no extra setup.

## Authentication

Auth mode is controlled by `-auth` (or left on its default, `auto`):

- `auto` (default) — uses Auth0 if `AUTH0_DOMAIN`, `AUTH0_CLIENT_ID` and
  `AUTH0_CLIENT_SECRET` are all set; otherwise falls back to `disabled`.
- `disabled` — **local testing mode.** No login wall at all — behaves exactly
  like the app did before auth existed. This is the default whenever the
  Auth0 environment variables aren't set, so `go run ./cmd/ukelonn` keeps
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
   go run ./cmd/ukelonn
   ```

   (or pass the equivalent `-auth0-domain`, `-auth0-client-id`,
   `-auth0-client-secret`, `-auth0-callback-url` flags).

Login uses the standard OAuth2 Authorization Code flow: `/auth/login`
redirects to Auth0, `/auth/callback` exchanges the code and fetches the
profile from Auth0's `/userinfo` endpoint, and a session is kept in-memory
(no JWT verification needed since tokens never leave the server). Sessions
don't survive a server restart. `/auth/logout` clears the session and signs
out of Auth0 too.

## Regenerating the Connect/protobuf code

After editing `proto/ukelonn/v1/ukelonn.proto`:

```bash
buf generate
```

## Project layout

- `proto/` — protobuf service/message definitions
- `gen/` — generated protobuf + Connect Go code (checked in, regenerate with `buf generate`)
- `internal/db` — SQLite schema and connection setup
- `internal/scheduling` — cron-expression date matching for recurring tasks
- `internal/server` — Connect service implementation
- `internal/auth` — Auth0 login (or the local-testing bypass) gating the app
- `web/` — embedded static frontend (vanilla HTML/CSS/JS, calls the Connect API directly via JSON)
- `cmd/ukelonn` — main entrypoint
