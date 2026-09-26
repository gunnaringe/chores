#!/usr/bin/env node
// Source of truth for the decorative --bg-pattern tiles in web/app.css.
// The tiles are inline SVG data URIs, which are unreadable and unsafe to
// hand-edit; edit the motifs/layouts here instead and regenerate:
//
//   node scripts/theme-patterns.js --write   # rewrite the tiles in app.css
//   node scripts/theme-patterns.js --check   # exit 1 if app.css is out of date
//   node scripts/theme-patterns.js --svg <theme>  # print one tile's raw SVG
//
// Then look at them — see the verify-ui skill's theme-preview.js, which
// renders every tile from app.css without needing the server.
//
// Cut-outs (eyes, holes, veins) are painted in the theme's own --bg, read
// from app.css at generation time. Because opacity sits on the whole <g>,
// they composite back to exactly --bg — so changing a theme's --bg means
// re-running --write, which --check will flag.
//
// Space Invaders' pixel alien isn't generated here; it's a hand-made tile
// and stays as is. Themes not listed below keep whatever app.css has.
//
// Keep every motif an original, generic shape — nothing resembling a real
// character, franchise, or brand (no studded bricks, no known insignia).

const fs = require("fs");
const { CSS_PATH, blockRange, tokensIn } = require("./theme-css");

const f1 = (n) => +n.toFixed(1);
const polar = (r, deg) => [f1(r * Math.cos((deg * Math.PI) / 180)), f1(r * Math.sin((deg * Math.PI) / 180))];

// n-pointed star, outer radius ro, inner ri, first point at `rot` degrees.
function star(n, ro, ri, rot = -90) {
  const pts = [];
  for (let i = 0; i < n * 2; i++) pts.push(polar(i % 2 ? ri : ro, rot + (i * 180) / n).join(" "));
  return "M" + pts.join(" L") + "Z";
}
function gear(teeth, ro, ri) {
  const pts = [];
  const step = 360 / teeth;
  for (let i = 0; i < teeth; i++) {
    const a0 = i * step;
    for (const [a, r] of [[a0 - step * 0.18, ro], [a0 + step * 0.18, ro], [a0 + step * 0.32, ri], [a0 + step * 0.68, ri]]) {
      pts.push(polar(r, a).join(" "));
    }
  }
  return "M" + pts.join(" L") + "Z";
}
const ring = (r, step) => {
  const out = [];
  for (let a = -90; a < 270; a += step) out.push(polar(r, a));
  return out;
};

// ---- motifs --------------------------------------------------------------
// Each draws around (0,0) in roughly a 48px box. c = motif colour,
// k = cut-out colour (the theme's --bg).

