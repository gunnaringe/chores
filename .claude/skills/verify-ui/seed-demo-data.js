#!/usr/bin/env node
// Seeds a family, one or more children, a repeating task, and completions
// spread across several months (crossing a couple of year boundaries) so
// screenshot.js — or any custom verify-ui flow — has something worth looking
// at, instead of an empty onboarding screen or a single flat month of data.
//
//   node .claude/skills/verify-ui/seed-demo-data.js \
//     --url http://localhost:8080/ --children "Anna,Erik"
//
// Logs in through cmd/devauth as "Test Parent" (see the run-local skill).
// If the database is fresh, creates a family; otherwise seeds into whatever
// family that account already belongs to. Prints the created ids as JSON.

const { execSync } = require("child_process");

const arg = (name, fallback) => {
  const i = process.argv.indexOf("--" + name);
  return i !== -1 && process.argv[i + 1] ? process.argv[i + 1] : fallback;
};

const URL = arg("url", "http://localhost:8080/");
const PW = arg("playwright", "/opt/node22/lib/node_modules/playwright");
const FAMILY_NAME = arg("family", "The Testsons");
const CHILDREN = arg("children", "Kid").split(",").map((s) => s.trim()).filter(Boolean);
const TASK_TITLE = arg("task", "Dishes");
const PRICE_CENTS = Number(arg("price-cents", "500"));

// Version-pinned directory name, so glob rather than hardcode.
const CHROME = execSync("ls -d /opt/pw-browsers/chromium-*/chrome-linux/chrome | head -1")
  .toString()
  .trim();

const { chromium } = require(PW);

// Relative to "now" rather than fixed calendar dates, so this keeps landing
// in the past — and still crossing a year boundary or two — no matter when
// it's run. Balance's monthly breakdown only shows its year-heading behavior
// with data that spans multiple calendar years.
function monthsAgo(n, day = 5) {
  const now = new Date();
  const d = new Date(now.getFullYear(), now.getMonth() - n, day);
  const pad = (x) => String(x).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}
const COMPLETION_OFFSETS = [0, 1, 14, 15, 26]; // this month, last month, then two year boundaries back

(async () => {
  const browser = await chromium.launch({
    executablePath: CHROME,
    args: [
      ...(process.env.HTTPS_PROXY ? ["--proxy-server=" + process.env.HTTPS_PROXY] : []),
      "--ignore-certificate-errors",
    ],
  });
  const page = await (await browser.newContext()).newPage();

  await page.goto(URL, { waitUntil: "networkidle" });
  const loginBtn = page.locator("#login-btn");
  if (await loginBtn.count()) {
    await loginBtn.click();
    await page.waitForLoadState("networkidle");
    await page.click('a:has-text("Test Parent")');
    await page.waitForLoadState("networkidle");
  }
  await page.waitForTimeout(1000);

  if (await page.locator("#onboard-family-name").count()) {
    await page.fill("#onboard-family-name", FAMILY_NAME);
    await page.click("#onboard-create-btn");
    await page.waitForLoadState("networkidle");
    await page.waitForTimeout(800);
  }

  const result = await page.evaluate(
    async ({ children, taskTitle, priceCents, dates }) => {
      const familyId = state.familyId;
      const childIds = [];
      for (const name of children) {
        const child = await call("CreateUser", { familyId, name, role: "USER_ROLE_CHILD" });
        childIds.push(child.user.id);
      }
      const task = await call("CreateTask", {
        familyId,
        title: taskTitle,
        schedule: { cron: { expression: "0 0 * * *" } },
        price: { cents: priceCents },
        childIds,
      });
      const taskId = task.task.id;
      for (const childId of childIds) {
        for (const dueDate of dates) {
          await call("CompleteTask", { taskId, childId, dueDate });
        }
      }
      return { familyId, childIds, taskId, dates };
    },
    {
      children: CHILDREN,
      taskTitle: TASK_TITLE,
      priceCents: PRICE_CENTS,
      dates: COMPLETION_OFFSETS.map((n) => monthsAgo(n)),
    }
  );

  await browser.close();
  console.log(JSON.stringify(result, null, 2));
})().catch((e) => {
  console.error("FAILED:", e);
  process.exit(1);
});
