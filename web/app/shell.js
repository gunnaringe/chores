// The app shell: render() and its routing, the app bar and tab bar, the
// device-preference switchers, onboarding and the user picker, QR codes and
// copy buttons, and resetTransientUiState() / rerenderPreservingFocus().

import { call } from "./api.js";
import { currentUser, getHomeUserId, isParent, loadFamilyData, loadMembership, roleLabel, selectMembership, setUserId, state, withError } from "./state.js";
import { CURRENCIES, avatarHtml, el, escapeHtml, getCurrency, getTheme, setCurrency, setTheme } from "./helpers.js";
import { renderChildTasksTab, renderTodayTab } from "./today.js";
import { renderAddTaskFab, renderTaskSheet, renderTasksManagementTab, resetTasksUiState, taskSheetIsOpen } from "./tasks.js";
import { renderHistoryTab, resetHistoryUiState } from "./history.js";
import { renderAccountingTab, resetBalanceUiState } from "./balance.js";
import { resetFamilyUiState } from "./family.js";
import { renderSettingsTab } from "./settings.js";
import { apiKeysPageOpen, renderApiKeysPage, resetApiKeysUiState } from "./apikeys.js";
import { renderDashboardKeyPrompt, renderDashboardSettingsScreen, renderDashboardTodayView } from "./dashboard.js";

export function renderLangSwitcher() {
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
    // The app's name is translated, so the tab title and the name an install
    // would take have to be restamped, not just the rendered UI.
    applyAppName();
    render();
  });
  return card;
}

// Same shape as renderLangSwitcher — a device-level display preference,
// not something the server knows about. See formatMoney()'s comment for why
// "None" is the default rather than an assumed currency.
export function renderCurrencySwitcher() {
  const current = getCurrency();
  const options = CURRENCIES.map((c) => {
    const label = c.code ? `${c.code} — ${c.symbol}` : t("settings.currencyNone");
    return `<option value="${c.code}" ${c.code === current ? "selected" : ""}>${escapeHtml(label)}</option>`;
  }).join("");
  const card = el(`
    <div class="card">
      <h2>${escapeHtml(t("settings.currencyHeading"))}</h2>
      <p class="hint" style="margin:0 0 10px;">${escapeHtml(t("settings.currencyHint"))}</p>
      <select id="currency-switcher" aria-label="${escapeHtml(t("settings.currencyHeading"))}">
        ${options}
      </select>
    </div>
  `);
  card.querySelector("#currency-switcher").addEventListener("change", (e) => {
    setCurrency(e.target.value);
    render();
  });
  return card;
}