const M = {
  heart: (c) => `<path fill='${c}' d='M0 12C-20-2-12-20 0-9C12-20 20-2 0 12Z'/>`,
  star: (c) => `<path fill='${c}' stroke='${c}' stroke-width='3' stroke-linejoin='round' d='${star(5, 14, 6.5)}'/>`,
  sparkle: (c) => `<path fill='${c}' d='M0-14Q2-2 14 0Q2 2 0 14Q-2 2-14 0Q-2-2 0-14Z'/>`,
  balloon: (c, k) =>
    `<ellipse fill='${c}' cx='0' cy='-8' rx='10' ry='12.5'/><path fill='${c}' d='M-3 5L3 5L0 1Z'/>` +
    `<path fill='none' stroke='${c}' stroke-width='1.6' stroke-linecap='round' d='M0 5C-5 11 5 15 0 22'/>` +
    `<ellipse fill='${k}' cx='-4' cy='-12' rx='2.2' ry='3.5' transform='rotate(25 -4 -12)'/>`,
  flower: (c, k) =>
    ring(8, 72).map(([x, y]) => `<circle fill='${c}' cx='${x}' cy='${y}' r='6.5'/>`).join("") +
    `<circle fill='${k}' cx='0' cy='0' r='4.5'/><circle fill='${c}' cx='0' cy='0' r='2.5'/>`,
  confetti: (c) =>
    `<rect fill='${c}' x='-10' y='-3' width='7' height='3' rx='1.5' transform='rotate(30)'/>` +
    `<circle fill='${c}' cx='6' cy='-6' r='2'/><circle fill='${c}' cx='2' cy='8' r='1.5'/>`,
  dot: (c, k, r = 2) => `<circle fill='${c}' cx='0' cy='0' r='${r}'/>`,

  pine: (c) =>
    `<g fill='${c}' stroke='${c}' stroke-width='2.5' stroke-linejoin='round'><path d='M0-24L9-12L4-12L13 0L6 0L16 12L-16 12L-6 0L-13 0L-4-12L-9-12Z'/></g>` +
    `<rect fill='${c}' x='-2.5' y='12' width='5' height='8' rx='1'/>`,
  toadstool: (c, k) =>
    `<path fill='${c}' d='M-17 0C-17-13-8-18 0-18C8-18 17-13 17 0Z'/><path fill='${c}' d='M-5 2L5 2L6 16Q0 19-6 16Z'/>` +
    `<circle fill='${k}' cx='-8' cy='-8' r='3'/><circle fill='${k}' cx='4' cy='-12' r='2.3'/><circle fill='${k}' cx='9' cy='-4' r='2'/>`,
  leaf: (c, k) =>
    `<path fill='${c}' d='M0-16C11-9 11 7 0 16C-11 7-11-9 0-16Z'/>` +
    `<path fill='none' stroke='${k}' stroke-width='1.6' stroke-linecap='round' d='M0-10L0 20M0-2L5-6M0 5L-5 1M0 10L4 7'/>`,

  compass: (c, k) =>
    `<circle fill='none' stroke='${c}' stroke-width='1.6' cx='0' cy='0' r='15'/>` +
    `<path fill='${c}' d='${star(4, 24, 5)}'/><path fill='${c}' d='${star(4, 13, 3.5, -45)}'/>` +
    `<circle fill='${k}' cx='0' cy='0' r='2.5'/>`,
  routeToX: (c) =>
    `<path fill='none' stroke='${c}' stroke-width='2.5' stroke-linecap='round' stroke-dasharray='1 7' d='M-40 22C-25 0-10 30 5 8S20-12 28-14'/>` +
    `<path stroke='${c}' stroke-width='5' stroke-linecap='round' d='M31-24L45-10M45-24L31-10'/>`,
  anchor: (c) =>
    `<g fill='none' stroke='${c}' stroke-width='3.5' stroke-linecap='round'><circle cx='0' cy='-17' r='4'/><path d='M0-13L0 17M-8-7L8-7M-15 5Q-13 17 0 17Q13 17 15 5'/></g>` +
    `<path fill='${c}' d='M-15 0L-19 8L-11 6ZM15 0L19 8L11 6Z'/>`,
  waves: (c) =>
    `<path fill='none' stroke='${c}' stroke-width='2.5' stroke-linecap='round' d='M-16-3q4-5 8 0t8 0t8 0t8 0M-12 5q4-5 8 0t8 0t8 0'/>`,

  fish: (c, k) =>
    `<path fill='${c}' d='M-14 0C-6-11 10-11 16 0C10 11-6 11-14 0Z'/><path fill='${c}' d='M-12 0L-24-9Q-21 0-24 9Z'/>` +
    `<path fill='${c}' d='M-2-7Q2-15 8-9Z'/><circle fill='${k}' cx='9' cy='-2' r='2.4'/>` +
    `<path fill='none' stroke='${k}' stroke-width='1.4' d='M3-6Q0 0 3 6'/>`,
  fishLeft: (c, k) => `<g transform='scale(-1 1)'>${M.fish(c, k)}</g>`,
  bubbles: (c) =>
    `<g fill='none' stroke='${c}' stroke-width='1.8'><circle cx='0' cy='0' r='6'/><circle cx='8' cy='-12' r='4'/><circle cx='3' cy='-22' r='2.6'/></g>`,
  seaweed: (c) =>
    `<g fill='none' stroke='${c}' stroke-width='4' stroke-linecap='round'><path d='M-4 22C-12 12 4 4-4-6S-2-18 0-24'/><path d='M4 22C10 14 0 8 7 0S6-10 10-14' stroke-width='3'/></g>`,
  starfish: (c, k) =>
    `<path fill='${c}' stroke='${c}' stroke-width='4' stroke-linejoin='round' d='${star(5, 15, 7)}'/>` +
    ring(9, 72).map(([x, y]) => `<circle fill='${k}' cx='${x}' cy='${y}' r='1.1'/>`).join(""),
  shell: (c, k) =>
    `<path fill='${c}' d='M0 13L-15-3C-15-13-7-17 0-17C7-17 15-13 15-3Z'/><path fill='${c}' d='M-6 12L6 12L4 17L-4 17Z'/>` +
    `<path fill='none' stroke='${k}' stroke-width='1.5' d='M0 11L0-15M0 11L-6-15M0 11L6-15M0 11L-11-9M0 11L11-9'/>`,

  cog: (c, k, teeth = 10) => `<path fill='${c}' d='${gear(teeth, 16, teeth > 8 ? 12.5 : 12)}'/><circle fill='${k}' cx='0' cy='0' r='5'/>`,
  nut: (c, k) => {
    const pts = [];
    for (let a = 0; a < 360; a += 60) pts.push(polar(11, a).join(" "));
    return `<path fill='${c}' d='M${pts.join(" L")}Z'/><circle fill='${k}' cx='0' cy='0' r='4.5'/>`;
  },
  wrench: (c, k) =>
    `<path fill='${c}' d='M-3-10L3-10L3 16Q3 19 0 19Q-3 19-3 16Z'/><circle fill='${c}' cx='0' cy='-14' r='8'/>` +
    `<path fill='${k}' d='M-3.5-24L3.5-24L3.5-15Q0-12-3.5-15Z'/>`,
  circuit: (c) =>
    `<g fill='none' stroke='${c}' stroke-width='2' stroke-linecap='round' stroke-linejoin='round'><path d='M-22-10L-6-10L2-2L20-2'/><path d='M-22 4L-10 4L-4 10L14 10'/></g>` +
    `<g fill='${c}'><circle cx='-22' cy='-10' r='3'/><circle cx='20' cy='-2' r='3'/><circle cx='-22' cy='4' r='3'/><circle cx='14' cy='10' r='3'/></g>`,
  robot: (c, k) =>
    `<rect fill='${c}' x='-13' y='-9' width='26' height='20' rx='5'/><path stroke='${c}' stroke-width='2' d='M0-9L0-16'/><circle fill='${c}' cx='0' cy='-17' r='2.8'/>` +
    `<rect fill='${c}' x='-16' y='-3' width='3' height='8' rx='1'/><rect fill='${c}' x='13' y='-3' width='3' height='8' rx='1'/>` +
    `<circle fill='${k}' cx='-5' cy='-1' r='3'/><circle fill='${k}' cx='5' cy='-1' r='3'/><rect fill='${k}' x='-6' y='5' width='12' height='2.5' rx='1'/>`,

  monster: (c, k) =>
    `<path fill='${c}' d='M-15 16L-15-2C-15-14-8-18 0-18C8-18 15-14 15-2L15 16L10 12L5 16L0 12L-5 16L-10 12Z'/>` +
    `<path fill='${c}' d='M-10-14L-14-24L-5-17ZM10-14L14-24L5-17Z'/>` +
    `<circle fill='${k}' cx='0' cy='-5' r='7'/><circle fill='${c}' cx='1.5' cy='-4' r='3.2'/>` +
    `<path fill='none' stroke='${k}' stroke-width='1.8' stroke-linecap='round' d='M-5 6Q0 9 5 6'/>`,
  blob: (c, k) =>
    `<path fill='${c}' d='M-14 12C-18 0-12-14 0-14C12-14 18 0 14 12C9 16 5 12 0 15C-5 12-9 16-14 12Z'/>` +
    `<circle fill='${k}' cx='-6' cy='-4' r='3.5'/><circle fill='${k}' cx='3' cy='-7' r='2.6'/><circle fill='${k}' cx='8' cy='-1' r='3'/>` +
    `<circle fill='${c}' cx='-5' cy='-3.5' r='1.5'/><circle fill='${c}' cx='3.5' cy='-6.5' r='1.1'/><circle fill='${c}' cx='8.5' cy='-0.5' r='1.3'/>`,
  eyeball: (c, k) => `<circle fill='${c}' cx='0' cy='0' r='11'/><circle fill='${k}' cx='2' cy='-1' r='5.5'/><circle fill='${c}' cx='3' cy='-1' r='2.6'/>`,
  flask: (c) =>
    `<path fill='none' stroke='${c}' stroke-width='2.5' stroke-linejoin='round' d='M-5-18L5-18M-3.5-18L-3.5-6L-15 14Q-16 17-13 17L13 17Q16 17 15 14L3.5-6L3.5-18'/>` +
    `<path fill='${c}' d='M-9 4L9 4L15 14Q16 17 13 17L-13 17Q-16 17-15 14Z'/>` +
    `<g fill='none' stroke='${c}' stroke-width='1.5'><circle cx='2' cy='-2' r='2'/><circle cx='-1' cy='-10' r='1.5'/><circle cx='4' cy='-24' r='2.5'/></g>`,
  bolt: (c) => `<path fill='${c}' stroke='${c}' stroke-width='1.5' stroke-linejoin='round' d='M4-18L-9 3L-1 3L-5 18L9-4L1-4Z'/>`,

  // A three-toed theropod track: long pointed toes meeting at a small heel.
  // (Round toes on a round pad read as a paw or a rabbit — keep them pointed.)
  footprint: (c) =>
    `<g fill='${c}' stroke='${c}' stroke-width='2' stroke-linejoin='round'><path d='M-4 6L-3.5-13L0-22L3.5-13L4 6Z'/>` +
    `<path d='M-3.5 6L-3-7L0-15L3-7L3.5 6Z' transform='rotate(-34 0 8)'/><path d='M-3.5 6L-3-7L0-15L3-7L3.5 6Z' transform='rotate(34 0 8)'/>` +
    `<ellipse cx='0' cy='9' rx='6.5' ry='6'/></g>`,
  fern: (c) => {
    let leaves = "";
    for (let i = 0; i < 5; i++) {
      for (const s of [-1, 1]) {
        const x = f1(s * (7 - i * 0.7));
        const y = -14 + i * 6 + (s < 0 ? 0 : 3);
        leaves += `<ellipse fill='${c}' cx='${x}' cy='${y}' rx='${f1(6 - i * 0.6)}' ry='2.4' transform='rotate(${s * -25} ${x} ${y})'/>`;
      }
    }
    return `<path fill='none' stroke='${c}' stroke-width='2' stroke-linecap='round' d='M0 22Q2 0 0-22'/>` + leaves;
  },
  volcano: (c) =>
    `<path fill='${c}' d='M-22 16L-6-8L-3-5L0-9L3-5L6-8L22 16Z'/>` +
    `<g fill='${c}'><circle cx='-1' cy='-14' r='3.5'/><circle cx='4' cy='-19' r='4.5'/><circle cx='10' cy='-24' r='3'/></g>`,
  bone: (c) =>
    `<rect fill='${c}' x='-14' y='-3' width='28' height='6'/>` +
    `<g fill='${c}'><circle cx='-15' cy='-4' r='4.5'/><circle cx='-15' cy='4' r='4.5'/><circle cx='15' cy='-4' r='4.5'/><circle cx='15' cy='4' r='4.5'/></g>`,
  egg: (c, k) =>
    `<path fill='${c}' d='M0-16C8-16 12-2 12 5C12 12 7 16 0 16C-7 16-12 12-12 5C-12-2-8-16 0-16Z'/>` +
    `<path fill='none' stroke='${k}' stroke-width='1.6' stroke-linejoin='round' d='M-12 1L-7-3L-3 2L2-3L6 2L12-2'/>` +
    `<g fill='${k}'><circle cx='-5' cy='-8' r='1.6'/><circle cx='5' cy='8' r='2'/></g>`,

  rocket: (c, k) =>
    `<path fill='${c}' d='M0-24C9-15 9-3 7 10L-7 10C-9-3-9-15 0-24Z'/>` +
    `<path fill='${c}' d='M-8 0L-15 12L-15 16L-7 12ZM8 0L15 12L15 16L7 12Z'/><path fill='${c}' d='M-5 13Q0 26 5 13Z'/>` +
    `<circle fill='${k}' cx='0' cy='-8' r='3.6'/><path fill='none' stroke='${k}' stroke-width='1.4' d='M-7 7L7 7'/>`,
  // Ring drawn twice: a --bg-coloured band first so it visibly passes in
  // front of the planet, then the ring itself; the lower half of the planet
  // is repainted over it so the ring goes *behind* at the top.
  planet: (c, k) =>
    `<circle fill='${c}' cx='0' cy='0' r='11'/>` +
    `<ellipse fill='none' stroke='${k}' stroke-width='5' cx='0' cy='0' rx='22' ry='6' transform='rotate(-20)'/>` +
    `<ellipse fill='none' stroke='${c}' stroke-width='2' cx='0' cy='0' rx='22' ry='6' transform='rotate(-20)'/>` +
    `<path fill='${c}' d='M-11 0A11 11 0 0 0 11 0Z' transform='rotate(-20)'/>` +
    `<path fill='none' stroke='${c}' stroke-width='2' d='M-22 0A22 6 0 0 0 22 0' transform='rotate(-20)'/>`,
  satellite: (c, k) =>
    `<rect fill='${c}' x='-5' y='-5' width='10' height='10' rx='1.5'/>` +
    `<rect fill='${c}' x='-22' y='-4' width='14' height='8'/><rect fill='${c}' x='8' y='-4' width='14' height='8'/>` +
    `<path stroke='${k}' stroke-width='1' d='M-15-4L-15 4M15-4L15 4'/>` +
    `<path fill='none' stroke='${c}' stroke-width='1.6' stroke-linecap='round' d='M0-5L0-11M-4-14Q0-10 4-14'/>`,
  moon: (c, k) => `<circle fill='${c}' cx='0' cy='0' r='11'/><circle fill='${k}' cx='6' cy='-4' r='10'/>`,

  // Isometric cube; `c` here is [top, left, right] rather than one colour.
  cube: ([top, left, right]) =>
    `<path fill='${top}' d='M0-12L14-5L0 2L-14-5Z'/><path fill='${left}' d='M-14-5L0 2L0 18L-14 11Z'/><path fill='${right}' d='M14-5L0 2L0 18L14 11Z'/>`,
};

