// Applies a stored theme choice (Settings > Theme) before first paint, so
// the app never flashes its default theme first on a device that picked
// something else. Loaded as a blocking <script src> in <head> of every
// page, which runs just as early as an inline script did — and inline
// script is off the table under the Content-Security-Policy (see
// web/security.go). A standalone copy of applyTheme() in app/helpers.js, which
// loads too late in <body> to beat paint; keep the two in step.
(function () {
  var theme = localStorage.getItem("chores.theme");
  if (theme === "space-invaders") theme = "hacker";
  if (theme) document.documentElement.setAttribute("data-theme", theme);
  var meta = document.getElementById("theme-color-meta");
  if (meta) meta.setAttribute("content", getComputedStyle(document.documentElement).getPropertyValue("--bg").trim());
})();
