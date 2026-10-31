// Entry point: the only script index.html loads as a module; everything
// else is reached through its imports. The classic scripts loaded before it
// (i18n.js, qrcode.js) provide t(), appName(), qrcode() and friends as
// globals.

import { loadAuth, loadFamilyData, loadMembership, loadPushConfig, selectMembership, setFamilyId, setUserId, state, withError } from "./state.js";
import { applyTheme, getTheme } from "./helpers.js";
import { bootDashboard, isDashboardRoute } from "./dashboard.js";

// Read-only fields holding something to copy (invite links, dashboard
// keys, tokens) select their whole value on click. Delegated from here
// rather than an inline handler attribute: the Content-Security-Policy
// (web/security.go) blocks inline handlers.
document.addEventListener("click", (e) => {
  const field = e.target.closest?.("[data-select-on-click]");
  if (field) field.select();
});

// ---- boot -----------------------------------------------------

// index.html/login.html already applied the stored theme before first
// paint (see the comment on applyTheme() in helpers.js); this call is just to
// pick up a change made in another tab since then. When no theme is
// stored ("follow the OS"), the OS can still flip while this tab stays
// open, and only JS — not the meta tag's own media attribute, gone now
// that applyTheme() owns it — can react to that.
applyTheme();
if (window.matchMedia) {
  window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", () => {
    if (!getTheme()) applyTheme();
  });
}

if (isDashboardRoute()) {
  bootDashboard();
} else {
  withError(async () => {
    await loadAuth();
    // A login may be bound to more than one family (e.g. a child who's a
    // member of two households). Always resolve this from the server on
    // boot rather than trusting stale localStorage, since a different
    // login could have used this same browser before.
    await loadMembership();
    if (state.membership.bound) {
      const memberships = state.membership.memberships;
      const stored = memberships.find((m) => m.family.id === state.familyId && m.user.id === state.userId);
      // No family-selection landing page: a still-valid stored choice wins,
      // otherwise auto-pick the first membership. Picking between several
      // families happens via the topbar switcher once inside the app, not
      // as a gate in front of it.
      const toSelect = stored || memberships[0];
      if (toSelect) {
        selectMembership(toSelect);
        await loadFamilyData();
      } else {
        setFamilyId(null);
        setUserId(null);
      }
    } else {
      setFamilyId(null);
      setUserId(null);
    }
    // Non-fatal: push notifications are an optional extra, so a failure here
    // (e.g. the server has no VAPID keys yet) shouldn't surface as a
    // page-wide error banner.
    try {
      await loadPushConfig();
    } catch (e) {
      console.warn("push config unavailable:", e);
    }
  });
}
