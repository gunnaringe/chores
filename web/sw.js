// Minimal service worker: caches the static app shell so it (and the
// install prompt) keep working offline, and stays completely out of the
// way of the Connect API and auth routes, which must always hit the
// network — nothing about family data or login state is ever cached here.

// Bump this whenever a release changes app.js in a way that cannot talk to
// the older server, or vice versa. The fetch handler below is cache-first,
// so without a bump an already-installed client loads fresh index.html
// against a *cached* app.js — fine for a cosmetic change, broken for an
// API change, since the stale script sends fields the server no longer
// accepts.
//
// v16: prices and amounts became a Money message, the repeat fields became
// a Schedule oneof, and completions folded into occurrences.
const CACHE_NAME = "chores-shell-v25";
const PRECACHE_URLS = [
  "/app.js",
  "/app.css",
  "/i18n.js",
  "/login.html",
  "/icons/icon-192.png",
  "/icons/icon-512.png",
];

self.addEventListener("install", (event) => {
  event.waitUntil(
    caches
      .open(CACHE_NAME)
      .then((cache) => cache.addAll(PRECACHE_URLS))
      .then(() => self.skipWaiting())
  );
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) => Promise.all(keys.filter((k) => k !== CACHE_NAME).map((k) => caches.delete(k))))
      .then(() => self.clients.claim())
  );
});

// "/" is excluded here too, even though it's the app's own shell — Gate
// (see internal/auth/auth.go) serves completely different content there
// depending on login state (login page vs. the real app), so caching it
// risks serving a stale login page right after a successful login, forcing
// a second one to see it clear. Every other document is a fixed static
// file that's fine to cache.
// The manifest is excluded for the same reason as "/": it is generated per
// request now, with the app's name localized to ?lang= (see web/manifest.go).
// Caching it would hand a Norwegian install the English name, which is
// exactly the thing that name is supposed to follow.
function isDynamic(pathname) {
  return (
    pathname === "/" ||
    pathname === "/manifest.webmanifest" ||
    pathname.startsWith("/chores.v1.ChoresService/") ||
    pathname.startsWith("/auth/") ||
    pathname.startsWith("/invite/")
  );
}

// Push payloads are plain JSON ({title, body}) built server-side in
// notifyTaskCompleted — nothing here fetches or caches anything.
self.addEventListener("push", (event) => {
  let data = {};
  try {
    data = event.data ? event.data.json() : {};
  } catch (_) {
    data = { body: event.data ? event.data.text() : "" };
  }
  event.waitUntil(
    self.registration.showNotification(data.title || "Chores", {
      body: data.body || "",
      icon: "/icons/icon-192.png",
      badge: "/icons/icon-192.png",
    })
  );
});

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  event.waitUntil(
    self.clients.matchAll({ type: "window", includeUncontrolled: true }).then((clients) => {
      for (const client of clients) {
        if ("focus" in client) return client.focus();
      }
      return self.clients.openWindow("/");
    })
  );
});

self.addEventListener("fetch", (event) => {
  const req = event.request;
  if (req.method !== "GET") return;

  const url = new URL(req.url);
  if (url.origin !== self.location.origin || isDynamic(url.pathname)) return;

  event.respondWith(
    caches.match(req).then((cached) => {
      const network = fetch(req)
        .then((res) => {
          if (res.ok) {
            const copy = res.clone();
            caches.open(CACHE_NAME).then((cache) => cache.put(req, copy));
          }
          return res;
        })
        .catch(() => cached);
      return cached || network;
    })
  );
});
