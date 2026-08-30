#!/usr/bin/env node
// Seeds a family, one or more children, a repeating task, and completions
// spread across several months (crossing a couple of year boundaries) so
// screenshot.js — or any custom verify-ui flow — has something worth looking
// at, instead of an empty onboarding screen or a single flat month of data.
//
// Talks to the Connect API directly over plain HTTP; no browser needed. The
// only non-trivial part is the login, since every RPC sits behind a session
// cookie (see internal/auth/auth.go) — but devauth's identity picker is
// plain HTML with an `identity=` shortcut (see cmd/devauth/main.go), so the
// whole handshake is three GETs with redirects followed by hand, no
// client_id/secret needed on this end: the actual code/token exchange
// happens server-side, between chores and devauth, inside CallbackHandler.
//
//   node .claude/skills/verify-ui/seed-demo-data.js \
//     --url http://localhost:8080/ --children "Anna,Erik"
//
// Logs in as devauth's "Test Parent" identity. If that login has no family
// yet, creates one; otherwise seeds into whatever family it already has.
// Prints the created ids as JSON.

const arg = (name, fallback) => {
  const i = process.argv.indexOf("--" + name);
  return i !== -1 && process.argv[i + 1] ? process.argv[i + 1] : fallback;
};

const BASE = arg("url", "http://localhost:8080/").replace(/\/$/, "");
const FAMILY_NAME = arg("family", "The Testsons");
const CHILDREN = arg("children", "Kid").split(",").map((s) => s.trim()).filter(Boolean);
const TASK_TITLE = arg("task", "Dishes");
const PRICE_CENTS = Number(arg("price-cents", "500"));
// devauth's default parent identity ("Test Parent") — see cmd/devauth.
const IDENTITY_SUB = "devauth|local-parent";

// ---- cookie jar -----------------------------------------------------------
// No domain scoping: the devauth hop below ends up receiving chores' cookies
// too, which is harmless since devauth's /authorize ignores cookies entirely
// and only this script ever reads the jar back.

const jar = new Map();
function rememberCookies(res) {
  for (const raw of res.headers.getSetCookie()) {
    const pair = raw.split(";", 1)[0];
    const eq = pair.indexOf("=");
    jar.set(pair.slice(0, eq).trim(), pair.slice(eq + 1).trim());
  }
}
function cookieHeader() {
  return [...jar].map(([k, v]) => `${k}=${v}`).join("; ");
}

// A GET that doesn't follow its redirect automatically, so the Location can
// be inspected (and mutated, for the devauth hop) and cookies from each hop
// are captured before the next request fires.
async function hop(url) {
  const res = await fetch(url, { redirect: "manual", headers: { Cookie: cookieHeader() } });
  rememberCookies(res);
  const location = res.headers.get("location");
  if (!location) throw new Error(`expected a redirect from ${url}, got ${res.status}`);
  return new URL(location, url);
}

// auth.go's LoginHandler redirects to devauth's /authorize (setting a state
// cookie); devauth's /authorize, told which identity to use, redirects
// straight to /auth/callback with a code (no picker — see resolveIdentity in
// cmd/devauth/main.go); auth.go's CallbackHandler exchanges that code for a
// token itself and sets the session cookie everything else needs.
async function login() {
  const authorizeURL = await hop(`${BASE}/auth/login`);
  authorizeURL.searchParams.set("identity", IDENTITY_SUB);
  const callbackURL = await hop(authorizeURL);
  await hop(callbackURL);
  if (!jar.has("chores_session")) {
    throw new Error("login did not produce a chores_session cookie");
  }
}

async function call(method, req) {
  const res = await fetch(`${BASE}/chores.v1.ChoresService/${method}`, {
    method: "POST",
    headers: { "Content-Type": "application/json", Cookie: cookieHeader() },
    body: JSON.stringify(req || {}),
  });
  if (!res.ok) {
    throw new Error(`${method} failed: ${res.status} ${await res.text()}`);
  }
  return res.json();
}

// Relative to "now" rather than fixed calendar dates, so this keeps landing
// in the past — and still crossing a year boundary or two — no matter when
// it's run. Balance's monthly breakdown only shows its year-heading behavior
// with data that spans multiple calendar years.
function monthsAgo(n, day = 5) {
  const now = new Date();
  const d = new Date(now.getFullYear(), now.getMonth() - n, day);
  const pad = (x) => String(x).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}
const COMPLETION_OFFSETS = [0, 1, 14, 15, 26]; // this month, last month, then two year boundaries back

(async () => {
  await login();

  const membership = await call("GetMyMembership", {});
  let familyId = membership.memberships?.[0]?.family?.id;
  if (!familyId) {
    const created = await call("CreateFamily", { name: FAMILY_NAME });
    familyId = created.family.id;
  }

  const childIds = [];
  for (const name of CHILDREN) {
    const child = await call("CreateUser", { familyId, name, role: "USER_ROLE_CHILD" });
    childIds.push(child.user.id);
  }
  const task = await call("CreateTask", {
    familyId,
    title: TASK_TITLE,
    schedule: { cron: { expression: "0 0 * * *" } },
    price: { cents: PRICE_CENTS },
    childIds,
  });
  const taskId = task.task.id;
  const dates = COMPLETION_OFFSETS.map((n) => monthsAgo(n));
  for (const childId of childIds) {
    for (const dueDate of dates) {
      await call("CompleteTask", { taskId, childId, dueDate });
    }
  }

  console.log(JSON.stringify({ familyId, childIds, taskId, dates }, null, 2));
})().catch((e) => {
  console.error("FAILED:", e);
  process.exit(1);
});
