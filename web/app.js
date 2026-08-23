// Chores frontend — vanilla JS, talks to the Connect service using the
// Connect protocol's unary JSON encoding directly (no generated client).
// Translation strings live in i18n.js (loaded before this file).

const API = "/chores.v1.ChoresService";

async function call(method, req) {
  const res = await fetch(`${API}/${method}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req || {}),
  });
  if (res.status === 401) {
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

function DOW() {
  return [
    { code: 0, label: t("days.sun") },
    { code: 1, label: t("days.mon") },
    { code: 2, label: t("days.tue") },
    { code: 3, label: t("days.wed") },
    { code: 4, label: t("days.thu") },
    { code: 5, label: t("days.fri") },
    { code: 6, label: t("days.sat") },
  ];
}

function buildScheduleFromDays(days) {
  if (!days.length) return "0 0 * * *"; // default: every day
  return `0 0 * * ${days.slice().sort().join(",")}`;
}

function daysFromSchedule(schedule) {
  const parts = (schedule || "").trim().split(/\s+/);
  if (parts.length !== 5) return null;
  const dow = parts[4];
  if (dow === "*") return DOW().map((d) => d.code);
  const days = dow
    .split(",")
    .map((s) => parseInt(s, 10))
    .filter((n) => !Number.isNaN(n));
  if (days.some((d) => d < 0 || d > 6)) return null;
  return days;
}

// ---- state -----------------------------------------------------------

const state = {
  familyId: localStorage.getItem("chores.familyId") || null,
  userId: localStorage.getItem("chores.userId") || null,
  tab: "tasks",
  families: [],
  users: [],
  tasks: [],
  occurrences: [],
  summaries: [],
  payouts: [],
  error: null,
  auth: null,
  membership: null,
  invitations: [],
  lastInviteLink: null,
};

function setFamilyId(id) {
  state.familyId = id;
  if (id) localStorage.setItem("chores.familyId", id);
  else localStorage.removeItem("chores.familyId");
}
function setUserId(id) {
  state.userId = id;
  if (id) localStorage.setItem("chores.userId", id);
  else localStorage.removeItem("chores.userId");
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

function isAuth0Mode() {
  return !!state.auth && state.auth.mode === "auth0";
}

async function loadMembership() {
  state.membership = await call("GetMyMembership", {});
}

async function loadFamilies() {
  const resp = await call("ListFamilies", {});
  state.families = resp.families || [];
}

async function loadFamilyData() {
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

  if (isAuth0Mode()) {
    const invResp = await call("ListInvitations", { familyId: state.familyId });
    state.invitations = invResp.invitations || [];
  }
}

async function refreshAll() {
  await loadFamilies();
  if (state.familyId && !state.families.find((f) => f.id === state.familyId)) {
    setFamilyId(null);
    setUserId(null);
  }
  if (state.familyId) await loadFamilyData();
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

  if (state.auth && state.auth.mode === "auth0" && state.auth.authenticated) {
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

  if (isAuth0Mode() && (!state.membership || !state.membership.bound)) {
    app.appendChild(renderOnboarding());
    return;
  }

  if (!state.familyId) {
    app.appendChild(renderFamilyPicker());
    return;
  }

  if (!state.userId) {
    app.appendChild(renderUserPicker());
    return;
  }

  app.appendChild(renderTopbar());

  const tabs = el(`
    <div class="tabs">
      <button data-tab="tasks" class="${state.tab === "tasks" ? "active" : ""}">${escapeHtml(t("tabs.tasks"))}</button>
      <button data-tab="accounting" class="${state.tab === "accounting" ? "active" : ""}">${escapeHtml(t("tabs.accounting"))}</button>
      <button data-tab="family" class="${state.tab === "family" ? "active" : ""}">${escapeHtml(t("tabs.family"))}</button>
    </div>
  `);
  tabs.querySelectorAll("button").forEach((b) =>
    b.addEventListener("click", () => {
      state.tab = b.dataset.tab;
      render();
    })
  );
  app.appendChild(tabs);

  if (state.tab === "tasks") app.appendChild(renderTasksTab());
  else if (state.tab === "accounting") app.appendChild(renderAccountingTab());
  else app.appendChild(renderFamilyTab());
}

function escapeHtml(s) {
  const d = document.createElement("div");
  d.textContent = s;
  return d.innerHTML;
}

function renderOnboarding() {
  const wrap = el(`<div></div>`);
  wrap.appendChild(el(`
    <h1>${window.APP_NAME}</h1>
    <p>${escapeHtml(t("onboarding.subtitle"))}</p>
  `));

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
      await call("CreateFamily", { name: familyName, parentName });
      await loadMembership();
      if (state.membership.bound) {
        state.families = [state.membership.family];
        setFamilyId(state.membership.family.id);
        setUserId(state.membership.user.id);
        await loadFamilyData();
      }
    })
  );
  wrap.appendChild(form);
  return wrap;
}

function renderFamilyPicker() {
  const wrap = el(`<div></div>`);
  wrap.appendChild(el(`<h1>${window.APP_NAME}</h1><p>${escapeHtml(t("familyPicker.subtitle"))}</p>`));

  const card = el(`<div class="card"></div>`);
  if (state.families.length) {
    state.families.forEach((f) => {
      const row = el(`
        <div class="row">
          <span>${escapeHtml(f.name)}</span>
          <button data-id="${f.id}">${escapeHtml(t("familyPicker.open"))}</button>
        </div>
      `);
      row.querySelector("button").addEventListener("click", () =>
        withError(async () => {
          setFamilyId(f.id);
          await loadFamilyData();
        })
      );
      card.appendChild(row);
    });
  } else {
    card.appendChild(el(`<p class="empty">${escapeHtml(t("familyPicker.noFamilies"))}</p>`));
  }
  wrap.appendChild(card);

  const form = el(`
    <div class="card">
      <h2>${escapeHtml(t("familyPicker.createHeading"))}</h2>
      <div class="field">
        <label>${escapeHtml(t("family.nameLabel"))}</label>
        <input type="text" id="new-family-name" placeholder="${escapeHtml(t("family.namePlaceholder"))}" />
      </div>
      <div class="field">
        <label>${escapeHtml(t("onboarding.yourNameLabel"))}</label>
        <input type="text" id="new-family-your-name" placeholder="${escapeHtml(t("onboarding.yourNamePlaceholder"))}" />
      </div>
      <button id="create-family-btn">${escapeHtml(t("family.createBtn"))}</button>
    </div>
  `);
  form.querySelector("#create-family-btn").addEventListener("click", () =>
    withError(async () => {
      const name = form.querySelector("#new-family-name").value.trim();
      const yourName = form.querySelector("#new-family-your-name").value.trim();
      if (!name) throw new Error(t("familyPicker.nameRequired"));
      const resp = await call("CreateFamily", { name });
      await loadFamilies();
      setFamilyId(resp.family.id);
      // Creating a family shouldn't leave its creator as a stranger to it —
      // add them as the first parent right away instead of dropping them on
      // an empty "no family members yet" screen.
      if (yourName) {
        const userResp = await call("CreateUser", { familyId: resp.family.id, name: yourName, role: "USER_ROLE_PARENT" });
        setUserId(userResp.user.id);
      }
      await loadFamilyData();
    })
  );
  wrap.appendChild(form);
  return wrap;
}

function renderUserPicker() {
  const wrap = el(`<div></div>`);
  const family = state.families.find((f) => f.id === state.familyId);
  const switchFamilyBtn = isAuth0Mode() ? "" : `<button class="secondary" id="switch-family">${escapeHtml(t("userPicker.switchFamily"))}</button>`;
  wrap.appendChild(
    el(`
      <div class="topbar">
        <h1>${escapeHtml(family ? family.name : window.APP_NAME)}</h1>
        ${switchFamilyBtn}
      </div>
      <p>${escapeHtml(t("userPicker.whoIsUsing"))}</p>
    `)
  );
  const switchFamilyEl = wrap.querySelector("#switch-family");
  if (switchFamilyEl) {
    switchFamilyEl.addEventListener("click", () => {
      setFamilyId(null);
      setUserId(null);
      render();
    });
  }

  const card = el(`<div class="card"></div>`);
  if (state.users.length) {
    state.users.forEach((u) => {
      const row = el(`
        <div class="row">
          <span>${escapeHtml(u.name)} <span class="pill ${u.role === "USER_ROLE_PARENT" ? "parent" : "child"}">${escapeHtml(roleLabel(u.role))}</span></span>
          <button data-id="${u.id}">${escapeHtml(t("userPicker.continue"))}</button>
        </div>
      `);
      row.querySelector("button").addEventListener("click", () =>
        withError(async () => {
          setUserId(u.id);
          await loadFamilyData();
        })
      );
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

function renderTopbar() {
  const family = state.families.find((f) => f.id === state.familyId);
  const user = currentUser();
  // A bound child's login can only ever act as themselves — the switcher
  // is only useful (and only allowed server-side) for a bound parent
  // acting on behalf of a child who doesn't have their own login, or in
  // local-testing mode where there's no login binding at all.
  const canSwitch = !isAuth0Mode() || isParent();
  const switchBtn = canSwitch ? `<button class="secondary" id="switch-user">${escapeHtml(t("topbar.switchUser"))}</button>` : "";
  const bar = el(`
    <div class="topbar">
      <div>
        <h1>${escapeHtml(family ? family.name : window.APP_NAME)}</h1>
        <p style="margin:0">${escapeHtml(user ? user.name : "")} <span class="pill ${isParent() ? "parent" : "child"}">${escapeHtml(isParent() ? t("role.parent") : t("role.child"))}</span></p>
      </div>
      <div class="actions">${switchBtn}</div>
    </div>
  `);
  const switchBtnEl = bar.querySelector("#switch-user");
  if (switchBtnEl) {
    switchBtnEl.addEventListener("click", () => {
      setUserId(null);
      render();
    });
  }
  return bar;
}

// ---- Tasks tab -----------------------------------------------------

function renderTasksTab() {
  const wrap = el(`<div></div>`);

  if (isParent()) {
    wrap.appendChild(renderTaskList());
    wrap.appendChild(renderAddTaskForm());
  } else {
    wrap.appendChild(renderChildOccurrences());
  }
  return wrap;
}

function renderChildOccurrences() {
  const card = el(`<div class="card"><h2>${escapeHtml(t("childTasks.heading"))}</h2></div>`);
  const mine = state.occurrences.filter((o) => o.task.active !== false);
  if (!mine.length) {
    card.appendChild(el(`<p class="empty">${escapeHtml(t("childTasks.empty"))}</p>`));
    return card;
  }
  mine.forEach((occ) => {
    const done = !!occ.completed;
    const row = el(`
      <div class="row">
        <div>
          <div class="task-title">${escapeHtml(occ.task.title)}</div>
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

function renderTaskList() {
  const card = el(`<div class="card"><h2>${escapeHtml(t("taskList.heading"))}</h2></div>`);
  if (!state.tasks.length) {
    card.appendChild(el(`<p class="empty">${escapeHtml(t("taskList.empty"))}</p>`));
    return card;
  }
  const dow = DOW();
  state.tasks.forEach((t_) => {
    const days = daysFromSchedule(t_.schedule);
    const dayLabel = days && days.length < 7 ? days.map((d) => dow[d].label).join(", ") : t("taskList.everyDay");
    const row = el(`
      <div class="row">
        <div>
          <div class="task-title">${escapeHtml(t_.title)} ${t_.active === false ? `<span class="pill">${escapeHtml(t("taskList.inactive"))}</span>` : ""}</div>
          <div class="task-meta">kr ${money(t_.priceCents)} · ${escapeHtml(dayLabel)}${t_.description ? " · " + escapeHtml(t_.description) : ""}</div>
        </div>
        <div class="actions">
          <button class="secondary" data-action="toggle">${escapeHtml(t_.active === false ? t("taskList.activate") : t("taskList.deactivate"))}</button>
          <button class="danger" data-action="delete">${escapeHtml(t("taskList.delete"))}</button>
        </div>
      </div>
    `);
    row.querySelector('[data-action="toggle"]').addEventListener("click", () =>
      withError(async () => {
        await call("UpdateTask", {
          taskId: t_.id,
          title: t_.title,
          description: t_.description,
          priceCents: t_.priceCents,
          schedule: t_.schedule,
          active: t_.active === false,
        });
        await loadFamilyData();
      })
    );
    row.querySelector('[data-action="delete"]').addEventListener("click", () =>
      withError(async () => {
        if (!confirm(t("taskList.confirmDelete", { title: t_.title }))) return;
        await call("DeleteTask", { taskId: t_.id });
        await loadFamilyData();
      })
    );
    card.appendChild(row);
  });
  return card;
}

function renderAddTaskForm() {
  const form = el(`
    <div class="card">
      <h2>${escapeHtml(t("addTask.heading"))}</h2>
      <div class="field">
        <label>${escapeHtml(t("addTask.titleLabel"))}</label>
        <input type="text" id="task-title" placeholder="${escapeHtml(t("addTask.titlePlaceholder"))}" />
      </div>
      <div class="field">
        <label>${escapeHtml(t("addTask.descLabel"))}</label>
        <input type="text" id="task-desc" />
      </div>
      <div class="grid-2">
        <div class="field">
          <label>${escapeHtml(t("addTask.priceLabel"))}</label>
          <input type="number" id="task-price" min="0" step="0.5" value="10" />
        </div>
        <div class="field">
          <label>${escapeHtml(t("addTask.repeatsOn"))}</label>
          <div id="task-days"></div>
        </div>
      </div>
      <button id="add-task-btn">${escapeHtml(t("addTask.addBtn"))}</button>
    </div>
  `);
  const daysWrap = form.querySelector("#task-days");
  const dow = DOW();
  dow.forEach((d) => {
    const id = `day-${d.code}`;
    const label = el(`<label style="display:inline-flex;align-items:center;gap:4px;margin-right:8px;font-size:0.85rem;">
      <input type="checkbox" id="${id}" ${d.code >= 1 && d.code <= 5 ? "checked" : ""}/> ${escapeHtml(d.label)}
    </label>`);
    daysWrap.appendChild(label);
  });

  form.querySelector("#add-task-btn").addEventListener("click", () =>
    withError(async () => {
      const title = form.querySelector("#task-title").value.trim();
      const description = form.querySelector("#task-desc").value.trim();
      const priceKr = parseFloat(form.querySelector("#task-price").value || "0");
      const days = dow.filter((d) => form.querySelector(`#day-${d.code}`).checked).map((d) => d.code);
      if (!title) throw new Error(t("addTask.titleRequired"));
      if (!(priceKr >= 0)) throw new Error(t("addTask.pricePositive"));
      const schedule = buildScheduleFromDays(days);
      await call("CreateTask", {
        familyId: state.familyId,
        title,
        description,
        priceCents: Math.round(priceKr * 100),
        schedule,
      });
      await loadFamilyData();
    })
  );
  return form;
}

// ---- Accounting tab -----------------------------------------------------

function renderAccountingTab() {
  const wrap = el(`<div></div>`);
  const summaries = isParent() ? state.summaries : state.summaries.filter((s) => s.child.id === state.userId);

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
    if (isParent()) {
      const payoutForm = el(`
        <div class="card">
          <h3>${escapeHtml(t("accounting.payoutHeading"))}</h3>
          <div class="field">
            <label>${escapeHtml(t("accounting.amountLabel"))}</label>
            <input type="number" min="0" step="0.5" id="payout-amount-${s.child.id}" value="${(Number(s.balanceCents || 0) / 100).toFixed(2)}" />
          </div>
          <div class="field">
            <label>${escapeHtml(t("accounting.noteLabel"))}</label>
            <input type="text" id="payout-note-${s.child.id}" />
          </div>
          <div class="actions">
            <button data-action="full">${escapeHtml(t("accounting.payFull"))}</button>
            <button class="secondary" data-action="partial">${escapeHtml(t("accounting.payPartial"))}</button>
          </div>
        </div>
      `);
      payoutForm.querySelector('[data-action="full"]').addEventListener("click", () =>
        withError(async () => {
          const note = payoutForm.querySelector(`#payout-note-${s.child.id}`).value.trim();
          await call("CreatePayout", { childId: s.child.id, fullPayout: true, note });
          await loadFamilyData();
        })
      );
      payoutForm.querySelector('[data-action="partial"]').addEventListener("click", () =>
        withError(async () => {
          const amountKr = parseFloat(payoutForm.querySelector(`#payout-amount-${s.child.id}`).value || "0");
          const note = payoutForm.querySelector(`#payout-note-${s.child.id}`).value.trim();
          if (!(amountKr > 0)) throw new Error(t("accounting.amountPositive"));
          await call("CreatePayout", {
            childId: s.child.id,
            fullPayout: false,
            amountCents: Math.round(amountKr * 100),
            note,
          });
          await loadFamilyData();
        })
      );
      card.appendChild(payoutForm);
    }

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

// ---- Family tab -----------------------------------------------------

function renderFamilyTab() {
  const wrap = el(`<div></div>`);
  const pendingUserIds = new Set(state.invitations.filter((i) => !i.acceptedAt).map((i) => i.userId));
  const card = el(`<div class="card"><h2>${escapeHtml(t("familyTab.heading"))}</h2></div>`);
  state.users.forEach((u) => {
    const pendingTag = !u.authBound && pendingUserIds.has(u.id) ? ` <span class="pill">${escapeHtml(t("familyTab.invitePending"))}</span>` : "";
    const youTag = u.id === state.userId ? ` · ${escapeHtml(t("familyTab.you"))}` : "";
    card.appendChild(
      el(`
        <div class="row">
          <span>${escapeHtml(u.name)} <span class="pill ${u.role === "USER_ROLE_PARENT" ? "parent" : "child"}">${escapeHtml(roleLabel(u.role))}</span>${youTag}${pendingTag}</span>
        </div>
      `)
    );
  });
  wrap.appendChild(card);
  if (isParent()) {
    wrap.appendChild(renderAddUserForm());
  }
  if (isAuth0Mode() && isParent()) {
    wrap.appendChild(renderInvitationsSection());
  }
  return wrap;
}

function renderInvitationsSection() {
  const wrap = el(`<div></div>`);

  const pending = state.invitations.filter((i) => !i.acceptedAt);
  const listCard = el(`<div class="card"><h2>${escapeHtml(t("invitations.pendingHeading"))}</h2></div>`);
  if (!pending.length) {
    listCard.appendChild(el(`<p class="empty">${escapeHtml(t("invitations.none"))}</p>`));
  } else {
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
  }
  wrap.appendChild(listCard);

  const form = el(`
    <div class="card">
      <h2>${escapeHtml(t("invitations.inviteHeading"))}</h2>
      <p style="margin-top:-4px;">${escapeHtml(t("invitations.inviteDesc"))}</p>
      <div class="field">
        <label>${escapeHtml(t("invitations.theirNameLabel"))}</label>
        <input type="text" id="invite-name" placeholder="${escapeHtml(t("invitations.theirNamePlaceholder"))}" />
      </div>
      <div class="grid-2">
        <div class="field">
          <label>${escapeHtml(t("invitations.roleLabel"))}</label>
          <select id="invite-role">
            <option value="USER_ROLE_PARENT">${escapeHtml(t("role.parent"))}</option>
            <option value="USER_ROLE_CHILD">${escapeHtml(t("role.child"))}</option>
          </select>
        </div>
        <div class="field">
          <label>${escapeHtml(t("invitations.theirEmailLabel"))}</label>
          <input type="text" id="invite-email" />
        </div>
      </div>
      <button id="invite-create-btn">${escapeHtml(t("invitations.createBtn"))}</button>
    </div>
  `);
  form.querySelector("#invite-create-btn").addEventListener("click", () =>
    withError(async () => {
      const name = form.querySelector("#invite-name").value.trim();
      const role = form.querySelector("#invite-role").value;
      const email = form.querySelector("#invite-email").value.trim();
      if (!name) throw new Error(t("invitations.nameRequired"));
      const resp = await call("CreateInvitation", { familyId: state.familyId, name, role, email });
      state.lastInviteLink = window.location.origin + resp.acceptPath;
      await loadFamilyData();
    })
  );
  if (state.lastInviteLink) {
    form.appendChild(
      el(`
        <div class="field" style="margin-top:12px;">
          <label>${escapeHtml(t("invitations.shareLabel"))}</label>
          <input type="text" readonly value="${escapeHtml(state.lastInviteLink)}" onclick="this.select()" />
        </div>
      `)
    );
  }
  wrap.appendChild(form);
  return wrap;
}

// ---- boot -----------------------------------------------------

withError(async () => {
  await loadAuth();
  if (isAuth0Mode()) {
    // A login is bound to at most one family member. Always resolve that
    // from the server on boot rather than trusting stale localStorage,
    // since a different login could have used this same browser before.
    await loadMembership();
    if (state.membership.bound) {
      state.families = [state.membership.family];
      setFamilyId(state.membership.family.id);
      setUserId(state.membership.user.id);
      await loadFamilyData();
    } else {
      setFamilyId(null);
      setUserId(null);
    }
  } else {
    await refreshAll();
  }
});
