#!/usr/bin/env bash
# Pushes screenshots to a throwaway pr-assets/<topic> orphan branch and
# prints the Markdown to embed them in a PR body (see "Pull requests" in
# CLAUDE.md for why: GitHub has no API for attaching images). Running it
# again with the same topic adds to the branch instead of replacing it.
#
#   .claude/skills/verify-ui/pr-shots.sh <topic> /tmp/shots/a.png /tmp/shots/b.png ...
set -euo pipefail

topic="${1:?usage: pr-shots.sh <topic> <png>...}"
shift
[ "$#" -gt 0 ] || { echo "no files given" >&2; exit 1; }
branch="pr-assets/$topic"
repo="$(gh repo view --json nameWithOwner -q .nameWithOwner)"
work="$(mktemp -d)"
trap 'git worktree remove --force "$work" >/dev/null 2>&1 || true; rm -rf "$work"' EXIT

if git fetch -q origin "$branch" 2>/dev/null; then
  git worktree add -q --detach "$work" FETCH_HEAD
else
  git worktree add -q --detach "$work"
  git -C "$work" checkout -q --orphan "$branch"
  git -C "$work" rm -rfq .
fi
cp "$@" "$work/"
git -C "$work" add -A
git -C "$work" commit -qm "Screenshots for $topic"
git -C "$work" push -q origin "HEAD:refs/heads/$branch"

for f in "$@"; do
  echo "![]($(printf 'https://raw.githubusercontent.com/%s/%s/%s' "$repo" "$branch" "$(basename "$f")"))"
done
echo
echo "(Say in the PR that branch $branch can be deleted once merged.)"
