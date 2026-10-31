// The Balance tab (parents): balances, payouts, and each child's monthly
// earnings breakdown.

import { call } from "./api.js";
import { loadFamilyData, state, withError } from "./state.js";
import { avatarHtml, el, escapeHtml, formatAmount, formatMonthName, labelWithCurrencyUnit } from "./helpers.js";
import { render } from "./shell.js";

// ---- Accounting tab -----------------------------------------------------

// Which child's month-by-month earnings breakdown is showing, if any —
// transient UI state like expandedFamilyRow below: module-level rather than
// in `state`, reset on navigation by resetTransientUiState(). Only one
// child's table is open at a time, the same restriction expandedFamilyRow
// puts on the family list.
let expandedMonthlyChildId = null;

export function resetBalanceUiState() {
  expandedMonthlyChildId = null;
}

// This-month/last-month totals as a stat strip (matching the child summary
// cards elsewhere), with an expandable row underneath listing every month
// on record for this one child — including months whose occurrence rows
// have long since been purged, via ListMonthlyEarnings' merge of live and
// compacted totals.
function renderMonthlyEarningsCard(childId) {
  const months = state.monthlyEarningsByChild[childId] || [];
  const thisMonth = months[0] || null;
  const lastMonth = months[1] || null;
  const card = el(`
    <div class="card">
      <div class="grid-2 stat-strip" style="margin-top:0;">
        <div class="stat"><div class="value">${formatAmount(thisMonth ? thisMonth.earned : { cents: 0 })}</div><div class="label">${escapeHtml(t("accounting.earnedThisMonth"))}</div></div>
        <div class="stat"><div class="value">${formatAmount(lastMonth ? lastMonth.earned : { cents: 0 })}</div><div class="label">${escapeHtml(t("accounting.earnedLastMonth"))}</div></div>
      </div>
    </div>
  `);

  const expanded = expandedMonthlyChildId === childId;
  const toggle = el(`
    <div class="row expandable">
      <span>${escapeHtml(t("accounting.monthlyBreakdown"))}</span>
      <span class="material-symbols-outlined chevron">${expanded ? "expand_less" : "expand_more"}</span>
    </div>
  `);
  toggle.addEventListener("click", () => {
    expandedMonthlyChildId = expanded ? null : childId;
    render();
  });
  card.appendChild(toggle);

  if (expanded) {
    const detail = el(`<div class="row-detail"></div>`);
    if (!months.length) {
      detail.appendChild(el(`<p class="empty">${escapeHtml(t("accounting.noMonthlyEarnings"))}</p>`));
    } else {
      // Months read as bare names ("August", "July", …) for the current
      // year, the year itself being implied; a heading marks the boundary
      // the first time the list crosses into an earlier one, and again for
      // every year after that — so a table spanning several years reads as
      // "February / January / 2025 / December / … / 2024 / December / …"
      // rather than repeating the year on every single row.
      let yearShown = new Date().getFullYear();
      months.forEach((m) => {
        const year = Number(m.yearMonth.split("-")[0]);
        if (year !== yearShown) {
          detail.appendChild(el(`<div class="section-label occurrence-group">${escapeHtml(String(year))}</div>`));
          yearShown = year;
        }
        detail.appendChild(el(`
          <div class="row">
            <span>${escapeHtml(formatMonthName(m.yearMonth))}</span>
            <strong>${formatAmount(m.earned)}</strong>
          </div>
        `));
      });
    }
    card.appendChild(detail);
  }

  return card;
}

export function renderAccountingTab() {
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
          <div class="stat"><div class="value">${formatAmount(s.earnedLast7Days)}</div><div class="label">${escapeHtml(t("accounting.last7Days"))}</div></div>
          <div class="stat"><div class="value">${formatAmount(s.balance)}</div><div class="label">${escapeHtml(t("accounting.balanceOwed"))}</div></div>
        </div>
      </div>
    `);
    wrapForChild.appendChild(card);
    wrapForChild.appendChild(renderMonthlyEarningsCard(s.child.id));
    const balanceCents = Number((s.balance && s.balance.cents) || 0);
    const balanceKr = balanceCents / 100;
    const payoutForm = el(`
      <div class="card">
        <h3>${escapeHtml(t("accounting.payoutHeading"))}</h3>
        <div class="field">
          <label>${escapeHtml(labelWithCurrencyUnit(t("accounting.amountLabel")))}</label>
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
          amount: { cents: amountCents },
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
                <strong>${formatAmount(p.amount)}</strong>
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
