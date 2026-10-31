// Leaf helpers with no imports of their own, so any module can use them
// without joining an import cycle: money and currency, theme, dates,
// el()/escapeHtml(), task icons and classification. Keep it that way —
// anything that needs `state` or render() belongs somewhere else.

const money = (cents) => {
  const n = Number(cents || 0);
  return (n / 100).toLocaleString(localeTag(), { minimumFractionDigits: 2, maximumFractionDigits: 2 });
};

// ---- currency (device-level display preference) -----------------------
//
// Purely cosmetic, like the language choice: which symbol (if any)
// formatMoney() prefixes onto a number. Never sent to the server and never
// affects what's stored or compared — amounts stay integer cents
// everywhere else, exactly as before. Defaults to "" (no symbol) rather
// than assuming a currency, since this is a hobby project that could be
// run for any family in any currency — see CURRENCIES below for the
// curated list a device can pick from instead.
export const CURRENCIES = [
  { code: "", symbol: "" },
  { code: "NOK", symbol: "kr" },
  { code: "SEK", symbol: "kr" },
  { code: "DKK", symbol: "kr" },
  { code: "EUR", symbol: "€" },
  { code: "USD", symbol: "$" },
  { code: "GBP", symbol: "£" },
];

export function getCurrency() {
  return localStorage.getItem("chores.currency") || "";
}
export function setCurrency(code) {
  if (code) localStorage.setItem("chores.currency", code);
  else localStorage.removeItem("chores.currency");
}
function currencySymbol() {
  const c = CURRENCIES.find((c) => c.code === getCurrency());
  return c ? c.symbol : "";
}

// Every amount shown in the UI goes through this rather than money()
// directly, so the currency preference only has to be threaded through in
// one place. With no currency picked (the default) it's just the number.
function formatMoney(cents) {
  const symbol = currencySymbol();
  return symbol ? `${symbol} ${money(cents)}` : money(cents);
}

// Formats a Money message from the API — task.price, occurrence.amount, and
// the ChildSummary figures. Amounts come off the wire as protobuf int64,
// which protojson encodes as a *string*, so formatMoney's Number() coercion
// is doing real work rather than being defensive.
export function formatAmount(m) {
  return formatMoney(m && m.cents);
}

// Appends "(symbol)" to a field label when a currency is set — used on the
// two inputs where someone types an amount (task price, payout amount) so
// they read consistently with however amounts are displayed elsewhere.
export function labelWithCurrencyUnit(label) {
  const symbol = currencySymbol();
  return symbol ? `${label} (${symbol})` : label;
}

// ---- theme (device-level display preference) ---------------------------
//
// Same shape as currency and language: a client-only preference, never sent
// to the server. "" (the default) means "follow the OS", which is exactly
// what the app did before this setting existed — see app.css, where the
// dark tokens under prefers-color-scheme only apply to :root:not([data-theme]).
// index.html and login.html carry a standalone copy of applyTheme() in their
// <head>, since it has to run before first paint (like the icons-loading
// script) and app/ isn't loaded that early; keep the three in step.
const THEMES = [
  "light",
  "dark",
  "retro",
  "playful",
  "hacker",
  "high-contrast",
  "fairytale-forest",
  "pirate-map",
  "underwater",
  "robot-workshop",
  "cabin",
  "monster-lab",
  "dino-park",
  "mission-base",
  "block-world",
  "cozy-home",
];

export function getTheme() {
  let stored = localStorage.getItem("chores.theme");
  // Space Invaders was folded into Hacker; carry anyone who picked it over.
  if (stored === "space-invaders") stored = "hacker";
  return THEMES.includes(stored) ? stored : "";
}
export function setTheme(theme) {
  if (THEMES.includes(theme)) localStorage.setItem("chores.theme", theme);
  else localStorage.removeItem("chores.theme");
  applyTheme();
}

// Sets data-theme (or clears it, for "follow the OS") and syncs the browser
// chrome color to match. Reads --bg back off the CSSOM rather than
// hardcoding per-theme hex values here, so app.css stays the only place a
// theme's palette is actually defined.
export function applyTheme() {
  const theme = getTheme();
  if (theme) document.documentElement.setAttribute("data-theme", theme);
  else document.documentElement.removeAttribute("data-theme");
  const meta = document.getElementById("theme-color-meta");
  if (meta) meta.setAttribute("content", getComputedStyle(document.documentElement).getPropertyValue("--bg").trim());
}

export const todayStr = () => {
  const d = new Date();
  const tz = d.getTimezoneOffset();
  const local = new Date(d.getTime() - tz * 60000);
  return local.toISOString().slice(0, 10);
};

// One day before a "YYYY-MM-DD" string, computed from its Y/M/D components
// (not by parsing it as a Date, which would be UTC-midnight and risk an
// off-by-one in a timezone behind UTC — same reasoning as formatDateStr).
export function dayBeforeStr(dateStr) {
  const [y, m, d] = dateStr.split("-").map(Number);
  const dt = new Date(y, m - 1, d - 1);
  const tz = dt.getTimezoneOffset();
  const local = new Date(dt.getTime() - tz * 60000);
  return local.toISOString().slice(0, 10);
}

// Monday of the current calendar week, matching the Monday-first week the
// UI shows elsewhere (as opposed to a rolling 7-day window).
export function mondayOfWeekStr() {
  const d = new Date();
  const daysSinceMonday = (d.getDay() + 6) % 7; // Mon=0..Sun=6
  d.setDate(d.getDate() - daysSinceMonday);
  const tz = d.getTimezoneOffset();
  const local = new Date(d.getTime() - tz * 60000);
  return local.toISOString().slice(0, 10);
}

