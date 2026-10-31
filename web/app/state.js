// The `state` object and the setters that persist parts of it, loading
// family data from the API, and the background auto-refresh.

import { call } from "./api.js";
import { mondayOfWeekStr, todayStr } from "./helpers.js";
import { render } from "./shell.js";
import { loadDashboardData } from "./dashboard.js";

// ---- state -----------------------------------------------------------

export const state = {
  familyId: localStorage.getItem("chores.familyId") || null,
  userId: localStorage.getItem("chores.userId") || null,
  tab: "home",
  families: [],
  users: [],
  tasks: [],
  occurrences: [],
  summaries: [],
  payouts: [],
  error: null,
  auth: null,
  membership: null, // { bound, memberships: [{ user, family }] }
  invitations: [],
  editingTaskId: null,
  addingTask: false, // whether the task editor sheet is open on a new task
  pushConfig: null, // { vapidPublicKey }
  historyRecent: [], // completions from this Monday through today
  monthlyEarningsByChild: {}, // child id -> [{yearMonth, earned}], most recent first (Balance tab)
  historyLater: [], // accumulated older pages, oldest loaded last
  historyLaterOffset: 0,
  historyLaterHasMore: true,
  historyLaterLoaded: false,
  historySearchQuery: "",
  historySearchResults: null, // null = not searching; array once a search has run
  historySearchOffset: 0,
  historySearchHasMore: false,
  // Whether the History tab also shows occurrences that were due but never
  // completed. A device-level display preference (not per-family), so it's
  // persisted straight to localStorage rather than reset in
  // resetHistoryState() below.
  historyShowIncomplete: localStorage.getItem("chores.historyShowIncomplete") === "1",
  dashboardMode: false, // true when the page was loaded at /dashboard
  dashboardKey: null,
  dashboardConfig: null, // { enabled, dashboardKey } — this family's own kiosk config, shown in Settings
  dashboardScreen: "today", // "today" | "settings" — which screen the kiosk itself is showing; resets to "today" on every reload
  // This login's personal access tokens (see settings.apiTokensHeading) —
  // identity-scoped rather than family-scoped, so unlike dashboardConfig
  // this is NOT reset in setFamilyId when switching families.
  personalAccessTokens: null,
};

function resetHistoryState() {
  state.historyRecent = [];
  state.historyLater = [];
  state.historyLaterOffset = 0;
  state.historyLaterHasMore = true;
  state.historyLaterLoaded = false;
  state.historySearchQuery = "";
  state.historySearchResults = null;
  state.historySearchOffset = 0;
  state.historySearchHasMore = false;
}

export function setFamilyId(id) {
  if (id !== state.familyId) {
    resetHistoryState();
    state.dashboardConfig = null;
  }
  state.familyId = id;
  if (id) localStorage.setItem("chores.familyId", id);
  else localStorage.removeItem("chores.familyId");
}
export function setUserId(id) {
  state.userId = id;
  if (id) localStorage.setItem("chores.userId", id);
  else localStorage.removeItem("chores.userId");
}

// The parent identity this browser is anchored to for the current family —
// used to stop the user picker from ever offering "continue as" a *different*
// parent (no impersonating a co-parent), while still allowing switching to
// any child (the picker's actual intended use: previewing a kid's view, or
// marking a chore done on their behalf) and switching back to yourself.
// This is simply your bound identity — there's nothing to persist, since
// login already pins you to exactly one user per family.
export function getHomeUserId() {
  const m = state.membership && state.membership.memberships && state.membership.memberships.find((x) => x.family.id === state.familyId);
  return m && m.user.role === "USER_ROLE_PARENT" ? m.user.id : null;
}

export function currentUser() {
  return state.users.find((u) => u.id === state.userId) || null;
}
export function isParent() {
  const u = currentUser();
  return !!u && u.role === "USER_ROLE_PARENT";
}
export function roleLabel(role) {
  return role === "USER_ROLE_PARENT" ? t("role.parent") : t("role.child");
}

export async function withError(fn) {
  try {
    state.error = null;
    await fn();
  } catch (e) {
    state.error = e.message || String(e);
  }
  render();
}

