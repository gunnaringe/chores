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

  const invResp = await call("ListInvitations", { familyId: state.familyId });
  state.invitations = invResp.invitations || [];

  // The History tab's today/yesterday/this-week groups are cheap (at most a
  // week of rows) and stay fresh via the same auto-refresh as everything
  // else; the paginated "later" bucket and search results are loaded
  // separately, on demand, only while that tab is actually open.
  if (isParent()) {
    const histResp = await call("ListTaskCompletions", {
      familyId: state.familyId,
      startDate: mondayOfWeekStr(),
      endDate: todayStr(),
    });
    state.historyRecent = histResp.completions || [];
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
  const bar = el(`
    <div style="display:flex;justify-content:flex-end;margin-bottom:4px;">
      <select id="lang-switcher" aria-label="${escapeHtml(t("lang.label"))}" style="font-size:0.8rem;padding:4px 8px;">
        ${options}
      </select>
    </div>
  `);
  bar.querySelector("#lang-switcher").addEventListener("change", (e) => {
    setLang(e.target.value);
    render();
  });
  return bar;
}

function render() {
  const app = document.getElementById("app");
  app.innerHTML = "";

  app.appendChild(renderLangSwitcher());

  // The kiosk dashboard is a completely separate, much smaller UI — no
  // login, no family/user picker, no tabs — so it's handled before any of
  // the normal app's routing below even looks at auth/membership state,
  // none of which applies to it.
  if (state.dashboardMode) {
    if (state.error) {
      app.appendChild(el(`<div class="error">${escapeHtml(state.error)}</div>`));
    }
    if (!state.dashboardKey) {
      app.appendChild(renderDashboardKeyPrompt());
    } else {
      app.appendChild(el(`<h1>${escapeHtml(window.APP_NAME)}</h1>`));
      app.appendChild(renderTodayTab());
    }
    return;
  }

  if (state.auth && state.auth.authenticated) {
    app.appendChild(
      el(`
        <div style="display:flex;justify-content:flex-end;gap:10px;align-items:center;font-size:0.85rem;color:var(--muted);margin-bottom:8px;">
          <span>${escapeHtml(t("auth.signedInAs", { name: state.auth.name || state.auth.email || "" }))}</span>
          <a class="link-btn" href="/auth/logout">${escapeHtml(t("auth.logout"))}</a>
        </div>
      `)
    );
  }

  if (state.error) {
    app.appendChild(el(`<div class="error">${escapeHtml(state.error)}</div>`));
  }

  if (!state.membership || !state.membership.bound) {
    app.appendChild(renderOnboarding());
    return;
  }

  // A login can be bound to more than one family (e.g. a child who's a
  // member of two households). If there's more than one and none is
  // currently selected, ask which one to open.
  if (state.membership.memberships.length > 1 && !state.familyId) {
    app.appendChild(renderHouseholdPicker());
    return;
  }

  if (!state.userId) {
    app.appendChild(renderUserPicker());
    return;
  }

  app.appendChild(renderTopbar());

  if (state.tab === "settings") {
    app.appendChild(renderSettingsTab());
    return;
  }

  if (state.tab === "createFamily") {
    app.appendChild(renderCreateFamilyTab());
    return;
  }

  // Parents get a dashboard-style "Today" tab (today's status per child, at
  // a glance) plus separate tabs for managing tasks, payouts/accounting, and
  // browsing history — those are different activities done at different
  // times, not one big page. A child has exactly one thing to do here
  // (their own tasks, with their own earnings shown right there), so they
  // get no tab bar at all rather than a bar with a single button on it.
  const parentMode = isParent();
  if (!parentMode) {
    app.appendChild(renderChildTasksTab());
    return;
  }

  const tabDefs = [
    { key: "home", label: t("tabs.today") },
    { key: "history", label: t("tabs.history") },
    { key: "tasks", label: t("tabs.tasks") },
    { key: "accounting", label: t("tabs.accounting") },
  ];
  const activeTab = tabDefs.some((d) => d.key === state.tab) ? state.tab : tabDefs[0].key;

  const tabs = el(
    `<div class="tabs">${tabDefs
      .map((d) => `<button data-tab="${d.key}" class="${activeTab === d.key ? "active" : ""}">${escapeHtml(d.label)}</button>`)
      .join("")}</div>`
  );
  tabs.querySelectorAll("button").forEach((b) =>
    b.addEventListener("click", () => {
      state.tab = b.dataset.tab;
      state.editingTaskId = null;
      confirmingDeleteCompletionId = null;
      confirmingDeleteTaskId = null;
      render();
    })
  );
  app.appendChild(tabs);

  if (activeTab === "home") app.appendChild(renderTodayTab());
  else if (activeTab === "history") app.appendChild(renderHistoryTab());
  else if (activeTab === "tasks") app.appendChild(renderTasksManagementTab());
  else app.appendChild(renderAccountingTab());
}

function escapeHtml(s) {
  const d = document.createElement("div");
  d.textContent = s;
  return d.innerHTML;
}

