// The Tasks tab (parents): the task list, the add/edit sheet, and the icon
// picker's Material Symbols search.

import { call } from "./api.js";
import { loadFamilyData, state, withError } from "./state.js";
import { DOW, classificationPillHtml, el, escapeHtml, formatAmount, labelWithCurrencyUnit, materialIconName, repeatLabel, taskIconHtml, todayStr } from "./helpers.js";
import { render } from "./shell.js";

// The Material Symbols icon list with Google's search tags (synonyms —
// "trash" finds "delete"), fetched lazily and cached here. Each entry in
// the file is "name tag tag …", sorted most popular first; see
// scripts/update-material-symbols.sh for where it comes from and how the
// tags are pruned. Tags are English only, like Google's own search.
let materialSymbolsPromise = null;
function loadMaterialSymbols() {
  if (!materialSymbolsPromise) {
    materialSymbolsPromise = fetch("/material-symbols.json")
      .then((res) => res.json())
      .then((entries) =>
        entries.map((entry) => {
          const space = entry.indexOf(" ");
          const name = space === -1 ? entry : entry.slice(0, space);
          return { name, words: name.replace(/_/g, " "), tags: space === -1 ? "" : entry.slice(space + 1) };
        })
      )
      .catch(() => []);
  }
  return materialSymbolsPromise;
}

// Hand-picked Bokmål/Nynorsk/Swedish words for the ~250 icons chores
// actually use ("søppel" finds "delete"), fetched only for those languages —
// English already has Google's tags. The file maps icon name to
// [nb, nn extras, sv]; Nynorsk searches its own extras plus the Bokmål
// words, which its readers know. Icons without an entry are still found by
// their English name and tags. Resolves to {name: "words …"} for the
// current language, or {} for English or on failure.
let localIconWordsPromise = null;
let localIconWordsLang = "";
function loadLocalIconWords() {
  const lang = getLang();
  if (lang === "en") return Promise.resolve({});
  if (!localIconWordsPromise || localIconWordsLang !== lang) {
    localIconWordsLang = lang;
    localIconWordsPromise = fetch("/material-symbols-i18n.json")
      .then((res) => res.json())
      .then((entries) => {
        const pick = { nb: [0], nn: [1, 0], sv: [2] }[lang] || [];
        const words = {};
        for (const [name, perLang] of Object.entries(entries)) {
          words[name] = pick.map((i) => perLang[i]).filter(Boolean).join(" ");
        }
        return words;
      })
      .catch(() => ({}));
  }
  return localIconWordsPromise;
}

// Substring search, matching each whitespace-separated search word against
// the name (underscores as spaces, so "clean service" matches
// "cleaning_services"), the icon's words in the UI language, and Google's
// English tags. Ranking: name starts with the query; every word starts a
// word in the name or the UI-language words; every word appears somewhere in
// the name; then the same with the UI-language words and finally Google's
// tags. Within a tier Google's popularity order (the file's order) decides,
// except that tier 1 puts name matches first and then orders UI-language
// matches by how early they appear — those words are written most relevant
// first, so "tvätt" ranks the laundry icon above the car wash. Word starts
// outrank mid-word hits so that "hund" (dog) finds "pets" before
// "t[hund]erstorm".
function searchMaterialSymbols(icons, query, localWords = {}) {
  const q = query.trim().toLowerCase();
  if (!q) return [];
  const words = q.split(/\s+/).filter(Boolean);
  const tiers = [[], [], [], [], []];
  for (const icon of icons) {
    const local = localWords[icon.name] || "";
    const nameStart = " " + icon.words;
    const localStart = " " + local;
    if (icon.words.startsWith(q)) tiers[0].push(icon.name);
    else if (words.every((w) => nameStart.includes(" " + w) || localStart.includes(" " + w))) {
      tiers[1].push([icon.name, words.every((w) => nameStart.includes(" " + w)) ? -1 : local.indexOf(words[0])]);
    } else if (words.every((w) => icon.words.includes(w))) tiers[2].push(icon.name);
    else if (local && words.every((w) => icon.words.includes(w) || local.includes(w))) tiers[3].push(icon.name);
    else if (words.every((w) => icon.words.includes(w) || local.includes(w) || icon.tags.includes(w))) tiers[4].push(icon.name);
  }
  // Array.prototype.sort is stable, so popularity order survives ties.
  tiers[1] = tiers[1].sort((a, b) => a[1] - b[1]).map(([name]) => name);
  return tiers.flat().slice(0, 60);
}

// ---- Tasks tab (parents): manage task definitions -----------------------------------------------------

// The task list is the screen; adding or editing one opens over it (see
// renderTaskSheet) instead of appearing as a permanent form underneath,
// which on a phone meant scrolling past every task to reach it and losing
// your place in the list every time you edited something.
export function renderTasksManagementTab() {
  const wrap = el(`<div></div>`);
  wrap.appendChild(renderTaskList());
  return wrap;
}

export function taskSheetIsOpen() {
  return state.addingTask || !!state.editingTaskId;
}

