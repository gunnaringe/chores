// Minimal service worker: caches the static app shell so it (and the
// install prompt) keep working offline, and stays completely out of the
// way of the Connect API and auth routes, which must always hit the
// network — nothing about family data or login state is ever cached here.

const CACHE_NAME = "chores-shell-v2";
const PRECACHE_URLS = [
  "/",
  "/app.js",
  "/app.css",
  "/i18n.js",
  "/login.html",
  "/manifest.webmanifest",
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

function isDynamic(pathname) {
  return pathname.startsWith("/ukelonn.v1.UkelonnService/") || pathname.startsWith("/auth/") || pathname.startsWith("/invite/");
}

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
