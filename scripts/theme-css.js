// Shared by theme-contrast.js and theme-patterns.js: reads the theme blocks
// out of web/app.css so neither tool keeps its own copy of any palette.
// app.css stays the only place a theme's colours are defined.

const fs = require("fs");
const path = require("path");

const CSS_PATH = path.join(__dirname, "..", "web", "app.css");

// [start, end) of the body of the first rule whose selector is exactly `sel`
// (including the trailing " {"), brace-matched so nested rules don't end it early.
function blockRange(css, sel) {
  const at = css.indexOf(sel);
  if (at === -1) return null;
  const start = at + sel.length;
  let depth = 1;
  let i = start;
  while (depth) {
    i++;
    if (css[i] === "{") depth++;
    else if (css[i] === "}") depth--;
  }
  return [start, i];
}

function tokensIn(body) {
  const out = {};
  for (const m of body.matchAll(/(--[\w-]+):\s*([^;]+);/g)) out[m[1]] = m[2].trim();
  return out;
}

// Every palette the app can show, as fully resolved token maps: the bare
// :root (light), the OS-dark block ("system-dark"), and each
// :root[data-theme="…"] block layered over :root, the way the cascade does.
// var(--x) references are followed so --accent-text: var(--accent) works.
function themes(css = fs.readFileSync(CSS_PATH, "utf8")) {
  const [bs, be] = blockRange(css, ":root {");
  const base = tokensIn(css.slice(bs, be));
  const list = [];
  const add = (name, sel, own) => {
    const t = { ...base, ...own };
    const resolve = (v, seen = 0) => {
      const m = /^var\((--[\w-]+)\)$/.exec(v);
      return m && seen < 10 && t[m[1]] ? resolve(t[m[1]], seen + 1) : v;
    };
    for (const k of Object.keys(t)) t[k] = resolve(t[k]);
    list.push({ name, sel, own, tokens: t });
  };
  add("light (default)", ":root {", {});
  const sd = blockRange(css, ":root:not([data-theme]) {");
  if (sd) add("system-dark", ":root:not([data-theme]) {", tokensIn(css.slice(...sd)));
  for (const m of css.matchAll(/:root\[data-theme="([\w-]+)"\] \{/g)) {
    const r = blockRange(css, m[0]);
    add(m[1], m[0], tokensIn(css.slice(...r)));
  }
  return list;
}

// ---- colour maths (WCAG 2.x relative luminance) ------------------------

function hex(v) {
  const m = /^#([0-9a-f]{3,8})$/i.exec(String(v).trim());
  if (!m) return null;
  let h = m[1];
  if (h.length <= 4) h = [...h].map((c) => c + c).join("");
  const n = (i) => parseInt(h.slice(i, i + 2), 16) / 255;
  return [n(0), n(2), n(4), h.length === 8 ? n(6) : 1];
}
// Composite a (possibly translucent) colour over an opaque one.
const over = (f, b) => [0, 1, 2].map((i) => f[i] * f[3] + b[i] * (1 - f[3])).concat(1);
function luminance(c) {
  const f = (x) => (x <= 0.03928 ? x / 12.92 : ((x + 0.055) / 1.055) ** 2.4);
  return 0.2126 * f(c[0]) + 0.7152 * f(c[1]) + 0.0722 * f(c[2]);
}
function contrast(a, b) {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

module.exports = { CSS_PATH, blockRange, tokensIn, themes, hex, over, luminance, contrast };
