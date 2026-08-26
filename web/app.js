// Chores frontend — vanilla JS, talks to the Connect service using the
// Connect protocol's unary JSON encoding directly (no generated client).
// Translation strings live in i18n.js (loaded before this file).

const API = "/chores.v1.ChoresService";

async function call(method, req) {
  const headers = { "Content-Type": "application/json" };
  if (state.dashboardMode && state.dashboardKey) {
    headers[DASHBOARD_KEY_HEADER] = state.dashboardKey;
  }
  const res = await fetch(`${API}/${method}`, {
    method: "POST",
    headers,
    body: JSON.stringify(req || {}),
  });
  if (res.status === 401) {
    // A kiosk has no Auth0 session to redirect through — a 401 here just
    // means its dashboard key didn't work, which the key-prompt screen
    // handles as its own error rather than sending a wall-mounted device
    // off to a login page it can never complete.
    if (state.dashboardMode) {
      throw new Error(t("dashboard.invalidKey"));
    }
    window.location.href = "/auth/login";
    throw new Error("Login required");
  }
  if (!res.ok) {
    let msg = res.statusText;
    try {
      const body = await res.json();
      msg = body.message || msg;
    } catch (_) {}
    throw new Error(msg);
  }
  if (res.status === 204) return {};
  const text = await res.text();
  return text ? JSON.parse(text) : {};
}

const money = (cents) => {
  const n = Number(cents || 0);
  return (n / 100).toLocaleString(localeTag(), { minimumFractionDigits: 2, maximumFractionDigits: 2 });
};

const todayStr = () => {
  const d = new Date();
  const tz = d.getTimezoneOffset();
  const local = new Date(d.getTime() - tz * 60000);
  return local.toISOString().slice(0, 10);
};

// One day before a "YYYY-MM-DD" string, computed from its Y/M/D components
// (not by parsing it as a Date, which would be UTC-midnight and risk an
// off-by-one in a timezone behind UTC — same reasoning as formatDateStr).
function dayBeforeStr(dateStr) {
  const [y, m, d] = dateStr.split("-").map(Number);
  const dt = new Date(y, m - 1, d - 1);
  const tz = dt.getTimezoneOffset();
  const local = new Date(dt.getTime() - tz * 60000);
  return local.toISOString().slice(0, 10);
}

