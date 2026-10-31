// The Connect API client. call() speaks the Connect protocol's unary JSON
// encoding directly (no generated client); every request in the app goes
// through it. Also the deployment config the server hands the page.

import { state } from "./state.js";

const API = "/chores.v1.ChoresService";

// How a kiosk (see dashboard.js) authorizes itself instead of a session.
const DASHBOARD_KEY_HEADER = "X-Dashboard-Key";

export async function call(method, req) {
  const headers = { "Content-Type": "application/json" };
  if (state.dashboardMode && state.dashboardKey) {
    headers[DASHBOARD_KEY_HEADER] = state.dashboardKey;
  }
  const res = await fetch(`${API}/${method}`, {
    method: "POST",
    headers,
    body: JSON.stringify(req || {}),
  });
  if (res.status === 401) {
    // A kiosk has no Auth0 session to redirect through — a 401 here just
    // means its dashboard key didn't work, which the key-prompt screen
    // handles as its own error rather than sending a wall-mounted device
    // off to a login page it can never complete.
    if (state.dashboardMode) {
      throw new Error(t("dashboard.invalidKey"));
    }
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

// Deployment settings from the server (see web/client_config.go). Fetched
// once at startup without blocking anything: nothing on the first screen
// needs it, only the API example in Settings, long after this resolves.
export let clientConfig = {};
fetch("/client-config.json")
  .then((res) => res.json())
  .then((c) => (clientConfig = c))
  .catch(() => {});
