// The History tab (parents): recent occurrences, paging, search, and
// ticking off a past day after the fact.

import { call } from "./api.js";
import { loadFamilyData, state, withError } from "./state.js";
import { classificationPillHtml, dayBeforeStr, el, escapeHtml, formatAmount, formatDateStr, mondayOfWeekStr, taskIconHtml, todayStr } from "./helpers.js";
import { render, rerenderPreservingFocus } from "./shell.js";

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
  return `${occ.taskId}|${occ.childId}|${occ.dueDate}`;
}

// Which occurrence (by occurrenceKey) is showing its inline "are you sure"
// state, if any. Module-level rather than in `state`: it's transient
// UI-only, reset whenever the user navigates away from a row rather than
// something worth persisting or reacting to elsewhere.
let confirmingToggleKey = null;

export function resetHistoryUiState() {
  confirmingToggleKey = null;
}

// Toggles one occurrence's completion state — this is how a wrong entry in
// History gets fixed now (mark it not completed instead of deleting it, so
// it stays visible as a missed task rather than disappearing), and equally
// how a missed task gets backfilled as done after the fact.
async function toggleOccurrenceCompletion(occ) {
  if (occ.completedAt) {
    await call("UncompleteTask", { taskId: occ.taskId, childId: occ.childId, dueDate: occ.dueDate });
    occ.completedAt = null;
  } else {
    // The response carries the amount actually recorded, which for an
    // already-recorded occurrence is what it was frozen at rather than the
    // task's current price.
    const resp = await call("CompleteTask", { taskId: occ.taskId, childId: occ.childId, dueDate: occ.dueDate });
    occ.completedAt = resp.occurrence ? resp.occurrence.completedAt : null;
    if (resp.occurrence) occ.amount = resp.occurrence.amount;
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
  const actionIcon = occ.completedAt ? "close" : "check";
  const actionLabel = occ.completedAt ? t("history.markIncomplete") : t("history.markComplete");
  const confirmLabel = occ.completedAt ? t("history.confirmMarkIncomplete") : t("history.confirmMarkComplete");
  const badge = occ.completedAt ? "" : ` · <span class="pill notcompleted">${escapeHtml(t("history.notCompletedBadge"))}</span>`;
  // The amount rides in the meta line rather than as a separate right-hand
  // column: on a phone, name + date + money + button competing for one row
  // just wrapped the button onto a line of its own.
  const row = el(`
    <div class="row${occ.completedAt ? "" : " history-row-incomplete"}">
      <div class="task-row-main">
        ${taskIconHtml(occ)}
        <div class="task-row-text">
          <div class="task-title">${escapeHtml(occ.title)} ${classificationPillHtml(occ)}</div>
          <div class="task-meta">${escapeHtml(occ.childName)} · ${escapeHtml(formatDateStr(occ.dueDate))} · ${formatAmount(occ.amount)}${badge}</div>
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

export function renderHistoryTab() {
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
  const visible = (occs) => (state.historyShowIncomplete ? occs : occs.filter((o) => o.completedAt));

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
