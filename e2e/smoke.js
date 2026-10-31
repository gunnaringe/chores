// End-to-end smoke test: the one path every family uses, driven in a real
// browser against a running dev server (scripts/e2e.sh starts one on a
// fresh database and runs this). The Go tests cover the API; this covers
// what they can't see — that app.js actually renders, that a tap reaches
// the server, and that the Content-Security-Policy doesn't block anything
// (a blocked script or handler shows up as a console error, which fails
// the run like any other).
//
//   scripts/e2e.sh
//
// Uses the verify-ui helpers for the browser, the devauth login and error
// collection; see .claude/skills/verify-ui/SKILL.md for where Chromium and
// Playwright come from.
const path = require("path");
const { open, apiLogin, done } = require(path.join(__dirname, "..", ".claude", "skills", "verify-ui", "lib.js"));

const TASK = "Water the plants";
const CHILD = "Ada";
const PRICE_CENTS = 750;

let failures = 0;
function check(ok, what) {
  console.log(`${ok ? "ok  " : "FAIL"} ${what}`);
  if (!ok) failures++;
}

const row = (page) => page.locator(".occurrence-row", { hasText: TASK }).first();

(async () => {
  // Logged out: the welcome page and the privacy policy.
  const welcome = await open({});
  check((await welcome.page.locator("#login-btn").innerText()).trim() !== "", "welcome page renders its login button");
  await welcome.page.goto(new URL("/privacy.html", welcome.page.url()).href, { waitUntil: "networkidle" });
  check((await welcome.page.locator("h1").first().innerText()).trim() !== "", "privacy page renders");

  // Seed through the API, as devauth's Test Parent — the same identity the
  // browser logs in as below.
  const api = await apiLogin();
  const { family } = await api.call("CreateFamily", { name: "E2E Family" });
  const { user: child } = await api.call("CreateUser", { familyId: family.id, name: CHILD, role: "USER_ROLE_CHILD" });
  await api.call("CreateTask", {
    familyId: family.id,
    title: TASK,
    schedule: { cron: { expression: "0 0 * * *" } },
    price: { cents: PRICE_CENTS },
    childIds: [child.id],
  });
  const { dashboardKey } = await api.call("SetupDashboard", { familyId: family.id });

  // Parent: tick the chore off on Today, and it stays ticked after a reload.
  const s = await open({ storageState: await welcome.storageState() });
  await s.login();
  check(await row(s.page).isVisible(), "Today lists the chore");
  await row(s.page).locator(".checkbtn").click();
  await s.page.waitForSelector(".occurrence-row.done", { timeout: 5000 }).catch(() => {});
  check(await row(s.page).evaluate((el) => el.classList.contains("done")), "tapping the tick marks it done");
  await s.page.reload({ waitUntil: "networkidle" });
  check(await row(s.page).evaluate((el) => el.classList.contains("done")), "done survives a reload");

  const { summaries } = await api.call("ListChildSummaries", { familyId: family.id });
  const summary = (summaries || []).find((x) => x.child?.id === child.id);
  const balance = Number(summary?.balance?.cents ?? NaN); // int64 arrives as a JSON string
  check(balance === PRICE_CENTS, `balance is the chore's price (${balance} cents)`);

  // Kiosk: the dashboard key alone, no login, can untick it again.
  const kiosk = await open({ path: `/dashboard?key=${encodeURIComponent(dashboardKey)}` });
  await kiosk.page.waitForSelector(".occurrence-row", { timeout: 5000 }).catch(() => {});
  check(await row(kiosk.page).isVisible(), "kiosk dashboard lists the chore");
  await row(kiosk.page).locator(".checkbtn").click();
  await kiosk.page.waitForTimeout(800);
  check(!(await row(kiosk.page).evaluate((el) => el.classList.contains("done"))), "kiosk can untick it");

  await done();
  if (failures) {
    console.log(`\n${failures} check(s) failed`);
    process.exitCode = 1;
  }
})().catch(async (e) => {
  console.error(e);
  await done();
  process.exitCode = 1;
});