// Same shape again — a device-level display preference. "" is "Match
// device" (today's default: follow the OS), listed first as the default
// rather than defaulting to "Light" the way CURRENCIES defaults to "None",
// since an explicit light/dark choice here means "ignore the OS", not
// "no preference".
const THEME_OPTIONS = [
  { value: "", labelKey: "settings.themeSystem" },
  { value: "light", labelKey: "settings.themeLight" },
  { value: "dark", labelKey: "settings.themeDark" },
  { value: "retro", labelKey: "settings.themeRetro" },
  { value: "playful", labelKey: "settings.themePlayful" },
  { value: "hacker", labelKey: "settings.themeHacker" },
  { value: "high-contrast", labelKey: "settings.themeHighContrast" },
  { value: "fairytale-forest", labelKey: "settings.themeFairytaleForest" },
  { value: "pirate-map", labelKey: "settings.themePirateMap" },
  { value: "underwater", labelKey: "settings.themeUnderwater" },
  { value: "robot-workshop", labelKey: "settings.themeRobotWorkshop" },
  { value: "cabin", labelKey: "settings.themeCabin" },
  { value: "monster-lab", labelKey: "settings.themeMonsterLab" },
  { value: "dino-park", labelKey: "settings.themeDinoPark" },
  { value: "mission-base", labelKey: "settings.themeMissionBase" },
  { value: "block-world", labelKey: "settings.themeBlockWorld" },
  { value: "cozy-home", labelKey: "settings.themeCozyHome" },
];
export function renderThemeSwitcher() {
  const current = getTheme();
  const options = THEME_OPTIONS.map((o) => `<option value="${o.value}" ${o.value === current ? "selected" : ""}>${escapeHtml(t(o.labelKey))}</option>`).join("");
  const card = el(`
    <div class="card">
      <h2>${escapeHtml(t("settings.themeHeading"))}</h2>
      <p class="hint" style="margin:0 0 10px;">${escapeHtml(t("settings.themeHint"))}</p>
      <select id="theme-switcher" aria-label="${escapeHtml(t("settings.themeHeading"))}">
        ${options}
      </select>
    </div>
  `);
  card.querySelector("#theme-switcher").addEventListener("change", (e) => {
    setTheme(e.target.value);
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
//
// Each module owns its own module-level lets and exports a reset for them
// (an imported binding can't be assigned from here); a new piece of
// transient state goes in its module's reset.
function resetTransientUiState() {
  state.editingTaskId = null;
  state.addingTask = false;
  openQrPanelId = null;
  resetTasksUiState();
  resetHistoryUiState();
  resetBalanceUiState();
  resetFamilyUiState();
  resetApiKeysUiState();
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

export function render() {
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
      if (state.dashboardScreen === "settings") {
        kiosk.appendChild(el(`<h1 class="screen-title">${escapeHtml(t("dashboard.settingsButton"))}</h1>`));
        kiosk.appendChild(renderDashboardSettingsScreen());
      } else {
        kiosk.appendChild(el(`<h1 class="screen-title">${escapeHtml(t("tabs.today"))}</h1>`));
        kiosk.appendChild(renderDashboardTodayView());
      }
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
  // The API keys page is a page within Settings with its own title and a
  // back button, rather than a tab of its own.
  const apiKeysPage = activeTab === "settings" && apiKeysPageOpen();
  if (!apiKeysPage) main.appendChild(el(`<h1 class="screen-title">${escapeHtml(defs.find((d) => d.key === activeTab).label)}</h1>`));

  if (apiKeysPage) main.appendChild(renderApiKeysPage());
  else if (activeTab === "settings") main.appendChild(renderSettingsTab());
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

// Wires an icon button to copy `value` to the clipboard, with a transient
// checkmark + "Copied!" state before reverting to `label`. Shared by every
// copy-this-value-next-to-a-readonly-input control in the app (invite
// link/code, dashboard URL/key) since they're all the same interaction.
export function wireCopyButton(btn, value, label) {
  const icon = btn.querySelector(".material-symbols-outlined");
  btn.addEventListener("click", async () => {
    try {
      await navigator.clipboard.writeText(value);
      icon.textContent = "check";
      btn.title = t("invitations.copied");
      btn.setAttribute("aria-label", t("invitations.copied"));
      setTimeout(() => {
        icon.textContent = "content_copy";
        btn.title = label;
        btn.setAttribute("aria-label", label);
      }, 1500);
    } catch (e) {
      state.error = e.message || String(e);
      render();
    }
  });
}

// Which QR panel, if any, is currently open — "dashboard", or an
// invitation's own id, so multiple pending invites don't fight over one
// slot. Module-level rather than in `state` for the same reason as
// expandedFamilyRow: a background auto-refresh tears down and rebuilds the
// whole DOM, and without this the panel would silently vanish out from
// under someone mid-scan. resetTransientUiState() clears it on navigation
// like every other expanded/confirm flag.
export let openQrPanelId = null;

// The QR toggle next to every copy button whose value is also worth
// scanning (invite link, dashboard URL) — `id` is this toggle's slot in
// openQrPanelId above. Generated entirely client-side by the vendored
// qrcode.js (see its header) rather than a hosted QR API, since both an
// invite token and a dashboard key are bearer credentials that shouldn't
// have to leave the browser to become a picture of themselves.
export function renderQrButton(id) {
  const open = openQrPanelId === id;
  const label = t(open ? "qr.hide" : "qr.show");
  const btn = el(`
    <button type="button" class="secondary btn-icon" title="${escapeHtml(label)}" aria-label="${escapeHtml(label)}" aria-expanded="${open}">
      <span class="material-symbols-outlined">${open ? "close" : "qr_code_2"}</span>
    </button>
  `);
  btn.addEventListener("click", () => {
    openQrPanelId = open ? null : id;
    render();
  });
  return btn;
}

// The dropdown itself — only ever built while its toggle is open (see call
// sites), so this never runs when qrcode.js hasn't been needed yet.
// Fixed white-on-black regardless of theme, same as .terminal-cmd and for
// the same reason: a light-on-dark code is exactly the kind of thing a
// phone's scanner struggles with, and that has to hold in dark mode too.
export function renderQrPanel(value) {
  const qr = qrcode(0, "M");
  qr.addData(value);
  qr.make();
  return el(`<div class="qr-panel">${qr.createSvgTag(6, 8)}</div>`);
}

// Creates a family and binds the caller as its founding parent via
// identity auto-bind. Leaves the new family selected — callers still need
// their own loadFamilyData() afterward.
export async function createFamilyAndSwitchTo(name, yourName) {
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
export async function joinFamilyWithCode(code) {
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
      <h1>${escapeHtml(appName())}</h1>
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
        <h1 class="screen-title" style="margin-bottom:2px;">${escapeHtml(family ? family.name : appName())}</h1>
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
export function sortMembersForDisplay(users) {
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

// Both switchers in the app bar are a styled chip with a real, invisible
// <select> stretched over it: the chip is what gets to look like part of a
// header, while the platform's own picker (a wheel on a phone) is still
// what opens on tap — no hand-built dropdown to get wrong.
export function renderSwitcherChip({ id, label, options, selectedValue, plain, avatarUser, onChange }) {
  const switchable = options.length > 1;
  // avatarUser lets a caller outside the app bar (the kiosk's child
  // switcher, in single-child mode) show the selected child's own avatar
  // instead of the logged-in user's. Both app-bar switchers still work
  // unchanged, since neither passes it and this falls back to currentUser().
  const chip = el(`
    <div class="chip ${plain ? "chip-plain" : ""}">
      ${plain ? `<span class="appbar-family"></span>` : `${avatarHtml(avatarUser !== undefined ? avatarUser : currentUser())}<span class="chip-name"></span>`}
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
        : [{ value: state.familyId || "", label: family ? family.name : appName() }],
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
  const showingSettings = state.dashboardScreen === "settings";
  const label = showingSettings ? t("dashboard.backButton") : t("dashboard.settingsButton");
  const bar = el(`
    <header class="appbar">
      <div class="appbar-inner">
        <div class="appbar-family">${escapeHtml(appName())}</div>
        <button type="button" class="secondary btn-icon" id="dashboard-settings-toggle" title="${escapeHtml(label)}" aria-label="${escapeHtml(label)}">
          <span class="material-symbols-outlined">${showingSettings ? "arrow_back" : "settings"}</span>
        </button>
      </div>
    </header>
  `);
  bar.querySelector("#dashboard-settings-toggle").addEventListener("click", () => {
    state.dashboardScreen = showingSettings ? "today" : "settings";
    render();
  });
  return bar;
}

// Re-rendering rebuilds the whole DOM, which would normally steal focus
// (and the cursor position) right out from under whatever the user is
// typing — a plain withError() call would make the search box unusable.
// Restoring focus by id after the rebuild is what makes search-as-you-type
// tolerable here.
export function rerenderPreservingFocus() {
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
