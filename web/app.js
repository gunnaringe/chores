// Ukelønn frontend — vanilla JS, talks to the Connect service using the
// Connect protocol's unary JSON encoding directly (no generated client).

const API = "/ukelonn.v1.UkelonnService";

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
  return (n / 100).toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 });
};

const todayStr = () => {
  const d = new Date();
  const tz = d.getTimezoneOffset();
  const local = new Date(d.getTime() - tz * 60000);
  return local.toISOString().slice(0, 10);
};

const DOW = [
  { code: 0, label: "Sun" },
  { code: 1, label: "Mon" },
  { code: 2, label: "Tue" },
  { code: 3, label: "Wed" },
  { code: 4, label: "Thu" },
  { code: 5, label: "Fri" },
  { code: 6, label: "Sat" },
];

function buildScheduleFromDays(days) {
  if (!days.length) return "0 0 * * *"; // default: every day
  return `0 0 * * ${days.slice().sort().join(",")}`;
}

function daysFromSchedule(schedule) {
  const parts = (schedule || "").trim().split(/\s+/);
  if (parts.length !== 5) return null;
  const dow = parts[4];
  if (dow === "*") return DOW.map((d) => d.code);
  const days = dow
    .split(",")
    .map((s) => parseInt(s, 10))
    .filter((n) => !Number.isNaN(n));
  if (days.some((d) => d < 0 || d > 6)) return null;
  return days;
}

// ---- state -----------------------------------------------------------

const state = {
  familyId: localStorage.getItem("ukelonn.familyId") || null,
  userId: localStorage.getItem("ukelonn.userId") || null,
  tab: "tasks",
  families: [],
  users: [],
  tasks: [],
  occurrences: [],
  summaries: [],
  payouts: [],
  error: null,
  auth: null,
};

function setFamilyId(id) {
  state.familyId = id;
  if (id) localStorage.setItem("ukelonn.familyId", id);
  else localStorage.removeItem("ukelonn.familyId");
}
function setUserId(id) {
  state.userId = id;
  if (id) localStorage.setItem("ukelonn.userId", id);
  else localStorage.removeItem("ukelonn.userId");
}

function currentUser() {
  return state.users.find((u) => u.id === state.userId) || null;
}
function isParent() {
  const u = currentUser();
  return !!u && u.role === "USER_ROLE_PARENT";
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
  const t = document.createElement("template");
  t.innerHTML = html.trim();
  return t.content.firstElementChild;
}

