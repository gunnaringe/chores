// The Today tab (parents, and the kiosk, which shows the same view) and a
// child's own checklist — both built from renderOccurrenceRow().

import { call } from "./api.js";
import { loadFamilyData, state, withError } from "./state.js";
import { avatarHtml, el, escapeHtml, formatAmount, groupOccurrencesByClassification, isOptionalTask, taskIconHtml } from "./helpers.js";
import { render, renderSwitcherChip } from "./shell.js";
import { setDashboardSelectedChildId } from "./dashboard.js";

// ---- Today tab (parents): today's status per child, at a glance -----------------------------------------------------

// One of today's occurrences, with its tap-to-complete tick. Shared by the
// parent's Today tab (and the kiosk, which renders the same view) and by a
// child's own screen — the two had drifted into duplicate copies of the same
// markup and the same complete/uncomplete handler.
//
// The description sits on its own line, clamped to two lines so a long one
// can't take over a card; tapping the text shows all of it. A done row is
// dimmed and struck through so what's left stands out at a glance.
function renderOccurrenceRow(occ, childId) {
  const done = !!occ.completedAt;
  const row = el(`
    <div class="row occurrence-row${done ? " done" : ""}">
      <div class="task-row-main">
        ${taskIconHtml(occ)}
        <div class="task-row-text">
          <div class="task-title">${escapeHtml(occ.title)}</div>
          <div class="task-meta">${formatAmount(occ.amount)}</div>
          ${occ.description ? `<div class="task-desc">${escapeHtml(occ.description)}</div>` : ""}
        </div>
      </div>
      <button class="checkbtn ${done ? "done" : "todo"}" aria-pressed="${done}" title="${escapeHtml(done ? t("childTasks.markNotDone") : t("childTasks.markDone"))}"><span class="material-symbols-outlined">check</span></button>
    </div>
  `);
  const desc = row.querySelector(".task-desc");
  if (desc) {
    row.querySelector(".task-row-text").addEventListener("click", () => desc.classList.toggle("expanded"));
  }
  row.querySelector("button").addEventListener("click", () =>
    withError(async () => {
      const req = { taskId: occ.taskId, childId, dueDate: occ.dueDate };
      await call(done ? "UncompleteTask" : "CompleteTask", req);
      await loadFamilyData();
    })
  );
  return row;
}

// A cheer once a child's required chores for today are all ticked off —
// "all done" if nothing at all is left, "must-dos done" if optional ones
// still are. Null while anything required is open, and for a day where the
// only tasks are optional ones nobody has touched (nothing was required, so
// there's nothing to celebrate yet).
function allDoneBanner(occurrences) {
  const required = occurrences.filter((o) => !isOptionalTask(o));
  const allDone = occurrences.length && occurrences.every((o) => o.completedAt);
  if (!allDone && !(required.length && required.every((o) => o.completedAt))) return null;
  const text = allDone ? t("today.allDone") : t("today.mustDoDone");
  return el(`<div class="all-done-banner"><span class="material-symbols-outlined">celebration</span><span>${escapeHtml(text)}</span></div>`);
}

// Appends today's tasks under a "Must do" / "Can do" heading each, so what a
// child actually has to get done reads apart from what's optional extra —
// previously the two were one undifferentiated list and the distinction only
// existed as a pill in the parent's task editor.
function appendOccurrenceSections(container, occurrences, childId) {
  const banner = allDoneBanner(occurrences);
  if (banner) container.appendChild(banner);
  groupOccurrencesByClassification(occurrences).forEach((group) => {
    container.appendChild(
      el(`<div class="section-label occurrence-group">${escapeHtml(group.label)}</div>`)
    );
    group.items.forEach((occ) => container.appendChild(renderOccurrenceRow(occ, childId)));
  });
}

