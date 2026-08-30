#!/usr/bin/env bash
# Starts devauth + chores for local/manual testing, each in its own process
# group with a pidfile — see the run-local skill's "Stopping it cleanly"
# section for why that matters. `go run` execs a *child* binary, so a plain
# background job's pid stops being useful the moment you need to kill the
# whole thing; a pgid-based kill (see dev-down.sh) takes out both.
#
#   scripts/dev-up.sh [--fresh]
#
# --fresh wipes the dev database first, for a clean-slate run (onboarding
# from scratch). Safe to re-run without it: an already-running instance is
# left alone rather than double-started.
set -uo pipefail

DB=/tmp/chores-dev.db
DEVAUTH_LOG=/tmp/chores-devauth.log
DEVAUTH_PIDFILE=/tmp/chores-devauth.pgid
APP_LOG=/tmp/chores-app.log
APP_PIDFILE=/tmp/chores-app.pgid

if [ "${1:-}" = "--fresh" ]; then
  echo "chores: wiping $DB"
  rm -f "$DB" "$DB-shm" "$DB-wal"
fi

# Alive iff the pidfile exists and its pid still answers to a signal —
# leftover pidfiles from a crashed or already-stopped run shouldn't block a
# fresh start.
running() {
  [ -f "$1" ] && kill -0 "$(cat "$1")" 2>/dev/null
}

if running "$DEVAUTH_PIDFILE"; then
  echo "chores: devauth already running (pid $(cat "$DEVAUTH_PIDFILE"))"
else
  rm -f "$DEVAUTH_PIDFILE"
  setsid go run ./cmd/devauth -client-id=devclient -client-secret=devsecret \
    > "$DEVAUTH_LOG" 2>&1 < /dev/null &
  echo $! > "$DEVAUTH_PIDFILE"
  echo "chores: devauth starting (pid $(cat "$DEVAUTH_PIDFILE"), log $DEVAUTH_LOG)"
fi

if running "$APP_PIDFILE"; then
  echo "chores: chores already running (pid $(cat "$APP_PIDFILE"))"
else
  rm -f "$APP_PIDFILE"
  setsid env AUTH0_DOMAIN=http://localhost:9999 AUTH0_CLIENT_ID=devclient \
    AUTH0_CLIENT_SECRET=devsecret \
    go run ./cmd/chores -addr=:8080 -db="$DB" \
    > "$APP_LOG" 2>&1 < /dev/null &
  echo $! > "$APP_PIDFILE"
  echo "chores: chores starting (pid $(cat "$APP_PIDFILE"), log $APP_LOG)"
fi

# The first start compiles both binaries, which can take a while if the
# build cache is cold — poll rather than guessing a fixed sleep.
echo -n "chores: waiting for :8080"
for _ in $(seq 1 60); do
  if curl -sS --noproxy '*' -o /dev/null http://localhost:8080/app.css 2>/dev/null; then
    echo " — ready"
    exit 0
  fi
  echo -n "."
  sleep 1
done
echo " — still not answering after 60s; check $APP_LOG and $DEVAUTH_LOG" >&2
exit 1
