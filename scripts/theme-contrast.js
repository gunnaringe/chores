#!/usr/bin/env node
// Checks every theme's colour tokens in web/app.css against the contrast bar
// documented in docs/APP.md (Theme). No browser, no server — just the CSS —
// so run it after touching any palette instead of screenshotting all 18
// themes to eyeball readability:
//
//   node scripts/theme-contrast.js            # failures + warnings, exit 1 on failure
//   node scripts/theme-contrast.js --all      # every pair, with its ratio
//   node scripts/theme-contrast.js --css <file>  # e.g. another branch's app.css
//
// It measures tokens, not rendered pixels: a rule that hardcodes a colour
// (e.g. `color: #fff`) instead of using a token is invisible to it. That's
// exactly how white-on-neon ticks slipped through before — so don't.

const fs = require("fs");
const { themes, hex, over, contrast } = require("./theme-css");

// [foreground, background, minimum, level]. Backgrounds that are
// translucent (the dark themes' *-soft tints) are composited over --surface,
// which is what they sit on in practice.
const PAIRS = [
  ["--text", "--bg", 4.5],
  ["--text", "--surface", 4.5],
  ["--muted", "--bg", 4.5],
  ["--muted", "--surface", 4.5],
  // --faint is for icons and placeholders only: the 3:1 non-text bar.
  ["--faint", "--bg", 3],
  ["--faint", "--surface", 3],
  ["--accent-fg", "--accent", 4.5],
  ["--accent-text", "--bg", 4.5],
  ["--accent-text", "--surface", 4.5],
  // The done tick is an icon, so 3:1.
  ["--green-fg", "--green", 3],
  // Danger-button label. Several themes sit at ~3.4–3.8 with white on red
  // by choice, so this warns rather than fails.
  ["--red-fg", "--red", 4.5, "warn"],
  ["--red-text", "--red-soft", 4.5],
  ["--pill-parent-fg", "--pill-parent-bg", 4.5],
  ["--pill-child-fg", "--pill-child-bg", 4.5],
  ["--pill-notcompleted-fg", "--pill-notcompleted-bg", 4.5],
  ["--pill-mandatory-fg", "--pill-mandatory-bg", 4.5],
  ["--pill-optional-fg", "--pill-optional-bg", 4.5],
];

const all = process.argv.includes("--all");
const cssArg = process.argv.indexOf("--css");
const css = cssArg !== -1 ? fs.readFileSync(process.argv[cssArg + 1], "utf8") : undefined;
let failures = 0;
let warnings = 0;
for (const th of themes(css)) {
  const t = th.tokens;
  const surface = over(hex(t["--surface"]), hex(t["--bg"]));
  const lines = [];
  for (const [fg, bg, min, level = "fail"] of PAIRS) {
    const F = hex(t[fg]);
    const B = hex(t[bg]);
    if (!F || !B) {
      lines.push(`  ?    ${fg} on ${bg}: can't parse ${t[fg]} / ${t[bg]}`);
      failures++;
      continue;
    }
    const back = over(B, surface);
    const ratio = contrast(over(F, back), back);
    const ok = ratio >= min;
    if (!ok && level === "fail") failures++;
    if (!ok && level === "warn") warnings++;
    if (!ok || all) {
      const tag = ok ? "ok  " : level === "warn" ? "warn" : "FAIL";
      lines.push(`  ${tag} ${fg} on ${bg}: ${ratio.toFixed(2)} (min ${min})`);
    }
  }
  if (lines.length) console.log(`${th.name}\n${lines.join("\n")}`);
}
console.log(`\n${failures} failure(s), ${warnings} warning(s)`);
process.exit(failures ? 1 : 0);
