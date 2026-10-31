// The API keys page (personal access tokens), opened from Settings: listing,
// revoking and creating keys, each with its own permissions and an
// optional expiry. Account-level, not family-scoped — a key acts as this
// login wherever it's bound — so a child's own login gets it too, limited
// to whatever access that login already has.

import { call, clientConfig } from "./api.js";
import { state, withError } from "./state.js";
import { el, escapeHtml } from "./helpers.js";
import { render, wireCopyButton } from "./shell.js";

// In the order the page lists them; the names are the proto enum's, which
// is what the JSON API sends. Read and Tick are what a new key gets unless
// told otherwise — enough for a wall display, nothing that changes the
// family. Managing keys isn't here at all: no key can, by design (see
// internal/server/tokenperms.go).
const PERMISSIONS = [
  { value: "TOKEN_PERMISSION_READ", key: "read", byDefault: true },
  { value: "TOKEN_PERMISSION_TICK_CHORES", key: "tick", byDefault: true },
  { value: "TOKEN_PERMISSION_MANAGE_CHORES", key: "chores" },
  { value: "TOKEN_PERMISSION_MANAGE_CHILDREN", key: "children" },
  { value: "TOKEN_PERMISSION_PAYOUTS", key: "payouts" },
  { value: "TOKEN_PERMISSION_FAMILY_ADMIN", key: "family" },
];

const EXPIRY_DAYS = ["never", "30", "90", "365"];

// Transient, like the rest of this app's open/expanded state: a tab switch
// (resetApiKeysUiState via resetTransientUiState) closes the page and
// forgets an unsaved form, and a just-created secret is never kept beyond
// the page it was shown on.
let pageOpen = false;
let newSecret = null;
let confirmingRevokeId = null;

export function resetApiKeysUiState() {
  pageOpen = false;
  newSecret = null;
  confirmingRevokeId = null;
}

export function apiKeysPageOpen() {
  return pageOpen;
}

export function openApiKeysPage() {
  pageOpen = true;
  render();
  window.scrollTo({ top: 0 });
}

let loadInFlight = false;
function triggerLoad() {
  if (loadInFlight || state.personalAccessTokens !== null) return;
  loadInFlight = true;
  call("ListPersonalAccessTokens", {})
    .then((resp) => {
      state.personalAccessTokens = resp.tokens || [];
    })
    .catch((e) => {
      state.personalAccessTokens = [];
      state.error = e.message || String(e);
    })
    .finally(() => {
      loadInFlight = false;
      if (state.tab === "settings") render();
    });
}

// The entry point in Settings: a short description and a button, since the
// keys themselves need more room than a Settings card gives them.
export function renderApiKeysSettingsCard() {
  const card = el(`
    <div class="card">
      <h2>${escapeHtml(t("apiTokens.heading"))}</h2>
      <p class="hint">${escapeHtml(t("apiTokens.settingsDesc"))}</p>
      <button type="button" class="secondary">${escapeHtml(t("apiTokens.open"))}</button>
    </div>
  `);
  card.querySelector("button").addEventListener("click", openApiKeysPage);
  return card;
}

export function renderApiKeysPage() {
  const wrap = el(`<div></div>`);
  const header = el(`
    <div class="page-header">
      <button type="button" class="secondary btn-icon" title="${escapeHtml(t("apiTokens.back"))}" aria-label="${escapeHtml(t("apiTokens.back"))}"><span class="material-symbols-outlined">arrow_back</span></button>
      <h1 class="screen-title">${escapeHtml(t("apiTokens.heading"))}</h1>
    </div>
  `);
  header.querySelector("button").addEventListener("click", () => {
    resetApiKeysUiState();
    render();
  });
  wrap.appendChild(header);
  wrap.appendChild(el(`<p class="hint" style="margin:0 4px 16px;">${escapeHtml(t("apiTokens.desc"))}</p>`));

  if (newSecret) wrap.appendChild(renderNewSecret());

  if (state.personalAccessTokens === null) {
    wrap.appendChild(el(`<p class="empty">${escapeHtml(t("dashboard.loading"))}</p>`));
    triggerLoad();
    return wrap;
  }

  wrap.appendChild(renderKeyList());
  wrap.appendChild(renderCreateCard());
  // Only once a key exists — before that there's nothing to put in place
  // of $TOKEN, and the examples would just confuse.
  if (state.personalAccessTokens.length) wrap.appendChild(renderUsageCard());
  return wrap;
}