// ---- layouts -------------------------------------------------------------
// Each item: [x, y, scale, rotateDeg, motif, ...extra args]. Tiles are 240px;
// keep motifs a little inside the edges (nothing is wrapped across the seam)
// and vary size and angle so the repeat doesn't read as a grid.

const CUBE = {
  blue: ["#6f9bec", "#2e6fe0", "#2458b8"],
  green: ["#63c779", "#2fae4a", "#248a3a"],
  red: ["#ec8375", "#e04b3a", "#b53a2c"],
  yellow: ["#f7d86a", "#f2c230", "#c99c1a"],
};

const TILES = {
  playful: { color: "#f6a3cf", opacity: 0.6, items: [
    [40, 44, 1.1, -12, "heart"], [150, 30, 0.9, 10, "star"], [200, 110, 1.1, 8, "balloon"],
    [95, 120, 1, 0, "flower"], [30, 170, 0.8, 20, "star"], [150, 200, 0.8, 14, "heart"],
    [215, 205, 0.9, 0, "sparkle"], [100, 60, 1, 0, "confetti"], [60, 225, 1, 90, "confetti"],
    [200, 45, 1, 0, "dot", 2.5], [140, 150, 1, 0, "dot", 2], [20, 105, 1, 0, "dot", 3],
  ] },
  "fairytale-forest": { color: "#6f9460", opacity: 0.35, items: [
    [40, 50, 1.3, 0, "pine"], [70, 62, 0.9, 0, "pine"], [170, 40, 0.9, 0, "toadstool"],
    [130, 125, 1, 35, "leaf"], [205, 145, 1.4, 0, "pine"], [45, 175, 1, 0, "toadstool"],
    [120, 205, 0.8, -40, "leaf"], [215, 225, 0.6, 0, "sparkle"], [105, 30, 0.5, 0, "sparkle"],
    [20, 115, 0.5, 0, "sparkle"], [215, 80, 0.7, 60, "leaf"],
  ] },
  "pirate-map": { color: "#8a6a34", opacity: 0.35, items: [
    [50, 52, 1.2, 0, "compass"], [150, 110, 1.3, -8, "routeToX"], [200, 40, 0.9, 20, "anchor"],
    [50, 170, 1, 0, "waves"], [185, 210, 0.9, 0, "waves"], [110, 225, 0.5, 15, "compass"],
  ] },
  underwater: { color: "#2ea3a3", opacity: 0.3, items: [
    [55, 45, 1.2, 0, "fish"], [175, 150, 0.9, 0, "fishLeft"], [200, 45, 1, 0, "bubbles"],
    [30, 190, 1.4, 0, "seaweed"], [110, 120, 0.9, 15, "starfish"], [120, 215, 0.9, -12, "shell"],
    [225, 225, 0.8, 0, "bubbles"], [95, 60, 0.7, 0, "bubbles"],
  ] },
  "robot-workshop": { color: "#29c4d6", opacity: 0.22, items: [
    [50, 50, 1.3, 0, "cog"], [80, 82, 0.7, 12, "cog", 8], [175, 45, 1, 0, "robot"],
    [190, 150, 1, 40, "wrench"], [60, 170, 1, 0, "circuit"], [130, 120, 0.9, 15, "nut"],
    [140, 215, 0.8, 0, "nut"], [215, 225, 0.9, 20, "cog"],
  ] },
  "monster-lab": { color: "#9b6ae0", opacity: 0.3, items: [
    [50, 55, 1.2, -6, "monster"], [180, 40, 0.9, 12, "flask"], [175, 150, 1.1, 5, "blob"],
    [65, 175, 0.9, -15, "bolt"], [118, 110, 0.9, 0, "eyeball"], [110, 225, 0.8, 8, "flask"],
    [225, 220, 0.6, 20, "bolt"],
  ] },
  "dino-park": { color: "#7d8a3e", opacity: 0.32, items: [
    [40, 60, 1, -20, "footprint"], [85, 25, 0.9, -10, "footprint"], [190, 60, 1.2, 0, "volcano"],
    [60, 170, 1.2, 25, "fern"], [160, 150, 0.9, -25, "bone"], [200, 215, 0.9, 12, "egg"],
    [120, 220, 0.8, 30, "footprint"], [135, 95, 0.7, -35, "fern"],
  ] },
  "mission-base": { color: "#8fb4e0", opacity: 0.3, items: [
    [50, 60, 1.2, 30, "rocket"], [180, 55, 1.1, 0, "planet"], [165, 170, 1, -25, "satellite"],
    [55, 180, 0.9, -20, "moon"], [115, 110, 0.5, 0, "sparkle"], [215, 115, 0.4, 0, "sparkle"],
    [100, 230, 0.45, 0, "sparkle"], [15, 115, 1, 0, "dot", 1.8], [125, 20, 1, 0, "dot", 1.5],
    [230, 220, 1, 0, "dot", 2], [95, 160, 1, 0, "dot", 1.3], [235, 15, 1, 0, "dot", 1.3],
  ] },
  // Painter's order matters for the stack: lower cube first, the one on top
  // of it last (16px higher at scale 1, so 17.6px at 1.1).
  "block-world": { opacity: 0.3, items: [
    [45, 45, 1.2, 0, "cube", CUBE.blue], [160, 35, 0.9, 0, "cube", CUBE.yellow],
    [200, 150, 1.1, 0, "cube", CUBE.green], [200, 132.4, 1.1, 0, "cube", CUBE.red],
    [90, 135, 1, 0, "cube", CUBE.green], [45, 210, 0.8, 0, "cube", CUBE.red],
    [140, 215, 1, 0, "cube", CUBE.yellow], [125, 90, 0.6, 0, "cube", CUBE.blue],
  ] },
};

