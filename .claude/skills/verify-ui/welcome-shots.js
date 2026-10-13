#!/usr/bin/env node
// Regenerates the welcome page's screenshots, web/screenshots/<screen>-<lang>.webp
// (Today, Tasks and Balance, in English and Bokmål), from a demo family it
// seeds itself. Rerun it whenever those screens change:
//
//   scripts/dev-up.sh --fresh          # restart so web/ edits are live
//   node .claude/skills/verify-ui/welcome-shots.js
//
// Each language gets its own family ("The Smiths" / "Familien Hansen") with
// task titles in that language, since those are data rather than UI
// strings. Captured at 390x720 and 2x, the size the page frames them at, and
// re-encoded to WebP through Chromium's own canvas (there's no image tooling
// in the sandbox). Read the files back afterwards; this verifies nothing.

const fs = require("fs");
const path = require("path");
const { open, done, apiLogin } = require("./lib");

const OUT_DIR = path.join(__dirname, "..", "..", "..", "web", "screenshots");

// Ada has four tasks today: two must-dos (one done, one with a description,
// to show both) and two optional ones (one done).
const LANGS = {
  en: {
    family: "The Smiths",
    currency: "USD",
    locale: "en-US",
    child: "Ada",
    payoutNote: "Weekly allowance",
    tasks: [
      { title: "Make the bed", icon: "bed", cents: 50, done: true },
      { title: "Do the dishes", icon: "dishwasher_gen", cents: 100, desc: "Empty the dishwasher, then load what's in the sink." },
      { title: "Vacuum the car", icon: "local_car_wash", cents: 250, optional: true },
      { title: "Water the plants", icon: "potted_plant", cents: 150, optional: true, done: true },
    ],
  },
  nb: {
    family: "Familien Hansen",
    currency: "NOK",
    locale: "nb-NO",
    child: "Ada",
    payoutNote: "Ukepenger",
    tasks: [
      { title: "Re senga", icon: "bed", cents: 500, done: true },
      { title: "Ta oppvasken", icon: "dishwasher_gen", cents: 1000, desc: "Tøm oppvaskmaskinen, og sett inn det som står i vasken." },
      { title: "Støvsuge bilen", icon: "local_car_wash", cents: 2500, optional: true },
      { title: "Vanne blomstene", icon: "potted_plant", cents: 1500, optional: true, done: true },
    ],
  },
};

const pad = (n) => String(n).padStart(2, "0");
const dateStr = (d) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
const daysAgo = (n) => {
  const d = new Date();
  d.setDate(d.getDate() - n);
  return dateStr(d);
};

async function seed(api, spec) {
  // The demo parent must be in this family only, or the app bar grows a
  // family switcher; so start from no families, and main() deletes each
  // family again once it's captured.
  const before = await api.call("GetMyMembership", {});
  if ((before.memberships || []).length) {
    throw new Error("Test Parent already has a family; start from an empty database (scripts/dev-up.sh --fresh)");
  }
  await api.call("CreateFamily", { name: spec.family, parentName: "Test Parent" });
  const after = await api.call("GetMyMembership", {});
  const known = new Set((before.memberships || []).map((m) => m.family.id));
  const membership = after.memberships.find((m) => !known.has(m.family.id));
  const familyId = membership.family.id;

  const { user: child } = await api.call("CreateUser", { familyId, name: spec.child, role: "USER_ROLE_CHILD" });
  const doneToday = [];
  for (const [i, t] of spec.tasks.entries()) {
    const { task } = await api.call("CreateTask", {
      familyId,
      title: t.title,
      description: t.desc || "",
      icon: { type: "ICON_TYPE_MATERIAL_SYMBOLS", value: t.icon },
      classification: t.optional ? "TASK_CLASSIFICATION_OPTIONAL" : "TASK_CLASSIFICATION_MANDATORY",
      price: { cents: t.cents },
      schedule: { cron: { expression: "0 0 * * *" } },
      childIds: [child.id],
    });
    if (t.done) doneToday.push(task.id);
    // Last week's history. Only a week: the API records every completion
    // as done *now*, so anything older would still land in Balance's "last
    // 7 days" total and make it larger than the month. Deterministic, so a
    // rerun produces the same screenshots.
    for (let d = 1; d <= 6; d++) {
      if ((d * 7 + i * 3) % 5 < (t.optional ? 1 : 3)) {
        await api.call("CompleteTask", { taskId: task.id, childId: child.id, dueDate: daysAgo(d) });
      }
    }
  }
  // Paid out up to yesterday, so the outstanding balance is today's work.
  await api.call("CreatePayout", { childId: child.id, fullPayout: true, note: spec.payoutNote });
  for (const taskId of doneToday) await api.call("CompleteTask", { taskId, childId: child.id, dueDate: daysAgo(0) });
  return { familyId, userId: membership.user.id };
}

// PNG from Playwright -> WebP via the page's own canvas.
async function toWebp(page, png) {
  const dataUrl = await page.evaluate(async (b64) => {
    const img = new Image();
    img.src = "data:image/png;base64," + b64;
    await img.decode();
    const c = document.createElement("canvas");
    c.width = img.naturalWidth;
    c.height = img.naturalHeight;
    c.getContext("2d").drawImage(img, 0, 0);
    return c.toDataURL("image/webp", 0.88);
  }, png.toString("base64"));
  return Buffer.from(dataUrl.split(",")[1], "base64");
}

(async () => {
  const api = await apiLogin();
  for (const [lang, spec] of Object.entries(LANGS)) {
    const { familyId, userId } = await seed(api, spec);
    const s = await open({
      label: lang,
      lang,
      viewport: { width: 390, height: 720 },
      // The browser locale formats <input type="number">, so match it.
      context: { deviceScaleFactor: 2, isMobile: true, hasTouch: true, locale: spec.locale },
      storage: { "chores.currency": spec.currency, "chores.familyId": familyId, "chores.userId": userId },
    });
    await s.login();
    for (const [file, tab] of [["today", "home"], ["tasks", "tasks"], ["balance", "accounting"]]) {
      await s.tab(tab);
      await s.page.waitForTimeout(500);
      await s.page.mouse.move(0, 0);
      const webp = await toWebp(s.page, await s.page.screenshot());
      const out = path.join(OUT_DIR, `${file}-${lang}.webp`);
      fs.writeFileSync(out, webp);
      console.log(`${out} (${Math.round(webp.length / 1024)} KB)`);
    }
    await s.ctx.close();
    await api.call("DeleteFamily", { familyId });
  }
  await done();
})().catch((e) => {
  console.error("FAILED:", e);
  process.exit(1);
});
