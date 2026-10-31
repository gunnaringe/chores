// The kiosk dashboard at /dashboard: its key prompt, its own device-level
// settings, and its auto-refresh.

import { call } from "./api.js";
import { AUTO_REFRESH_MS, state, withError } from "./state.js";
import { el, escapeHtml, todayStr } from "./helpers.js";
import { render, renderLangSwitcher, renderThemeSwitcher } from "./shell.js";
import { renderChildCard, renderTodayTab } from "./today.js";

// ---- Dashboard mode: kiosk view of the Today tab, no login -----------------------------------------------------
//
// Reached at /dashboard, authorized by a per-family secret key instead of a
// login — meant for a wall-mounted tablet or shared screen showing every
// child's status for the day, with the same tap-to-complete checkboxes the
// parent Today tab has. The key comes in via ?key=... the first time (then
// stored locally and stripped from the URL) or can be typed directly.

const DASHBOARD_KEY_STORAGE = "chores.dashboardKey";

export function isDashboardRoute() {
  return window.location.pathname === "/dashboard";
}

export async function loadDashboardData() {
  const [summariesResp, occResp] = await Promise.all([
    call("ListChildSummaries", {}),
    call("ListTaskOccurrences", { startDate: todayStr(), endDate: todayStr() }),
  ]);
  state.summaries = summariesResp.summaries || [];
  state.occurrences = occResp.occurrences || [];
}

let dashboardAutoRefreshStarted = false;
function startDashboardAutoRefresh() {
  if (dashboardAutoRefreshStarted) return;
  dashboardAutoRefreshStarted = true;
  setInterval(() => {
    if (state.dashboardKey) withError(loadDashboardData);
  }, AUTO_REFRESH_MS);
}

// ---- Dashboard mode: the kiosk's own device-level display settings -----
//
// Same shape as theme/currency/lang — client-only, never sent to the
// server, read straight off localStorage rather than cached in `state`.
// Scoped to the kiosk screen itself (there's no login here to hang a
// preference off), reachable from the kiosk's own gear icon rather than
// the family's Settings tab.

const DASHBOARD_VIEW_MODE_STORAGE = "chores.dashboardViewMode";
function getDashboardViewMode() {
  return localStorage.getItem(DASHBOARD_VIEW_MODE_STORAGE) === "single" ? "single" : "all";
}
function setDashboardViewMode(mode) {
  if (mode === "single") localStorage.setItem(DASHBOARD_VIEW_MODE_STORAGE, "single");
  else localStorage.removeItem(DASHBOARD_VIEW_MODE_STORAGE);
}

const DASHBOARD_SELECTED_CHILD_STORAGE = "chores.dashboardSelectedChildId";
function getDashboardSelectedChildId() {
  return localStorage.getItem(DASHBOARD_SELECTED_CHILD_STORAGE) || null;
}
export function setDashboardSelectedChildId(childId) {
  if (childId) localStorage.setItem(DASHBOARD_SELECTED_CHILD_STORAGE, childId);
  else localStorage.removeItem(DASHBOARD_SELECTED_CHILD_STORAGE);
}

// Which children the kiosk shows at all — stored as the exclusions rather
// than the inclusions, so a child added to the family later defaults to
// shown instead of silently missing from a stale allow-list.
const DASHBOARD_EXCLUDED_CHILDREN_STORAGE = "chores.dashboardExcludedChildIds";
function getDashboardExcludedChildIds() {
  try {
    const raw = JSON.parse(localStorage.getItem(DASHBOARD_EXCLUDED_CHILDREN_STORAGE) || "[]");
    return Array.isArray(raw) ? raw : [];
  } catch (_) {
    return [];
  }
}
function setDashboardChildIncluded(childId, included) {
  const excluded = new Set(getDashboardExcludedChildIds());
  if (included) excluded.delete(childId);
  else excluded.add(childId);
  if (excluded.size) localStorage.setItem(DASHBOARD_EXCLUDED_CHILDREN_STORAGE, JSON.stringify([...excluded]));
  else localStorage.removeItem(DASHBOARD_EXCLUDED_CHILDREN_STORAGE);
}

function dashboardIncludedSummaries() {
  const excluded = getDashboardExcludedChildIds();
  return state.summaries.filter((s) => !excluded.includes(s.child.id));
}

// The kiosk's main screen: every included child's card (unchanged from
// before this setting existed) or, in single-child mode, just the selected
// one with a switcher chip standing in for its usual avatar+name.
export function renderDashboardTodayView() {
  const included = dashboardIncludedSummaries();
  if (getDashboardViewMode() === "single") return renderDashboardSingleChildView(included);
  return renderTodayTab(included);
}

function renderDashboardSingleChildView(included) {
  const wrap = el(`<div></div>`);
  if (!included.length) {
    wrap.appendChild(el(`<div class="card"><p class="empty">${escapeHtml(t("accounting.noChildren"))}</p></div>`));
    return wrap;
  }
  let selected = included.find((s) => s.child.id === getDashboardSelectedChildId());
  if (!selected) {
    // Nothing stored yet, or the stored id fell out of the included set
    // (excluded from Settings, or removed from the family) — fall back to
    // the first included child and remember that choice.
    selected = included[0];
    setDashboardSelectedChildId(selected.child.id);
  }
  wrap.appendChild(renderChildCard(selected, { switchOptions: included.map((s) => s.child) }));
  return wrap;
}

