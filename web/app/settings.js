// The Settings tab: device preferences, push notifications, creating and
// joining families, the kiosk dashboard key, and personal access tokens.

import { call, clientConfig } from "./api.js";
import { currentUser, getHomeUserId, isParent, loadFamilyData, state, withError } from "./state.js";
import { avatarHtml, el, escapeHtml } from "./helpers.js";
import { createFamilyAndSwitchTo, joinFamilyWithCode, openQrPanelId, render, renderCurrencySwitcher, renderLangSwitcher, renderQrButton, renderQrPanel, renderThemeSwitcher, wireCopyButton } from "./shell.js";
import { renderFamilyTab } from "./family.js";
import { cachedPushSubscription, disablePushNotifications, enablePushNotifications, pushSupported, refreshPushSubscriptionCache } from "./push.js";

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
export function renderSettingsTab() {
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
  // When the icon font can't load, every glyph degrades to a neutral
  // placeholder (see the probe in index.html). That's deliberate, but it's
  // silent, and the cause is always outside the app — so name it once, here,
  // rather than leaving someone to wonder why the UI is full of grey squares.
  // No icon in this notice on purpose: it would be a placeholder too.
  if (document.documentElement.classList.contains("icons-unavailable")) {
    wrap.appendChild(el(`
      <div class="notice-bar">
        <span class="notice-text">${escapeHtml(t("settings.iconsUnavailable"))}</span>
      </div>
    `));
  }

  wrap.appendChild(el(`<div class="section-label" style="margin-top:0;">${escapeHtml(t("settings.accountSection"))}</div>`));
  wrap.appendChild(accountCard);

  wrap.appendChild(renderLangSwitcher());
  wrap.appendChild(renderCurrencySwitcher());
  wrap.appendChild(renderThemeSwitcher());

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

  // A plain link out to the repo's issue tracker — this is a hobby project
  // (see the welcome page's disclaimer), so "something's wrong, now what"
  // has one answer: file it upstream, there's no in-app support flow. The
  // privacy policy rides along, so it's reachable after login too, not just
  // from the welcome page.
  const feedbackCard = el(`
    <div class="card">
      <h2>${escapeHtml(t("settings.feedbackHeading"))}</h2>
      <p class="hint" style="margin:0;">${escapeHtml(t("settings.feedbackHint"))} <a href="https://github.com/gunnaringe/chores/issues/new" target="_blank" rel="noopener">${escapeHtml(t("settings.feedbackLink"))}</a>.</p>
      <p class="hint" style="margin:8px 0 0;"><a href="/privacy.html">${escapeHtml(t("login.privacyLink"))}</a></p>
    </div>
  `);
  wrap.appendChild(feedbackCard);

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

  // Account-level and rarer still than switching households — a personal
  // access token acts as this login wherever it's bound, not tied to the
  // family currently open, so it sits last of all.
  wrap.appendChild(el(`<div class="section-label">${escapeHtml(t("apiTokens.section"))}</div>`));
  wrap.appendChild(renderPersonalAccessTokensSection());

  return wrap;
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

export function renderDashboardSettingsSection() {
  const card = el(`<div class="card"><h2>${escapeHtml(t("dashboard.settingsHeading"))}</h2></div>`);
  card.appendChild(el(`<p>${escapeHtml(t("dashboard.settingsDesc"))}</p>`));

  if (state.dashboardConfig === null) {
    card.appendChild(el(`<p class="empty">${escapeHtml(t("dashboard.loading"))}</p>`));
    triggerDashboardConfigLoad();
    return card;
  }

  if (state.dashboardConfig.enabled) {
    const url = `${window.location.origin}/dashboard?key=${encodeURIComponent(state.dashboardConfig.dashboardKey)}`;
    const fields = el(`
      <div>
        <div class="field">
          <label>${escapeHtml(t("dashboard.urlLabel"))}</label>
          <div class="input-row">
            <input type="text" readonly value="${escapeHtml(url)}" data-select-on-click />
            <button type="button" class="secondary btn-icon" data-copy="url" title="${escapeHtml(t("dashboard.copyUrl"))}" aria-label="${escapeHtml(t("dashboard.copyUrl"))}"><span class="material-symbols-outlined">content_copy</span></button>
          </div>
        </div>
        <div class="field">
          <label>${escapeHtml(t("dashboard.keyLabel"))}</label>
          <div class="input-row">
            <input type="text" readonly value="${escapeHtml(state.dashboardConfig.dashboardKey)}" data-select-on-click />
            <button type="button" class="secondary btn-icon" data-copy="key" title="${escapeHtml(t("dashboard.copyKey"))}" aria-label="${escapeHtml(t("dashboard.copyKey"))}"><span class="material-symbols-outlined">content_copy</span></button>
          </div>
        </div>
      </div>
    `);
    wireCopyButton(fields.querySelector('[data-copy="url"]'), url, t("dashboard.copyUrl"));
    wireCopyButton(fields.querySelector('[data-copy="key"]'), state.dashboardConfig.dashboardKey, t("dashboard.copyKey"));
    // The QR is for the URL (it already carries the key), so its toggle
    // sits next to that copy button — the first .input-row, not the key's.
    fields.querySelector(".input-row").appendChild(renderQrButton("dashboard"));
    card.appendChild(fields);
    if (openQrPanelId === "dashboard") card.appendChild(renderQrPanel(url));
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

// ---- Settings: personal access tokens -----------------------------------------------------
//
// A bearer credential for calling the API from outside the browser (a
// script, a cron job) as this login — not family-scoped like the dashboard
// key above, so this section sits outside the isParent() family block and
// is available to a child's own login too, mirroring whatever access that
// login already has.

let personalAccessTokensLoadInFlight = false;
function triggerPersonalAccessTokensLoad() {
  if (personalAccessTokensLoadInFlight || state.personalAccessTokens !== null) return;
  personalAccessTokensLoadInFlight = true;
  call("ListPersonalAccessTokens", {})
    .then((resp) => {
      state.personalAccessTokens = resp.tokens || [];
    })
    .catch((e) => {
      state.personalAccessTokens = [];
      state.error = e.message || String(e);
    })
    .finally(() => {
      personalAccessTokensLoadInFlight = false;
      if (state.tab === "settings") render();
    });
}

// The raw secret of a token just created, shown exactly once — like the
// token itself, this never touches localStorage and is gone the moment the
// user navigates away or dismisses it.
let newPersonalAccessTokenSecret = null;
let confirmingRevokeTokenId = null;

export function resetSettingsUiState() {
  newPersonalAccessTokenSecret = null;
  confirmingRevokeTokenId = null;
}

// A readonly, copyable, terminal-styled multi-line command example — one
// argument per \-continued line, same as a shell would read it back,
// rather than one crammed line. Shared by the grpcurl and plain-JSON usage
// examples on the personal access tokens page.
function renderTerminalCommandField(label, cmd, hint) {
  // A flat "+1" row of headroom isn't enough once a single logical line
  // (the request URL in the JSON example) is long enough to wrap two or
  // three times on a narrow phone — that silently clipped the tail of the
  // command instead of just wasting a little space on a wider screen.
  // Estimating wraps per line from character count instead, using the
  // narrowest width this app supports (320px, see the verify-ui skill) at
  // .terminal-cmd's font-size: 25 monospace characters is what fits across
  // that field's content width there. A DOM measurement would be exact,
  // but this field isn't attached to the page yet when this function runs,
  // so there's nothing to measure against.
  const CHARS_PER_ROW = 25;
  const rows = cmd.split("\n").reduce((total, line) => total + Math.max(1, Math.ceil(line.length / CHARS_PER_ROW)), 0);
  const field = el(`
    <div class="field" style="margin-top:16px;">
      <label>${escapeHtml(label)}</label>
      <div class="input-row">
        <textarea class="terminal-cmd" readonly rows="${rows}" data-select-on-click></textarea>
        <button type="button" class="secondary btn-icon" data-copy="usage" title="${escapeHtml(t("apiTokens.copyUsage"))}" aria-label="${escapeHtml(t("apiTokens.copyUsage"))}"><span class="material-symbols-outlined">content_copy</span></button>
      </div>
      <p class="hint" style="margin:6px 0 0;">${escapeHtml(hint)}</p>
    </div>
  `);
  // Set as a property rather than baked into the markup: a textarea's
  // value is its text content, not an attribute, so the usual
  // value="..." escaping bug can't happen here, but a command containing
  // a literal " and \ still makes "set it in JS" the simplest correct
  // option rather than hand-escaping for text-node context.
  field.querySelector("textarea").value = cmd;
  wireCopyButton(field.querySelector('[data-copy="usage"]'), cmd, t("apiTokens.copyUsage"));
  return field;
}

function renderPersonalAccessTokensSection() {
  const card = el(`<div class="card"><h2>${escapeHtml(t("apiTokens.heading"))}</h2></div>`);
  card.appendChild(el(`<p class="hint" style="margin:0 0 14px;">${escapeHtml(t("apiTokens.desc"))}</p>`));

  if (newPersonalAccessTokenSecret) {
    const secretField = el(`
      <div class="field" style="margin-bottom:16px;">
        <label>${escapeHtml(t("apiTokens.secretLabel"))}</label>
        <div class="input-row">
          <input type="text" readonly value="${escapeHtml(newPersonalAccessTokenSecret)}" data-select-on-click />
          <button type="button" class="secondary btn-icon" data-copy="secret" title="${escapeHtml(t("apiTokens.copySecret"))}" aria-label="${escapeHtml(t("apiTokens.copySecret"))}"><span class="material-symbols-outlined">content_copy</span></button>
        </div>
        <p class="hint" style="margin:6px 0 0;color:var(--red);">${escapeHtml(t("apiTokens.secretWarning"))}</p>
        <button type="button" class="secondary" style="margin-top:10px;" id="dismiss-secret-btn">${escapeHtml(t("apiTokens.secretDismiss"))}</button>
      </div>
    `);
    wireCopyButton(secretField.querySelector('[data-copy="secret"]'), newPersonalAccessTokenSecret, t("apiTokens.copySecret"));
    secretField.querySelector("#dismiss-secret-btn").addEventListener("click", () => {
      newPersonalAccessTokenSecret = null;
      render();
    });
    card.appendChild(secretField);
  }

  if (state.personalAccessTokens === null) {
    card.appendChild(el(`<p class="empty">${escapeHtml(t("dashboard.loading"))}</p>`));
    triggerPersonalAccessTokensLoad();
    return card;
  }

  if (!state.personalAccessTokens.length) {
    card.appendChild(el(`<p class="empty">${escapeHtml(t("apiTokens.empty"))}</p>`));
  } else {
    state.personalAccessTokens.forEach((tok) => {
      const confirmingDelete = confirmingRevokeTokenId === tok.id;
      const lastUsed = tok.lastUsedAt ? t("apiTokens.lastUsed", { date: new Date(tok.lastUsedAt).toLocaleDateString(localeTag()) }) : t("apiTokens.neverUsed");
      const row = el(`
        <div class="row">
          <div class="task-row-main">
            <div class="task-row-text">
              <div class="task-title">${escapeHtml(tok.name)}</div>
              <div class="task-meta">${escapeHtml(t("apiTokens.created", { date: new Date(tok.createdAt).toLocaleDateString(localeTag()) }))} · ${escapeHtml(lastUsed)}</div>
            </div>
          </div>
          <div class="actions">
            ${
              confirmingDelete
                ? `<button class="danger" data-action="confirm-revoke">${escapeHtml(t("history.confirmDelete"))}</button>
                   <button type="button" class="secondary" data-action="cancel-revoke">${escapeHtml(t("taskList.cancel"))}</button>`
                : `<button class="secondary btn-icon" data-action="revoke" title="${escapeHtml(t("apiTokens.revoke"))}" aria-label="${escapeHtml(t("apiTokens.revoke"))}" style="color:var(--red);"><span class="material-symbols-outlined">delete</span></button>`
            }
          </div>
        </div>
      `);
      const revokeBtn = row.querySelector('[data-action="revoke"]');
      if (revokeBtn) {
        revokeBtn.addEventListener("click", () => {
          confirmingRevokeTokenId = tok.id;
          render();
        });
      }
      const confirmBtn = row.querySelector('[data-action="confirm-revoke"]');
      if (confirmBtn) {
        confirmBtn.addEventListener("click", () =>
          withError(async () => {
            confirmingRevokeTokenId = null;
            await call("RevokePersonalAccessToken", { tokenId: tok.id });
            state.personalAccessTokens = null;
          })
        );
      }
      const cancelBtn = row.querySelector('[data-action="cancel-revoke"]');
      if (cancelBtn) {
        cancelBtn.addEventListener("click", () => {
          confirmingRevokeTokenId = null;
          render();
        });
      }
      card.appendChild(row);
    });

    // Only shown once a token actually exists — before that there's
    // nothing valid to put in place of $TOKEN, and the example would just
    // be confusing rather than useful.
    // window.location.host omits the port when it's the scheme's default
    // (443 for https), which is exactly the production case — grpcurl's
    // target is host:port, so the port is spelled out explicitly here
    // rather than relying on grpcurl's own default-to-443 behavior.
    const locationPort = window.location.port || (window.location.protocol === "https:" ? "443" : "80");
    // The server can name a separate gRPC host (the grpc-host flag), for a
    // deployment whose proxy can't carry gRPC: the hosted instance's
    // Cloudflare answers gRPC with a 525, so grpcurl goes to a DNS-only name.
    const grpcHost = clientConfig.grpcHost;
    const hostAndPort = grpcHost ? (grpcHost.includes(":") ? grpcHost : `${grpcHost}:443`) : `${window.location.hostname}:${locationPort}`;
    // One argument per line (\-continued, same as a shell would read it
    // back) rather than one crammed line — both more readable and more
    // "terminal-like" than a single-line field, and it's how a real
    // multi-arg command usually gets shown in docs.
    const usageCmd = `grpcurl \\\n  -H "Authorization: Bearer $TOKEN" \\\n  ${hostAndPort} \\\n  list`;
    // grpcurl needs reflection (this section's whole reason to name it),
    // but every RPC also accepts plain JSON over HTTP — the same protocol
    // this app's own frontend calls it with — for anyone without grpcurl
    // installed, or who'd rather not add a schema-aware tool at all.
    const jsonCmd = `curl \\\n  -H "Authorization: Bearer $TOKEN" \\\n  -H "Content-Type: application/json" \\\n  -d '{}' \\\n  ${window.location.origin}/chores.v1.ChoresService/ListFamilies`;
    card.appendChild(renderTerminalCommandField(t("apiTokens.usageLabel"), usageCmd, t("apiTokens.usageHint")));
    card.appendChild(renderTerminalCommandField(t("apiTokens.usageJsonLabel"), jsonCmd, t("apiTokens.usageJsonHint")));
  }

  const createField = el(`
    <div class="field" style="margin-top:16px;">
      <label>${escapeHtml(t("apiTokens.nameLabel"))}</label>
      <input type="text" id="new-token-name" class="input-full" placeholder="${escapeHtml(t("apiTokens.namePlaceholder"))}" />
    </div>
  `);
  card.appendChild(createField);
  const createBtn = el(`<button type="button">${escapeHtml(t("apiTokens.createBtn"))}</button>`);
  createBtn.addEventListener("click", () =>
    withError(async () => {
      const name = createField.querySelector("#new-token-name").value.trim();
      if (!name) throw new Error(t("apiTokens.nameRequired"));
      const resp = await call("CreatePersonalAccessToken", { name });
      newPersonalAccessTokenSecret = resp.secret;
      state.personalAccessTokens = null;
    })
  );
  card.appendChild(createBtn);

  return card;
}
