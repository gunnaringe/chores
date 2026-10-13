// Shared plumbing for verify-ui flows: finding a browser, logging in through
// devauth, presetting theme/language, collecting errors, and screenshots.
// screenshot.js is built on it; so should any one-off check, rather than
// re-deriving all of this in a throwaway script each time:
//
//   const { open } = require("./lib");          // or an absolute path
//   (async () => {
//     const s = await open({ theme: "hacker", lang: "nb" });
//     await s.login();
//     await s.tab("tasks");
//     await s.page.click(".fab");
//     await s.page.fill("#task-icon-search", "trash");
//     await s.shot("icon-search", "#task-icon-results");
//     await s.done();                            // prints errors, sets exit code
//   })();
//
// Everything is overridable by flag or environment variable, so the same
// flow runs unchanged in the managed sandbox and on a local machine:
//   --url / CHORES_URL                 default http://localhost:8080/
//   --out / CHORES_SHOTS               default /tmp/shots
//   --chrome / CHORES_CHROME           browser binary (auto-detected)
//   --playwright / CHORES_PLAYWRIGHT   playwright or playwright-core dir (auto-detected)

const fs = require("fs");
const os = require("os");
const path = require("path");
const { execSync } = require("child_process");

const arg = (name, env, fallback) => {
  const i = process.argv.indexOf("--" + name);
  if (i !== -1 && process.argv[i + 1]) return process.argv[i + 1];
  return process.env[env] || fallback;
};

const URL = arg("url", "CHORES_URL", "http://localhost:8080/");
const OUT = arg("out", "CHORES_SHOTS", "/tmp/shots");

// Where a private playwright-core gets installed when nothing else is found
// (see the install hint below). Under ~/.cache rather than /tmp so it
// survives a reboot on a local machine.
const PW_CACHE = path.join(os.homedir(), ".cache", "chores-verify-ui");

function findChrome() {
  const explicit = arg("chrome", "CHORES_CHROME", "");
  if (explicit) return explicit;
  // The managed Claude Code image: a version-pinned directory, so glob.
  try {
    const pre = execSync("ls -d /opt/pw-browsers/chromium-*/chrome-linux/chrome 2>/dev/null | head -1").toString().trim();
    if (pre) return pre;
  } catch {}
  for (const p of ["/usr/bin/google-chrome", "/usr/bin/chromium", "/usr/bin/chromium-browser"]) {
    if (fs.existsSync(p)) return p;
  }
  return "";
}

function findPlaywright() {
  const explicit = arg("playwright", "CHORES_PLAYWRIGHT", "");
  const candidates = explicit
    ? [explicit]
    : [
        "/opt/node22/lib/node_modules/playwright", // managed Claude Code image
        path.join(PW_CACHE, "node_modules", "playwright-core"),
        "playwright-core",
        "playwright",
      ];
  for (const c of candidates) {
    try {
      return require(c);
    } catch {}
  }
  return null;
}

function tools() {
  const chrome = findChrome();
  const pw = findPlaywright();
  if (!chrome || !pw) {
    if (!chrome) console.error("No Chrome/Chromium found; pass --chrome or set CHORES_CHROME.");
    if (!pw) {
      console.error(
        "No Playwright found. Install a private copy once (downloads no browser):\n" +
          `  npm install --prefix ${PW_CACHE} playwright-core`
      );
    }
    process.exit(1);
  }
  return { chromium: pw.chromium, devices: pw.devices, chrome };
}

const errors = [];
let browser = null;

// Errors from every page opened through open(), deduplicated by done().
function watch(page, label) {
  page.on("pageerror", (e) => errors.push(`[${label}] PAGEERROR: ${e.message}`));
  page.on("console", (m) => {
    // devauth (the test login provider) has no favicon; that 404 is not the app's.
    if (m.type() !== "error" || m.location().url.endsWith("/favicon.ico")) return;
    errors.push(`[${label}] CONSOLE: ${m.text()} @ ${m.location().url}`);
  });
  page.on("response", (r) => {
    if (r.status() >= 400) errors.push(`[${label}] HTTP${r.status()} ${r.url()}`);
  });
  // The app makes no third-party requests (the privacy policy promises it),
  // so any request leaving the app's origin is a regression worth flagging.
  const origin = new globalThis.URL(URL).origin;
  page.on("request", (r) => {
    const u = r.url();
    if (/^https?:/.test(u) && new globalThis.URL(u).origin !== origin && !u.includes("localhost:9999")) {
      errors.push(`[${label}] THIRD-PARTY REQUEST ${u}`);
    }
  });
}

