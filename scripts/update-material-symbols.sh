#!/usr/bin/env bash
# Refreshes web/material-symbols.json — the icon list the task icon picker's
# search runs against (see loadMaterialSymbols in web/app.js) — from the
# metadata fonts.google.com/icons' own search uses. Run this occasionally to
# pick up icons Google has added; the frontend just searches whatever is in
# the file.
#
# Each entry is one string: the icon name, then its search tags (Google's
# synonyms — "delete" carries "trash", "bin", "garbage", …) split into
# words, e.g. "delete basket bin can clean … trash waste". Entries are
# sorted by Google's popularity score, which the search uses as its
# tie-break. Only icons in the Material Symbols Outlined font are kept.
#
# Tags are pruned to keep the file small: words already in the name, short
# words, and words so generic they tag more than MAX_ICONS_PER_WORD icons
# ("symbol", "data", "device", …) — those match half the set and only drown
# out the useful hits.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MAX_ICONS_PER_WORD=80

curl -fsS "https://fonts.google.com/metadata/icons?incomplete=1&key=material_symbols" \
  | node -e '
const fs = require("fs");
let raw = "";
process.stdin.on("data", (c) => (raw += c)).on("end", () => {
  // The response starts with an XSSI guard line, ")]}'"'"'".
  const meta = JSON.parse(raw.slice(raw.indexOf("\n") + 1));
  const icons = meta.icons
    .filter((i) => !(i.unsupported_families || []).includes("Material Symbols Outlined"))
    .sort((a, b) => b.popularity - a.popularity || a.name.localeCompare(b.name));
  const stop = new Set("and the for with from".split(" "));
  const perIcon = icons.map((i) => {
    const inName = new Set(i.name.split("_"));
    const words = new Set();
    for (const tag of i.tags || []) {
      for (const w of tag.toLowerCase().split(/[^a-z0-9]+/)) {
        if (w.length > 2 && !stop.has(w) && !inName.has(w)) words.add(w);
      }
    }
    return [i.name, words];
  });
  const df = {};
  for (const [, words] of perIcon) for (const w of words) df[w] = (df[w] || 0) + 1;
  const max = Number(process.argv[2]);
  const out = perIcon.map(([name, words]) => [name, ...[...words].filter((w) => df[w] <= max)].join(" "));
  fs.writeFileSync(process.argv[1], JSON.stringify(out));
  console.error(`Wrote ${out.length} icons to ${process.argv[1]}`);
});
' "$repo_root/web/material-symbols.json" "$MAX_ICONS_PER_WORD"

echo "Done. Review the diff and commit web/material-symbols.json." >&2