function closeTaskSheet() {
  state.addingTask = false;
  state.editingTaskId = null;
  cloningSourceTask = null;
}

export function renderAddTaskFab() {
  const fab = el(`<button type="button" class="fab"><span class="material-symbols-outlined">add</span><span>${escapeHtml(t("addTask.addBtn"))}</span></button>`);
  fab.addEventListener("click", () => {
    confirmingDeleteTaskId = null;
    cloningSourceTask = null;
    state.addingTask = true;
    render();
  });
  return fab;
}

export function renderTaskSheet() {
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
  backdrop.querySelector(".sheet-body").appendChild(renderTaskForm(editingTask, editingTask ? null : cloningSourceTask));
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

// The task a newly-opened "add task" sheet should prefill from, if it was
// opened via the clone button rather than the add FAB. Module-level and
// reset on navigation/close like the other transient UI state above — it's
// only meant to survive the single render that opens the sheet.
let cloningSourceTask = null;

export function resetTasksUiState() {
  confirmingDeleteTaskId = null;
  cloningSourceTask = null;
}

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
            <div class="task-meta">${formatAmount(t_.price)} · ${escapeHtml(repeatLabel(t_))}${t_.description ? " · " + escapeHtml(t_.description) : ""}${assignedNames ? " · " + escapeHtml(assignedNames) : ""}</div>
          </div>
        </div>
        <div class="actions">
          ${
            confirmingDelete
              ? `<button class="danger" data-action="confirm-delete">${escapeHtml(t("history.confirmDelete"))}</button>
                 <button type="button" class="secondary" data-action="cancel-delete">${escapeHtml(t("taskList.cancel"))}</button>`
              : `<button class="secondary btn-icon" data-action="edit" title="${escapeHtml(t("taskList.edit"))}" aria-label="${escapeHtml(t("taskList.edit"))}"><span class="material-symbols-outlined">edit</span></button>
                 <button class="secondary btn-icon" data-action="clone" title="${escapeHtml(t("taskList.clone"))}" aria-label="${escapeHtml(t("taskList.clone"))}"><span class="material-symbols-outlined">content_copy</span></button>
                 <button class="secondary btn-icon" data-action="toggle" title="${escapeHtml(t_.active === false ? t("taskList.resume") : t("taskList.pause"))}" aria-label="${escapeHtml(t_.active === false ? t("taskList.resume") : t("taskList.pause"))}"><span class="material-symbols-outlined">${t_.active === false ? "play_arrow" : "pause"}</span></button>
                 <button class="secondary btn-icon" data-action="delete" title="${escapeHtml(t("taskList.delete"))}" aria-label="${escapeHtml(t("taskList.delete"))}" style="color:var(--red);"><span class="material-symbols-outlined">delete</span></button>`
          }
        </div>
      </div>
    `);

    const editBtn = row.querySelector('[data-action="edit"]');
    if (editBtn) {
      editBtn.addEventListener("click", () => {
        cloningSourceTask = null;
        state.addingTask = false;
        state.editingTaskId = t_.id;
        render();
      });
    }
    const cloneBtn = row.querySelector('[data-action="clone"]');
    if (cloneBtn) {
      cloneBtn.addEventListener("click", () => {
        confirmingDeleteTaskId = null;
        cloningSourceTask = t_;
        state.editingTaskId = null;
        state.addingTask = true;
        render();
      });
    }
    const toggleBtn = row.querySelector('[data-action="toggle"]');
    if (toggleBtn) {
      toggleBtn.addEventListener("click", () =>
        withError(async () => {
          // Pause/resume resends the task unchanged apart from `active`, so
          // the schedule and price go back exactly as they arrived.
          await call("UpdateTask", {
            taskId: t_.id,
            title: t_.title,
            description: t_.description,
            price: t_.price,
            schedule: t_.schedule,
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

// "Wash dishes" -> "Wash dishes (copy)"; if that title is already taken by
// another task, "(copy 2)", "(copy 3)", ... until one is free.
function cloneTaskTitle(sourceTitle) {
  const existingTitles = new Set(state.tasks.map((t_) => t_.title));
  let candidate = `${sourceTitle} ${t("taskList.copySuffix")}`;
  let n = 2;
  while (existingTitles.has(candidate)) {
    candidate = `${sourceTitle} ${t("taskList.copySuffixN", { n })}`;
    n++;
  }
  return candidate;
}

// `cloneSource` prefills a brand-new (non-edit) form from another task's
// data, for the task-list clone button — everything below that reads
// `existingTask` for prefill purposes also accepts a clone source, but
// `isEdit` stays tied to `existingTask` alone so saving always creates a
// new task rather than updating the one being cloned from.
function renderTaskForm(existingTask, cloneSource) {
  const isEdit = !!existingTask;
  const prefill = existingTask || cloneSource;
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
        <p class="hint" style="margin:6px 0 0;">${escapeHtml(t("addTask.iconSearchHint"))} <a href="https://fonts.google.com/icons" target="_blank" rel="noopener">${escapeHtml(t("addTask.iconSearchLink"))}</a></p>
      </div>
      <div class="field">
        <label>${escapeHtml(labelWithCurrencyUnit(t("addTask.priceLabel")))}</label>
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

  if (prefill) {
    form.querySelector("#task-title").value = isEdit ? prefill.title : cloneTaskTitle(prefill.title);
    form.querySelector("#task-desc").value = prefill.description || "";
    form.querySelector("#task-price").value = (Number((prefill.price && prefill.price.cents) || 0) / 100).toFixed(2);
  }

  const iconSearchInput = form.querySelector("#task-icon-search");
  const iconResultsWrap = form.querySelector("#task-icon-results");
  const iconSelectedWrap = form.querySelector("#task-icon-selected");
  // A pre-existing icon only pre-fills the picker when it's actually a
  // Material Symbols icon — a task whose icon predates that being the only
  // supported type (emoji, Font Awesome) starts this form with no icon
  // selected, since there's no way to keep editing that value here.
  let selectedIconValue = prefill && prefill.icon && prefill.icon.type === "ICON_TYPE_MATERIAL_SYMBOLS" ? materialIconName(prefill.icon.value) : "";
  let materialSymbols = [];
  let localIconWords = {};
  loadMaterialSymbols().then((icons) => {
    materialSymbols = icons;
    if (iconSearchInput.value.trim()) renderIconResults();
  });
  loadLocalIconWords().then((words) => {
    localIconWords = words;
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
    const matches = searchMaterialSymbols(materialSymbols, query, localIconWords);
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
  const existingSchedule = (prefill && prefill.schedule) || {};
  const preCheckedDays = (existingSchedule.weekly && existingSchedule.weekly.daysOfWeek) || [];
  // Tap-to-toggle chips rather than a row of tiny checkboxes: the checkbox
  // is still the source of truth (the save handler reads it by id), it's
  // just hidden inside a target big enough to hit with a thumb.
  dow.forEach((d) => {
    const id = `day-${d.code}`;
    const checked = prefill ? preCheckedDays.includes(d.code) : d.code >= 1 && d.code <= 5;
    const label = el(`<label class="toggle-chip">
      <input type="checkbox" id="${id}" ${checked ? "checked" : ""}/>${escapeHtml(d.label)}
    </label>`);
    daysWrap.appendChild(label);
  });

  const classificationButtons = [...form.querySelectorAll(".classification-toggle button")];
  let selectedClassification = prefill && prefill.classification === "TASK_CLASSIFICATION_OPTIONAL" ? "OPTIONAL" : "MANDATORY";
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

  // The schedule is a oneof: exactly one of once/weekly/cron is present,
  // and which one it is selects the mode the form opens on.
  let selectedRepeatMode = "WEEKLY";
  if (existingSchedule.once) selectedRepeatMode = "ONCE";
  else if (existingSchedule.cron) selectedRepeatMode = "CRON";

  onceDateInput.value = (existingSchedule.once && existingSchedule.once.date) || todayStr();
  intervalWeeksInput.value = (existingSchedule.weekly && Number(existingSchedule.weekly.intervalWeeks)) || 1;
  cronInput.value = (existingSchedule.cron && existingSchedule.cron.expression) || "";

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
    const assignedIds = new Set(prefill ? prefill.childIds || [] : []);
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

      // Exactly one branch of the oneof, so an invalid combination — a
      // one-off carrying a cron expression, say — can't be built here.
      let schedule;
      if (selectedRepeatMode === "ONCE") {
        const date = onceDateInput.value;
        if (!date) throw new Error(t("addTask.onceDateRequired"));
        schedule = { once: { date } };
      } else if (selectedRepeatMode === "WEEKLY") {
        const daysOfWeek = dow.filter((d) => form.querySelector(`#day-${d.code}`).checked).map((d) => d.code);
        if (!daysOfWeek.length) throw new Error(t("addTask.daysRequired"));
        const intervalWeeks = parseInt(intervalWeeksInput.value || "1", 10);
        if (!(intervalWeeks >= 1)) throw new Error(t("addTask.intervalPositive"));
        // Keep the existing anchor when a task was already weekly, so
        // editing something else about it doesn't shift which weeks an
        // every-N-weeks task falls on.
        const anchorDate = (existingSchedule.weekly && existingSchedule.weekly.anchorDate) || todayStr();
        schedule = { weekly: { daysOfWeek, intervalWeeks, anchorDate } };
      } else {
        const expression = cronInput.value.trim();
        if (!expression) throw new Error(t("addTask.cronRequired"));
        schedule = { cron: { expression } };
      }

      const classification = `TASK_CLASSIFICATION_${selectedClassification}`;
      const price = { cents: Math.round(priceKr * 100) };
      if (isEdit) {
        await call("UpdateTask", {
          taskId: existingTask.id,
          title,
          description,
          icon,
          price,
          schedule,
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
          price,
          schedule,
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
