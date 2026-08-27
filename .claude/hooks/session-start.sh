#!/bin/bash
# Warms the Go module and build caches so the first `go test` / `go run` of a
# session isn't a cold two-minute download. The container image is snapshotted
# after this runs, so the work is done once, not once per session.
#
# Deliberately never fails the session: a warm cache is an optimisation, and a
# network blip here should not stop the agent from starting.
set -uo pipefail

# Local checkouts already have a warm cache and a real mise/toolchain setup.
if [ "${CLAUDE_CODE_REMOTE:-}" != "true" ]; then
  exit 0
fi

cd "${CLAUDE_PROJECT_DIR:-$(dirname "$0")/../..}" || exit 0

echo "chores: warming Go caches..."

if ! command -v go >/dev/null 2>&1; then
  echo "chores: no go on PATH, skipping cache warm" >&2
  exit 0
fi

if ! go mod download 2>&1 | tail -3; then
  echo "chores: go mod download failed (continuing anyway)" >&2
fi

# Populates the build cache, so the first test run compiles only what changed.
if ! go build ./... 2>&1 | tail -5; then
  echo "chores: go build failed (continuing anyway)" >&2
fi

echo "chores: ready"
exit 0