// Monday of the current calendar week, matching the Monday-first week the
// UI shows elsewhere (as opposed to a rolling 7-day window).
function mondayOfWeekStr() {
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
function DOW() {
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
function formatDateStr(s) {
  if (!s) return "";
  const [y, m, d] = s.split("-").map(Number);
  return new Date(y, m - 1, d).toLocaleDateString(localeTag());
}

// Describes a task's repeat rule (one-off date, weekly pattern, or raw
// cron) for display in the task list.
function repeatLabel(t_) {
  const dow = DOW();
  switch (t_.repeatMode) {
    case "REPEAT_MODE_ONCE":
      return t("taskList.onceOn", { date: formatDateStr(t_.startDate) });
    case "REPEAT_MODE_WEEKLY": {
      const days = t_.daysOfWeek || [];
      const dayLabel = days.length && days.length < 7 ? dow.filter((d) => days.includes(d.code)).map((d) => d.label).join(", ") : t("taskList.everyDay");
      const interval = t_.repeatIntervalWeeks || 1;
      return interval > 1 ? t("taskList.everyNWeeks", { n: interval, days: dayLabel }) : dayLabel;
    }
    case "REPEAT_MODE_CRON":
      return t_.schedule;
    default:
      return "";
  }
}

// ---- state -----------------------------------------------------------

const state = {
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

function setFamilyId(id) {
  if (id !== state.familyId) {
    resetHistoryState();
    state.dashboardConfig = null;
  }
  state.familyId = id;
  if (id) localStorage.setItem("chores.familyId", id);
  else localStorage.removeItem("chores.familyId");
}
function setUserId(id) {
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
function getHomeUserId() {
  const m = state.membership && state.membership.memberships && state.membership.memberships.find((x) => x.family.id === state.familyId);
  return m && m.user.role === "USER_ROLE_PARENT" ? m.user.id : null;
}

function currentUser() {
  return state.users.find((u) => u.id === state.userId) || null;
}
function isParent() {
  const u = currentUser();
  return !!u && u.role === "USER_ROLE_PARENT";
}
function roleLabel(role) {
  return role === "USER_ROLE_PARENT" ? t("role.parent") : t("role.child");
}

async function withError(fn) {
  try {
    state.error = null;
    await fn();
  } catch (e) {
    state.error = e.message || String(e);
  }
  render();
}

// ---- data loading ------------------------------------------------------

async function loadAuth() {
  const res = await fetch("/auth/me");
  state.auth = await res.json();
}

async function loadMembership() {
  state.membership = await call("GetMyMembership", {});
}

function selectMembership(m) {
  state.families = [m.family];
  setFamilyId(m.family.id);
  setUserId(m.user.id);
}

async function loadFamilyData() {
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
}

async function loadPushConfig() {
  state.pushConfig = await call("GetPushConfig", {});
}

// ---- rendering -----------------------------------------------------

function el(html) {
  const tpl = document.createElement("template");
  tpl.innerHTML = html.trim();
  return tpl.content.firstElementChild;
}

function renderLangSwitcher() {
  const current = getLang();
  const options = window.LANGUAGES.map((l) => `<option value="${l.code}" ${l.code === current ? "selected" : ""}>${l.label}</option>`).join("");
  const card = el(`
    <div class="card">
      <h2>${escapeHtml(t("lang.label"))}</h2>
      <select id="lang-switcher" aria-label="${escapeHtml(t("lang.label"))}">
        ${options}
      </select>
    </div>
  `);
  card.querySelector("#lang-switcher").addEventListener("change", (e) => {
    setLang(e.target.value);
    render();
  });
  return card;
}

// The app is laid out as a shell — a sticky app bar, a tab bar pinned to
// the bottom edge on a phone, and the scrolling screen between them — so
// that on a phone it reads as an app rather than as a web page: navigation
// stays put under the thumb instead of scrolling away with the content.
//
// Parents get "Today" (today's status per child, at a glance) plus separate
// tabs for managing tasks, payouts/accounting, and browsing history — those
// are different activities done at different times, not one big page. A
// child has exactly one thing to do here (their own tasks, with their own
// earnings shown right there), so their bar is just that screen plus
// Settings, which has to live somewhere.
function tabDefsFor(parentMode) {
  const settings = { key: "settings", label: t("topbar.settings"), icon: "settings" };
  const today = { key: "home", label: t("tabs.today"), icon: "checklist" };
  if (!parentMode) return [today, settings];
  return [
    today,
    { key: "history", label: t("tabs.history"), icon: "history" },
    { key: "tasks", label: t("tabs.tasks"), icon: "list_alt" },
    { key: "accounting", label: t("tabs.accounting"), icon: "savings" },
    settings,
  ];
}

// Inline confirmations, open editors and expanded rows are all "where you
// were on the screen you just left" — none of it should survive a jump to
// a different tab and reappear the next time you come back.
function resetTransientUiState() {
  state.editingTaskId = null;
  state.addingTask = false;
  confirmingToggleKey = null;
  confirmingDeleteTaskId = null;
  confirmingRemoveChildId = null;
  confirmingRevokeInvitationId = null;
  confirmingLeaveFamily = false;
  confirmingDeleteFamily = false;
  expandedFamilyRow = null;
}

function renderTabBar(defs, activeKey) {
  const nav = el(
    `<nav class="tabbar">
      <div class="tabbar-inner">
        ${defs
          .map(
            (d) => `<button type="button" data-tab="${d.key}" class="${activeKey === d.key ? "active" : ""}" ${activeKey === d.key ? 'aria-current="page"' : ""}>
              <span class="material-symbols-outlined">${d.icon}</span>
              <span class="tab-label">${escapeHtml(d.label)}</span>
            </button>`
          )
          .join("")}
      </div>
    </nav>`
  );
  nav.querySelectorAll("button").forEach((b) =>
    b.addEventListener("click", () => {
      if (state.tab === b.dataset.tab) {
        window.scrollTo({ top: 0, behavior: "smooth" });
        return;
      }
      state.tab = b.dataset.tab;
      resetTransientUiState();
      render();
      window.scrollTo({ top: 0 });
    })
  );
  return nav;
}

// A screen shown without the tab bar — onboarding, the "who's using this?"
// picker, the kiosk key prompt. They're all pre-navigation: there's nowhere
// else to go from them yet.
function renderStandalone(node) {
  const main = el(`<main class="content no-tabbar"></main>`);
  main.appendChild(node);
  return main;
}

function renderErrorToast() {
  const toast = el(`
    <div class="error" role="alert">
      <span class="material-symbols-outlined">error</span>
      <span class="error-text"></span>
      <button type="button" aria-label="${escapeHtml(t("common.dismiss"))}"><span class="material-symbols-outlined">close</span></button>
    </div>
  `);
  toast.querySelector(".error-text").textContent = state.error;
  toast.querySelector("button").addEventListener("click", () => {
    state.error = null;
    render();
  });
  return toast;
}

function render() {
  const app = document.getElementById("app");
  app.innerHTML = "";
  // The sheet gets its own scroll container, so the page behind it must
  // stop scrolling while it's open — otherwise a flick aimed at the form
  // drags the list underneath instead.
  document.body.classList.toggle("sheet-open", taskSheetIsOpen());

  if (state.error) app.appendChild(renderErrorToast());

  // The kiosk dashboard is a completely separate, much smaller UI — no
  // login, no family/user picker, no tabs — so it's handled before any of
  // the normal app's routing below even looks at auth/membership state,
  // none of which applies to it.
  if (state.dashboardMode) {
    if (!state.dashboardKey) {
      app.appendChild(renderStandalone(renderDashboardKeyPrompt()));
    } else {
      app.appendChild(renderDashboardBar());
      const kiosk = el(`<main class="content no-tabbar"></main>`);
      kiosk.appendChild(el(`<h1 class="screen-title">${escapeHtml(t("tabs.today"))}</h1>`));
      kiosk.appendChild(renderTodayTab());
      app.appendChild(kiosk);
    }
    return;
  }

  if (!state.membership || !state.membership.bound) {
    app.appendChild(renderStandalone(renderOnboarding()));
    return;
  }

  if (!state.userId) {
    app.appendChild(renderStandalone(renderUserPicker()));
    return;
  }

  const parentMode = isParent();
  const defs = tabDefsFor(parentMode);
  const activeTab = defs.some((d) => d.key === state.tab) ? state.tab : "home";

  app.appendChild(renderAppBar());
  app.appendChild(renderTabBar(defs, activeTab));

  const main = el(`<main class="content"></main>`);
  const viewingAs = renderViewingAsNotice();
  if (viewingAs) main.appendChild(viewingAs);
  main.appendChild(el(`<h1 class="screen-title">${escapeHtml(defs.find((d) => d.key === activeTab).label)}</h1>`));

  if (activeTab === "settings") main.appendChild(renderSettingsTab());
  else if (!parentMode) main.appendChild(renderChildTasksTab());
  else if (activeTab === "home") main.appendChild(renderTodayTab());
  else if (activeTab === "history") main.appendChild(renderHistoryTab());
  else if (activeTab === "tasks") main.appendChild(renderTasksManagementTab());
  else main.appendChild(renderAccountingTab());

  app.appendChild(main);

  // Both float above the content, so they're appended to the shell rather
  // than into the scrolling screen.
  if (parentMode && activeTab === "tasks") app.appendChild(renderAddTaskFab());
  const sheet = renderTaskSheet();
  if (sheet) app.appendChild(sheet);
}

function escapeHtml(s) {
  const d = document.createElement("div");
  d.textContent = s;
  return d.innerHTML;
}

// Material Symbols are rendered as ligature text content (not a class
// name), so escapeHtml() alone already makes them injection-safe; this
// whitelist is just hygiene, matching how the names actually look
// (lowercase, underscores).
function materialIconName(value) {
  const cleaned = String(value || "")
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9_]/g, "");
  return cleaned || "star";
}

function isOptionalTask(task) {
  return task.classification === "TASK_CLASSIFICATION_OPTIONAL";
}

// The mandatory/optional badge. Shared so the Tasks list and History label a
// task identically — two hand-rolled copies drifted apart the moment one of
// them was touched.
function classificationPillHtml(task) {
  const optional = isOptionalTask(task);
  return `<span class="pill ${optional ? "optional" : "mandatory"}">${escapeHtml(
    optional ? t("taskList.optional") : t("taskList.mandatory")
  )}</span>`;
}

// Today's occurrences split into what has to happen and what is up for grabs.
// Groups with nothing in them are dropped, so a child with only required
// chores doesn't get an empty "Can do" heading.
function groupOccurrencesByClassification(occurrences) {
  return [
    { label: t("today.mustDo"), items: occurrences.filter((o) => !isOptionalTask(o.task)) },
    { label: t("today.canDo"), items: occurrences.filter((o) => isOptionalTask(o.task)) },
  ].filter((group) => group.items.length);
}

// The leading tile in a task row: the task's own icon on a tinted square,
// falling back to a generic one so every row lines up at the same left
// edge whether or not an icon was ever picked for it.
function taskIconHtml(task) {
  const icon = task.icon || {};
  if (icon.value && icon.type !== "ICON_TYPE_MATERIAL_SYMBOLS") {
    // An icon predating Material Symbols becoming the only supported type
    // (an emoji, or a Font Awesome name) — rendered as plain text.
    return `<span class="task-icon">${escapeHtml(icon.value)}</span>`;
  }
  const name = icon.value ? materialIconName(icon.value) : "task_alt";
  return `<span class="task-icon"><span class="material-symbols-outlined">${escapeHtml(name)}</span></span>`;
}

// The full official Material Symbols icon name list (fonts.google.com/icons),
// fetched lazily and cached here — see loadMaterialSymbolNames. Search
// matches against these names directly (see searchMaterialSymbols) rather
// than a hand-authored keyword/synonym mapping.
let materialSymbolNamesPromise = null;
function loadMaterialSymbolNames() {
  if (!materialSymbolNamesPromise) {
    materialSymbolNamesPromise = fetch("/material-symbols.json")
      .then((res) => res.json())
      .catch(() => []);
  }
  return materialSymbolNamesPromise;
}

// Substring search over the official icon names, matching each whitespace-
// separated search word against the name with underscores treated as
// spaces (so "clean service" matches "cleaning_services"). Prefix matches
// on the full query sort first, then shorter (more specific) names.
function searchMaterialSymbols(names, query) {
  const q = query.trim().toLowerCase();
  if (!q) return [];
  const words = q.split(/\s+/).filter(Boolean);
  const matches = names.filter((name) => {
    const haystack = name.replace(/_/g, " ");
    return words.every((w) => haystack.includes(w));
  });
  matches.sort((a, b) => {
    const aStarts = a.replace(/_/g, " ").startsWith(q) ? 0 : 1;
    const bStarts = b.replace(/_/g, " ").startsWith(q) ? 0 : 1;
    if (aStarts !== bStarts) return aStarts - bStarts;
    return a.length - b.length || a.localeCompare(b);
  });
  return matches.slice(0, 60);
}

// Creates a family and binds the caller as its founding parent via
// identity auto-bind. Leaves the new family selected — callers still need
// their own loadFamilyData() afterward.
async function createFamilyAndSwitchTo(name, yourName) {
  await call("CreateFamily", { name, parentName: yourName });
  await loadMembership();
  if (state.membership.bound && state.membership.memberships.length) {
    selectMembership(state.membership.memberships[state.membership.memberships.length - 1]);
  }
}

// Redeems an invite code for the logged-in identity and refreshes
// membership — requires Auth0 (see AcceptInvitation). Doesn't switch the
// active family on its own; callers that want to land in the newly joined
// family call selectMembership() themselves afterward. Callers validate
// that code is non-empty first (the required message differs by context).
async function joinFamilyWithCode(code) {
  await call("AcceptInvitation", { token: code });
  await loadMembership();
}

// The onboarding screen's two forms: create a family from scratch, or join
// one with an invite code. Once logged in and settled into a family, the
// same two actions live in Settings instead (renderCreateFamilySection,
// renderJoinFamilySection) for adding another one.
function renderCreateAndJoinFamilyForms() {
  const wrap = el(`<div></div>`);

  // Pre-filled from the Auth0 profile so the name stored on the family
  // member record matches the identity's own name by default — still
  // editable, but nudges toward one consistent name rather than a
  // second, independently-typed one.
  const defaultName = (state.auth && (state.auth.name || state.auth.email)) || "";
  const form = el(`
    <div class="card">
      <h2>${escapeHtml(t("onboarding.heading"))}</h2>
      <div class="field">
        <label>${escapeHtml(t("onboarding.yourNameLabel"))}</label>
        <input type="text" class="input-full" id="onboard-parent-name" placeholder="${escapeHtml(t("onboarding.yourNamePlaceholder"))}" value="${escapeHtml(defaultName)}" />
      </div>
      <div class="field">
        <label>${escapeHtml(t("family.nameLabel"))}</label>
        <input type="text" class="input-full" id="onboard-family-name" placeholder="${escapeHtml(t("family.namePlaceholder"))}" />
      </div>
      <button class="block" id="onboard-create-btn" style="margin-top:4px;">${escapeHtml(t("family.createBtn"))}</button>
    </div>
  `);
  form.querySelector("#onboard-create-btn").addEventListener("click", () =>
    withError(async () => {
      const parentName = form.querySelector("#onboard-parent-name").value.trim();
      const familyName = form.querySelector("#onboard-family-name").value.trim();
      if (!familyName) throw new Error(t("familyPicker.nameRequired"));
      await createFamilyAndSwitchTo(familyName, parentName);
      await loadFamilyData();
    })
  );
  wrap.appendChild(form);

  const joinForm = el(`
    <div class="card">
      <h2>${escapeHtml(t("onboarding.joinHeading"))}</h2>
      <div class="field">
        <label>${escapeHtml(t("onboarding.joinCodeLabel"))}</label>
        <input type="text" class="input-full" id="onboard-join-code" autocapitalize="off" autocorrect="off" spellcheck="false" />
      </div>
      <button type="button" id="onboard-join-btn" class="secondary block" style="margin-top:4px;">${escapeHtml(t("onboarding.joinBtn"))}</button>
    </div>
  `);
  joinForm.querySelector("#onboard-join-btn").addEventListener("click", () =>
    withError(async () => {
      const code = joinForm.querySelector("#onboard-join-code").value.trim();
      if (!code) throw new Error(t("onboarding.joinCodeRequired"));
      await joinFamilyWithCode(code);
      if (state.membership.bound && state.membership.memberships.length) {
        selectMembership(state.membership.memberships[state.membership.memberships.length - 1]);
        await loadFamilyData();
      }
    })
  );
  wrap.appendChild(joinForm);

  return wrap;
}

function renderOnboarding() {
  const wrap = el(`<div></div>`);
  // el() only ever returns one element, so the heading and its subtitle
  // have to share a wrapper — as two siblings the subtitle was silently
  // dropped on the floor.
  wrap.appendChild(el(`
    <div class="hero" style="margin-top:6vh;">
      <img src="/icons/logo.png" alt="" width="88" height="88" class="logo" />
      <h1>${escapeHtml(window.APP_NAME)}</h1>
      <p>${escapeHtml(t("onboarding.subtitle"))}</p>
    </div>
  `));
  wrap.appendChild(renderCreateAndJoinFamilyForms());
  return wrap;
}

function renderUserPicker() {
  const wrap = el(`<div></div>`);
  const family = state.families.find((f) => f.id === state.familyId);
  wrap.appendChild(
    el(`
      <div style="margin-bottom:18px;">
        <h1 class="screen-title" style="margin-bottom:2px;">${escapeHtml(family ? family.name : window.APP_NAME)}</h1>
        <p style="margin:0;">${escapeHtml(t("userPicker.whoIsUsing"))}</p>
      </div>
    `)
  );

  // Every family member is listed here, but "continue as" only ever works
  // for a child, or for the one parent this browser is already anchored to
  // for this family — a co-parent still shows up (so the picker still gives
  // a full picture of who's in the family), just without a way to act as
  // them. See getHomeUserId's comment for the full rationale.
  const homeUserId = getHomeUserId();
  const canContinueAs = (u) => u.role !== "USER_ROLE_PARENT" || !homeUserId || u.id === homeUserId;

  const card = el(`<div class="card"></div>`);
  if (state.users.length) {
    state.users.forEach((u) => {
      const action = canContinueAs(u) ? `<button data-id="${u.id}">${escapeHtml(t("userPicker.continue"))}</button>` : "";
      const row = el(`
        <div class="row">
          <div class="task-row-main">
            ${avatarHtml(u, "lg")}
            <div class="task-row-text">
              <div class="task-title">${escapeHtml(u.name)}</div>
              <div class="task-meta">${escapeHtml(roleLabel(u.role))}</div>
            </div>
          </div>
          ${action}
        </div>
      `);
      const btn = row.querySelector("button");
      if (btn) {
        btn.addEventListener("click", () =>
          withError(async () => {
            setUserId(u.id);
            await loadFamilyData();
          })
        );
      }
      card.appendChild(row);
    });
  } else {
    card.appendChild(el(`<p class="empty">${escapeHtml(t("userPicker.noMembers"))}</p>`));
  }
  wrap.appendChild(card);
  wrap.appendChild(renderAddUserForm());
  return wrap;
}

function renderAddUserForm() {
  const form = el(`
    <div class="card">
      <h2>${escapeHtml(t("addUser.heading"))}</h2>
      <div class="field">
        <label>${escapeHtml(t("addUser.nameLabel"))}</label>
        <input type="text" class="input-full" id="new-user-name" placeholder="${escapeHtml(t("addUser.namePlaceholder"))}" />
      </div>
      <div class="field">
        <label>${escapeHtml(t("addUser.roleLabel"))}</label>
        <select id="new-user-role">
          <option value="USER_ROLE_PARENT">${escapeHtml(t("role.parent"))}</option>
          <option value="USER_ROLE_CHILD">${escapeHtml(t("role.child"))}</option>
        </select>
      </div>
      <button class="block" id="add-user-btn">${escapeHtml(t("addUser.add"))}</button>
    </div>
  `);
  form.querySelector("#add-user-btn").addEventListener("click", () =>
    withError(async () => {
      const name = form.querySelector("#new-user-name").value.trim();
      const role = form.querySelector("#new-user-role").value;
      if (!name) throw new Error(t("addUser.nameRequired"));
      await call("CreateUser", { familyId: state.familyId, name, role });
      await loadFamilyData();
    })
  );
  return form;
}

// Parents before children, alphabetical by name within each group — used
// everywhere a family's members are listed (the topbar switcher, the
// Settings family overview) so the ordering reads the same in both places.
function sortMembersForDisplay(users) {
  return users.slice().sort((a, b) => {
    if (a.role !== b.role) return a.role === "USER_ROLE_PARENT" ? -1 : 1;
    return a.name.localeCompare(b.name);
  });
}

// Every user the current login can directly switch to right from the
// topbar — mirrors renderUserPicker's old canContinueAs rule: a bound
// child's login can only ever act as themselves, so this is only
// meaningful for a bound parent (picking a child, or switching back to
// themselves). Empty when switching isn't possible, in which case the
// topbar just shows the current user's name as plain text instead of a
// dropdown.
function switchableUsers() {
  // Gated on the login's own bound role (getHomeUserId), not on whichever
  // user happens to be selected right now — using isParent() here instead
  // meant that once you switched to a child, currentUser() became that
  // child, isParent() went false, and the switcher vanished with no way
  // back to yourself.
  const homeUserId = getHomeUserId();
  if (!homeUserId) return [];
  const canContinueAs = (u) => u.role !== "USER_ROLE_PARENT" || u.id === homeUserId;
  return sortMembersForDisplay(state.users.filter(canContinueAs));
}

// Every family the topbar's family-name dropdown can switch straight to —
// the login's own memberships (what "Switch household" used to list).
function familySwitchOptions() {
  return (state.membership && state.membership.memberships) || [];
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

function avatarHtml(user, extraClass) {
  const cls = user && user.role === "USER_ROLE_CHILD" ? "child" : "parent";
  return `<span class="avatar ${cls} ${extraClass || ""}">${escapeHtml(initials(user ? user.name : ""))}</span>`;
}

// Both switchers in the app bar are a styled chip with a real, invisible
// <select> stretched over it: the chip is what gets to look like part of a
// header, while the platform's own picker (a wheel on a phone) is still
// what opens on tap — no hand-built dropdown to get wrong.
function renderSwitcherChip({ id, label, options, selectedValue, plain, onChange }) {
  const switchable = options.length > 1;
  const chip = el(`
    <div class="chip ${plain ? "chip-plain" : ""}">
      ${plain ? `<span class="appbar-family"></span>` : `${avatarHtml(currentUser())}<span class="chip-name"></span>`}
      ${switchable ? `<span class="material-symbols-outlined chip-caret">unfold_more</span>` : ""}
    </div>
  `);
  const text = chip.querySelector(plain ? ".appbar-family" : ".chip-name");
  const selected = options.find((o) => o.value === selectedValue);
  text.textContent = selected ? selected.label : "";
  if (!switchable) return chip;

  const select = el(`
    <select class="chip-select" id="${id}" aria-label="${escapeHtml(label)}">
      ${options.map((o) => `<option value="${escapeHtml(o.value)}" ${o.value === selectedValue ? "selected" : ""}>${escapeHtml(o.label)}</option>`).join("")}
    </select>
  `);
  select.addEventListener("change", (e) => onChange(e.target.value));
  chip.appendChild(select);
  return chip;
}

function renderAppBar() {
  const family = state.families.find((f) => f.id === state.familyId);
  const user = currentUser();
  const familyOptions = familySwitchOptions();
  const userOptions = switchableUsers();

  const bar = el(`
    <header class="appbar">
      <div class="appbar-inner">
        <div class="appbar-left"></div>
        <div class="appbar-right"></div>
      </div>
    </header>
  `);

  bar.querySelector(".appbar-left").appendChild(
    renderSwitcherChip({
      id: "family-switch",
      label: t("familyTab.switchFamilyLabel"),
      plain: true,
      selectedValue: state.familyId || "",
      options: familyOptions.length
        ? familyOptions.map((o) => ({ value: o.family.id, label: o.family.name }))
        : [{ value: state.familyId || "", label: family ? family.name : window.APP_NAME }],
      onChange: (value) =>
        withError(async () => {
          const selected = familyOptions.find((o) => o.family.id === value);
          if (!selected) return;
          selectMembership(selected);
          resetTransientUiState();
          await loadFamilyData();
        }),
    })
  );

  bar.querySelector(".appbar-right").appendChild(
    renderSwitcherChip({
      id: "user-switch",
      label: t("familyTab.switchUserLabel"),
      selectedValue: state.userId || "",
      options: userOptions.length
        ? userOptions.map((u) => ({ value: u.id, label: u.name }))
        : [{ value: state.userId || "", label: user ? user.name : "" }],
      onChange: (value) =>
        withError(async () => {
          const selected = userOptions.find((u) => u.id === value);
          if (!selected) return;
          setUserId(selected.id);
          resetTransientUiState();
          state.tab = "home";
          await loadFamilyData();
        }),
    })
  );

  return bar;
}

// Switching to one of your children in the app bar quietly changes what
// every screen means — a standing bar says so, and gets you back in one
// tap, rather than leaving the swapped name in the corner as the only clue.
function renderViewingAsNotice() {
  const homeUserId = getHomeUserId();
  if (!homeUserId || !state.userId || state.userId === homeUserId) return null;
  const me = state.users.find((u) => u.id === homeUserId);
  const viewed = currentUser();
  const bar = el(`
    <div class="notice-bar">
      <span class="material-symbols-outlined">visibility</span>
      <span class="notice-text">${escapeHtml(t("topbar.viewingAs", { name: viewed ? viewed.name : "" }))}</span>
      <button type="button" class="secondary">${escapeHtml(t("topbar.switchBack"))}</button>
    </div>
  `);
  bar.querySelector("button").addEventListener("click", () =>
    withError(async () => {
      setUserId(homeUserId);
      resetTransientUiState();
      state.tab = "home";
      await loadFamilyData();
    })
  );
  if (me) bar.querySelector("button").title = me.name;
  return bar;
}

// The kiosk has no family switcher, no user, and nothing to navigate to —
// its app bar is just the app's own name, so a wall-mounted tablet still
// looks like it's running something rather than showing a bare list.
function renderDashboardBar() {
  return el(`
    <header class="appbar">
      <div class="appbar-inner">
        <div class="appbar-family">${escapeHtml(window.APP_NAME)}</div>
      </div>
    </header>
  `);
}

// ---- Today tab (parents): today's status per child, at a glance -----------------------------------------------------

// One of today's occurrences, with its tap-to-complete tick. Shared by the
// parent's Today tab (and the kiosk, which renders the same view) and by a
// child's own screen — the two had drifted into duplicate copies of the same
// markup and the same complete/uncomplete handler.
function renderOccurrenceRow(occ, childId, opts) {
  const done = !!occ.completed;
  const showDescription = !!(opts && opts.showDescription);
  const description = showDescription && occ.task.description ? " — " + escapeHtml(occ.task.description) : "";
  const row = el(`
    <div class="row">
      <div class="task-row-main">
        ${taskIconHtml(occ.task)}
        <div class="task-row-text">
          <div class="task-title">${escapeHtml(occ.task.title)}</div>
          <div class="task-meta">kr ${money(occ.task.priceCents)}${description}</div>
        </div>
      </div>
      <button class="checkbtn ${done ? "done" : "todo"}" aria-pressed="${done}" title="${escapeHtml(done ? t("childTasks.markNotDone") : t("childTasks.markDone"))}"><span class="material-symbols-outlined">check</span></button>
    </div>
  `);
  row.querySelector("button").addEventListener("click", () =>
    withError(async () => {
      const req = { taskId: occ.task.id, childId, dueDate: occ.dueDate };
      await call(done ? "UncompleteTask" : "CompleteTask", req);
      await loadFamilyData();
    })
  );
  return row;
}

// Appends today's tasks under a "Must do" / "Can do" heading each, so what a
// child actually has to get done reads apart from what's optional extra —
// previously the two were one undifferentiated list and the distinction only
// existed as a pill in the parent's task editor.
function appendOccurrenceSections(container, occurrences, childId, opts) {
  groupOccurrencesByClassification(occurrences).forEach((group) => {
    container.appendChild(
      el(`<div class="section-label occurrence-group">${escapeHtml(group.label)}</div>`)
    );
    group.items.forEach((occ) => container.appendChild(renderOccurrenceRow(occ, childId, opts)));
  });
}

function renderTodayTab() {
  const wrap = el(`<div></div>`);
  if (!state.summaries.length) {
    wrap.appendChild(el(`<div class="card"><p class="empty">${escapeHtml(t("accounting.noChildren"))}</p></div>`));
    return wrap;
  }

  state.summaries.forEach((s) => {
    const todays = state.occurrences.filter((o) => o.childId === s.child.id && o.task.active !== false);
    const doneCount = todays.filter((o) => o.completed).length;
    const card = el(`
      <div class="card">
        <div class="row" style="padding-top:0;">
          <div class="task-row-main">
            ${avatarHtml(s.child, "lg")}
            <div class="task-row-text">
              <div class="task-title">${escapeHtml(s.child.name)}</div>
              <div class="task-meta">${escapeHtml(todays.length ? t("today.progress", { done: doneCount, total: todays.length }) : t("childTasks.empty"))}</div>
            </div>
          </div>
        </div>
      </div>
    `);

    if (!todays.length) {
      card.appendChild(el(`<p class="empty">${escapeHtml(t("childTasks.empty"))}</p>`));
    } else {
      appendOccurrenceSections(card, todays, s.child.id);
    }

    card.appendChild(el(`
      <div class="grid-2 stat-strip">
        <div class="stat"><div class="value">kr ${money(s.earnedTodayCents)}</div><div class="label">${escapeHtml(t("accounting.earnedToday"))}</div></div>
        <div class="stat"><div class="value">kr ${money(s.balanceCents)}</div><div class="label">${escapeHtml(t("accounting.balanceOwed"))}</div></div>
      </div>
    `));
    wrap.appendChild(card);
  });

  return wrap;
}

// ---- Tasks tab (children): today's checklist + earnings -----------------------------------------------------
//
// A child has nowhere else to go in the app — no separate balance or family
// page — so this single view carries both their checklist and the numbers
// (earned today, earned this week, current balance) that used to live on a
// dedicated Accounting tab.

function renderChildTasksTab() {
  const wrap = el(`<div></div>`);
  const summary = state.summaries.find((s) => s.child.id === state.userId);
  if (summary) {
    wrap.appendChild(el(`
      <div class="card">
        <div class="grid-3 stat-strip" style="margin-top:0;">
          <div class="stat"><div class="value">kr ${money(summary.earnedTodayCents)}</div><div class="label">${escapeHtml(t("accounting.earnedToday"))}</div></div>
          <div class="stat"><div class="value">kr ${money(summary.earnedThisWeekCents)}</div><div class="label">${escapeHtml(t("accounting.earnedThisWeek"))}</div></div>
          <div class="stat"><div class="value">kr ${money(summary.balanceCents)}</div><div class="label">${escapeHtml(t("accounting.balanceOwed"))}</div></div>
        </div>
      </div>
    `));
  }
  wrap.appendChild(renderChildOccurrences());
  return wrap;
}

function renderChildOccurrences() {
  const card = el(`<div class="card"><h2>${escapeHtml(t("childTasks.heading"))}</h2></div>`);
  const mine = state.occurrences.filter((o) => o.task.active !== false && o.childId === state.userId);
  if (!mine.length) {
    card.appendChild(el(`<p class="empty">${escapeHtml(t("childTasks.empty"))}</p>`));
    return card;
  }
  appendOccurrenceSections(card, mine, state.userId, { showDescription: true });
  return card;
}

// ---- Tasks tab (parents): manage task definitions -----------------------------------------------------

// The task list is the screen; adding or editing one opens over it (see
// renderTaskSheet) instead of appearing as a permanent form underneath,
// which on a phone meant scrolling past every task to reach it and losing
// your place in the list every time you edited something.
function renderTasksManagementTab() {
  const wrap = el(`<div></div>`);
  wrap.appendChild(renderTaskList());
  return wrap;
}

function taskSheetIsOpen() {
  return state.addingTask || !!state.editingTaskId;
}

function closeTaskSheet() {
  state.addingTask = false;
  state.editingTaskId = null;
}

function renderAddTaskFab() {
  const fab = el(`<button type="button" class="fab"><span class="material-symbols-outlined">add</span><span>${escapeHtml(t("addTask.addBtn"))}</span></button>`);
  fab.addEventListener("click", () => {
    confirmingDeleteTaskId = null;
    state.addingTask = true;
    render();
  });
  return fab;
}

function renderTaskSheet() {
  if (!taskSheetIsOpen()) return null;
  const editingTask = state.editingTaskId ? state.tasks.find((t_) => t_.id === state.editingTaskId) : null;
  // The task being edited can vanish under us (deleted elsewhere, or a
  // refresh that dropped it) — close rather than render an empty editor.
  if (state.editingTaskId && !editingTask) {
    closeTaskSheet();
    return null;
  }
  const backdrop = el(`
    <div class="sheet-backdrop">
      <div class="sheet" role="dialog" aria-modal="true" aria-label="${escapeHtml(editingTask ? t("taskList.editHeading") : t("addTask.heading"))}">
        <div class="sheet-header">
          <h2 class="sheet-title">${escapeHtml(editingTask ? t("taskList.editHeading") : t("addTask.heading"))}</h2>
          <button type="button" class="sheet-close secondary" aria-label="${escapeHtml(t("common.close"))}"><span class="material-symbols-outlined">close</span></button>
        </div>
        <div class="sheet-body"></div>
      </div>
    </div>
  `);
  backdrop.querySelector(".sheet-body").appendChild(renderTaskForm(editingTask));
  const close = () => {
    closeTaskSheet();
    render();
  };
  backdrop.querySelector(".sheet-close").addEventListener("click", close);
  // Only a tap on the dimmed area itself dismisses — not one that started
  // inside the form and happened to end up here.
  backdrop.addEventListener("click", (e) => {
    if (e.target === backdrop) close();
  });
  return backdrop;
}

// Which task (by id) is showing its inline "are you sure" state, if any —
// same module-level, reset-on-navigation pattern as confirmingToggleKey in
// the History tab, so both confirm flows look and behave identically.
let confirmingDeleteTaskId = null;

function renderTaskList() {
  const card = el(`<div class="card"></div>`);
  if (!state.tasks.length) {
    card.appendChild(el(`<p class="empty">${escapeHtml(t("taskList.empty"))}</p>`));
    return card;
  }
  const usersById = new Map(state.users.map((u) => [u.id, u]));
  state.tasks.forEach((t_) => {
    const assignedNames = (t_.childIds || []).map((id) => (usersById.get(id) ? usersById.get(id).name : "?")).join(", ");
    const confirmingDelete = confirmingDeleteTaskId === t_.id;
    const row = el(`
      <div class="row">
        <div class="task-row-main">
          ${taskIconHtml(t_)}
          <div class="task-row-text">
            <div class="task-title">${escapeHtml(t_.title)} ${classificationPillHtml(t_)} ${
              t_.active === false ? `<span class="pill">${escapeHtml(t("taskList.paused"))}</span>` : ""
            }</div>
            <div class="task-meta">kr ${money(t_.priceCents)} · ${escapeHtml(repeatLabel(t_))}${t_.description ? " · " + escapeHtml(t_.description) : ""}${assignedNames ? " · " + escapeHtml(assignedNames) : ""}</div>
          </div>
        </div>
        <div class="actions">
          ${
            confirmingDelete
              ? `<button class="danger" data-action="confirm-delete">${escapeHtml(t("history.confirmDelete"))}</button>
                 <button type="button" class="secondary" data-action="cancel-delete">${escapeHtml(t("taskList.cancel"))}</button>`
              : `<button class="secondary btn-icon" data-action="edit" title="${escapeHtml(t("taskList.edit"))}" aria-label="${escapeHtml(t("taskList.edit"))}"><span class="material-symbols-outlined">edit</span></button>
                 <button class="secondary btn-icon" data-action="toggle" title="${escapeHtml(t_.active === false ? t("taskList.resume") : t("taskList.pause"))}" aria-label="${escapeHtml(t_.active === false ? t("taskList.resume") : t("taskList.pause"))}"><span class="material-symbols-outlined">${t_.active === false ? "play_arrow" : "pause"}</span></button>
                 <button class="secondary btn-icon" data-action="delete" title="${escapeHtml(t("taskList.delete"))}" aria-label="${escapeHtml(t("taskList.delete"))}" style="color:var(--red);"><span class="material-symbols-outlined">delete</span></button>`
          }
        </div>
      </div>
    `);

    const editBtn = row.querySelector('[data-action="edit"]');
    if (editBtn) {
      editBtn.addEventListener("click", () => {
        state.addingTask = false;
        state.editingTaskId = t_.id;
        render();
      });
    }
    const toggleBtn = row.querySelector('[data-action="toggle"]');
    if (toggleBtn) {
      toggleBtn.addEventListener("click", () =>
        withError(async () => {
          await call("UpdateTask", {
            taskId: t_.id,
            title: t_.title,
            description: t_.description,
            priceCents: t_.priceCents,
            schedule: t_.schedule,
            repeatMode: t_.repeatMode,
            daysOfWeek: t_.daysOfWeek,
            repeatIntervalWeeks: t_.repeatIntervalWeeks,
            startDate: t_.startDate,
            active: t_.active === false,
            childIds: t_.childIds,
            icon: t_.icon,
            classification: t_.classification,
          });
          await loadFamilyData();
        })
      );
    }
    const deleteBtn = row.querySelector('[data-action="delete"]');
    if (deleteBtn) {
      deleteBtn.addEventListener("click", () => {
        confirmingDeleteTaskId = t_.id;
        render();
      });
    }
    const confirmDeleteBtn = row.querySelector('[data-action="confirm-delete"]');
    if (confirmDeleteBtn) {
      confirmDeleteBtn.addEventListener("click", () =>
        withError(async () => {
          confirmingDeleteTaskId = null;
          await call("DeleteTask", { taskId: t_.id });
          if (state.editingTaskId === t_.id) closeTaskSheet();
          await loadFamilyData();
        })
      );
    }
    const cancelDeleteBtn = row.querySelector('[data-action="cancel-delete"]');
    if (cancelDeleteBtn) {
      cancelDeleteBtn.addEventListener("click", () => {
        confirmingDeleteTaskId = null;
        render();
      });
    }
    card.appendChild(row);
  });
  return card;
}

function renderTaskForm(existingTask) {
  const isEdit = !!existingTask;
  const children = state.users.filter((u) => u.role === "USER_ROLE_CHILD");
  const form = el(`
    <div>
      <div class="field">
        <label>${escapeHtml(t("addTask.titleLabel"))}</label>
        <input type="text" id="task-title" class="input-full" placeholder="${escapeHtml(t("addTask.titlePlaceholder"))}" />
      </div>
      <div class="field">
        <label>${escapeHtml(t("addTask.descLabel"))}</label>
        <input type="text" id="task-desc" class="input-full" />
      </div>
      <div class="field">
        <label>${escapeHtml(t("addTask.iconLabel"))}</label>
        <div id="task-icon-selected" class="icon-selected" style="display:none;"></div>
        <input type="text" id="task-icon-search" maxlength="64" class="input-full" autocomplete="off" placeholder="${escapeHtml(t("addTask.iconSearchPlaceholder"))}" />
        <div id="task-icon-results" class="icon-choices" style="margin-top:6px;display:none;"></div>
      </div>
      <div class="field">
        <label>${escapeHtml(t("addTask.priceLabel"))}</label>
        <input type="number" id="task-price" min="0" step="0.5" value="10" inputmode="decimal" class="input-price" />
      </div>
      <div class="field">
        <label>${escapeHtml(t("addTask.classificationLabel"))}</label>
        <div class="segmented classification-toggle">
          <button type="button" data-classification="MANDATORY">${escapeHtml(t("taskList.mandatory"))}</button>
          <button type="button" data-classification="OPTIONAL">${escapeHtml(t("taskList.optional"))}</button>
        </div>
      </div>
      <div class="field">
        <label>${escapeHtml(t("addTask.repeatLabel"))}</label>
        <div class="segmented repeat-mode-toggle">
          <button type="button" data-repeat-mode="ONCE">${escapeHtml(t("addTask.repeatOnce"))}</button>
          <button type="button" data-repeat-mode="WEEKLY">${escapeHtml(t("addTask.repeatWeekly"))}</button>
          <button type="button" data-repeat-mode="CRON">${escapeHtml(t("addTask.repeatCron"))}</button>
        </div>
        <div id="repeat-once-fields" style="margin-top:12px;">
          <label>${escapeHtml(t("addTask.onceDateLabel"))}</label>
          <input type="date" id="task-once-date" class="input-date" />
        </div>
        <div id="repeat-weekly-fields" style="margin-top:12px;">
          <div id="task-days" class="chip-group"></div>
          <label style="margin-top:14px;">${escapeHtml(t("addTask.everyNWeeksLabel"))}</label>
          <input type="number" id="task-interval-weeks" min="1" max="52" value="1" inputmode="numeric" class="input-interval" />
        </div>
        <div id="repeat-cron-fields" style="margin-top:12px;">
          <label>${escapeHtml(t("addTask.cronLabel"))}</label>
          <input type="text" id="task-cron" placeholder="0 0 * * 1,3,5" autocapitalize="off" autocorrect="off" spellcheck="false" class="input-cron" />
          <p class="hint" style="margin:6px 0 0;">${escapeHtml(t("addTask.cronHint"))}</p>
        </div>
      </div>
      <div class="field">
        <label>${escapeHtml(t("addTask.assignLabel"))}</label>
        <div id="task-children"></div>
      </div>
      <div class="actions stack" style="margin-top:20px;">
        <button id="save-task-btn">${escapeHtml(isEdit ? t("taskList.saveChanges") : t("addTask.addBtn"))}</button>
        <button type="button" class="secondary" id="cancel-edit-btn">${escapeHtml(t("taskList.cancel"))}</button>
      </div>
    </div>
  `);

  if (isEdit) {
    form.querySelector("#task-title").value = existingTask.title;
    form.querySelector("#task-desc").value = existingTask.description || "";
    form.querySelector("#task-price").value = (Number(existingTask.priceCents || 0) / 100).toFixed(2);
  }

  const iconSearchInput = form.querySelector("#task-icon-search");
  const iconResultsWrap = form.querySelector("#task-icon-results");
  const iconSelectedWrap = form.querySelector("#task-icon-selected");
  // A pre-existing icon only pre-fills the picker when it's actually a
  // Material Symbols icon — a task whose icon predates that being the only
  // supported type (emoji, Font Awesome) starts this form with no icon
  // selected, since there's no way to keep editing that value here.
  let selectedIconValue = isEdit && existingTask.icon && existingTask.icon.type === "ICON_TYPE_MATERIAL_SYMBOLS" ? materialIconName(existingTask.icon.value) : "";
  let materialSymbolNames = [];
  loadMaterialSymbolNames().then((names) => {
    materialSymbolNames = names;
    if (iconSearchInput.value.trim()) renderIconResults();
  });

  function renderSelectedIcon() {
    if (!selectedIconValue) {
      iconSelectedWrap.style.display = "none";
      iconSelectedWrap.innerHTML = "";
      return;
    }
    iconSelectedWrap.style.display = "";
    iconSelectedWrap.innerHTML = `
      <span class="material-symbols-outlined">${escapeHtml(selectedIconValue)}</span>
      <span>${escapeHtml(selectedIconValue.replace(/_/g, " "))}</span>
      <button type="button" class="secondary" id="task-icon-clear">${escapeHtml(t("addTask.iconClear"))}</button>
    `;
    iconSelectedWrap.querySelector("#task-icon-clear").addEventListener("click", () => {
      selectedIconValue = "";
      renderSelectedIcon();
    });
  }

  function renderIconResults() {
    iconResultsWrap.innerHTML = "";
    const query = iconSearchInput.value;
    // The results panel has a background of its own, so it only exists
    // while there's actually something to show in it.
    iconResultsWrap.style.display = query.trim() ? "" : "none";
    if (!query.trim()) return;
    const matches = searchMaterialSymbols(materialSymbolNames, query);
    if (!matches.length) {
      iconResultsWrap.appendChild(el(`<p class="empty" style="margin:0;">${escapeHtml(t("addTask.iconNoResults"))}</p>`));
      return;
    }
    matches.forEach((name) => {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "secondary";
      btn.title = name.replace(/_/g, " ");
      const span = document.createElement("span");
      span.className = "material-symbols-outlined";
      span.textContent = name;
      btn.appendChild(span);
      btn.addEventListener("click", () => {
        selectedIconValue = name;
        renderSelectedIcon();
        iconSearchInput.value = "";
        iconResultsWrap.innerHTML = "";
        iconResultsWrap.style.display = "none";
      });
      iconResultsWrap.appendChild(btn);
    });
  }

  iconSearchInput.addEventListener("input", renderIconResults);
  renderSelectedIcon();

  const daysWrap = form.querySelector("#task-days");
  const dow = DOW();
  const preCheckedDays = isEdit ? existingTask.daysOfWeek || [] : [];
  // Tap-to-toggle chips rather than a row of tiny checkboxes: the checkbox
  // is still the source of truth (the save handler reads it by id), it's
  // just hidden inside a target big enough to hit with a thumb.
  dow.forEach((d) => {
    const id = `day-${d.code}`;
    const checked = isEdit ? preCheckedDays.includes(d.code) : d.code >= 1 && d.code <= 5;
    const label = el(`<label class="toggle-chip">
      <input type="checkbox" id="${id}" ${checked ? "checked" : ""}/>${escapeHtml(d.label)}
    </label>`);
    daysWrap.appendChild(label);
  });

  const classificationButtons = [...form.querySelectorAll(".classification-toggle button")];
  let selectedClassification = isEdit && existingTask.classification === "TASK_CLASSIFICATION_OPTIONAL" ? "OPTIONAL" : "MANDATORY";
  function selectClassification(value) {
    selectedClassification = value;
    classificationButtons.forEach((b) => b.classList.toggle("active", b.dataset.classification === value));
  }
  classificationButtons.forEach((btn) => btn.addEventListener("click", () => selectClassification(btn.dataset.classification)));
  selectClassification(selectedClassification);

  const repeatModeButtons = [...form.querySelectorAll(".repeat-mode-toggle button")];
  const onceFieldsWrap = form.querySelector("#repeat-once-fields");
  const weeklyFieldsWrap = form.querySelector("#repeat-weekly-fields");
  const cronFieldsWrap = form.querySelector("#repeat-cron-fields");
  const onceDateInput = form.querySelector("#task-once-date");
  const intervalWeeksInput = form.querySelector("#task-interval-weeks");
  const cronInput = form.querySelector("#task-cron");

  const repeatModeKey = { REPEAT_MODE_ONCE: "ONCE", REPEAT_MODE_WEEKLY: "WEEKLY", REPEAT_MODE_CRON: "CRON" };
  let selectedRepeatMode = isEdit ? repeatModeKey[existingTask.repeatMode] || "WEEKLY" : "WEEKLY";

  onceDateInput.value = (isEdit && existingTask.repeatMode === "REPEAT_MODE_ONCE" && existingTask.startDate) || todayStr();
  intervalWeeksInput.value = (isEdit && existingTask.repeatIntervalWeeks) || 1;
  cronInput.value = (isEdit && existingTask.repeatMode === "REPEAT_MODE_CRON" && existingTask.schedule) || "";

  function selectRepeatMode(mode) {
    selectedRepeatMode = mode;
    repeatModeButtons.forEach((b) => b.classList.toggle("active", b.dataset.repeatMode === mode));
    onceFieldsWrap.style.display = mode === "ONCE" ? "" : "none";
    weeklyFieldsWrap.style.display = mode === "WEEKLY" ? "" : "none";
    cronFieldsWrap.style.display = mode === "CRON" ? "" : "none";
  }
  repeatModeButtons.forEach((btn) => btn.addEventListener("click", () => selectRepeatMode(btn.dataset.repeatMode)));
  selectRepeatMode(selectedRepeatMode);

  const childrenWrap = form.querySelector("#task-children");
  if (!children.length) {
    childrenWrap.appendChild(el(`<p class="empty" style="margin:0;">${escapeHtml(t("addTask.noChildren"))}</p>`));
  } else {
    const assignedIds = new Set(isEdit ? existingTask.childIds || [] : []);
    const checksWrap = el(`<div class="chip-group" style="margin-bottom:10px;"></div>`);
    children.forEach((c) => {
      const label = el(`<label class="toggle-chip">
        <input type="checkbox" data-child-id="${c.id}" ${assignedIds.has(c.id) ? "checked" : ""} />${escapeHtml(c.name)}
      </label>`);
      checksWrap.appendChild(label);
    });
    childrenWrap.appendChild(checksWrap);
    // With one child there's nothing for "select all" to do that tapping
    // the single chip doesn't already do.
    if (children.length > 1) {
      const selectAllBtn = el(`<button type="button" class="secondary">${escapeHtml(t("addTask.selectAll"))}</button>`);
      selectAllBtn.addEventListener("click", () => {
        checksWrap.querySelectorAll('input[type="checkbox"]').forEach((cb) => (cb.checked = true));
      });
      childrenWrap.appendChild(selectAllBtn);
    }
  }

  form.querySelector("#save-task-btn").addEventListener("click", () =>
    withError(async () => {
      const title = form.querySelector("#task-title").value.trim();
      const description = form.querySelector("#task-desc").value.trim();
      const icon = selectedIconValue ? { type: "ICON_TYPE_MATERIAL_SYMBOLS", value: selectedIconValue } : undefined;
      const priceKr = parseFloat(form.querySelector("#task-price").value || "0");
      const childIds = [...form.querySelectorAll('#task-children input[type="checkbox"]:checked')].map((cb) => cb.dataset.childId);
      if (!title) throw new Error(t("addTask.titleRequired"));
      if (!(priceKr >= 0)) throw new Error(t("addTask.pricePositive"));
      if (!childIds.length) throw new Error(t("addTask.childRequired"));

      let repeatMode, schedule, daysOfWeek, repeatIntervalWeeks, startDate;
      if (selectedRepeatMode === "ONCE") {
        repeatMode = "REPEAT_MODE_ONCE";
        startDate = onceDateInput.value;
        if (!startDate) throw new Error(t("addTask.onceDateRequired"));
      } else if (selectedRepeatMode === "WEEKLY") {
        repeatMode = "REPEAT_MODE_WEEKLY";
        daysOfWeek = dow.filter((d) => form.querySelector(`#day-${d.code}`).checked).map((d) => d.code);
        if (!daysOfWeek.length) throw new Error(t("addTask.daysRequired"));
        repeatIntervalWeeks = parseInt(intervalWeeksInput.value || "1", 10);
        if (!(repeatIntervalWeeks >= 1)) throw new Error(t("addTask.intervalPositive"));
        startDate = isEdit && existingTask.repeatMode === "REPEAT_MODE_WEEKLY" ? existingTask.startDate : todayStr();
      } else {
        repeatMode = "REPEAT_MODE_CRON";
        schedule = cronInput.value.trim();
        if (!schedule) throw new Error(t("addTask.cronRequired"));
      }

      const classification = `TASK_CLASSIFICATION_${selectedClassification}`;
      const repeatFields = { repeatMode, schedule, daysOfWeek, repeatIntervalWeeks, startDate };
      if (isEdit) {
        await call("UpdateTask", {
          taskId: existingTask.id,
          title,
          description,
          icon,
          priceCents: Math.round(priceKr * 100),
          ...repeatFields,
          childIds,
          active: existingTask.active !== false,
          classification,
        });
      } else {
        await call("CreateTask", {
          familyId: state.familyId,
          title,
          description,
          icon,
          priceCents: Math.round(priceKr * 100),
          ...repeatFields,
          childIds,
          classification,
        });
      }
      closeTaskSheet();
      await loadFamilyData();
    })
  );
  form.querySelector("#cancel-edit-btn").addEventListener("click", () => {
    closeTaskSheet();
    render();
  });
  return form;
}

// ---- Accounting tab -----------------------------------------------------

function renderAccountingTab() {
  const wrap = el(`<div></div>`);
  const summaries = state.summaries;

  if (!summaries.length) {
    wrap.appendChild(el(`<div class="card"><p class="empty">${escapeHtml(t("accounting.noChildren"))}</p></div>`));
    return wrap;
  }

  // One child's figures, payout form and payout log are three separate
  // cards under a heading naming them, rather than cards nested inside a
  // card — nesting read as depth that isn't there, and on a phone the
  // doubled padding cost most of the usable width.
  summaries.forEach((s) => {
    const wrapForChild = el(`<div style="margin-bottom:8px;"></div>`);
    wrapForChild.appendChild(
      el(`
        <div class="row" style="border-bottom:none;padding-bottom:6px;">
          <div class="task-row-main">
            ${avatarHtml(s.child, "lg")}
            <div class="task-row-text"><div class="task-title">${escapeHtml(s.child.name)}</div></div>
          </div>
        </div>
      `)
    );
    const card = el(`
      <div class="card">
        <div class="grid-2">
          <div class="stat"><div class="value">kr ${money(s.earnedLast7DaysCents)}</div><div class="label">${escapeHtml(t("accounting.last7Days"))}</div></div>
          <div class="stat"><div class="value">kr ${money(s.balanceCents)}</div><div class="label">${escapeHtml(t("accounting.balanceOwed"))}</div></div>
        </div>
      </div>
    `);
    wrapForChild.appendChild(card);
    const balanceCents = Number(s.balanceCents || 0);
    const balanceKr = balanceCents / 100;
    const payoutForm = el(`
      <div class="card">
        <h3>${escapeHtml(t("accounting.payoutHeading"))}</h3>
        <div class="field">
          <label>${escapeHtml(t("accounting.amountLabel"))}</label>
          <p class="hint" style="margin:-2px 0 6px;">${escapeHtml(t("accounting.amountHint"))}</p>
          <input type="number" min="0.01" max="${balanceKr}" step="0.5" inputmode="decimal" class="input-full" id="payout-amount-${s.child.id}" value="${balanceKr.toFixed(2)}" />
        </div>
        <div class="field">
          <label>${escapeHtml(t("accounting.noteLabel"))}</label>
          <input type="text" class="input-full" id="payout-note-${s.child.id}" />
        </div>
        <div class="actions stack" style="margin-top:16px;">
          <button data-action="pay">${escapeHtml(t("accounting.payFull"))}</button>
        </div>
      </div>
    `);
    const amountInput = payoutForm.querySelector(`#payout-amount-${s.child.id}`);
    const payBtn = payoutForm.querySelector('[data-action="pay"]');
    const updatePayButtonLabel = () => {
      const amountCents = Math.round(parseFloat(amountInput.value || "0") * 100);
      payBtn.textContent = amountCents === balanceCents ? t("accounting.payFull") : t("accounting.payPartial");
    };
    amountInput.addEventListener("input", updatePayButtonLabel);
    payBtn.addEventListener("click", () =>
      withError(async () => {
        const amountCents = Math.round(parseFloat(amountInput.value || "0") * 100);
        const note = payoutForm.querySelector(`#payout-note-${s.child.id}`).value.trim();
        if (!(amountCents > 0)) throw new Error(t("accounting.amountPositive"));
        if (amountCents > balanceCents) throw new Error(t("accounting.amountExceedsBalance"));
        await call("CreatePayout", {
          childId: s.child.id,
          fullPayout: amountCents === balanceCents,
          amountCents,
          note,
        });
        await loadFamilyData();
      })
    );
    wrapForChild.appendChild(payoutForm);

    const history = state.payouts.filter((p) => p.childId === s.child.id);
    const histCard = el(`<div class="card"><h3>${escapeHtml(t("accounting.historyHeading"))}</h3></div>`);
    if (!history.length) {
      histCard.appendChild(el(`<p class="empty">${escapeHtml(t("accounting.noPayouts"))}</p>`));
    } else {
      history
        .slice()
        .sort((a, b) => new Date(b.createdAt) - new Date(a.createdAt))
        .forEach((p) => {
          histCard.appendChild(
            el(`
              <div class="row">
                <span>${new Date(p.createdAt).toLocaleDateString(localeTag())} <span class="pill">${escapeHtml(p.fullPayout ? t("accounting.full") : t("accounting.partial"))}</span> ${p.note ? "— " + escapeHtml(p.note) : ""}</span>
                <strong>kr ${money(p.amountCents)}</strong>
              </div>
            `)
          );
        });
    }
    wrapForChild.appendChild(histCard);
    wrap.appendChild(wrapForChild);
  });

  return wrap;
}

// ---- Family (shown inside Settings, parents only) -----------------------------------------------------

// Builds a "type X to confirm" widget: an instruction line, a text input,
// and a button that stays disabled until the input matches expectedWord
// (case-insensitively) — used for the family-management actions below,
// which are destructive enough (losing a child's whole task/payout
// history, losing membership, losing the family outright) that a single
// click isn't enough friction.
function renderTypeToConfirm(expectedWord, hint, buttonLabel, onConfirm) {
  const wrap = el(`
    <div>
      <p class="hint" style="margin-bottom:6px;">${hint}</p>
      <div class="input-row">
        <input type="text" autocapitalize="off" autocorrect="off" spellcheck="false" placeholder="${escapeHtml(t("familyTab.typeToConfirmPlaceholder", { word: expectedWord }))}" />
        <button class="danger" disabled>${escapeHtml(buttonLabel)}</button>
        <button type="button" class="secondary" data-action="cancel">${escapeHtml(t("taskList.cancel"))}</button>
      </div>
    </div>
  `);
  const input = wrap.querySelector("input");
  const confirmBtn = wrap.querySelector("button.danger");
  input.addEventListener("input", () => {
    confirmBtn.disabled = input.value.trim().toLowerCase() !== expectedWord.toLowerCase();
  });
  confirmBtn.addEventListener("click", () => {
    if (!confirmBtn.disabled) onConfirm();
  });
  return wrap;
}

// After leaving a family, removing yourself isn't possible, or deleting one
// outright, there's nothing left to show for it here — fall back to
// whatever the app would normally land on with no family selected: any
// remaining membership gets auto-selected (mirroring boot's own selection
// logic; the topbar switcher is how you'd then pick a different one),
// otherwise render() naturally lands on onboarding.
async function afterLeavingFamily() {
  setUserId(null);
  setFamilyId(null);
  state.tab = "home";
  await loadMembership();
  if (state.membership.bound && state.membership.memberships.length) {
    selectMembership(state.membership.memberships[0]);
    await loadFamilyData();
  }
}

let confirmingRemoveChildId = null;
let confirmingRevokeInvitationId = null;
let confirmingLeaveFamily = false;
let confirmingDeleteFamily = false;
// Which row of the family members list is expanded: a user id, the
// sentinel "__add__" for the "add a family member" row, or null if none is.
let expandedFamilyRow = null;

function toggleFamilyRow(key) {
  expandedFamilyRow = expandedFamilyRow === key ? null : key;
  render();
}

function renderExpandableRow(label, key, buildDetail) {
  const expanded = expandedFamilyRow === key;
  const header = el(`
    <div class="row expandable">
      <span>${label}</span>
      <span class="material-symbols-outlined chevron">${expanded ? "expand_less" : "expand_more"}</span>
    </div>
  `);
  header.addEventListener("click", () => toggleFamilyRow(key));
  const frag = [header];
  if (expanded) {
    const detail = el(`<div class="row-detail"></div>`);
    buildDetail(detail);
    frag.push(detail);
  }
  return frag;
}

function renderFamilyTab() {
  const wrap = el(`<div></div>`);
  const pendingInvitesByUserId = new Map(state.invitations.filter((i) => !i.acceptedAt).map((i) => [i.userId, i]));
  const family = state.families.find((f) => f.id === state.familyId);
  const familyName = family ? family.name : "";

  const renameCard = el(`
    <div class="card">
      <h2>${escapeHtml(t("family.renameHeading"))}</h2>
      <div class="field">
        <div class="input-row">
          <input type="text" id="rename-family-input" value="${escapeHtml(familyName)}" />
          <button type="button" id="rename-family-btn">${escapeHtml(t("family.renameSave"))}</button>
        </div>
      </div>
    </div>
  `);
  renameCard.querySelector("#rename-family-btn").addEventListener("click", () =>
    withError(async () => {
      const name = renameCard.querySelector("#rename-family-input").value.trim();
      if (!name) throw new Error(t("family.renameRequired"));
      await call("UpdateFamily", { familyId: state.familyId, name });
      // loadFamilyData() doesn't touch state.families/state.membership (it
      // only refreshes users/tasks/summaries/payouts), so the topbar's
      // family name — and its switch-family dropdown — would otherwise
      // keep showing the old name until the next full membership reload.
      const fam = state.families.find((f) => f.id === state.familyId);
      if (fam) fam.name = name;
      const membership = state.membership && state.membership.memberships && state.membership.memberships.find((m) => m.family.id === state.familyId);
      if (membership) membership.family.name = name;
      await loadFamilyData();
    })
  );
  wrap.appendChild(renameCard);

  const card = el(`<div class="card"><h2>${escapeHtml(t("familyTab.heading"))}</h2></div>`);

  sortMembersForDisplay(state.users).forEach((u) => {
    const pendingInvite = !u.authBound ? pendingInvitesByUserId.get(u.id) : undefined;
    const pendingTag = pendingInvite ? ` <span class="pill">${escapeHtml(t("familyTab.invitePending"))}</span>` : "";
    const isYou = u.id === state.userId;
    const youTag = isYou ? ` · ${escapeHtml(t("familyTab.you"))}` : "";
    // A co-parent (not you) has nothing to manage here — no rename, no
    // remove, no leave — so their row doesn't expand at all, unless their
    // invite is still pending and needs a place to show its link/revoke.
    const canExpand = isYou || u.role === "USER_ROLE_CHILD" || !!pendingInvite;
    const label = `<div class="task-row-main">
        ${avatarHtml(u)}
        <div class="task-row-text">
          <div class="task-title">${escapeHtml(u.name)}${pendingTag}</div>
          <div class="task-meta">${escapeHtml(roleLabel(u.role))}${youTag}</div>
        </div>
      </div>`;

    if (!canExpand) {
      card.appendChild(el(`<div class="row">${label}</div>`));
      return;
    }

    renderExpandableRow(label, u.id, (detail) => {
      if (isYou) {
        if (u.role === "USER_ROLE_PARENT" && confirmingLeaveFamily) {
          const confirmArea = renderTypeToConfirm(
            t("familyTab.leaveWord"),
            t("familyTab.leaveConfirmHint", { family: familyName, word: t("familyTab.leaveWord") }),
            t("familyTab.leave"),
            () =>
              withError(async () => {
                confirmingLeaveFamily = false;
                await call("LeaveFamily", { userId: u.id });
                await afterLeavingFamily();
              })
          );
          confirmArea.querySelector('[data-action="cancel"]').addEventListener("click", () => {
            confirmingLeaveFamily = false;
            render();
          });
          detail.appendChild(confirmArea);
          return;
        }

        const renameField = el(`
          <div class="field">
            <label>${escapeHtml(t("familyTab.renameLabel"))}</label>
            <div class="input-row">
              <input type="text" value="${escapeHtml(u.name)}" />
              <button type="button" data-action="save-name">${escapeHtml(t("familyTab.save"))}</button>
            </div>
          </div>
        `);
        const nameInput = renameField.querySelector("input");
        renameField.querySelector('[data-action="save-name"]').addEventListener("click", () =>
          withError(async () => {
            const name = nameInput.value.trim();
            if (!name) throw new Error(t("addUser.nameRequired"));
            await call("UpdateUser", { userId: u.id, name });
            await loadFamilyData();
          })
        );
        detail.appendChild(renameField);

        if (u.role === "USER_ROLE_PARENT") {
          const leaveBtn = el(`<button type="button" class="secondary" style="margin-top:10px;">${escapeHtml(t("familyTab.leave"))}</button>`);
          leaveBtn.addEventListener("click", () => {
            confirmingLeaveFamily = true;
            render();
          });
          detail.appendChild(leaveBtn);
        }
        return;
      }

      if (pendingInvite) {
        if (confirmingRevokeInvitationId === pendingInvite.id) {
          const confirmArea = renderTypeToConfirm(
            t("invitations.revokeWord"),
            t("invitations.revokeConfirmHint", { name: u.name, word: t("invitations.revokeWord") }),
            t("invitations.revoke"),
            () =>
              withError(async () => {
                confirmingRevokeInvitationId = null;
                expandedFamilyRow = null;
                await call("RevokeInvitation", { invitationId: pendingInvite.id });
                await loadFamilyData();
              })
          );
          confirmArea.querySelector('[data-action="cancel"]').addEventListener("click", () => {
            confirmingRevokeInvitationId = null;
            render();
          });
          detail.appendChild(confirmArea);
          return;
        }

        if (pendingInvite.token) {
          // Built from the browser's own origin — the invite is only ever
          // created from within the app itself, so whatever host served
          // this page is exactly what a recipient should hit too.
          // /invite/accept (see cmd/chores/main.go) forces login first if
          // needed, then binds the token and lands them in the app.
          const acceptUrl = `${window.location.origin}/invite/accept?token=${encodeURIComponent(pendingInvite.token)}`;
          detail.appendChild(el(`
            <div class="field">
              <label>${escapeHtml(t("invitations.linkLabel"))}</label>
              <input type="text" class="input-full" readonly value="${escapeHtml(acceptUrl)}" onclick="this.select()" />
              <label style="margin-top:6px;">${escapeHtml(t("invitations.codeLabel"))}</label>
              <input type="text" class="input-full" readonly value="${escapeHtml(pendingInvite.token)}" onclick="this.select()" />
            </div>
          `));
        }
        const revokeBtn = el(`<button type="button" class="danger" style="margin-top:10px;">${escapeHtml(t("invitations.revoke"))}</button>`);
        revokeBtn.addEventListener("click", () => {
          confirmingRevokeInvitationId = pendingInvite.id;
          render();
        });
        detail.appendChild(revokeBtn);
        return;
      }

      // u.role === "USER_ROLE_CHILD" — the only other case canExpand allows.
      if (confirmingRemoveChildId === u.id) {
        const confirmArea = renderTypeToConfirm(
          t("familyTab.removeWord"),
          t("familyTab.removeConfirmHint", { name: u.name, word: t("familyTab.removeWord") }),
          t("familyTab.remove"),
          () =>
            withError(async () => {
              confirmingRemoveChildId = null;
              expandedFamilyRow = null;
              await call("RemoveChild", { childId: u.id });
              await loadFamilyData();
            })
        );
        confirmArea.querySelector('[data-action="cancel"]').addEventListener("click", () => {
          confirmingRemoveChildId = null;
          render();
        });
        detail.appendChild(confirmArea);
      } else {
        const removeBtn = el(`<button type="button" class="danger">${escapeHtml(t("familyTab.remove"))}</button>`);
        removeBtn.addEventListener("click", () => {
          confirmingRemoveChildId = u.id;
          render();
        });
        detail.appendChild(removeBtn);
      }
    }).forEach((n) => card.appendChild(n));
  });

  renderExpandableRow(
    `<span style="display:inline-flex;align-items:center;gap:10px;color:var(--accent);font-weight:600;"><span class="material-symbols-outlined">person_add</span>${escapeHtml(t("addUser.heading"))}</span>`,
    "__add__",
    (detail) => {
      detail.appendChild(el(`
        <div class="field">
          <label>${escapeHtml(t("addUser.nameLabel"))}</label>
          <input type="text" class="input-full" id="new-member-name" placeholder="${escapeHtml(t("addUser.namePlaceholder"))}" />
        </div>
      `));
      detail.appendChild(el(`
        <div class="field">
          <label>${escapeHtml(t("addUser.roleLabel"))}</label>
          <select id="new-member-role">
            <option value="USER_ROLE_PARENT">${escapeHtml(t("role.parent"))}</option>
            <option value="USER_ROLE_CHILD">${escapeHtml(t("role.child"))}</option>
          </select>
        </div>
      `));
      const actions = el(`<div class="actions stack"></div>`);
    // Adding a member always goes through CreateInvitation: every family
    // member gets a shareable code for binding their own login, and that
    // code stays visible (see the pending invite's own row in the members
    // list above) for as long as it's unclaimed — there's no separate
    // no-login "just add them" path.
      const addBtn = el(`<button type="button">${escapeHtml(t("addUser.add"))}</button>`);
      addBtn.addEventListener("click", () =>
        withError(async () => {
          const name = detail.querySelector("#new-member-name").value.trim();
          const role = detail.querySelector("#new-member-role").value;
          if (!name) throw new Error(t("addUser.nameRequired"));
          const resp = await call("CreateInvitation", { familyId: state.familyId, name, role });
          // Expand the new member's own row so their invite link/code — the
          // whole point of adding them — is immediately visible, instead of
          // leaving it one tap away in a row that just looks collapsed.
          expandedFamilyRow = resp.invitation ? resp.invitation.userId : null;
          await loadFamilyData();
        })
      );
      actions.appendChild(addBtn);
      detail.appendChild(actions);

      detail.appendChild(el(`<p class="hint" style="margin-top:10px;">${escapeHtml(t("invitations.inviteDesc"))}</p>`));
    }
  ).forEach((n) => card.appendChild(n));

  wrap.appendChild(card);
  wrap.appendChild(renderDashboardSettingsSection());

  const dangerCard = el(`
    <div class="card">
      <h2>${escapeHtml(t("familyTab.dangerZoneHeading"))}</h2>
      <p>${escapeHtml(t("familyTab.deleteFamilyDesc", { family: familyName }))}</p>
    </div>
  `);
  if (confirmingDeleteFamily) {
    const confirmArea = renderTypeToConfirm(
      t("familyTab.deleteWord"),
      t("familyTab.deleteConfirmHint", { word: t("familyTab.deleteWord") }),
      t("familyTab.deleteFamilyButton"),
      () =>
        withError(async () => {
          confirmingDeleteFamily = false;
          await call("DeleteFamily", { familyId: state.familyId });
          await afterLeavingFamily();
        })
    );
    confirmArea.querySelector('[data-action="cancel"]').addEventListener("click", () => {
      confirmingDeleteFamily = false;
      render();
    });
    dangerCard.appendChild(confirmArea);
  } else {
    const deleteBtn = el(`<button class="danger">${escapeHtml(t("familyTab.deleteFamilyButton"))}</button>`);
    deleteBtn.addEventListener("click", () => {
      confirmingDeleteFamily = true;
      render();
    });
    dangerCard.appendChild(deleteBtn);
  }
  wrap.appendChild(dangerCard);

  return wrap;
}

// ---- History tab (parents): today/yesterday/this week, with an
// incrementally-loaded "later" bucket and a search box -----------------------------------------------------

const HISTORY_PAGE_SIZE = 20;

async function loadHistoryLater(reset) {
  if (reset) {
    state.historyLater = [];
    state.historyLaterOffset = 0;
    state.historyLaterHasMore = true;
  }
  if (!state.historyLaterHasMore) return;
  const resp = await call("ListTaskOccurrences", {
    familyId: state.familyId,
    endDate: dayBeforeStr(mondayOfWeekStr()),
    limit: HISTORY_PAGE_SIZE,
    offset: state.historyLaterOffset,
  });
  const page = resp.occurrences || [];
  state.historyLater = state.historyLater.concat(page);
  state.historyLaterOffset += page.length;
  state.historyLaterHasMore = !!resp.hasMore;
}

let historyLaterLoadInFlight = false;
function triggerHistoryLaterLoad() {
  if (historyLaterLoadInFlight || state.historyLaterLoaded) return;
  historyLaterLoadInFlight = true;
  loadHistoryLater(true)
    .catch((e) => {
      state.error = e.message || String(e);
    })
    .finally(() => {
      state.historyLaterLoaded = true;
      historyLaterLoadInFlight = false;
      if (state.tab === "history") render();
    });
}

async function loadHistorySearch(reset) {
  if (reset) {
    state.historySearchResults = [];
    state.historySearchOffset = 0;
    state.historySearchHasMore = true;
  }
  const resp = await call("ListTaskOccurrences", {
    familyId: state.familyId,
    search: state.historySearchQuery.trim(),
    limit: HISTORY_PAGE_SIZE,
    offset: state.historySearchOffset,
  });
  const page = resp.occurrences || [];
  state.historySearchResults = (state.historySearchResults || []).concat(page);
  state.historySearchOffset += page.length;
  state.historySearchHasMore = !!resp.hasMore;
}

// Re-rendering rebuilds the whole DOM, which would normally steal focus
// (and the cursor position) right out from under whatever the user is
// typing — a plain withError() call would make the search box unusable.
// Restoring focus by id after the rebuild is what makes search-as-you-type
// tolerable here.
function rerenderPreservingFocus() {
  const active = document.activeElement;
  const id = active && active.id;
  const selStart = id && "selectionStart" in active ? active.selectionStart : null;
  const selEnd = id && "selectionEnd" in active ? active.selectionEnd : null;
  render();
  if (!id) return;
  const restored = document.getElementById(id);
  if (!restored) return;
  restored.focus();
  if (selStart !== null && restored.setSelectionRange) {
    try {
      restored.setSelectionRange(selStart, selEnd);
    } catch (_) {}
  }
}

let historySearchDebounceTimer = null;

function onHistorySearchInput(query) {
  state.historySearchQuery = query;
  clearTimeout(historySearchDebounceTimer);
  historySearchDebounceTimer = setTimeout(() => {
    (async () => {
      state.error = null;
      try {
        if (!query.trim()) {
          state.historySearchResults = null;
        } else {
          await loadHistorySearch(true);
        }
      } catch (e) {
        state.error = e.message || String(e);
      }
      rerenderPreservingFocus();
    })();
  }, 300);
}

// A stable identity for an occurrence — it has no id of its own (unlike its
// optional nested `completion`), so task+child+date (mirroring the server's
// completionKey) is what ties a rendered row back to the right occurrence.
function occurrenceKey(occ) {
  return `${occ.task.id}|${occ.childId}|${occ.dueDate}`;
}

// Which occurrence (by occurrenceKey) is showing its inline "are you sure"
// state, if any. Module-level rather than in `state`: it's transient
// UI-only, reset whenever the user navigates away from a row rather than
// something worth persisting or reacting to elsewhere.
let confirmingToggleKey = null;

// Toggles one occurrence's completion state — this is how a wrong entry in
// History gets fixed now (mark it not completed instead of deleting it, so
// it stays visible as a missed task rather than disappearing), and equally
// how a missed task gets backfilled as done after the fact.
async function toggleOccurrenceCompletion(occ) {
  if (occ.completed) {
    await call("UncompleteTask", { taskId: occ.task.id, childId: occ.childId, dueDate: occ.dueDate });
    occ.completed = false;
    occ.completion = null;
  } else {
    const resp = await call("CompleteTask", { taskId: occ.task.id, childId: occ.childId, dueDate: occ.dueDate });
    occ.completed = true;
    occ.completion = resp.completion;
  }
  // Reloads historyRecent along with everything else the change affects —
  // the child's earned-today/this-week figures and balance. historyLater
  // and historySearchResults keep the object we just mutated in place, so
  // their accumulated pagination isn't disturbed.
  await loadFamilyData();
}

function renderHistoryRow(occ) {
  const key = occurrenceKey(occ);
  const confirming = confirmingToggleKey === key;
  const amountCents = occ.completed ? (occ.completion ? occ.completion.amountCents : 0) : occ.task.priceCents;
  const actionIcon = occ.completed ? "close" : "check";
  const actionLabel = occ.completed ? t("history.markIncomplete") : t("history.markComplete");
  const confirmLabel = occ.completed ? t("history.confirmMarkIncomplete") : t("history.confirmMarkComplete");
  const badge = occ.completed ? "" : ` · <span class="pill notcompleted">${escapeHtml(t("history.notCompletedBadge"))}</span>`;
  // The amount rides in the meta line rather than as a separate right-hand
  // column: on a phone, name + date + money + button competing for one row
  // just wrapped the button onto a line of its own.
  const row = el(`
    <div class="row${occ.completed ? "" : " history-row-incomplete"}">
      <div class="task-row-main">
        ${taskIconHtml(occ.task)}
        <div class="task-row-text">
          <div class="task-title">${escapeHtml(occ.task.title)} ${classificationPillHtml(occ.task)}</div>
          <div class="task-meta">${escapeHtml(occ.childName)} · ${escapeHtml(formatDateStr(occ.dueDate))} · kr ${money(amountCents)}${badge}</div>
        </div>
      </div>
      <div class="actions" style="align-items:center;">
        ${
          confirming
            ? `<button class="danger" data-action="confirm-toggle">${escapeHtml(confirmLabel)}</button>
               <button type="button" class="secondary" data-action="cancel-toggle">${escapeHtml(t("taskList.cancel"))}</button>`
            : `<button type="button" class="secondary btn-icon" data-action="toggle" title="${escapeHtml(actionLabel)}" aria-label="${escapeHtml(actionLabel)}"><span class="material-symbols-outlined">${actionIcon}</span></button>`
        }
      </div>
    </div>
  `);
  const toggleBtn = row.querySelector('[data-action="toggle"]');
  if (toggleBtn) {
    toggleBtn.addEventListener("click", () => {
      confirmingToggleKey = key;
      render();
    });
  }
  const confirmBtn = row.querySelector('[data-action="confirm-toggle"]');
  if (confirmBtn) {
    confirmBtn.addEventListener("click", () =>
      withError(async () => {
        confirmingToggleKey = null;
        await toggleOccurrenceCompletion(occ);
      })
    );
  }
  const cancelBtn = row.querySelector('[data-action="cancel-toggle"]');
  if (cancelBtn) {
    cancelBtn.addEventListener("click", () => {
      confirmingToggleKey = null;
      render();
    });
  }
  return row;
}

// Returns null for a group with nothing in it. Four "Nothing here." cards
// stacked down a phone screen said nothing and cost the whole viewport;
// the caller falls back to a single empty message when every group is bare.
function renderHistoryGroup(heading, occurrences) {
  if (!occurrences.length) return null;
  const card = el(`<div class="card"><div class="section-label" style="margin:0 0 4px;">${escapeHtml(heading)}</div></div>`);
  occurrences.forEach((occ) => card.appendChild(renderHistoryRow(occ)));
  return card;
}

function renderHistoryTab() {
  const wrap = el(`<div></div>`);

  const searchCard = el(`
    <div class="card" style="padding:12px 16px;">
      <input type="search" id="history-search" class="input-full" autocomplete="off" placeholder="${escapeHtml(t("history.searchPlaceholder"))}" />
    </div>
  `);
  const searchInput = searchCard.querySelector("#history-search");
  searchInput.value = state.historySearchQuery;
  searchInput.addEventListener("input", (e) => onHistorySearchInput(e.target.value));
  wrap.appendChild(searchCard);

  const toggleCard = el(`
    <div class="card" style="padding:12px 16px;">
      <label class="switch-row">
        <span>${escapeHtml(t("history.showIncomplete"))}</span>
        <input type="checkbox" id="history-show-incomplete" ${state.historyShowIncomplete ? "checked" : ""} />
      </label>
    </div>
  `);
  toggleCard.querySelector("#history-show-incomplete").addEventListener("change", (e) => {
    state.historyShowIncomplete = e.target.checked;
    localStorage.setItem("chores.historyShowIncomplete", state.historyShowIncomplete ? "1" : "0");
    render();
  });
  wrap.appendChild(toggleCard);

  // Not-completed occurrences are always fetched alongside completed ones
  // (see loadFamilyData/loadHistoryLater/loadHistorySearch); this just
  // controls which of them get rendered, so toggling the checkbox never
  // needs a fresh network round trip.
  const visible = (occs) => (state.historyShowIncomplete ? occs : occs.filter((o) => o.completed));

  if (state.historySearchResults !== null) {
    const results = renderHistoryGroup(t("history.searchResultsHeading"), visible(state.historySearchResults));
    wrap.appendChild(results || el(`<div class="card"><p class="empty">${escapeHtml(t("history.empty"))}</p></div>`));
    if (state.historySearchResults.length && state.historySearchHasMore) {
      const btn = el(`<button class="secondary block">${escapeHtml(t("history.loadMore"))}</button>`);
      btn.addEventListener("click", () => withError(() => loadHistorySearch(false)));
      wrap.appendChild(btn);
    }
    return wrap;
  }

  const today = todayStr();
  const yesterday = dayBeforeStr(today);
  const monday = mondayOfWeekStr();
  const todays = state.historyRecent.filter((c) => c.dueDate === today);
  const yesterdays = state.historyRecent.filter((c) => c.dueDate === yesterday);
  const restOfWeek = state.historyRecent.filter((c) => c.dueDate !== today && c.dueDate !== yesterday && c.dueDate >= monday);

  const groups = [
    renderHistoryGroup(t("history.todayHeading"), visible(todays)),
    renderHistoryGroup(t("history.yesterdayHeading"), visible(yesterdays)),
    renderHistoryGroup(t("history.thisWeekHeading"), visible(restOfWeek)),
  ];

  triggerHistoryLaterLoad();
  const visibleLater = visible(state.historyLater);
  // The "later" bucket is paginated in on demand, so unlike the groups
  // above it still needs a card of its own while it's loading.
  if (!state.historyLaterLoaded) {
    groups.push(el(`<div class="card"><div class="section-label" style="margin:0 0 4px;">${escapeHtml(t("history.laterHeading"))}</div><p class="empty">${escapeHtml(t("history.loading"))}</p></div>`));
  } else {
    groups.push(renderHistoryGroup(t("history.laterHeading"), visibleLater));
  }

  const shown = groups.filter(Boolean);
  if (!shown.length) {
    wrap.appendChild(el(`<div class="card"><p class="empty">${escapeHtml(t("history.empty"))}</p></div>`));
  } else {
    shown.forEach((g) => wrap.appendChild(g));
  }
  if (state.historyLaterLoaded && state.historyLaterHasMore) {
    const btn = el(`<button class="secondary block">${escapeHtml(t("history.loadMore"))}</button>`);
    btn.addEventListener("click", () => withError(() => loadHistoryLater(false)));
    wrap.appendChild(btn);
  }

  return wrap;
}

// ---- Auto-refresh -----------------------------------------------------
// Always on — a background refresh every few minutes keeps task status up
// to date if someone else marks something done while you're looking at
// this page. Not user-configurable: there's no real downside to it, and it
// already pauses itself below whenever it would actually get in the way.

const AUTO_REFRESH_MS = 5 * 60 * 1000;

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

// ---- Settings: push notifications -----------------------------------------------------

function pushSupported() {
  return "serviceWorker" in navigator && "PushManager" in window && "Notification" in window;
}

// VAPID applicationServerKey must be a Uint8Array; the server hands it over
// as the base64url string PushManager.subscribe() itself can't consume.
function urlBase64ToUint8Array(base64String) {
  const padding = "=".repeat((4 - (base64String.length % 4)) % 4);
  const base64 = (base64String + padding).replace(/-/g, "+").replace(/_/g, "/");
  const rawData = atob(base64);
  const output = new Uint8Array(rawData.length);
  for (let i = 0; i < rawData.length; i++) output[i] = rawData.charCodeAt(i);
  return output;
}

async function getCurrentPushSubscription() {
  if (!pushSupported()) return null;
  const reg = await navigator.serviceWorker.ready;
  return reg.pushManager.getSubscription();
}

// undefined = not checked yet, null = checked and not subscribed, object =
// subscribed. Module-level (not in `state`) since it's derived from the
// browser's own PushManager, not server data.
let cachedPushSubscription;
let pushSubscriptionCheckInFlight = false;

function refreshPushSubscriptionCache() {
  if (pushSubscriptionCheckInFlight) return;
  pushSubscriptionCheckInFlight = true;
  getCurrentPushSubscription()
    .then((sub) => {
      cachedPushSubscription = sub || null;
    })
    .catch(() => {
      cachedPushSubscription = null;
    })
    .finally(() => {
      pushSubscriptionCheckInFlight = false;
      if (state.tab === "settings") render();
    });
}

async function enablePushNotifications() {
  if (Notification.permission === "denied") throw new Error(t("settings.notificationsDenied"));
  const permission = await Notification.requestPermission();
  if (permission !== "granted") throw new Error(t("settings.notificationsDenied"));
  if (!state.pushConfig || !state.pushConfig.vapidPublicKey) throw new Error(t("settings.notificationsUnavailable"));

  const reg = await navigator.serviceWorker.ready;
  const sub = await reg.pushManager.subscribe({
    userVisibleOnly: true,
    applicationServerKey: urlBase64ToUint8Array(state.pushConfig.vapidPublicKey),
  });
  const subJson = sub.toJSON();
  await call("SubscribeToPush", {
    userId: state.userId,
    subscription: { endpoint: subJson.endpoint, p256dh: subJson.keys.p256dh, auth: subJson.keys.auth },
  });
  cachedPushSubscription = sub;
}

async function disablePushNotifications() {
  const sub = await getCurrentPushSubscription();
  if (sub) {
    await call("UnsubscribeFromPush", { endpoint: sub.endpoint });
    await sub.unsubscribe();
  }
  cachedPushSubscription = null;
}

// ---- Settings tab -----------------------------------------------------

// Joining a family isn't about the family currently open — it's account-
// level, the same for a parent or a child — so it sits at the very top of
// Settings, above everything scoped to "this" family, rather than inside
// renderFamilyTab. Creating another family from scratch is the same kind
// of account-level action (a parent co-running two households, or a child
// eventually starting their own), so it sits right alongside it.
function renderCreateFamilySection() {
  const defaultName = (state.auth && (state.auth.name || state.auth.email)) || "";
  const card = el(`
    <div class="card">
      <h2>${escapeHtml(t("familyTab.createHeading"))}</h2>
      <p class="hint">${escapeHtml(t("familyTab.createDesc"))}</p>
      <div class="field">
        <label>${escapeHtml(t("onboarding.yourNameLabel"))}</label>
        <input type="text" class="input-full" id="create-family-your-name" placeholder="${escapeHtml(t("onboarding.yourNamePlaceholder"))}" value="${escapeHtml(defaultName)}" />
      </div>
      <div class="field">
        <label>${escapeHtml(t("family.nameLabel"))}</label>
        <input type="text" class="input-full" id="create-family-name" placeholder="${escapeHtml(t("family.namePlaceholder"))}" />
      </div>
      <button type="button" class="block" id="create-family-btn">${escapeHtml(t("family.createBtn"))}</button>
    </div>
  `);
  card.querySelector("#create-family-btn").addEventListener("click", () =>
    withError(async () => {
      const yourName = card.querySelector("#create-family-your-name").value.trim();
      const familyName = card.querySelector("#create-family-name").value.trim();
      if (!familyName) throw new Error(t("familyPicker.nameRequired"));
      await createFamilyAndSwitchTo(familyName, yourName);
      await loadFamilyData();
      state.tab = "home";
    })
  );
  return card;
}

function renderJoinFamilySection() {
  const card = el(`
    <div class="card">
      <h2>${escapeHtml(t("familyTab.joinHeading"))}</h2>
      <p class="hint">${escapeHtml(t("familyTab.joinDesc"))}</p>
      <div class="field">
        <label>${escapeHtml(t("familyTab.joinCodeLabel"))}</label>
        <input type="text" class="input-full" id="join-family-code" />
      </div>
      <button type="button" class="block" id="join-family-btn">${escapeHtml(t("familyTab.joinBtn"))}</button>
    </div>
  `);
  card.querySelector("#join-family-btn").addEventListener("click", () =>
    withError(async () => {
      const code = card.querySelector("#join-family-code").value.trim();
      if (!code) throw new Error(t("familyTab.joinCodeRequired"));
      await joinFamilyWithCode(code);
      render();
    })
  );
  return card;
}

// Settings is a tab of its own now, so it needs no back button — the tab
// bar is always right there. Grouped account first (who you're signed in
// as, and the two device-level preferences), then everything scoped to the
// family currently open, then the account-level "other households" actions
// that aren't about this family at all.
function renderSettingsTab() {
  const wrap = el(`<div></div>`);
  const user = currentUser();
  // The signed-in email belongs to the login, not to whoever's view is
  // currently selected — showing it under a child's name while a parent is
  // viewing as them would claim the wrong thing about that child.
  const homeUserId = getHomeUserId();
  const showIdentity = !homeUserId || state.userId === homeUserId;
  const identity = (showIdentity && state.auth && (state.auth.email || state.auth.name)) || "";

  const accountCard = el(`
    <div class="card">
      <div class="row" style="padding-top:0;">
        <div class="task-row-main">
          ${avatarHtml(user, "lg")}
          <div class="task-row-text">
            <div class="task-title">${escapeHtml(user ? user.name : "")} <span class="pill ${isParent() ? "parent" : "child"}">${escapeHtml(isParent() ? t("role.parent") : t("role.child"))}</span></div>
            ${identity ? `<div class="task-meta">${escapeHtml(identity)}</div>` : ""}
          </div>
        </div>
      </div>
      <div class="actions stack" style="margin-top:12px;">
        <button type="button" class="secondary" id="logout-btn">${escapeHtml(t("auth.logout"))}</button>
      </div>
    </div>
  `);
  accountCard.querySelector("#logout-btn").addEventListener("click", () => {
    location.href = "/auth/logout";
  });
  wrap.appendChild(el(`<div class="section-label" style="margin-top:0;">${escapeHtml(t("settings.accountSection"))}</div>`));
  wrap.appendChild(accountCard);

  wrap.appendChild(renderLangSwitcher());

  const notifCard = el(`<div class="card"><h2>${escapeHtml(t("settings.notificationsHeading"))}</h2></div>`);
  if (!pushSupported()) {
    notifCard.appendChild(el(`<p class="empty">${escapeHtml(t("settings.notificationsUnsupported"))}</p>`));
  } else if (state.pushConfig && !state.pushConfig.vapidPublicKey) {
    notifCard.appendChild(el(`<p class="empty">${escapeHtml(t("settings.notificationsUnavailable"))}</p>`));
  } else if (Notification.permission === "denied") {
    notifCard.appendChild(el(`<p class="empty">${escapeHtml(t("settings.notificationsDenied"))}</p>`));
  } else if (cachedPushSubscription === undefined) {
    notifCard.appendChild(el(`<p class="empty">${escapeHtml(t("settings.notificationsChecking"))}</p>`));
    refreshPushSubscriptionCache();
  } else if (cachedPushSubscription) {
    notifCard.appendChild(el(`<p>${escapeHtml(t("settings.notificationsEnabledOnDevice"))}</p>`));
    const btn = el(`<button class="secondary">${escapeHtml(t("settings.notificationsDisable"))}</button>`);
    btn.addEventListener("click", () => withError(disablePushNotifications));
    notifCard.appendChild(btn);
  } else {
    notifCard.appendChild(el(`<p class="hint">${escapeHtml(t("settings.notificationsDesc"))}</p>`));
    const btn = el(`<button>${escapeHtml(t("settings.notificationsEnable"))}</button>`);
    btn.addEventListener("click", () => withError(enablePushNotifications));
    notifCard.appendChild(btn);
  }
  wrap.appendChild(notifCard);

  // Managing who's in the family, and inviting new members, isn't something
  // you do often — and isn't relevant to a child at all — so it lives here
  // rather than as its own always-visible tab.
  if (isParent()) {
    const family = state.families.find((f) => f.id === state.familyId);
    wrap.appendChild(el(`<div class="section-label">${escapeHtml(family ? family.name : t("settings.familySection"))}</div>`));
    wrap.appendChild(renderFamilyTab());
  }

  // Joining or starting another family isn't about the family currently
  // open — it's account-level, the same for a parent or a child — so it
  // sits apart from everything above, at the end.
  wrap.appendChild(el(`<div class="section-label">${escapeHtml(t("settings.householdsSection"))}</div>`));
  wrap.appendChild(renderCreateFamilySection());
  wrap.appendChild(renderJoinFamilySection());

  return wrap;
}

// ---- Dashboard mode: kiosk view of the Today tab, no login -----------------------------------------------------
//
// Reached at /dashboard, authorized by a per-family secret key instead of a
// login — meant for a wall-mounted tablet or shared screen showing every
// child's status for the day, with the same tap-to-complete checkboxes the
// parent Today tab has. The key comes in via ?key=... the first time (then
// stored locally and stripped from the URL) or can be typed directly.

const DASHBOARD_KEY_HEADER = "X-Dashboard-Key";
const DASHBOARD_KEY_STORAGE = "chores.dashboardKey";

function isDashboardRoute() {
  return window.location.pathname === "/dashboard";
}

async function loadDashboardData() {
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

function renderDashboardKeyPrompt() {
  const wrap = el(`<div class="hero" style="text-align:left;"></div>`);
  wrap.appendChild(el(`
    <div style="text-align:center;">
      <img src="/icons/logo.png" alt="" width="80" height="80" class="logo" />
      <h1>${escapeHtml(window.APP_NAME)}</h1>
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
      const key = form.querySelector("#dashboard-key-input").value.trim();
      if (!key) throw new Error(t("dashboard.keyRequired"));
      await tryDashboardKey(key);
    });
  form.querySelector("#dashboard-key-submit").addEventListener("click", submit);
  form.querySelector("#dashboard-key-input").addEventListener("keydown", (e) => {
    if (e.key === "Enter") submit();
  });
  wrap.appendChild(form);
  return wrap;
}

async function bootDashboard() {
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

// ---- Settings: dashboard setup (parents only) -----------------------------------------------------

let dashboardConfigLoadInFlight = false;
function triggerDashboardConfigLoad() {
  if (dashboardConfigLoadInFlight || state.dashboardConfig !== null) return;
  dashboardConfigLoadInFlight = true;
  call("GetDashboardConfig", { familyId: state.familyId })
    .then((resp) => {
      state.dashboardConfig = resp;
    })
    .catch((e) => {
      state.dashboardConfig = { enabled: false };
      state.error = e.message || String(e);
    })
    .finally(() => {
      dashboardConfigLoadInFlight = false;
      if (state.tab === "settings") render();
    });
}

function renderDashboardSettingsSection() {
  const card = el(`<div class="card"><h2>${escapeHtml(t("dashboard.settingsHeading"))}</h2></div>`);
  card.appendChild(el(`<p>${escapeHtml(t("dashboard.settingsDesc"))}</p>`));

  if (state.dashboardConfig === null) {
    card.appendChild(el(`<p class="empty">${escapeHtml(t("dashboard.loading"))}</p>`));
    triggerDashboardConfigLoad();
    return card;
  }

  if (state.dashboardConfig.enabled) {
    const url = `${window.location.origin}/dashboard?key=${encodeURIComponent(state.dashboardConfig.dashboardKey)}`;
    card.appendChild(el(`
      <div class="field">
        <label>${escapeHtml(t("dashboard.urlLabel"))}</label>
        <input type="text" class="input-full" readonly value="${escapeHtml(url)}" onclick="this.select()" />
      </div>
      <div class="field">
        <label>${escapeHtml(t("dashboard.keyLabel"))}</label>
        <input type="text" class="input-full" readonly value="${escapeHtml(state.dashboardConfig.dashboardKey)}" onclick="this.select()" />
      </div>
    `));
    const actions = el(`<div class="actions"></div>`);
    const regenBtn = el(`<button class="secondary">${escapeHtml(t("dashboard.regenerate"))}</button>`);
    regenBtn.addEventListener("click", () =>
      withError(async () => {
        const resp = await call("SetupDashboard", { familyId: state.familyId });
        state.dashboardConfig = { enabled: true, dashboardKey: resp.dashboardKey };
      })
    );
    const disableBtn = el(`<button class="danger">${escapeHtml(t("dashboard.disable"))}</button>`);
    disableBtn.addEventListener("click", () =>
      withError(async () => {
        await call("DisableDashboard", { familyId: state.familyId });
        state.dashboardConfig = { enabled: false, dashboardKey: "" };
      })
    );
    actions.appendChild(regenBtn);
    actions.appendChild(disableBtn);
    card.appendChild(actions);
  } else {
    const setupBtn = el(`<button>${escapeHtml(t("dashboard.setup"))}</button>`);
    setupBtn.addEventListener("click", () =>
      withError(async () => {
        const resp = await call("SetupDashboard", { familyId: state.familyId });
        state.dashboardConfig = { enabled: true, dashboardKey: resp.dashboardKey };
      })
    );
    card.appendChild(setupBtn);
  }
  return card;
}

// ---- boot -----------------------------------------------------

if (isDashboardRoute()) {
  bootDashboard();
} else {
  withError(async () => {
    await loadAuth();
    // A login may be bound to more than one family (e.g. a child who's a
    // member of two households). Always resolve this from the server on
    // boot rather than trusting stale localStorage, since a different
    // login could have used this same browser before.
    await loadMembership();
    if (state.membership.bound) {
      const memberships = state.membership.memberships;
      const stored = memberships.find((m) => m.family.id === state.familyId && m.user.id === state.userId);
      // No family-selection landing page: a still-valid stored choice wins,
      // otherwise auto-pick the first membership. Picking between several
      // families happens via the topbar switcher once inside the app, not
      // as a gate in front of it.
      const toSelect = stored || memberships[0];
      if (toSelect) {
        selectMembership(toSelect);
        await loadFamilyData();
      } else {
        setFamilyId(null);
        setUserId(null);
      }
    } else {
      setFamilyId(null);
      setUserId(null);
    }
    // Non-fatal: push notifications are an optional extra, so a failure here
    // (e.g. the server has no VAPID keys yet) shouldn't surface as a
    // page-wide error banner.
    try {
      await loadPushConfig();
    } catch (e) {
      console.warn("push config unavailable:", e);
    }
  });
}