// Display order is Monday-first; `code` stays the standard cron
// day-of-week numbering (0=Sunday..6=Saturday) that scheduling.go expects,
// so this only reorders how days are shown, not how schedules are stored.
export function DOW() {
  return [
    { code: 1, label: t("days.mon") },
    { code: 2, label: t("days.tue") },
    { code: 3, label: t("days.wed") },
    { code: 4, label: t("days.thu") },
    { code: 5, label: t("days.fri") },
    { code: 6, label: t("days.sat") },
    { code: 0, label: t("days.sun") },
  ];
}

// Formats a "YYYY-MM-DD" date string for display. Deliberately avoids
// `new Date("YYYY-MM-DD")`, which parses as UTC midnight and can render as
// the previous day in a timezone behind UTC; constructing from the Y/M/D
// components directly keeps it a local-time date with no shift.
export function formatDateStr(s) {
  if (!s) return "";
  const [y, m, d] = s.split("-").map(Number);
  return new Date(y, m - 1, d).toLocaleDateString(localeTag());
}

// Formats the month of a "YYYY-MM" string (as returned by
// ListMonthlyEarnings) on its own, e.g. "August" — the year is shown
// separately, as a heading, whenever it isn't the current one; see
// renderMonthlyEarningsCard. Same reasoning as formatDateStr: build the
// Date from components rather than parsing the string directly.
export function formatMonthName(s) {
  if (!s) return "";
  const [y, m] = s.split("-").map(Number);
  return new Date(y, m - 1, 1).toLocaleDateString(localeTag(), { month: "long" });
}

// Describes a task's repeat rule (one-off date, weekly pattern, or raw
// cron) for display in the task list.
export function repeatLabel(t_) {
  const sched = t_.schedule || {};
  if (sched.once) {
    return t("taskList.onceOn", { date: formatDateStr(sched.once.date) });
  }
  if (sched.weekly) {
    const days = sched.weekly.daysOfWeek || [];
    const dow = DOW();
    const dayLabel =
      days.length && days.length < 7 ? dow.filter((d) => days.includes(d.code)).map((d) => d.label).join(", ") : t("taskList.everyDay");
    const interval = Number(sched.weekly.intervalWeeks || 1);
    return interval > 1 ? t("taskList.everyNWeeks", { n: interval, days: dayLabel }) : dayLabel;
  }
  if (sched.cron) {
    return sched.cron.expression;
  }
  return "";
}

// ---- rendering -----------------------------------------------------

export function el(html) {
  const tpl = document.createElement("template");
  tpl.innerHTML = html.trim();
  return tpl.content.firstElementChild;
}

export function escapeHtml(s) {
  const d = document.createElement("div");
  d.textContent = s;
  return d.innerHTML;
}

// Material Symbols are rendered as ligature text content (not a class
// name), so escapeHtml() alone already makes them injection-safe; this
// whitelist is just hygiene, matching how the names actually look
// (lowercase, underscores).
export function materialIconName(value) {
  const cleaned = String(value || "")
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9_]/g, "");
  return cleaned || "star";
}

export function isOptionalTask(task) {
  return task.classification === "TASK_CLASSIFICATION_OPTIONAL";
}

// The mandatory/optional badge. Shared so the Tasks list and History label a
// task identically — two hand-rolled copies drifted apart the moment one of
// them was touched.
export function classificationPillHtml(task) {
  const optional = isOptionalTask(task);
  return `<span class="pill ${optional ? "optional" : "mandatory"}">${escapeHtml(
    optional ? t("taskList.optional") : t("taskList.mandatory")
  )}</span>`;
}

// Today's occurrences split into what has to happen and what is up for grabs.
// Groups with nothing in them are dropped, so a child with only required
// chores doesn't get an empty "Can do" heading.
export function groupOccurrencesByClassification(occurrences) {
  return [
    { label: t("today.mustDo"), items: occurrences.filter((o) => !isOptionalTask(o)) },
    { label: t("today.canDo"), items: occurrences.filter((o) => isOptionalTask(o)) },
  ].filter((group) => group.items.length);
}

// The leading tile in a task row: the task's own icon on a tinted square,
// falling back to a generic one so every row lines up at the same left
// edge whether or not an icon was ever picked for it.
export function taskIconHtml(task) {
  const icon = task.icon || {};
  if (icon.value && icon.type !== "ICON_TYPE_MATERIAL_SYMBOLS") {
    // An icon predating Material Symbols becoming the only supported type
    // (an emoji, or a Font Awesome name) — rendered as plain text.
    return `<span class="task-icon">${escapeHtml(icon.value)}</span>`;
  }
  const name = icon.value ? materialIconName(icon.value) : "task_alt";
  return `<span class="task-icon"><span class="material-symbols-outlined">${escapeHtml(name)}</span></span>`;
}

// Up to two letters, for the round avatar that stands in for a family
// member wherever one is named — cheaper to recognise at a glance than
// reading the name, which is the whole point on a small screen.
function initials(name) {
  const parts = String(name || "")
    .trim()
    .split(/\s+/)
    .filter(Boolean);
  if (!parts.length) return "?";
  const first = parts[0][0];
  const last = parts.length > 1 ? parts[parts.length - 1][0] : "";
  return (first + last).toUpperCase();
}

export function avatarHtml(user, extraClass) {
  const cls = user && user.role === "USER_ROLE_CHILD" ? "child" : "parent";
  return `<span class="avatar ${cls} ${extraClass || ""}">${escapeHtml(initials(user ? user.name : ""))}</span>`;
}