// One child's card: avatar + name (or, in the kiosk's single-child mode, a
// switcher chip in that exact spot — see opts.switchOptions), today's
// tasks, and the earned/owed stat strip. Extracted out of renderTodayTab so
// the kiosk dashboard's "one child at a time" mode can reuse this same card
// for whichever child is currently selected instead of duplicating it.
export function renderChildCard(s, opts) {
  opts = opts || {};
  const todays = state.occurrences.filter((o) => o.childId === s.child.id);
  const doneCount = todays.filter((o) => o.completedAt).length;
  const metaText = todays.length ? t("today.progress", { done: doneCount, total: todays.length }) : t("childTasks.empty");

  const card = el(`<div class="card"></div>`);
  const headerRow = el(`<div class="row" style="padding-top:0;"><div class="task-row-main"></div></div>`);
  const mainRow = headerRow.querySelector(".task-row-main");

  if (opts.switchOptions && opts.switchOptions.length > 1) {
    mainRow.appendChild(
      renderSwitcherChip({
        id: "dashboard-child-switch",
        label: t("dashboard.switchChildLabel"),
        avatarUser: s.child,
        selectedValue: s.child.id,
        options: opts.switchOptions.map((c) => ({ value: c.id, label: c.name })),
        onChange: (value) => {
          setDashboardSelectedChildId(value);
          render();
        },
      })
    );
    mainRow.appendChild(el(`<div class="task-row-text"><div class="task-meta">${escapeHtml(metaText)}</div></div>`));
  } else {
    mainRow.appendChild(el(avatarHtml(s.child, "lg")));
    mainRow.appendChild(el(`
      <div class="task-row-text">
        <div class="task-title">${escapeHtml(s.child.name)}</div>
        <div class="task-meta">${escapeHtml(metaText)}</div>
      </div>
    `));
  }
  card.appendChild(headerRow);

  if (!todays.length) {
    card.appendChild(el(`<p class="empty">${escapeHtml(t("childTasks.empty"))}</p>`));
  } else {
    appendOccurrenceSections(card, todays, s.child.id);
  }

  card.appendChild(el(`
    <div class="grid-2 stat-strip">
      <div class="stat"><div class="value">${formatAmount(s.earnedToday)}</div><div class="label">${escapeHtml(t("accounting.earnedToday"))}</div></div>
      <div class="stat"><div class="value">${formatAmount(s.balance)}</div><div class="label">${escapeHtml(t("accounting.balanceOwed"))}</div></div>
    </div>
  `));
  return card;
}

// summaries defaults to state.summaries (the parent's own Today tab); the
// kiosk dashboard passes a filtered list instead, so the two never drift
// into separate copies of the same card markup.
export function renderTodayTab(summaries) {
  const list = summaries || state.summaries;
  const wrap = el(`<div></div>`);
  if (!list.length) {
    wrap.appendChild(el(`<div class="card"><p class="empty">${escapeHtml(t("accounting.noChildren"))}</p></div>`));
    return wrap;
  }

  list.forEach((s) => wrap.appendChild(renderChildCard(s)));

  return wrap;
}

// ---- Tasks tab (children): today's checklist + earnings -----------------------------------------------------
//
// A child has nowhere else to go in the app — no separate balance or family
// page — so this single view carries both their checklist and the numbers
// (earned today, earned this week, current balance) that used to live on a
// dedicated Accounting tab.

export function renderChildTasksTab() {
  const wrap = el(`<div></div>`);
  const summary = state.summaries.find((s) => s.child.id === state.userId);
  if (summary) {
    wrap.appendChild(el(`
      <div class="card">
        <div class="grid-3 stat-strip" style="margin-top:0;">
          <div class="stat"><div class="value">${formatAmount(summary.earnedToday)}</div><div class="label">${escapeHtml(t("accounting.earnedToday"))}</div></div>
          <div class="stat"><div class="value">${formatAmount(summary.earnedThisWeek)}</div><div class="label">${escapeHtml(t("accounting.earnedThisWeek"))}</div></div>
          <div class="stat"><div class="value">${formatAmount(summary.balance)}</div><div class="label">${escapeHtml(t("accounting.balanceOwed"))}</div></div>
        </div>
      </div>
    `));
  }
  wrap.appendChild(renderChildOccurrences());
  return wrap;
}

function renderChildOccurrences() {
  const card = el(`<div class="card"><h2>${escapeHtml(t("childTasks.heading"))}</h2></div>`);
  const mine = state.occurrences.filter((o) => o.childId === state.userId);
  if (!mine.length) {
    card.appendChild(el(`<p class="empty">${escapeHtml(t("childTasks.empty"))}</p>`));
    return card;
  }
  appendOccurrenceSections(card, mine, state.userId);
  return card;
}
