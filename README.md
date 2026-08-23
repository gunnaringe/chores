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

There is no authentication yet — anyone with access to the app can act as any
family member (switched via a simple picker). This is meant to evolve into a
proper multi-user, authenticated app later.

## Running

```bash
go run ./cmd/ukelonn -addr=:8080 -db=ukelonn.db
```

Then open http://localhost:8080.

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
- `web/` — embedded static frontend (vanilla HTML/CSS/JS, calls the Connect API directly via JSON)
- `cmd/ukelonn` — main entrypoint