function svgFor(name, bg) {
  const { color, opacity, items } = TILES[name];
  const body = items
    .map(([x, y, s, r, motif, ...extra]) => {
      if (!M[motif]) throw new Error(`${name}: unknown motif "${motif}"`);
      const inner = motif === "cube" ? M.cube(extra[0]) : M[motif](color, bg, ...extra);
      return `<g transform='translate(${x} ${y}) rotate(${r}) scale(${s})'>${inner}</g>`;
    })
    .join("");
  return `<svg xmlns='http://www.w3.org/2000/svg' width='240' height='240' viewBox='0 0 240 240'><g opacity='${opacity}'>${body}</g></svg>`;
}

// Same encoding as the hand-made tiles: single-quoted attributes, and only
// what would break url("…") percent-encoded.
function dataUri(svg) {
  const enc = encodeURIComponent(svg).replace(/%2F/g, "/").replace(/%3A/g, ":").replace(/%3D/g, "=").replace(/%2C/g, ",");
  return `url("data:image/svg+xml,${enc}")`;
}

function render(css) {
  for (const name of Object.keys(TILES)) {
    const sel = `:root[data-theme="${name}"] {`;
    const range = blockRange(css, sel);
    if (!range) throw new Error(`no ${sel} block in app.css`);
    const body = css.slice(...range);
    const bg = tokensIn(body)["--bg"];
    if (!/^--bg-pattern:/m.test(body.replace(/^\s+/gm, ""))) throw new Error(`${name}: add a --bg-pattern line to its block first`);
    const next = body.replace(/--bg-pattern:[^;]*;/, `--bg-pattern: ${dataUri(svgFor(name, bg))};`);
    css = css.slice(0, range[0]) + next + css.slice(range[1]);
  }
  return css;
}

const argv = process.argv.slice(2);
const css = fs.readFileSync(CSS_PATH, "utf8");
if (argv[0] === "--svg") {
  const name = argv[1];
  const r = blockRange(css, `:root[data-theme="${name}"] {`);
  if (!TILES[name] || !r) throw new Error(`not a generated theme: ${name} (have: ${Object.keys(TILES).join(", ")})`);
  process.stdout.write(svgFor(name, tokensIn(css.slice(...r))["--bg"]) + "\n");
} else if (argv[0] === "--write") {
  fs.writeFileSync(CSS_PATH, render(css));
  console.log(`wrote ${Object.keys(TILES).length} tiles into ${CSS_PATH}`);
} else if (argv[0] === "--check") {
  if (render(css) !== css) {
    console.error("app.css tiles are out of date — run: node scripts/theme-patterns.js --write");
    process.exit(1);
  }
  console.log("app.css tiles are up to date");
} else {
  console.error("usage: theme-patterns.js --write | --check | --svg <theme>");
  process.exit(2);
}
