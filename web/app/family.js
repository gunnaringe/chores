// Family management, shown inside Settings for parents: members,
// invitations, leaving and deleting the family.

import { call } from "./api.js";
import { loadFamilyData, loadMembership, roleLabel, selectMembership, setFamilyId, setUserId, state, withError } from "./state.js";
import { avatarHtml, el, escapeHtml } from "./helpers.js";
import { openQrPanelId, render, renderQrButton, renderQrPanel, sortMembersForDisplay, wireCopyButton } from "./shell.js";
import { renderDashboardSettingsSection } from "./settings.js";

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
      <div class="input-row">
        <input type="text" autocapitalize="off" autocorrect="off" spellcheck="false" placeholder="${escapeHtml(t("familyTab.typeToConfirmPlaceholder", { word: expectedWord }))}" />
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
// whatever the app would normally land on with no family selected: any
// remaining membership gets auto-selected (mirroring boot's own selection
// logic; the topbar switcher is how you'd then pick a different one),
// otherwise render() naturally lands on onboarding.
async function afterLeavingFamily() {
  setUserId(null);
  setFamilyId(null);
  state.tab = "home";
  await loadMembership();
  if (state.membership.bound && state.membership.memberships.length) {
    selectMembership(state.membership.memberships[0]);
    await loadFamilyData();
  }
}

let confirmingRemoveChildId = null;
let confirmingRevokeInvitationId = null;
let confirmingLeaveFamily = false;
let confirmingDeleteFamily = false;
// Which row of the family members list is expanded: a user id, the
// sentinel "__add__" for the "add a family member" row, or null if none is.
let expandedFamilyRow = null;

export function resetFamilyUiState() {
  confirmingRemoveChildId = null;
  confirmingRevokeInvitationId = null;
  confirmingLeaveFamily = false;
  confirmingDeleteFamily = false;
  expandedFamilyRow = null;
}

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

