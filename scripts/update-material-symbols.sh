#!/usr/bin/env bash
# Refreshes web/material-symbols.json — the list of Material Symbols icon
# names the task icon picker's search runs against (see loadMaterialSymbolNames
# in web/app.js) — from @material-symbols/metadata, an npm mirror of
# Google's own Material Symbols codepoints. Run this occasionally to pick up
# icons Google has added since the list was last refreshed; nothing else
# needs to change; the frontend just searches whatever names are in the file.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT

echo "Looking up latest @material-symbols/metadata version..." >&2
version="$(curl -sS https://registry.npmjs.org/@material-symbols/metadata \
  | node -e 'let d="";process.stdin.on("data",c=>d+=c);process.stdin.on("end",()=>console.log(JSON.parse(d)["dist-tags"].latest))')"
echo "Latest version: $version" >&2

tarball="$work_dir/metadata.tgz"
curl -sS -o "$tarball" "https://registry.npmjs.org/@material-symbols/metadata/-/metadata-$version.tgz"
tar -xzf "$tarball" -C "$work_dir"

node -e '
const fs = require("fs");
const names = Object.keys(require(process.argv[1])).sort();
fs.writeFileSync(process.argv[2], JSON.stringify(names));
console.error(`Wrote ${names.length} icon names to ${process.argv[2]}`);
' "$work_dir/package/versions.json" "$repo_root/web/material-symbols.json"

echo "Done. Review the diff and commit web/material-symbols.json." >&2
