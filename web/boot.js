// Page bootstrap for index.html, after i18n.js. A file of its own because
// the Content-Security-Policy (web/security.go) forbids inline script.
applyAppName();
if ("serviceWorker" in navigator) {
  window.addEventListener("load", () => navigator.serviceWorker.register("/sw.js"));
}