export function renderFamilyTab() {
  const wrap = el(`<div></div>`);
  const pendingInvitesByUserId = new Map(state.invitations.filter((i) => !i.acceptedAt).map((i) => [i.userId, i]));
  const family = state.families.find((f) => f.id === state.familyId);
  const familyName = family ? family.name : "";

  const renameCard = el(`
    <div class="card">
      <h2>${escapeHtml(t("family.renameHeading"))}</h2>
      <div class="field">
        <div class="input-row">
          <input type="text" id="rename-family-input" value="${escapeHtml(familyName)}" />
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
    const pendingInvite = !u.authBound ? pendingInvitesByUserId.get(u.id) : undefined;
    const pendingTag = pendingInvite ? ` <span class="pill">${escapeHtml(t("familyTab.invitePending"))}</span>` : "";
    const isYou = u.id === state.userId;
    const youTag = isYou ? ` · ${escapeHtml(t("familyTab.you"))}` : "";
    // A co-parent (not you) has nothing to manage here — no rename, no
    // remove, no leave — so their row doesn't expand at all, unless their
    // invite is still pending and needs a place to show its link/revoke.
    const canExpand = isYou || u.role === "USER_ROLE_CHILD" || !!pendingInvite;
    const label = `<div class="task-row-main">
        ${avatarHtml(u)}
        <div class="task-row-text">
          <div class="task-title">${escapeHtml(u.name)}${pendingTag}</div>
          <div class="task-meta">${escapeHtml(roleLabel(u.role))}${youTag}</div>
        </div>
      </div>`;

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
            <div class="input-row">
              <input type="text" value="${escapeHtml(u.name)}" />
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

      if (pendingInvite) {
        if (confirmingRevokeInvitationId === pendingInvite.id) {
          const confirmArea = renderTypeToConfirm(
            t("invitations.revokeWord"),
            t("invitations.revokeConfirmHint", { name: u.name, word: t("invitations.revokeWord") }),
            t("invitations.revoke"),
            () =>
              withError(async () => {
                confirmingRevokeInvitationId = null;
                expandedFamilyRow = null;
                await call("RevokeInvitation", { invitationId: pendingInvite.id });
                await loadFamilyData();
              })
          );
          confirmArea.querySelector('[data-action="cancel"]').addEventListener("click", () => {
            confirmingRevokeInvitationId = null;
            render();
          });
          detail.appendChild(confirmArea);
          return;
        }

        if (pendingInvite.token) {
          // Built from the browser's own origin — the invite is only ever
          // created from within the app itself, so whatever host served
          // this page is exactly what a recipient should hit too.
          // /invite/accept (see cmd/chores/main.go) forces login first if
          // needed, then binds the token and lands them in the app.
          const acceptUrl = `${window.location.origin}/invite/accept?token=${encodeURIComponent(pendingInvite.token)}`;
          const inviteField = el(`
            <div class="field">
              <label>${escapeHtml(t("invitations.linkLabel"))}</label>
              <div class="input-row">
                <input type="text" readonly value="${escapeHtml(acceptUrl)}" data-select-on-click />
                <button type="button" class="secondary btn-icon" data-copy="url" title="${escapeHtml(t("invitations.copyLink"))}" aria-label="${escapeHtml(t("invitations.copyLink"))}"><span class="material-symbols-outlined">content_copy</span></button>
              </div>
              <label style="margin-top:6px;">${escapeHtml(t("invitations.codeLabel"))}</label>
              <div class="input-row">
                <input type="text" readonly value="${escapeHtml(pendingInvite.token)}" data-select-on-click />
                <button type="button" class="secondary btn-icon" data-copy="code" title="${escapeHtml(t("invitations.copyCode"))}" aria-label="${escapeHtml(t("invitations.copyCode"))}"><span class="material-symbols-outlined">content_copy</span></button>
              </div>
            </div>
          `);
          // Two separate buttons rather than one, since the link and the raw
          // code serve different handoffs (send a link vs. read a code aloud)
          // and a recipient copying one usually doesn't want the other.
          wireCopyButton(inviteField.querySelector('[data-copy="url"]'), acceptUrl, t("invitations.copyLink"));
          wireCopyButton(inviteField.querySelector('[data-copy="code"]'), pendingInvite.token, t("invitations.copyCode"));
          // The QR is for the link (the code alone is meant to be read or
          // typed, not scanned), so its toggle sits next to that copy
          // button — the first .input-row in this field, not the code's.
          const qrId = `invite:${pendingInvite.id}`;
          inviteField.querySelector(".input-row").appendChild(renderQrButton(qrId));
          detail.appendChild(inviteField);
          if (openQrPanelId === qrId) detail.appendChild(renderQrPanel(acceptUrl));
        }
        const revokeBtn = el(`<button type="button" class="danger" style="margin-top:10px;">${escapeHtml(t("invitations.revoke"))}</button>`);
        revokeBtn.addEventListener("click", () => {
          confirmingRevokeInvitationId = pendingInvite.id;
          render();
        });
        detail.appendChild(revokeBtn);
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

  renderExpandableRow(
    `<span style="display:inline-flex;align-items:center;gap:10px;color:var(--accent);font-weight:600;"><span class="material-symbols-outlined">person_add</span>${escapeHtml(t("addUser.heading"))}</span>`,
    "__add__",
    (detail) => {
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
      const actions = el(`<div class="actions stack"></div>`);
    // Adding a member always goes through CreateInvitation: every family
    // member gets a shareable code for binding their own login, and that
    // code stays visible (see the pending invite's own row in the members
    // list above) for as long as it's unclaimed — there's no separate
    // no-login "just add them" path.
      const addBtn = el(`<button type="button">${escapeHtml(t("addUser.add"))}</button>`);
      addBtn.addEventListener("click", () =>
        withError(async () => {
          const name = detail.querySelector("#new-member-name").value.trim();
          const role = detail.querySelector("#new-member-role").value;
          if (!name) throw new Error(t("addUser.nameRequired"));
          const resp = await call("CreateInvitation", { familyId: state.familyId, name, role });
          // Expand the new member's own row so their invite link/code — the
          // whole point of adding them — is immediately visible, instead of
          // leaving it one tap away in a row that just looks collapsed.
          expandedFamilyRow = resp.invitation ? resp.invitation.userId : null;
          await loadFamilyData();
        })
      );
      actions.appendChild(addBtn);
      detail.appendChild(actions);

      detail.appendChild(el(`<p class="hint" style="margin-top:10px;">${escapeHtml(t("invitations.inviteDesc"))}</p>`));
    }
  ).forEach((n) => card.appendChild(n));

  wrap.appendChild(card);
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
