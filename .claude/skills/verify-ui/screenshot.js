#!/usr/bin/env node
// Drives the Chores frontend in Chromium and writes screenshots of every tab
// at phone size, plus dark mode, a 320px screen and desktop.
//
// Logs in through cmd/devauth as "Test Parent", so the app must be running
// with AUTH0_DOMAIN pointed at devauth (see the run-local skill).
//
//   node .claude/skills/verify-ui/screenshot.js [--out /tmp/shots] [--theme hacker] [--lang nb]
//
// Browser/Playwright discovery and the other flags live in lib.js.

const { open, OUT } = require("./lib");

const arg = (name) => {
  const i = process.argv.indexOf("--" + name);
  return i !== -1 ? process.argv[i + 1] : undefined;
};
const prefs = { theme: arg("theme"), lang: arg("lang") };

(async () => {
  // Log in once, then reuse the session for every other viewport.
  const phone = await open({ ...prefs, label: "phone" });
  await phone.shot("00-login");
  await phone.login();
  const storageState = await phone.storageState();

  // Whatever tabs this login actually has — a child sees only two.
  const tabs = await phone.tabs();
  if (!tabs.length) {
    await phone.shot("01-onboarding");
    console.log("No tab bar — the account has no family yet; showing onboarding only.");
  }
  for (const tab of tabs) {
    await phone.tab(tab);
    await phone.shot(`10-phone-${tab}`);
  }

  // Dark mode + a 320px screen, where layout breaks show up first.
  const narrow = await open({ ...prefs, device: "iPhone SE", colorScheme: "dark", storageState, label: "dark-320" });
  await narrow.page.waitForTimeout(800);
  await narrow.shot("20-dark-320");

  // Desktop: the tab bar becomes a row of pills under the app bar.
  const desk = await open({ ...prefs, viewport: { width: 1280, height: 900 }, storageState, label: "desktop" });
  await desk.page.waitForTimeout(800);
  await desk.shot("30-desktop", null, { fullPage: false });

  console.log(`\nscreenshots -> ${OUT}`);
  await desk.done();
})();