export function renderDashboardSettingsScreen() {
  const wrap = el(`<div></div>`);
  wrap.appendChild(renderDashboardViewModeSetting());
  wrap.appendChild(renderDashboardIncludedChildrenSetting());
  wrap.appendChild(renderThemeSwitcher());
  wrap.appendChild(renderLangSwitcher());
  return wrap;
}

function renderDashboardViewModeSetting() {
  const current = getDashboardViewMode();
  const card = el(`
    <div class="card">
      <h2>${escapeHtml(t("dashboard.viewModeHeading"))}</h2>
      <p class="hint" style="margin:0 0 10px;">${escapeHtml(t("dashboard.viewModeHint"))}</p>
      <select id="dashboard-view-mode" aria-label="${escapeHtml(t("dashboard.viewModeHeading"))}">
        <option value="all" ${current === "all" ? "selected" : ""}>${escapeHtml(t("dashboard.viewModeAll"))}</option>
        <option value="single" ${current === "single" ? "selected" : ""}>${escapeHtml(t("dashboard.viewModeSingle"))}</option>
      </select>
    </div>
  `);
  card.querySelector("#dashboard-view-mode").addEventListener("change", (e) => {
    setDashboardViewMode(e.target.value);
    render();
  });
  return card;
}

// Same tap-to-toggle chip list as the task editor's "which children is
// this assigned to" picker (see .toggle-chip in app.css) — a checked chip
// means shown on the kiosk.
function renderDashboardIncludedChildrenSetting() {
  const card = el(`<div class="card"><h2>${escapeHtml(t("dashboard.includeHeading"))}</h2></div>`);
  card.appendChild(el(`<p class="hint" style="margin:0 0 10px;">${escapeHtml(t("dashboard.includeHint"))}</p>`));
  if (!state.summaries.length) {
    card.appendChild(el(`<p class="empty">${escapeHtml(t("accounting.noChildren"))}</p>`));
    return card;
  }
  const excluded = getDashboardExcludedChildIds();
  const checksWrap = el(`<div class="chip-group"></div>`);
  state.summaries.forEach((s) => {
    const label = el(`<label class="toggle-chip">
      <input type="checkbox" data-child-id="${s.child.id}" ${excluded.includes(s.child.id) ? "" : "checked"} />${escapeHtml(s.child.name)}
    </label>`);
    label.querySelector("input").addEventListener("change", (e) => {
      setDashboardChildIncluded(s.child.id, e.target.checked);
      render();
    });
    checksWrap.appendChild(label);
  });
  card.appendChild(checksWrap);
  return card;
}

// Shared by the boot sequence (a stored or ?key=-supplied key) and the
// key-prompt form (a typed one). On success the key is what render() then
// treats as "unlocked"; on failure it's dropped so the prompt reappears
// instead of showing an empty dashboard with just an error banner.
async function tryDashboardKey(key) {
  state.dashboardKey = key;
  try {
    state.error = null;
    await loadDashboardData();
    localStorage.setItem(DASHBOARD_KEY_STORAGE, key);
    startDashboardAutoRefresh();
  } catch (e) {
    state.dashboardKey = null;
    localStorage.removeItem(DASHBOARD_KEY_STORAGE);
    state.error = e.message || String(e);
  }
  render();
}

// Undocumented: a parent who copies the whole dashboard URL (the copy
// button next to it in Settings) rather than just the key can paste that
// straight into this field too, instead of having to go pull the ?key=
// value back out of it themselves first.
function extractDashboardKey(raw) {
  try {
    return new URL(raw).searchParams.get("key") || raw;
  } catch {
    return raw;
  }
}

export function renderDashboardKeyPrompt() {
  const wrap = el(`<div class="hero" style="text-align:left;"></div>`);
  wrap.appendChild(el(`
    <div style="text-align:center;">
      <img src="/icons/logo.png" alt="" width="80" height="80" class="logo" />
      <h1>${escapeHtml(appName())}</h1>
      <p>${escapeHtml(t("dashboard.enterKeyPrompt"))}</p>
    </div>
  `));
  const form = el(`
    <div class="card">
      <div class="field">
        <label>${escapeHtml(t("dashboard.keyLabel"))}</label>
        <input type="text" id="dashboard-key-input" class="input-full" autocomplete="off" spellcheck="false" />
      </div>
      <button class="block" id="dashboard-key-submit">${escapeHtml(t("dashboard.unlock"))}</button>
    </div>
  `);
  const submit = () =>
    withError(async () => {
      const raw = form.querySelector("#dashboard-key-input").value.trim();
      if (!raw) throw new Error(t("dashboard.keyRequired"));
      await tryDashboardKey(extractDashboardKey(raw));
    });
  form.querySelector("#dashboard-key-submit").addEventListener("click", submit);
  form.querySelector("#dashboard-key-input").addEventListener("keydown", (e) => {
    if (e.key === "Enter") submit();
  });
  wrap.appendChild(form);
  return wrap;
}

export async function bootDashboard() {
  state.dashboardMode = true;
  const params = new URLSearchParams(window.location.search);
  const keyFromQuery = params.get("key");
  if (keyFromQuery) {
    // Don't leave the secret sitting in the URL (browser history, anyone
    // glancing at the address bar on a shared screen) once it's stored.
    window.history.replaceState({}, "", "/dashboard");
  }
  const key = keyFromQuery || localStorage.getItem(DASHBOARD_KEY_STORAGE);
  if (key) {
    await tryDashboardKey(key);
  } else {
    render();
  }
}