// ---- data loading ------------------------------------------------------

export async function loadAuth() {
  const res = await fetch("/auth/me");
  state.auth = await res.json();
}

export async function loadMembership() {
  state.membership = await call("GetMyMembership", {});
}

export function selectMembership(m) {
  state.families = [m.family];
  setFamilyId(m.family.id);
  setUserId(m.user.id);
}

export async function loadFamilyData() {
  if (state.dashboardMode) return loadDashboardData();
  if (!state.familyId) return;
  const [usersResp, tasksResp, summariesResp, payoutsResp] = await Promise.all([
    call("ListUsers", { familyId: state.familyId }),
    call("ListTasks", { familyId: state.familyId }),
    call("ListChildSummaries", { familyId: state.familyId }),
    call("ListPayouts", { familyId: state.familyId }),
  ]);
  state.users = usersResp.users || [];
  state.tasks = tasksResp.tasks || [];
  state.summaries = summariesResp.summaries || [];
  state.payouts = payoutsResp.payouts || [];

  if (state.userId && !state.users.find((u) => u.id === state.userId)) {
    setUserId(null);
  }

  const start = todayStr();
  const end = todayStr();
  const occResp = await call("ListTaskOccurrences", {
    familyId: state.familyId,
    startDate: start,
    endDate: end,
  });
  state.occurrences = occResp.occurrences || [];

  // Parent-only RPC — a bound child hitting it gets PermissionDenied, and
  // invitations aren't shown anywhere in the child view anyway.
  if (isParent()) {
    const invResp = await call("ListInvitations", { familyId: state.familyId });
    state.invitations = invResp.invitations || [];
  }

  // The History tab's today/yesterday/this-week groups are cheap (at most a
  // week of occurrences) and stay fresh via the same auto-refresh as
  // everything else; the paginated "later" bucket and search results are
  // loaded separately, on demand, only while that tab is actually open.
  if (isParent()) {
    const histResp = await call("ListTaskOccurrences", {
      familyId: state.familyId,
      startDate: mondayOfWeekStr(),
      endDate: todayStr(),
    });
    state.historyRecent = histResp.occurrences || [];
  }

  // Balance's per-child month-by-month earnings. Parent-only, like History
  // above — a child has no Balance tab to show it on.
  if (isParent()) {
    const monthlyEntries = await Promise.all(
      state.summaries.map((s) => call("ListMonthlyEarnings", { childId: s.child.id }).then((r) => [s.child.id, r.months || []]))
    );
    state.monthlyEarningsByChild = Object.fromEntries(monthlyEntries);
  }
}

export async function loadPushConfig() {
  state.pushConfig = await call("GetPushConfig", {});
}

// ---- Auto-refresh -----------------------------------------------------
// Always on — a background refresh every few minutes keeps task status up
// to date if someone else marks something done while you're looking at
// this page. Not user-configurable: there's no real downside to it, and it
// already pauses itself below whenever it would actually get in the way.

export const AUTO_REFRESH_MS = 5 * 60 * 1000;

// A background refresh rebuilds the whole DOM via render(), which would
// wipe out anything the user is mid-typing into a form. Skipping the tick
// while a text/number/select field has focus avoids that at the cost of
// simply trying again next tick.
function isEditingSomething() {
  const active = document.activeElement;
  if (!active) return false;
  return active.tagName === "INPUT" || active.tagName === "TEXTAREA" || active.tagName === "SELECT";
}

let lastAutoRefreshAt = Date.now();

async function autoRefreshTick() {
  if (!state.familyId || !state.userId) return;
  if (document.hidden || isEditingSomething()) return;
  lastAutoRefreshAt = Date.now();
  await withError(loadFamilyData);
}

setInterval(autoRefreshTick, AUTO_REFRESH_MS);
// Also catch up immediately when the tab regains focus after being hidden
// long enough that a tick would otherwise have fired while backgrounded
// (browsers throttle/suspend timers in hidden tabs).
document.addEventListener("visibilitychange", () => {
  if (!document.hidden && Date.now() - lastAutoRefreshAt >= AUTO_REFRESH_MS) {
    autoRefreshTick();
  }
});
