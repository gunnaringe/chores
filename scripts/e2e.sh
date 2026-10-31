#!/usr/bin/env bash
# Runs the browser smoke test (e2e/smoke.js) against a throwaway instance:
# its own temp database and its own ports (devauth :9998, chores :8081), so
# it never touches scripts/dev-up.sh's dev database or a server already
# running on :8080. Builds real binaries first, so what's tested is what
# would ship, and so the trap below kills the servers themselves rather
# than a `go run` wrapper.
#
#   scripts/e2e.sh
#
# Needs node, Chromium and Playwright — see .claude/skills/verify-ui/SKILL.md.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
pids=()
cleanup() {
  for p in "${pids[@]}"; do kill "$p" 2>/dev/null || true; done
  rm -rf "$work"
}
trap cleanup EXIT

cd "$repo_root"
go build -o "$work/devauth" ./cmd/devauth
go build -o "$work/chores" ./cmd/chores

"$work/devauth" -addr=:9998 -client-id=e2e -client-secret=e2e-secret > "$work/devauth.log" 2>&1 &
pids+=($!)
AUTH0_DOMAIN=http://localhost:9998 AUTH0_CLIENT_ID=e2e AUTH0_CLIENT_SECRET=e2e-secret \
  "$work/chores" -addr=:8081 -db="$work/e2e.db" > "$work/chores.log" 2>&1 &
pids+=($!)

for _ in $(seq 1 50); do
  curl -fsS -o /dev/null http://localhost:8081/ 2>/dev/null && break
  sleep 0.2
done

status=0
CHORES_URL=http://localhost:8081/ CHORES_DEVAUTH_URL=http://localhost:9998 CHORES_SHOTS="$work/shots" node e2e/smoke.js || status=$?
if [ "$status" -ne 0 ]; then
  echo "--- chores log ---"; tail -30 "$work/chores.log"
  echo "--- devauth log ---"; tail -10 "$work/devauth.log"
fi
exit "$status"