function renderNewSecret() {
  const card = el(`
    <div class="card">
      <div class="field" style="margin:0;">
        <label>${escapeHtml(t("apiTokens.secretLabel"))}</label>
        <div class="input-row">
          <input type="text" readonly value="${escapeHtml(newSecret)}" data-select-on-click />
          <button type="button" class="secondary btn-icon" data-copy="secret" title="${escapeHtml(t("apiTokens.copySecret"))}" aria-label="${escapeHtml(t("apiTokens.copySecret"))}"><span class="material-symbols-outlined">content_copy</span></button>
        </div>
        <p class="hint" style="margin:6px 0 0;color:var(--red);">${escapeHtml(t("apiTokens.secretWarning"))}</p>
        <button type="button" class="secondary" style="margin-top:10px;" id="dismiss-secret-btn">${escapeHtml(t("apiTokens.secretDismiss"))}</button>
      </div>
    </div>
  `);
  wireCopyButton(card.querySelector('[data-copy="secret"]'), newSecret, t("apiTokens.copySecret"));
  card.querySelector("#dismiss-secret-btn").addEventListener("click", () => {
    newSecret = null;
    render();
  });
  return card;
}

function formatDate(ts) {
  return new Date(ts).toLocaleDateString(localeTag());
}

function renderKeyList() {
  const card = el(`<div class="card"><h2>${escapeHtml(t("apiTokens.listHeading"))}</h2></div>`);
  if (!state.personalAccessTokens.length) {
    card.appendChild(el(`<p class="empty">${escapeHtml(t("apiTokens.empty"))}</p>`));
    return card;
  }
  state.personalAccessTokens.forEach((tok) => {
    const confirming = confirmingRevokeId === tok.id;
    const meta = [
      t("apiTokens.created", { date: formatDate(tok.createdAt) }),
      tok.lastUsedAt ? t("apiTokens.lastUsed", { date: formatDate(tok.lastUsedAt) }) : t("apiTokens.neverUsed"),
    ];
    if (tok.expiresAt) {
      const expired = new Date(tok.expiresAt) <= new Date();
      meta.push(t(expired ? "apiTokens.expiredOn" : "apiTokens.expiresOn", { date: formatDate(tok.expiresAt) }));
    }
    const perms = tok.legacy
      ? `<span class="pill notcompleted">${escapeHtml(t("apiTokens.legacy"))}</span>`
      : PERMISSIONS.filter((p) => (tok.permissions || []).includes(p.value))
          .map((p) => `<span class="pill">${escapeHtml(t(`apiTokens.perm.${p.key}`))}</span>`)
          .join(" ");
    const row = el(`
      <div class="row">
        <div class="task-row-main">
          <div class="task-row-text">
            <div class="task-title">${escapeHtml(tok.name)}</div>
            <div class="task-meta">${escapeHtml(meta.join(" · "))}</div>
            <div class="key-perms">${perms}</div>
            ${tok.legacy ? `<div class="task-meta">${escapeHtml(t("apiTokens.legacyHint"))}</div>` : ""}
          </div>
        </div>
        <div class="actions">
          ${
            confirming
              ? `<button class="danger" data-action="confirm-revoke">${escapeHtml(t("history.confirmDelete"))}</button>
                 <button type="button" class="secondary" data-action="cancel-revoke">${escapeHtml(t("taskList.cancel"))}</button>`
              : `<button class="secondary btn-icon" data-action="revoke" title="${escapeHtml(t("apiTokens.revoke"))}" aria-label="${escapeHtml(t("apiTokens.revoke"))}" style="color:var(--red);"><span class="material-symbols-outlined">delete</span></button>`
          }
        </div>
      </div>
    `);
    row.querySelector('[data-action="revoke"]')?.addEventListener("click", () => {
      confirmingRevokeId = tok.id;
      render();
    });
    row.querySelector('[data-action="confirm-revoke"]')?.addEventListener("click", () =>
      withError(async () => {
        confirmingRevokeId = null;
        await call("RevokePersonalAccessToken", { tokenId: tok.id });
        state.personalAccessTokens = null;
      })
    );
    row.querySelector('[data-action="cancel-revoke"]')?.addEventListener("click", () => {
      confirmingRevokeId = null;
      render();
    });
    card.appendChild(row);
  });
  return card;
}

