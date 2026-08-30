#!/usr/bin/env bash
# Stops whatever scripts/dev-up.sh started, via the pgid each start recorded
# — never by scanning /proc for a name to match, which is the trap the
# run-local skill warns about (a pattern that matches your own shell's
# command line kills your own shell; matching the go-run wrapper's pid
# leaves its exec'd child binary holding the port).
set -uo pipefail

stop() {
  local pidfile="$1" label="$2"
  if [ ! -f "$pidfile" ]; then
    echo "chores: $label not running (no pidfile)"
    return
  fi
  local pid
  pid="$(cat "$pidfile")"
  # The leading "-" targets the whole process group setsid created, so the
  # go-run wrapper and the binary it exec'd both go down together.
  if kill -- "-$pid" 2>/dev/null; then
    echo "chores: stopped $label (pid $pid)"
  else
    echo "chores: $label already gone (stale pidfile)"
  fi
  rm -f "$pidfile"
}

stop /tmp/chores-app.pgid chores
stop /tmp/chores-devauth.pgid devauth