// Opens a new browser context and page. Options:
//   device       Playwright device name, default "iPhone 13"; or
//   viewport     {width, height} instead of a device (desktop)
//   colorScheme  "light" | "dark"
//   theme        a data-theme value ("hacker", "dark", …), preset in localStorage
//   lang         "en" | "nb" | "nn" | "sv", preset in localStorage
//   storageState reuse another session's login (from s.storageState())
//   label        prefix for error lines, default the device/viewport
//   path         where to go first, default "/"
async function open(opts = {}) {
  const { chromium, devices, chrome } = tools();
  if (!browser) {
    browser = await chromium.launch({
      executablePath: chrome,
      args: [
        ...(process.env.HTTPS_PROXY ? ["--proxy-server=" + process.env.HTTPS_PROXY] : []),
        "--ignore-certificate-errors",
      ],
    });
  }
  const base = opts.viewport ? { viewport: opts.viewport } : { ...devices[opts.device || "iPhone 13"] };
  const ctx = await browser.newContext({
    ...base,
    ...(opts.colorScheme ? { colorScheme: opts.colorScheme } : {}),
    ...(opts.storageState ? { storageState: opts.storageState } : {}),
  });
  if (opts.theme || opts.lang) {
    await ctx.addInitScript(([theme, lang]) => {
      if (theme) localStorage.setItem("chores.theme", theme);
      if (lang) localStorage.setItem("chores.lang", lang);
    }, [opts.theme || "", opts.lang || ""]);
  }
  const page = await ctx.newPage();
  watch(page, opts.label || opts.device || (opts.viewport ? `${opts.viewport.width}px` : "phone"));
  await page.goto(new globalThis.URL(opts.path || "/", URL).href, { waitUntil: "networkidle" });

  const s = {
    page,
    ctx,
    browser,
    // Logs in through devauth as `identity` (default "Test Parent"). A no-op
    // when already logged in (no #login-btn on the page).
    async login(identity = "Test Parent") {
      const btn = page.locator("#login-btn");
      if (await btn.count()) {
        await btn.click();
        await page.waitForLoadState("networkidle");
        await page.click(`a:has-text("${identity}")`);
        await page.waitForLoadState("networkidle");
      }
      await page.waitForTimeout(1000);
    },
    // The data-tab keys the tab bar has for this login (a child sees fewer).
    async tabs() {
      return page.locator(".tabbar button").evaluateAll((els) => els.map((e) => e.dataset.tab));
    },
    async tab(key) {
      await page.click(`.tabbar button[data-tab="${key}"]`);
      await page.waitForTimeout(700);
    },
    // Screenshot to OUT/<name>.png. `target` is optional: a selector or
    // locator to capture just that element (scrolled into view), otherwise
    // the page. opts.fullPage defaults to true for page shots — note that it
    // renders fixed bars mid-page (see the skill's "Reading the results").
    async shot(name, target, shotOpts = {}) {
      fs.mkdirSync(OUT, { recursive: true });
      const file = path.join(OUT, `${name}.png`);
      await page.waitForTimeout(300);
      // Playwright parks the pointer wherever it last clicked, which
      // photographs that control in its hover state.
      await page.mouse.move(0, 0);
      if (target) {
        const loc = typeof target === "string" ? page.locator(target).first() : target;
        await loc.scrollIntoViewIfNeeded();
        await page.mouse.move(0, 0);
        await loc.screenshot({ path: file });
      } else {
        await page.screenshot({ path: file, fullPage: shotOpts.fullPage !== false });
      }
      console.log(file);
      return file;
    },
    storageState: () => ctx.storageState(),
    // Closes the browser, prints the collected errors, and sets the exit
    // code. Call once, at the end, from the last session.
    done,
  };
  return s;
}

async function done() {
  if (browser) await browser.close();
  browser = null;
  const unique = [...new Set(errors)];
  if (unique.length) {
    console.log("\nERRORS (treat as a failure):");
    unique.forEach((e) => console.log("  " + e));
    process.exitCode = 1;
  } else {
    console.log("no page/console/HTTP errors");
  }
}

module.exports = { open, done, URL, OUT };