function renderCreateCard() {
  const card = el(`
    <div class="card">
      <h2>${escapeHtml(t("apiTokens.createHeading"))}</h2>
      <div class="field">
        <label for="new-token-name">${escapeHtml(t("apiTokens.nameLabel"))}</label>
        <input type="text" id="new-token-name" class="input-full" placeholder="${escapeHtml(t("apiTokens.namePlaceholder"))}" />
      </div>
      <div class="field">
        <label>${escapeHtml(t("apiTokens.permissionsLabel"))}</label>
        <div class="perm-list">
          ${PERMISSIONS.map(
            (p) => `<label class="switch-row perm-row">
              <span>${escapeHtml(t(`apiTokens.perm.${p.key}`))}<span class="hint">${escapeHtml(t(`apiTokens.perm.${p.key}Desc`))}</span></span>
              <input type="checkbox" data-perm="${p.value}" ${p.byDefault ? "checked" : ""} />
            </label>`
          ).join("")}
        </div>
      </div>
      <div class="field">
        <label for="new-token-expiry">${escapeHtml(t("apiTokens.expiryLabel"))}</label>
        <select id="new-token-expiry">
          ${EXPIRY_DAYS.map((d) => `<option value="${d}">${escapeHtml(t(`apiTokens.expiry.${d}`))}</option>`).join("")}
        </select>
      </div>
      <button type="button" id="create-token-btn">${escapeHtml(t("apiTokens.createBtn"))}</button>
    </div>
  `);
  card.querySelector("#create-token-btn").addEventListener("click", () =>
    withError(async () => {
      const name = card.querySelector("#new-token-name").value.trim();
      if (!name) throw new Error(t("apiTokens.nameRequired"));
      const permissions = [...card.querySelectorAll("[data-perm]:checked")].map((c) => c.dataset.perm);
      // An empty list would quietly get the server's defaults instead of
      // what the form shows, so it's refused here rather than sent.
      if (!permissions.length) throw new Error(t("apiTokens.permissionRequired"));
      const req = { name, permissions };
      const days = card.querySelector("#new-token-expiry").value;
      if (days !== "never") req.expiresAt = new Date(Date.now() + Number(days) * 86400000).toISOString();
      const resp = await call("CreatePersonalAccessToken", req);
      newSecret = resp.secret;
      state.personalAccessTokens = null;
      window.scrollTo({ top: 0 });
    })
  );
  return card;
}

// A readonly, copyable, terminal-styled multi-line command example — one
// argument per \-continued line, same as a shell would read it back,
// rather than one crammed line.
function renderTerminalCommandField(label, cmd, hint) {
  // A flat "+1" row of headroom isn't enough once a single logical line
  // (the request URL in the JSON example) is long enough to wrap two or
  // three times on a narrow phone — that silently clipped the tail of the
  // command. Estimating wraps per line from character count instead, using
  // the narrowest width this app supports (320px) at .terminal-cmd's
  // font-size: 25 monospace characters fit across the field there. A DOM
  // measurement would be exact, but the field isn't attached yet.
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
  // Set as a property: a textarea's value is its text content, and a
  // command with literal " and \ is simplest set from JS.
  field.querySelector("textarea").value = cmd;
  wireCopyButton(field.querySelector('[data-copy="usage"]'), cmd, t("apiTokens.copyUsage"));
  return field;
}

function renderUsageCard() {
  const card = el(`<div class="card"><h2>${escapeHtml(t("apiTokens.usageLabel"))}</h2></div>`);
  // window.location.port is empty for the scheme's default port, which is
  // the production case — grpcurl's target is host:port, so spell it out.
  const locationPort = window.location.port || (window.location.protocol === "https:" ? "443" : "80");
  // The server can name a separate gRPC host (the grpc-host flag), for a
  // deployment whose proxy can't carry gRPC: the hosted instance's
  // Cloudflare answers gRPC with a 525, so grpcurl goes to a DNS-only name.
  const grpcHost = clientConfig.grpcHost;
  const hostAndPort = grpcHost ? (grpcHost.includes(":") ? grpcHost : `${grpcHost}:443`) : `${window.location.hostname}:${locationPort}`;
  const usageCmd = `grpcurl \\\n  -H "Authorization: Bearer $TOKEN" \\\n  ${hostAndPort} \\\n  list`;
  // Every RPC also accepts plain JSON over HTTP — the protocol this app's
  // own frontend uses — for anyone without grpcurl.
  const jsonCmd = `curl \\\n  -H "Authorization: Bearer $TOKEN" \\\n  -H "Content-Type: application/json" \\\n  -d '{}' \\\n  ${window.location.origin}/chores.v1.ChoresService/ListFamilies`;
  const grpcField = renderTerminalCommandField("grpcurl", usageCmd, t("apiTokens.usageHint"));
  grpcField.style.marginTop = "0";
  card.appendChild(grpcField);
  card.appendChild(renderTerminalCommandField(t("apiTokens.usageJsonLabel"), jsonCmd, t("apiTokens.usageJsonHint")));
  return card;
}