// Font Awesome icon names only ever legitimately contain lowercase
// letters, digits and hyphens. Whitelisting down to that (after stripping
// common paste artifacts like a leading "fa-") is what actually makes it
// safe to drop this user-supplied value straight into a class="..."
// attribute — HTML-escaping alone isn't enough there, since escapeHtml
// only guards text-node content, not attribute-breaking characters.
function faIconClass(value) {
  let cleaned = String(value || "")
    .trim()
    .toLowerCase()
    .replace(/^fa-solid\s+/, "")
    .replace(/^fas\s+/, "")
    .replace(/^fa-/, "")
    .replace(/[^a-z0-9-]/g, "");
  return cleaned || "star";
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

function taskLabel(task) {
  if (!task.icon || !task.icon.value) return escapeHtml(task.title);
  if (task.icon.type === "ICON_TYPE_FONT_AWESOME") {
    return `<i class="fa-solid fa-${faIconClass(task.icon.value)}"></i> ${escapeHtml(task.title)}`;
  }
  if (task.icon.type === "ICON_TYPE_MATERIAL_SYMBOLS") {
    return `<span class="material-symbols-outlined" style="vertical-align:middle;font-size:1.1em;">${escapeHtml(
      materialIconName(task.icon.value)
    )}</span> ${escapeHtml(task.title)}`;
  }
  return escapeHtml(task.icon.value) + " " + escapeHtml(task.title);
}

const EMOJI_CHOICES = ["🧹", "🧺", "🍽️", "🛏️", "🐶", "🗑️", "📚", "🧽", "🚗", "🌱", "🪥", "🧸"];
const FA_CHOICES = ["broom", "shirt", "utensils", "bed", "dog", "trash", "book", "soap", "car", "seedling", "tooth", "paw"];
const MATERIAL_CHOICES = [
  "cleaning_services",
  "checkroom",
  "restaurant",
  "bed",
  "pets",
  "delete",
  "menu_book",
  "soap",
  "directions_car",
  "eco",
  "brush",
  "toys",
];

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

// Shared by the onboarding screen (genuinely no family yet) and the
// "+ new family" tab reached from an already-open family (see
// renderCreateFamilyTab) — same two forms either way: create one, or join
// one with an invite code. onDone runs after either succeeds, once the
// newly created/joined family is already selected and its data loaded.
function renderCreateAndJoinFamilyForms(onDone) {
  const wrap = el(`<div></div>`);

  const form = el(`
    <div class="card">
      <h2>${escapeHtml(t("onboarding.heading"))}</h2>
      <div class="field">
        <label>${escapeHtml(t("onboarding.yourNameLabel"))}</label>
        <input type="text" id="onboard-parent-name" placeholder="${escapeHtml(t("onboarding.yourNamePlaceholder"))}" />
      </div>
      <div class="field">
        <label>${escapeHtml(t("family.nameLabel"))}</label>
        <input type="text" id="onboard-family-name" placeholder="${escapeHtml(t("family.namePlaceholder"))}" />
      </div>
      <button id="onboard-create-btn">${escapeHtml(t("family.createBtn"))}</button>
    </div>
  `);
  form.querySelector("#onboard-create-btn").addEventListener("click", () =>
    withError(async () => {
      const parentName = form.querySelector("#onboard-parent-name").value.trim();
      const familyName = form.querySelector("#onboard-family-name").value.trim();
      if (!familyName) throw new Error(t("familyPicker.nameRequired"));
      await createFamilyAndSwitchTo(familyName, parentName);
      await loadFamilyData();
      onDone();
    })
  );
  wrap.appendChild(form);

  const joinForm = el(`
    <div class="card">
      <h2>${escapeHtml(t("onboarding.joinHeading"))}</h2>
      <div class="field">
        <label>${escapeHtml(t("onboarding.joinCodeLabel"))}</label>
        <input type="text" id="onboard-join-code" />
      </div>
      <button type="button" id="onboard-join-btn" class="secondary">${escapeHtml(t("onboarding.joinBtn"))}</button>
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
      onDone();
    })
  );
  wrap.appendChild(joinForm);

  return wrap;
}

function renderOnboarding() {
  const wrap = el(`<div></div>`);
  wrap.appendChild(el(`
    <h1>${window.APP_NAME}</h1>
    <p>${escapeHtml(t("onboarding.subtitle"))}</p>
  `));
  wrap.appendChild(renderCreateAndJoinFamilyForms(() => {}));
  return wrap;
}

function renderHouseholdPicker() {
  const wrap = el(`<div></div>`);
  wrap.appendChild(el(`<h1>${window.APP_NAME}</h1><p>${escapeHtml(t("householdPicker.subtitle"))}</p>`));

  const card = el(`<div class="card"></div>`);
  state.membership.memberships.forEach((m) => {
    const row = el(`
      <div class="row">
        <span>${escapeHtml(m.family.name)} — ${escapeHtml(m.user.name)} <span class="pill ${m.user.role === "USER_ROLE_PARENT" ? "parent" : "child"}">${escapeHtml(
      roleLabel(m.user.role)
    )}</span></span>
        <button>${escapeHtml(t("familyPicker.open"))}</button>
      </div>
    `);
    row.querySelector("button").addEventListener("click", () =>
      withError(async () => {
        selectMembership(m);
        await loadFamilyData();
      })
    );
    card.appendChild(row);
  });
  wrap.appendChild(card);
  return wrap;
}

// Reached via the "+" next to the family name in the topbar — the same
// create/join forms as onboarding, just for someone who's already a member
// of at least one family and wants another (a parent co-running two
// households, or anyone redeeming a second invite code).
function renderCreateFamilyTab() {
  const wrap = el(`<div></div>`);
  const backBtn = el(`<button class="secondary" style="margin-bottom:16px;">${escapeHtml(t("settings.back"))}</button>`);
  backBtn.addEventListener("click", () => {
    state.tab = isParent() ? "home" : "tasks";
    render();
  });
  wrap.appendChild(backBtn);
  wrap.appendChild(el(`<h2>${escapeHtml(t("family.createOrJoinHeading"))}</h2>`));
  wrap.appendChild(
    renderCreateAndJoinFamilyForms(() => {
      state.tab = isParent() ? "home" : "tasks";
    })
  );
  return wrap;
}

function renderUserPicker() {
  const wrap = el(`<div></div>`);
  const family = state.families.find((f) => f.id === state.familyId);
  wrap.appendChild(
    el(`
      <div class="topbar">
        <h1>${escapeHtml(family ? family.name : window.APP_NAME)}</h1>
      </div>
      <p>${escapeHtml(t("userPicker.whoIsUsing"))}</p>
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
          <span>${escapeHtml(u.name)} <span class="pill ${u.role === "USER_ROLE_PARENT" ? "parent" : "child"}">${escapeHtml(roleLabel(u.role))}</span></span>
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
        <input type="text" id="new-user-name" placeholder="${escapeHtml(t("addUser.namePlaceholder"))}" />
      </div>
      <div class="field">
        <label>${escapeHtml(t("addUser.roleLabel"))}</label>
        <select id="new-user-role">
          <option value="USER_ROLE_PARENT">${escapeHtml(t("role.parent"))}</option>
          <option value="USER_ROLE_CHILD">${escapeHtml(t("role.child"))}</option>
        </select>
      </div>
      <button id="add-user-btn">${escapeHtml(t("addUser.add"))}</button>
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
  if (!isParent()) return [];
  const homeUserId = getHomeUserId();
  const canContinueAs = (u) => u.role !== "USER_ROLE_PARENT" || !homeUserId || u.id === homeUserId;
  return sortMembersForDisplay(state.users.filter(canContinueAs));
}

// Every family the topbar's family-name dropdown can switch straight to —
// the login's own memberships (what "Switch household" used to list).
function familySwitchOptions() {
  return (state.membership && state.membership.memberships) || [];
}

function renderTopbar() {
  const family = state.families.find((f) => f.id === state.familyId);
  const user = currentUser();
  const familyOptions = familySwitchOptions();
  const userOptions = switchableUsers();

  const familyNameEl =
    familyOptions.length > 1
      ? el(`
          <select class="plain-select" id="family-switch">
            ${familyOptions
              .map((o) => `<option value="${o.family.id}" ${o.family.id === state.familyId ? "selected" : ""}>${escapeHtml(o.family.name)}</option>`)
              .join("")}
          </select>
        `)
      : el(`<h1>${escapeHtml(family ? family.name : window.APP_NAME)}</h1>`);

  const userNameEl =
    userOptions.length > 1
      ? el(`
          <select class="plain-select" id="user-switch">
            ${userOptions.map((u) => `<option value="${u.id}" ${u.id === state.userId ? "selected" : ""}>${escapeHtml(u.name)}</option>`).join("")}
          </select>
        `)
      : el(`<span>${escapeHtml(user ? user.name : "")}</span>`);

  const bar = el(`
    <div class="topbar">
      <div>
        <div class="family-row" style="display:flex;align-items:center;gap:4px;"></div>
        <p style="margin:0;display:flex;align-items:center;gap:6px;"><span class="pill ${isParent() ? "parent" : "child"}">${escapeHtml(isParent() ? t("role.parent") : t("role.child"))}</span></p>
      </div>
      <div class="actions">
        <button class="secondary" id="open-settings">${escapeHtml(t("topbar.settings"))}</button>
      </div>
    </div>
  `);
  const familyRow = bar.querySelector(".family-row");
  familyRow.appendChild(familyNameEl);
  const addFamilyBtn = el(`<button type="button" class="secondary btn-icon" id="add-family" title="${escapeHtml(t("family.createOrJoinHeading"))}">+</button>`);
  familyRow.appendChild(addFamilyBtn);
  bar.querySelector("p").prepend(userNameEl);

  if (familyOptions.length > 1) {
    bar.querySelector("#family-switch").addEventListener("change", (e) =>
      withError(async () => {
        const selected = familyOptions.find((o) => o.family.id === e.target.value);
        if (!selected) return;
        selectMembership(selected);
        await loadFamilyData();
      })
    );
  }
  if (userOptions.length > 1) {
    bar.querySelector("#user-switch").addEventListener("change", (e) =>
      withError(async () => {
        const selected = userOptions.find((u) => u.id === e.target.value);
        if (!selected) return;
        setUserId(selected.id);
        await loadFamilyData();
      })
    );
  }
  addFamilyBtn.addEventListener("click", () => {
    state.tab = "createFamily";
    render();
  });
  bar.querySelector("#open-settings").addEventListener("click", () => {
    state.tab = "settings";
    render();
  });
  return bar;
}

// ---- Today tab (parents): today's status per child, at a glance -----------------------------------------------------

function renderTodayTab() {
  const wrap = el(`<div></div>`);
  if (!state.summaries.length) {
    wrap.appendChild(el(`<div class="card"><p class="empty">${escapeHtml(t("accounting.noChildren"))}</p></div>`));
    return wrap;
  }

  state.summaries.forEach((s) => {
    const card = el(`<div class="card"><h2>${escapeHtml(s.child.name)}</h2></div>`);

    const todays = state.occurrences.filter((o) => o.childId === s.child.id && o.task.active !== false);
    if (!todays.length) {
      card.appendChild(el(`<p class="empty">${escapeHtml(t("childTasks.empty"))}</p>`));
    } else {
      todays.forEach((occ) => {
        const done = !!occ.completed;
        const row = el(`
          <div class="row">
            <div>
              <div class="task-title">${taskLabel(occ.task)}</div>
              <div class="task-meta">kr ${money(occ.task.priceCents)}</div>
            </div>
            <button class="checkbtn ${done ? "done" : "todo"}" title="${escapeHtml(done ? t("childTasks.markNotDone") : t("childTasks.markDone"))}">${done ? "✓" : ""}</button>
          </div>
        `);
        row.querySelector("button").addEventListener("click", () =>
          withError(async () => {
            if (done) {
              await call("UncompleteTask", { taskId: occ.task.id, childId: s.child.id, dueDate: occ.dueDate });
            } else {
              await call("CompleteTask", { taskId: occ.task.id, childId: s.child.id, dueDate: occ.dueDate });
            }
            await loadFamilyData();
          })
        );
        card.appendChild(row);
      });
    }

    card.appendChild(el(`
      <div class="grid-2" style="margin-top:10px;">
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
        <div class="grid-3">
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
  mine.forEach((occ) => {
    const done = !!occ.completed;
    const row = el(`
      <div class="row">
        <div>
          <div class="task-title">${taskLabel(occ.task)}</div>
          <div class="task-meta">kr ${money(occ.task.priceCents)}${occ.task.description ? " — " + escapeHtml(occ.task.description) : ""}</div>
        </div>
        <button class="checkbtn ${done ? "done" : "todo"}" title="${escapeHtml(done ? t("childTasks.markNotDone") : t("childTasks.markDone"))}">${done ? "✓" : ""}</button>
      </div>
    `);
    row.querySelector("button").addEventListener("click", () =>
      withError(async () => {
        if (done) {
          await call("UncompleteTask", {
            taskId: occ.task.id,
            childId: state.userId,
            dueDate: occ.dueDate,
          });
        } else {
          await call("CompleteTask", {
            taskId: occ.task.id,
            childId: state.userId,
            dueDate: occ.dueDate,
          });
        }
        await loadFamilyData();
      })
    );
    card.appendChild(row);
  });
  return card;
}

// ---- Tasks tab (parents): manage task definitions -----------------------------------------------------

function renderTasksManagementTab() {
  const wrap = el(`<div></div>`);
  wrap.appendChild(renderTaskList());
  const editingTask = state.editingTaskId ? state.tasks.find((t_) => t_.id === state.editingTaskId) : null;
  wrap.appendChild(renderTaskForm(editingTask));
  return wrap;
}

// Which task (by id) is showing its inline "are you sure" state, if any —
// same module-level, reset-on-navigation pattern as
// confirmingDeleteCompletionId in the History tab, so both delete flows
// look and behave identically.
let confirmingDeleteTaskId = null;

function renderTaskList() {
  const card = el(`<div class="card"><h2>${escapeHtml(t("taskList.heading"))}</h2></div>`);
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
        <div>
          <div class="task-title">${taskLabel(t_)} ${t_.active === false ? `<span class="pill">${escapeHtml(t("taskList.paused"))}</span>` : ""}</div>
          <div class="task-meta">kr ${money(t_.priceCents)} · ${escapeHtml(repeatLabel(t_))}${t_.description ? " · " + escapeHtml(t_.description) : ""}${assignedNames ? " · " + escapeHtml(assignedNames) : ""}</div>
        </div>
        <div class="actions">
          ${
            confirmingDelete
              ? `<button class="danger" data-action="confirm-delete">${escapeHtml(t("history.confirmDelete"))}</button>
                 <button type="button" class="secondary" data-action="cancel-delete">${escapeHtml(t("taskList.cancel"))}</button>`
              : `<button class="secondary" data-action="edit">${escapeHtml(t("taskList.edit"))}</button>
                 <button class="secondary btn-icon" data-action="toggle" title="${escapeHtml(t_.active === false ? t("taskList.resume") : t("taskList.pause"))}"><span class="material-symbols-outlined">${t_.active === false ? "play_arrow" : "pause"}</span></button>
                 <button class="danger" data-action="delete">${escapeHtml(t("taskList.delete"))}</button>`
          }
        </div>
      </div>
    `);

    const editBtn = row.querySelector('[data-action="edit"]');
    if (editBtn) {
      editBtn.addEventListener("click", () => {
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
          if (state.editingTaskId === t_.id) state.editingTaskId = null;
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

function iconTypeKey(icon) {
  if (!icon) return "EMOJI";
  if (icon.type === "ICON_TYPE_FONT_AWESOME") return "FONT_AWESOME";
  if (icon.type === "ICON_TYPE_MATERIAL_SYMBOLS") return "MATERIAL_SYMBOLS";
  return "EMOJI";
}

function renderTaskForm(existingTask) {
  const isEdit = !!existingTask;
  const children = state.users.filter((u) => u.role === "USER_ROLE_CHILD");
  const form = el(`
    <div class="card">
      <h2>${escapeHtml(isEdit ? t("taskList.editHeading") : t("addTask.heading"))}</h2>
      <div class="field">
        <label>${escapeHtml(t("addTask.titleLabel"))}</label>
        <input type="text" id="task-title" placeholder="${escapeHtml(t("addTask.titlePlaceholder"))}" />
      </div>
      <div class="field">
        <label>${escapeHtml(t("addTask.descLabel"))}</label>
        <input type="text" id="task-desc" />
      </div>
      <div class="field">
        <label>${escapeHtml(t("addTask.iconLabel"))}</label>
        <div class="icon-type-toggle">
          <button type="button" class="secondary" data-icon-type="EMOJI">${escapeHtml(t("addTask.iconTypeEmoji"))}</button>
          <button type="button" class="secondary" data-icon-type="FONT_AWESOME">${escapeHtml(t("addTask.iconTypeFontAwesome"))}</button>
          <button type="button" class="secondary" data-icon-type="MATERIAL_SYMBOLS">${escapeHtml(t("addTask.iconTypeMaterialSymbols"))}</button>
        </div>
        <input type="text" id="task-icon" maxlength="32" class="input-icon" />
        <div id="task-icon-choices" class="icon-choices" style="margin-top:6px;"></div>
      </div>
      <div class="field">
        <label>${escapeHtml(t("addTask.priceLabel"))}</label>
        <input type="number" id="task-price" min="0" step="0.5" value="10" class="input-price" />
      </div>
      <div class="field">
        <label>${escapeHtml(t("addTask.repeatLabel"))}</label>
        <div class="repeat-mode-toggle">
          <button type="button" class="secondary" data-repeat-mode="ONCE">${escapeHtml(t("addTask.repeatOnce"))}</button>
          <button type="button" class="secondary" data-repeat-mode="WEEKLY">${escapeHtml(t("addTask.repeatWeekly"))}</button>
          <button type="button" class="secondary" data-repeat-mode="CRON">${escapeHtml(t("addTask.repeatCron"))}</button>
        </div>
        <div id="repeat-once-fields" style="margin-top:8px;">
          <label>${escapeHtml(t("addTask.onceDateLabel"))}</label>
          <input type="date" id="task-once-date" class="input-date" />
        </div>
        <div id="repeat-weekly-fields" style="margin-top:8px;">
          <div id="task-days"></div>
          <label style="margin-top:8px;">${escapeHtml(t("addTask.everyNWeeksLabel"))}</label>
          <input type="number" id="task-interval-weeks" min="1" max="52" value="1" class="input-interval" />
        </div>
        <div id="repeat-cron-fields" style="margin-top:8px;">
          <label>${escapeHtml(t("addTask.cronLabel"))}</label>
          <input type="text" id="task-cron" placeholder="0 0 * * 1,3,5" class="input-cron" />
          <p class="hint" style="margin:4px 0 0;font-size:0.8rem;">${escapeHtml(t("addTask.cronHint"))}</p>
        </div>
      </div>
      <div class="field">
        <label>${escapeHtml(t("addTask.assignLabel"))}</label>
        <div id="task-children"></div>
      </div>
      <div class="actions">
        <button id="save-task-btn">${escapeHtml(isEdit ? t("taskList.saveChanges") : t("addTask.addBtn"))}</button>
        ${isEdit ? `<button type="button" class="secondary" id="cancel-edit-btn">${escapeHtml(t("taskList.cancel"))}</button>` : ""}
      </div>
    </div>
  `);

  if (isEdit) {
    form.querySelector("#task-title").value = existingTask.title;
    form.querySelector("#task-desc").value = existingTask.description || "";
    form.querySelector("#task-price").value = (Number(existingTask.priceCents || 0) / 100).toFixed(2);
  }

  const iconInput = form.querySelector("#task-icon");
  const iconChoicesWrap = form.querySelector("#task-icon-choices");
  const iconTypeButtons = [...form.querySelectorAll(".icon-type-toggle button")];
  let selectedIconType = iconTypeKey(isEdit ? existingTask.icon : null);

  const iconPlaceholders = { EMOJI: "🧹", FONT_AWESOME: "broom", MATERIAL_SYMBOLS: "cleaning_services" };
  const iconChoiceLists = { EMOJI: EMOJI_CHOICES, FONT_AWESOME: FA_CHOICES, MATERIAL_SYMBOLS: MATERIAL_CHOICES };

  function renderIconChoices() {
    iconChoicesWrap.innerHTML = "";
    iconChoiceLists[selectedIconType].forEach((value) => {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "secondary";
      if (selectedIconType === "EMOJI") {
        btn.textContent = value;
      } else if (selectedIconType === "FONT_AWESOME") {
        const i = document.createElement("i");
        i.className = `fa-solid fa-${faIconClass(value)}`;
        btn.appendChild(i);
      } else {
        const span = document.createElement("span");
        span.className = "material-symbols-outlined";
        span.textContent = materialIconName(value);
        btn.appendChild(span);
      }
      btn.addEventListener("click", () => {
        iconInput.value = value;
      });
      iconChoicesWrap.appendChild(btn);
    });
  }

  function selectIconType(newType) {
    selectedIconType = newType;
    iconTypeButtons.forEach((b) => b.classList.toggle("active", b.dataset.iconType === newType));
    iconInput.placeholder = iconPlaceholders[newType];
    renderIconChoices();
  }

  iconTypeButtons.forEach((btn) => btn.addEventListener("click", () => selectIconType(btn.dataset.iconType)));
  selectIconType(selectedIconType);
  if (isEdit && existingTask.icon) iconInput.value = existingTask.icon.value;

  const daysWrap = form.querySelector("#task-days");
  const dow = DOW();
  const preCheckedDays = isEdit ? existingTask.daysOfWeek || [] : [];
  dow.forEach((d) => {
    const id = `day-${d.code}`;
    const checked = isEdit ? preCheckedDays.includes(d.code) : d.code >= 1 && d.code <= 5;
    const label = el(`<label style="display:inline-flex;align-items:center;gap:4px;margin-right:8px;font-size:0.85rem;">
      <input type="checkbox" id="${id}" ${checked ? "checked" : ""}/> ${escapeHtml(d.label)}
    </label>`);
    daysWrap.appendChild(label);
  });

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
    const checksWrap = el(`<div style="display:flex;flex-wrap:wrap;gap:4px 12px;margin-bottom:8px;"></div>`);
    children.forEach((c) => {
      const label = el(`<label style="display:inline-flex;align-items:center;gap:4px;font-size:0.85rem;">
        <input type="checkbox" data-child-id="${c.id}" ${assignedIds.has(c.id) ? "checked" : ""} /> ${escapeHtml(c.name)}
      </label>`);
      checksWrap.appendChild(label);
    });
    childrenWrap.appendChild(checksWrap);
    const selectAllBtn = el(`<button type="button" class="secondary">${escapeHtml(t("addTask.selectAll"))}</button>`);
    selectAllBtn.addEventListener("click", () => {
      checksWrap.querySelectorAll('input[type="checkbox"]').forEach((cb) => (cb.checked = true));
    });
    childrenWrap.appendChild(selectAllBtn);
  }

  form.querySelector("#save-task-btn").addEventListener("click", () =>
    withError(async () => {
      const title = form.querySelector("#task-title").value.trim();
      const description = form.querySelector("#task-desc").value.trim();
      const iconValueRaw = iconInput.value.trim();
      const iconTypeProto = { EMOJI: "ICON_TYPE_EMOJI", FONT_AWESOME: "ICON_TYPE_FONT_AWESOME", MATERIAL_SYMBOLS: "ICON_TYPE_MATERIAL_SYMBOLS" }[
        selectedIconType
      ];
      const iconValue =
        selectedIconType === "FONT_AWESOME" ? faIconClass(iconValueRaw) : selectedIconType === "MATERIAL_SYMBOLS" ? materialIconName(iconValueRaw) : iconValueRaw;
      const icon = iconValueRaw ? { type: iconTypeProto, value: iconValue } : undefined;
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
        });
        state.editingTaskId = null;
      } else {
        await call("CreateTask", {
          familyId: state.familyId,
          title,
          description,
          icon,
          priceCents: Math.round(priceKr * 100),
          ...repeatFields,
          childIds,
        });
      }
      await loadFamilyData();
    })
  );
  const cancelBtn = form.querySelector("#cancel-edit-btn");
  if (cancelBtn) {
    cancelBtn.addEventListener("click", () => {
      state.editingTaskId = null;
      render();
    });
  }
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

  summaries.forEach((s) => {
    const card = el(`
      <div class="card">
        <h2>${escapeHtml(s.child.name)}</h2>
        <div class="grid-2">
          <div class="stat"><div class="value">kr ${money(s.earnedLast7DaysCents)}</div><div class="label">${escapeHtml(t("accounting.last7Days"))}</div></div>
          <div class="stat"><div class="value">kr ${money(s.balanceCents)}</div><div class="label">${escapeHtml(t("accounting.balanceOwed"))}</div></div>
        </div>
      </div>
    `);
    const balanceCents = Number(s.balanceCents || 0);
    const balanceKr = balanceCents / 100;
    const payoutForm = el(`
      <div class="card">
        <h3>${escapeHtml(t("accounting.payoutHeading"))}</h3>
        <div class="field">
          <label>${escapeHtml(t("accounting.amountLabel"))}</label>
          <input type="number" min="0.01" max="${balanceKr}" step="0.5" id="payout-amount-${s.child.id}" value="${balanceKr.toFixed(2)}" />
        </div>
        <div class="field">
          <label>${escapeHtml(t("accounting.noteLabel"))}</label>
          <input type="text" id="payout-note-${s.child.id}" />
        </div>
        <div class="actions">
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
    card.appendChild(payoutForm);

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
    card.appendChild(histCard);
    wrap.appendChild(card);
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
      <div style="display:flex;gap:8px;flex-wrap:wrap;align-items:center;">
        <input type="text" class="input-full" style="width:auto;flex:1 1 160px;" placeholder="${escapeHtml(t("familyTab.typeToConfirmPlaceholder", { word: expectedWord }))}" />
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
// whatever the app would normally land on with no family selected: a
// single remaining membership gets reselected automatically (mirroring
// boot's own selection logic), otherwise render() naturally lands on the
// household picker (2+ remaining) or onboarding (0 remaining).
async function afterLeavingFamily() {
  setUserId(null);
  setFamilyId(null);
  state.tab = "home";
  await loadMembership();
  if (state.membership.bound && state.membership.memberships.length === 1) {
    selectMembership(state.membership.memberships[0]);
    await loadFamilyData();
  }
}

let confirmingRemoveChildId = null;
let confirmingLeaveFamily = false;
let confirmingDeleteFamily = false;
// Which row of the family members list is expanded: a user id, the
// sentinel "__add__" for the "add a family member" row, or null if none is.
let expandedFamilyRow = null;
// The most recently created invite's code — shown once, right in the
// add-member row that created it, same as the dashboard key is.
let lastCreatedInviteCode = null;

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
  const pendingUserIds = new Set(state.invitations.filter((i) => !i.acceptedAt).map((i) => i.userId));
  const family = state.families.find((f) => f.id === state.familyId);
  const familyName = family ? family.name : "";

  const renameCard = el(`
    <div class="card">
      <h2>${escapeHtml(t("family.renameHeading"))}</h2>
      <div class="field">
        <label>${escapeHtml(t("family.renameNameLabel"))}</label>
        <div style="display:flex;gap:8px;flex-wrap:wrap;">
          <input type="text" class="input-full" style="width:auto;flex:1 1 160px;" id="rename-family-input" value="${escapeHtml(familyName)}" />
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
    const pendingTag = !u.authBound && pendingUserIds.has(u.id) ? ` <span class="pill">${escapeHtml(t("familyTab.invitePending"))}</span>` : "";
    const isYou = u.id === state.userId;
    const youTag = isYou ? ` · ${escapeHtml(t("familyTab.you"))}` : "";
    // A co-parent (not you) has nothing to manage here — no rename, no
    // remove, no leave — so their row doesn't expand at all.
    const canExpand = isYou || u.role === "USER_ROLE_CHILD";
    const label = `<span>${escapeHtml(u.name)} <span class="pill ${u.role === "USER_ROLE_PARENT" ? "parent" : "child"}">${escapeHtml(roleLabel(u.role))}</span>${youTag}${pendingTag}</span>`;

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
            <div style="display:flex;gap:8px;flex-wrap:wrap;">
              <input type="text" class="input-full" style="width:auto;flex:1 1 160px;" value="${escapeHtml(u.name)}" />
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

  renderExpandableRow(`+ ${escapeHtml(t("addUser.heading"))}`, "__add__", (detail) => {
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
    detail.appendChild(el(`
      <div class="field">
        <label>${escapeHtml(t("invitations.theirEmailLabel"))}</label>
        <input type="text" id="new-member-email" />
      </div>
    `));

    const actions = el(`<div class="actions"></div>`);
    const addBtn = el(`<button type="button">${escapeHtml(t("addUser.add"))}</button>`);
    addBtn.addEventListener("click", () =>
      withError(async () => {
        const name = detail.querySelector("#new-member-name").value.trim();
        const role = detail.querySelector("#new-member-role").value;
        if (!name) throw new Error(t("addUser.nameRequired"));
        await call("CreateUser", { familyId: state.familyId, name, role });
        lastCreatedInviteCode = null;
        expandedFamilyRow = null;
        await loadFamilyData();
      })
    );
    actions.appendChild(addBtn);
    const inviteBtn = el(`<button type="button" class="secondary">${escapeHtml(t("invitations.createBtn"))}</button>`);
    inviteBtn.addEventListener("click", () =>
      withError(async () => {
        const name = detail.querySelector("#new-member-name").value.trim();
        const role = detail.querySelector("#new-member-role").value;
        const email = detail.querySelector("#new-member-email").value.trim();
        if (!name) throw new Error(t("addUser.nameRequired"));
        const resp = await call("CreateInvitation", { familyId: state.familyId, name, role, email });
        lastCreatedInviteCode = resp.token;
        await loadFamilyData();
      })
    );
    actions.appendChild(inviteBtn);
    detail.appendChild(actions);

    detail.appendChild(el(`<p class="hint" style="margin-top:10px;">${escapeHtml(t("invitations.inviteDesc"))}</p>`));
    if (lastCreatedInviteCode) {
      detail.appendChild(el(`
        <div class="field">
          <label>${escapeHtml(t("invitations.codeLabel"))}</label>
          <input type="text" class="input-full" readonly value="${escapeHtml(lastCreatedInviteCode)}" onclick="this.select()" />
        </div>
      `));
    }
  }).forEach((n) => card.appendChild(n));

  wrap.appendChild(card);
  wrap.appendChild(renderPendingInvitationsList());
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

// The "create invite" form itself now lives inline in the family members
// list's "+ Add family member" row (see renderFamilyTab) — this is just the
// list of invites still awaiting a response, with a way to revoke one.
function renderPendingInvitationsList() {
  const pending = state.invitations.filter((i) => !i.acceptedAt);
  const listCard = el(`<div class="card"><h2>${escapeHtml(t("invitations.pendingHeading"))}</h2></div>`);
  if (!pending.length) {
    listCard.appendChild(el(`<p class="empty">${escapeHtml(t("invitations.none"))}</p>`));
    return listCard;
  }
  pending.forEach((inv) => {
    const row = el(`
      <div class="row">
        <span>${escapeHtml(inv.userName)} <span class="pill ${inv.role === "USER_ROLE_CHILD" ? "child" : "parent"}">${escapeHtml(roleLabel(inv.role))}</span>${inv.email ? " · " + escapeHtml(inv.email) : ""}</span>
        <button class="danger" data-id="${inv.id}">${escapeHtml(t("invitations.revoke"))}</button>
      </div>
    `);
    row.querySelector("button").addEventListener("click", () =>
      withError(async () => {
        await call("RevokeInvitation", { invitationId: inv.id });
        await loadFamilyData();
      })
    );
    listCard.appendChild(row);
  });
  return listCard;
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
  const resp = await call("ListTaskCompletions", {
    familyId: state.familyId,
    endDate: dayBeforeStr(mondayOfWeekStr()),
    limit: HISTORY_PAGE_SIZE,
    offset: state.historyLaterOffset,
  });
  const page = resp.completions || [];
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
  const resp = await call("ListTaskCompletions", {
    familyId: state.familyId,
    search: state.historySearchQuery.trim(),
    limit: HISTORY_PAGE_SIZE,
    offset: state.historySearchOffset,
  });
  const page = resp.completions || [];
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

// Which completion (by id) is showing its inline "are you sure" state, if
// any. Module-level rather than in `state`: it's transient UI-only, reset
// whenever the user navigates away from a row rather than something worth
// persisting or reacting to elsewhere.
let confirmingDeleteCompletionId = null;

// Removing a completion here is the only way to fix an old one that's no
// longer reachable through the checklist (which only ever shows today's
// occurrences) — e.g. one logged for the wrong child, or a duplicate.
async function deleteCompletion(c) {
  await call("UncompleteTask", { taskId: c.taskId, childId: c.childId, dueDate: c.dueDate });

  // Removing it from whichever paginated bucket it came from, and walking
  // that bucket's offset back by one, keeps "load more" correctly aligned
  // with what's now actually left server-side — otherwise the next page
  // would silently skip one row.
  const removeFrom = (arr) => {
    const idx = arr.findIndex((x) => x.id === c.id);
    if (idx !== -1) arr.splice(idx, 1);
    return idx !== -1;
  };
  if (removeFrom(state.historyLater)) {
    state.historyLaterOffset = Math.max(0, state.historyLaterOffset - 1);
  }
  if (state.historySearchResults && removeFrom(state.historySearchResults)) {
    state.historySearchOffset = Math.max(0, state.historySearchOffset - 1);
  }

  // Reloads historyRecent along with everything else the deletion affects —
  // the child's earned-today/this-week figures and balance.
  await loadFamilyData();
}

function renderHistoryRow(c) {
  const confirming = confirmingDeleteCompletionId === c.id;
  const row = el(`
    <div class="row">
      <span>${escapeHtml(c.childName)} — ${escapeHtml(c.taskTitle)}<div class="task-meta">${escapeHtml(formatDateStr(c.dueDate))}</div></span>
      <div class="actions" style="align-items:center;">
        <strong>kr ${money(c.amountCents)}</strong>
        ${
          confirming
            ? `<button class="danger" data-action="confirm-delete">${escapeHtml(t("history.confirmDelete"))}</button>
               <button type="button" class="secondary" data-action="cancel-delete">${escapeHtml(t("taskList.cancel"))}</button>`
            : `<button type="button" class="secondary btn-icon" data-action="delete" title="${escapeHtml(t("taskList.delete"))}"><span class="material-symbols-outlined">delete</span></button>`
        }
      </div>
    </div>
  `);
  const deleteBtn = row.querySelector('[data-action="delete"]');
  if (deleteBtn) {
    deleteBtn.addEventListener("click", () => {
      confirmingDeleteCompletionId = c.id;
      render();
    });
  }
  const confirmBtn = row.querySelector('[data-action="confirm-delete"]');
  if (confirmBtn) {
    confirmBtn.addEventListener("click", () =>
      withError(async () => {
        confirmingDeleteCompletionId = null;
        await deleteCompletion(c);
      })
    );
  }
  const cancelBtn = row.querySelector('[data-action="cancel-delete"]');
  if (cancelBtn) {
    cancelBtn.addEventListener("click", () => {
      confirmingDeleteCompletionId = null;
      render();
    });
  }
  return row;
}

function renderHistoryGroup(heading, completions) {
  const card = el(`<div class="card"><h2>${escapeHtml(heading)}</h2></div>`);
  if (!completions.length) {
    card.appendChild(el(`<p class="empty">${escapeHtml(t("history.empty"))}</p>`));
  } else {
    completions.forEach((c) => card.appendChild(renderHistoryRow(c)));
  }
  return card;
}

function renderHistoryTab() {
  const wrap = el(`<div></div>`);

  const searchCard = el(`
    <div class="card">
      <div class="field" style="margin-bottom:0;">
        <input type="text" id="history-search" class="input-full" placeholder="${escapeHtml(t("history.searchPlaceholder"))}" />
      </div>
    </div>
  `);
  const searchInput = searchCard.querySelector("#history-search");
  searchInput.value = state.historySearchQuery;
  searchInput.addEventListener("input", (e) => onHistorySearchInput(e.target.value));
  wrap.appendChild(searchCard);

  if (state.historySearchResults !== null) {
    wrap.appendChild(renderHistoryGroup(t("history.searchResultsHeading"), state.historySearchResults));
    if (state.historySearchResults.length && state.historySearchHasMore) {
      const btn = el(`<button class="secondary">${escapeHtml(t("history.loadMore"))}</button>`);
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

  wrap.appendChild(renderHistoryGroup(t("history.todayHeading"), todays));
  wrap.appendChild(renderHistoryGroup(t("history.yesterdayHeading"), yesterdays));
  wrap.appendChild(renderHistoryGroup(t("history.thisWeekHeading"), restOfWeek));

  triggerHistoryLaterLoad();
  const laterCard = el(`<div class="card"><h2>${escapeHtml(t("history.laterHeading"))}</h2></div>`);
  if (!state.historyLaterLoaded) {
    laterCard.appendChild(el(`<p class="empty">${escapeHtml(t("history.loading"))}</p>`));
  } else if (!state.historyLater.length) {
    laterCard.appendChild(el(`<p class="empty">${escapeHtml(t("history.empty"))}</p>`));
  } else {
    state.historyLater.forEach((c) => laterCard.appendChild(renderHistoryRow(c)));
  }
  wrap.appendChild(laterCard);
  if (state.historyLaterLoaded && state.historyLaterHasMore) {
    const btn = el(`<button class="secondary">${escapeHtml(t("history.loadMore"))}</button>`);
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
// renderFamilyTab.
function renderJoinFamilySection() {
  const card = el(`
    <div class="card">
      <h2>${escapeHtml(t("familyTab.joinHeading"))}</h2>
      <p class="hint">${escapeHtml(t("familyTab.joinDesc"))}</p>
      <div class="field">
        <label>${escapeHtml(t("familyTab.joinCodeLabel"))}</label>
        <input type="text" class="input-full" id="join-family-code" />
      </div>
      <button type="button" id="join-family-btn">${escapeHtml(t("familyTab.joinBtn"))}</button>
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

function renderSettingsTab() {
  const wrap = el(`<div></div>`);
  const backBtn = el(`<button class="secondary" style="margin-bottom:16px;">${escapeHtml(t("settings.back"))}</button>`);
  backBtn.addEventListener("click", () => {
    state.tab = isParent() ? "home" : "tasks";
    confirmingRemoveChildId = null;
    confirmingLeaveFamily = false;
    confirmingDeleteFamily = false;
    expandedFamilyRow = null;
    lastCreatedInviteCode = null;
    render();
  });
  wrap.appendChild(backBtn);

  wrap.appendChild(renderJoinFamilySection());
  wrap.appendChild(el(`<hr class="section-divider" />`));

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
    wrap.appendChild(renderFamilyTab());
  }

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
  const wrap = el(`<div></div>`);
  wrap.appendChild(el(`<h1>${window.APP_NAME}</h1><p>${escapeHtml(t("dashboard.enterKeyPrompt"))}</p>`));
  const form = el(`
    <div class="card">
      <div class="field">
        <label>${escapeHtml(t("dashboard.keyLabel"))}</label>
        <input type="text" id="dashboard-key-input" class="input-full" autocomplete="off" spellcheck="false" />
      </div>
      <button id="dashboard-key-submit">${escapeHtml(t("dashboard.unlock"))}</button>
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
      if (stored) {
        selectMembership(stored);
        await loadFamilyData();
      } else if (memberships.length === 1) {
        selectMembership(memberships[0]);
        await loadFamilyData();
      } else {
        // More than one membership and no (still valid) stored choice:
        // clear any stale selection so render() shows the household picker.
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
