#!/usr/bin/env node
// Drives the Chores frontend in Chromium and writes screenshots of every tab
// at phone size, plus dark mode, a 320px screen and desktop.
//
// Logs in through cmd/devauth as "Test Parent", so the app must be running
// with AUTH0_DOMAIN pointed at devauth (see the run-local skill).
//
//   node .claude/skills/verify-ui/screenshot.js --out /tmp/shots

const fs = require("fs");
const path = require("path");
const { execSync } = require("child_process");

const arg = (name, fallback) => {
  const i = process.argv.indexOf("--" + name);
  return i !== -1 && process.argv[i + 1] ? process.argv[i + 1] : fallback;
};

const OUT = arg("out", "/tmp/shots");
const URL = arg("url", "http://localhost:8080/");
const PW = arg("playwright", "/opt/node22/lib/node_modules/playwright");

// Version-pinned directory name, so glob rather than hardcode. --chrome
// overrides this for any sandbox that doesn't match this exact layout (only
// the managed Claude Code image is guaranteed to) — see the verify-ui skill.
function globPreinstalledChrome() {
  try {
    return execSync("ls -d /opt/pw-browsers/chromium-*/chrome-linux/chrome 2>/dev/null | head -1")
      .toString()
      .trim();
  } catch {
    return "";
  }
}
const CHROME = arg("chrome", globPreinstalledChrome());
if (!CHROME) {
  console.error("No Chromium found at /opt/pw-browsers and no --chrome given. See the verify-ui skill.");
  process.exit(1);
}

const { chromium, devices } = require(PW);
fs.mkdirSync(OUT, { recursive: true });

const errs = [];
function watch(page, label) {
  page.on("pageerror", (e) => errs.push(`[${label}] PAGEERROR: ${e.message}`));
  page.on("console", (m) => {
    // devauth (the test login provider) has no favicon; that 404 is not the app's.
    if (m.type() !== "error" || m.location().url.endsWith("/favicon.ico")) return;
    errs.push(`[${label}] CONSOLE: ${m.text()} @ ${m.location().url}`);
  });
  page.on("response", (r) => {
    if (r.status() >= 400) {
      errs.push(`[${label}] HTTP${r.status()} ${r.url()}`);
    }
  });
}

async function shot(page, name, fullPage = true) {
  await page.waitForTimeout(400);
  // Playwright parks the pointer wherever it last clicked, which photographs
  // that control in its hover state and reads as a styling bug.
  await page.mouse.move(0, 0);
  await page.screenshot({ path: path.join(OUT, `${name}.png`), fullPage });
}

(async () => {
  const browser = await chromium.launch({
    executablePath: CHROME,
    args: [
      ...(process.env.HTTPS_PROXY ? ["--proxy-server=" + process.env.HTTPS_PROXY] : []),
      "--ignore-certificate-errors",
    ],
  });

  // Log in once, then reuse the session for every other viewport.
  const ctx = await browser.newContext({ ...devices["iPhone 13"] });
  const page = await ctx.newPage();
  watch(page, "phone");

  await page.goto(URL, { waitUntil: "networkidle" });
  await shot(page, "00-login");

  const loginBtn = page.locator("#login-btn");
  if (await loginBtn.count()) {
    await loginBtn.click();
    await page.waitForLoadState("networkidle");
    await page.click('a:has-text("Test Parent")');
    await page.waitForLoadState("networkidle");
  }
  await page.waitForTimeout(1200);
  const storageState = await ctx.storageState();

  // Whatever tabs this login actually has — a child sees only two.
  const tabs = await page.locator(".tabbar button").evaluateAll((els) =>
    els.map((e) => e.dataset.tab)
  );
  if (!tabs.length) {
    await shot(page, "01-onboarding");
    console.log("No tab bar — the account has no family yet; showing onboarding only.");
  }
  for (const tab of tabs) {
    await page.click(`.tabbar button[data-tab="${tab}"]`);
    await page.waitForTimeout(900);
    await shot(page, `10-phone-${tab}`);
  }

  // Dark mode + a 320px screen, where layout breaks show up first.
  const narrow = await browser.newContext({
    ...devices["iPhone SE"],
    colorScheme: "dark",
    storageState,
  });
  const np = await narrow.newPage();
  watch(np, "dark-320");
  await np.goto(URL, { waitUntil: "networkidle" });
  await np.waitForTimeout(1500);
  await shot(np, "20-dark-320");

  // Desktop: the tab bar becomes a row of pills under the app bar.
  const desk = await browser.newContext({ viewport: { width: 1280, height: 900 }, storageState });
  const dp = await desk.newPage();
  watch(dp, "desktop");
  await dp.goto(URL, { waitUntil: "networkidle" });
  await dp.waitForTimeout(1500);
  await shot(dp, "30-desktop", false);

  await browser.close();

  const unique = [...new Set(errs)];
  console.log(`\nscreenshots -> ${OUT}`);
  if (unique.length) {
    console.log("\nERRORS (treat as a failure):");
    unique.forEach((e) => console.log("  " + e));
    process.exitCode = 1;
  } else {
    console.log("no page/console/HTTP errors");
  }
})();
