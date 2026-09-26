#!/usr/bin/env node
// Theme contact sheets: many themes in ONE png, so checking a palette or
// pattern change is one image to look at rather than one per theme.
//
//   # Background tiles only, straight from web/app.css — no server needed.
//   node .claude/skills/verify-ui/theme-preview.js patterns --out /tmp/patterns.png
//   #   --zoom: one tile per theme at 2x and near-full opacity, to judge the
//   #           drawing itself (at real opacity the motifs are deliberately faint)
//
//   # The running app (see run-local), logged in as Test Parent, one phone
//   # screenshot per theme. Seed data first or there's nothing on screen.
//   node .claude/skills/verify-ui/theme-preview.js app --out /tmp/themes.png \
//     --themes hacker,playful,dino-park --tab today --assets /tmp
//
// Common flags: --themes a,b,c (default: all; in app mode "system" means
// "Match device", i.e. no data-theme), --chrome, --playwright (same
// defaults and fallbacks as screenshot.js — see the verify-ui skill).
// app mode also takes --url, --tab (a data-tab value; default: first tab),
// --dark (emulate an OS dark preference, which is what "Match device" — the
// "" theme — follows) and --tick (tick the first open task so the done-state
// colours show up).

const fs = require("fs");
const path = require("path");
const { execSync } = require("child_process");
const { themes: readThemes } = require("../../../scripts/theme-css");

const mode = process.argv[2];
const arg = (name, fallback) => {
  const i = process.argv.indexOf("--" + name);
  return i !== -1 && process.argv[i + 1] ? process.argv[i + 1] : fallback;
};
const flag = (name) => process.argv.includes("--" + name);

const OUT = arg("out", `/tmp/theme-${mode}.png`);
const ASSETS = arg("assets", "/tmp");
const URL = arg("url", "http://localhost:8080/");
const PW = arg("playwright", "/opt/node22/lib/node_modules/playwright");
function globPreinstalledChrome() {
  try {
    return execSync("ls -d /opt/pw-browsers/chromium-*/chrome-linux/chrome 2>/dev/null | head -1").toString().trim();
  } catch {
    return "";
  }
}
const CHROME = arg("chrome", globPreinstalledChrome());
if (!["patterns", "app"].includes(mode) || !CHROME) {
  console.error("usage: theme-preview.js patterns|app [--out f.png] [--themes a,b] … (and --chrome if not in the managed sandbox)");
  process.exit(2);
}
const { chromium } = require(PW);

const all = readThemes().filter((t) => t.sel.startsWith(":root[data-theme"));
const wanted = arg("themes", "") ? arg("themes").split(",") : null;

async function sheet(browser, cells, styles = "") {
  const page = await browser.newPage({ viewport: { width: 1240, height: 200 } });
  await page.setContent(
    `<style>body{margin:0;display:flex;flex-wrap:wrap;gap:8px;font:13px sans-serif}.c>div:first-child{padding:2px 0}${styles}</style>` +
      cells.join("")
  );
  await page.screenshot({ path: OUT, fullPage: true });
  console.log(`wrote ${OUT}`);
}

async function patterns(browser) {
  const zoom = flag("zoom");
  const styles = [];
  const cells = [];
  for (const th of all) {
    if (wanted && !wanted.includes(th.name)) continue;
    let p = th.own["--bg-pattern"];
    if (!p || p === "none") continue;
    if (zoom) p = p.replace(/opacity='0\.\d+'/, "opacity='0.9'");
    const i = cells.length;
    // Via a <style> block, not style="": the data URI is full of quotes.
    styles.push(
      zoom
        ? `.t${i}{background:${th.tokens["--bg"]} ${p};width:240px;height:240px;zoom:2}`
        : `.t${i}{background:${th.tokens["--bg"]} ${p};width:400px;height:280px;position:relative}` +
          `.t${i} i{position:absolute;inset:100px 36px 100px;background:${th.tokens["--surface"]};border-radius:14px}`
    );
    cells.push(`<div class="c"><div>${th.name}</div><div class="t${i}">${zoom ? "" : "<i></i>"}</div></div>`);
  }
  await sheet(browser, cells, styles.join("\n"));
}

async function app(browser) {
  const names = wanted || all.map((t) => t.name);
  const ctx = await browser.newContext({
    viewport: { width: 390, height: 760 },
    deviceScaleFactor: 1, // contact sheets don't need retina pixels, and 1x keeps them cheap to read
    isMobile: true,
    hasTouch: true,
    colorScheme: flag("dark") ? "dark" : "light",
  });
  const css = path.join(ASSETS, "ms.css");
  const font = path.join(ASSETS, "ms.woff2");
  if (fs.existsSync(css) && fs.existsSync(font)) {
    const body = fs.readFileSync(css, "utf8").replace(/https:\/\/fonts\.gstatic\.com\/[^)]*woff2/g, "https://fonts.gstatic.com/ms.woff2");
    await ctx.route("https://fonts.googleapis.com/**", (r) => r.fulfill({ status: 200, contentType: "text/css", body }));
    await ctx.route("https://fonts.gstatic.com/**", (r) => r.fulfill({ status: 200, contentType: "font/woff2", body: fs.readFileSync(font) }));
  } else {
    console.warn("no icon font stub in --assets; run fetch-icon-font.sh (see the verify-ui skill)");
  }
  const page = await ctx.newPage();
  const errs = [];
  page.on("pageerror", (e) => errs.push(e.message));
  page.on("console", (m) => m.type() === "error" && errs.push(m.text()));

  await page.goto(URL, { waitUntil: "networkidle" });
  if (await page.locator("#login-btn").count()) {
    await page.click("#login-btn");
    await page.waitForLoadState("networkidle");
    await page.click('a:has-text("Test Parent")');
    await page.waitForLoadState("networkidle");
  }
  const cells = [];
  let ticked = false;
  for (const name of names) {
    const value = name === "system" ? "" : name;
    await page.evaluate((t) => (t ? localStorage.setItem("chores.theme", t) : localStorage.removeItem("chores.theme")), value);
    await page.goto(URL, { waitUntil: "networkidle" });
    await page.waitForTimeout(600);
    if (arg("tab")) await page.click(`.tabbar button[data-tab="${arg("tab")}"]`);
    // Completions persist server-side, so tick once and it stays done for every later theme.
    if (flag("tick") && !ticked) {
      const todo = page.locator(".checkbtn.todo").first();
      if (await todo.count()) await todo.click();
      ticked = true;
      await page.waitForTimeout(400);
    }
    await page.mouse.move(0, 0);
    await page.waitForTimeout(250);
    const b64 = (await page.screenshot()).toString("base64");
    cells.push(`<div class="c"><div>${name}</div><img src="data:image/png;base64,${b64}"></div>`);
  }
  await sheet(browser, cells);
  if (errs.length) {
    console.log("page errors:\n  " + errs.join("\n  "));
    process.exitCode = 1;
  }
}

(async () => {
  const browser = await chromium.launch({
    executablePath: CHROME,
    args: [...(process.env.HTTPS_PROXY ? ["--proxy-server=" + process.env.HTTPS_PROXY] : []), "--ignore-certificate-errors"],
  });
  try {
    await (mode === "patterns" ? patterns(browser) : app(browser));
  } finally {
    await browser.close();
  }
})();