function render() {
  const app = document.getElementById("app");
  app.innerHTML = "";

  if (state.auth && state.auth.mode === "auth0" && state.auth.authenticated) {
    app.appendChild(
      el(`
        <div style="display:flex;justify-content:flex-end;gap:10px;align-items:center;font-size:0.85rem;color:var(--muted);margin-bottom:8px;">
          <span>Signed in as ${escapeHtml(state.auth.name || state.auth.email || "")}</span>
          <a class="link-btn" href="/auth/logout">Log out</a>
        </div>
      `)
    );
  }

  if (state.error) {
    app.appendChild(el(`<div class="error">${escapeHtml(state.error)}</div>`));
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
      <button data-tab="tasks" class="${state.tab === "tasks" ? "active" : ""}">Tasks</button>
      <button data-tab="accounting" class="${state.tab === "accounting" ? "active" : ""}">Accounting</button>
      <button data-tab="family" class="${state.tab === "family" ? "active" : ""}">Family</button>
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

function renderFamilyPicker() {
  const wrap = el(`<div></div>`);
  wrap.appendChild(el(`<h1>Ukelønn</h1><p>Pick or create a family to get started.</p>`));

  const card = el(`<div class="card"></div>`);
  if (state.families.length) {
    state.families.forEach((f) => {
      const row = el(`
        <div class="row">
          <span>${escapeHtml(f.name)}</span>
          <button data-id="${f.id}">Open</button>
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
    card.appendChild(el(`<p class="empty">No families yet.</p>`));
  }
  wrap.appendChild(card);

  const form = el(`
    <div class="card">
      <h2>Create a family</h2>
      <div class="field">
        <label>Family name</label>
        <input type="text" id="new-family-name" placeholder="e.g. The Smiths" />
      </div>
      <button id="create-family-btn">Create family</button>
    </div>
  `);
  form.querySelector("#create-family-btn").addEventListener("click", () =>
    withError(async () => {
      const name = form.querySelector("#new-family-name").value.trim();
      if (!name) throw new Error("Family name is required");
      const resp = await call("CreateFamily", { name });
      await loadFamilies();
      setFamilyId(resp.family.id);
      await loadFamilyData();
    })
  );
  wrap.appendChild(form);
  return wrap;
}

function renderUserPicker() {
  const wrap = el(`<div></div>`);
  const family = state.families.find((f) => f.id === state.familyId);
  wrap.appendChild(
    el(`
      <div class="topbar">
        <h1>${escapeHtml(family ? family.name : "Ukelønn")}</h1>
        <button class="secondary" id="switch-family">Switch family</button>
      </div>
      <p>Who's using the app right now?</p>
    `)
  );
  wrap.querySelector("#switch-family").addEventListener("click", () => {
    setFamilyId(null);
    setUserId(null);
    render();
  });

  const card = el(`<div class="card"></div>`);
  if (state.users.length) {
    state.users.forEach((u) => {
      const row = el(`
        <div class="row">
          <span>${escapeHtml(u.name)} <span class="pill ${u.role === "USER_ROLE_PARENT" ? "parent" : "child"}">${u.role === "USER_ROLE_PARENT" ? "Parent" : "Child"}</span></span>
          <button data-id="${u.id}">Continue</button>
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
    card.appendChild(el(`<p class="empty">No family members yet — add one below.</p>`));
  }
  wrap.appendChild(card);
  wrap.appendChild(renderAddUserForm());
  return wrap;
}

function renderAddUserForm() {
  const form = el(`
    <div class="card">
      <h2>Add a family member</h2>
      <div class="field">
        <label>Name</label>
        <input type="text" id="new-user-name" placeholder="Name" />
      </div>
      <div class="field">
        <label>Role</label>
        <select id="new-user-role">
          <option value="USER_ROLE_PARENT">Parent</option>
          <option value="USER_ROLE_CHILD">Child</option>
        </select>
      </div>
      <button id="add-user-btn">Add</button>
    </div>
  `);
  form.querySelector("#add-user-btn").addEventListener("click", () =>
    withError(async () => {
      const name = form.querySelector("#new-user-name").value.trim();
      const role = form.querySelector("#new-user-role").value;
      if (!name) throw new Error("Name is required");
      await call("CreateUser", { familyId: state.familyId, name, role });
      await loadFamilyData();
    })
  );
  return form;
}

function renderTopbar() {
  const family = state.families.find((f) => f.id === state.familyId);
  const user = currentUser();
  const bar = el(`
    <div class="topbar">
      <div>
        <h1>${escapeHtml(family ? family.name : "Ukelønn")}</h1>
        <p style="margin:0">${escapeHtml(user ? user.name : "")} <span class="pill ${isParent() ? "parent" : "child"}">${isParent() ? "Parent" : "Child"}</span></p>
      </div>
      <div class="actions">
        <button class="secondary" id="switch-user">Switch user</button>
      </div>
    </div>
  `);
  bar.querySelector("#switch-user").addEventListener("click", () => {
    setUserId(null);
    render();
  });
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
  const card = el(`<div class="card"><h2>Today's tasks</h2></div>`);
  const mine = state.occurrences.filter((o) => o.task.active !== false);
  if (!mine.length) {
    card.appendChild(el(`<p class="empty">No tasks scheduled for today.</p>`));
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
        <button class="checkbtn ${done ? "done" : "todo"}" title="${done ? "Mark not done" : "Mark done"}">${done ? "✓" : ""}</button>
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
  const card = el(`<div class="card"><h2>Tasks</h2></div>`);
  if (!state.tasks.length) {
    card.appendChild(el(`<p class="empty">No tasks yet — add one below.</p>`));
    return card;
  }
  state.tasks.forEach((t) => {
    const days = daysFromSchedule(t.schedule);
    const dayLabel = days && days.length < 7 ? days.map((d) => DOW[d].label).join(", ") : "Every day";
    const row = el(`
      <div class="row">
        <div>
          <div class="task-title">${escapeHtml(t.title)} ${t.active === false ? '<span class="pill">inactive</span>' : ""}</div>
          <div class="task-meta">kr ${money(t.priceCents)} · ${escapeHtml(dayLabel)}${t.description ? " · " + escapeHtml(t.description) : ""}</div>
        </div>
        <div class="actions">
          <button class="secondary" data-action="toggle">${t.active === false ? "Activate" : "Deactivate"}</button>
          <button class="danger" data-action="delete">Delete</button>
        </div>
      </div>
    `);
    row.querySelector('[data-action="toggle"]').addEventListener("click", () =>
      withError(async () => {
        await call("UpdateTask", {
          taskId: t.id,
          title: t.title,
          description: t.description,
          priceCents: t.priceCents,
          schedule: t.schedule,
          active: t.active === false,
        });
        await loadFamilyData();
      })
    );
    row.querySelector('[data-action="delete"]').addEventListener("click", () =>
      withError(async () => {
        if (!confirm(`Delete task "${t.title}"?`)) return;
        await call("DeleteTask", { taskId: t.id });
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
      <h2>Add a task</h2>
      <div class="field">
        <label>Title</label>
        <input type="text" id="task-title" placeholder="e.g. Do the dishes" />
      </div>
      <div class="field">
        <label>Description (optional)</label>
        <input type="text" id="task-desc" />
      </div>
      <div class="grid-2">
        <div class="field">
          <label>Price (kr)</label>
          <input type="number" id="task-price" min="0" step="0.5" value="10" />
        </div>
        <div class="field">
          <label>Repeats on</label>
          <div id="task-days"></div>
        </div>
      </div>
      <button id="add-task-btn">Add task</button>
    </div>
  `);
  const daysWrap = form.querySelector("#task-days");
  DOW.forEach((d) => {
    const id = `day-${d.code}`;
    const label = el(`<label style="display:inline-flex;align-items:center;gap:4px;margin-right:8px;font-size:0.85rem;">
      <input type="checkbox" id="${id}" ${d.code >= 1 && d.code <= 5 ? "checked" : ""}/> ${d.label}
    </label>`);
    daysWrap.appendChild(label);
  });

  form.querySelector("#add-task-btn").addEventListener("click", () =>
    withError(async () => {
      const title = form.querySelector("#task-title").value.trim();
      const description = form.querySelector("#task-desc").value.trim();
      const priceKr = parseFloat(form.querySelector("#task-price").value || "0");
      const days = DOW.filter((d) => form.querySelector(`#day-${d.code}`).checked).map((d) => d.code);
      if (!title) throw new Error("Title is required");
      if (!(priceKr >= 0)) throw new Error("Price must be a positive number");
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
    wrap.appendChild(el(`<div class="card"><p class="empty">No children in this family yet.</p></div>`));
    return wrap;
  }

  summaries.forEach((s) => {
    const card = el(`
      <div class="card">
        <h2>${escapeHtml(s.child.name)}</h2>
        <div class="grid-2">
          <div class="stat"><div class="value">kr ${money(s.earnedLast7DaysCents)}</div><div class="label">Last 7 days</div></div>
          <div class="stat"><div class="value">kr ${money(s.balanceCents)}</div><div class="label">Balance owed</div></div>
        </div>
      </div>
    `);
    if (isParent()) {
      const payoutForm = el(`
        <div class="card">
          <h3>Pay out</h3>
          <div class="field">
            <label>Amount (kr) — leave as full balance or enter a partial amount</label>
            <input type="number" min="0" step="0.5" id="payout-amount-${s.child.id}" value="${(Number(s.balanceCents || 0) / 100).toFixed(2)}" />
          </div>
          <div class="field">
            <label>Note (optional)</label>
            <input type="text" id="payout-note-${s.child.id}" />
          </div>
          <div class="actions">
            <button data-action="full">Pay full balance</button>
            <button class="secondary" data-action="partial">Pay entered amount</button>
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
          if (!(amountKr > 0)) throw new Error("Enter an amount greater than zero");
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
    const histCard = el(`<div class="card"><h3>Payout history</h3></div>`);
    if (!history.length) {
      histCard.appendChild(el(`<p class="empty">No payouts yet.</p>`));
    } else {
      history
        .slice()
        .sort((a, b) => new Date(b.createdAt) - new Date(a.createdAt))
        .forEach((p) => {
          histCard.appendChild(
            el(`
              <div class="row">
                <span>${new Date(p.createdAt).toLocaleDateString()} ${p.fullPayout ? '<span class="pill">full</span>' : '<span class="pill">partial</span>'} ${p.note ? "— " + escapeHtml(p.note) : ""}</span>
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
  const card = el(`<div class="card"><h2>Family members</h2></div>`);
  state.users.forEach((u) => {
    card.appendChild(
      el(`
        <div class="row">
          <span>${escapeHtml(u.name)} <span class="pill ${u.role === "USER_ROLE_PARENT" ? "parent" : "child"}">${u.role === "USER_ROLE_PARENT" ? "Parent" : "Child"}</span></span>
        </div>
      `)
    );
  });
  wrap.appendChild(card);
  if (isParent()) {
    wrap.appendChild(renderAddUserForm());
  }
  return wrap;
}

// ---- boot -----------------------------------------------------

withError(async () => {
  await loadAuth();
  await refreshAll();
});
