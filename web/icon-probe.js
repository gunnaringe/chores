// Belt-and-braces for the font-display:block in app.css. Material Symbols are
// ligature *text*: an icon is the literal word "edit_note" until the font
// arrives and collapses it to one glyph. So a font that never arrives
// doesn't degrade to a missing icon, it degrades to that word, at full
// text width, spilling across the layout and swallowing taps meant for
// its neighbours. icons-loading (see app.css) hides glyphs until we know
// which case we're in; icons-unavailable is the terminal fallback.
//
// Deciding that needs a measurement, not the Font Loading API. When no
// @font-face for the family is registered (app.css not loaded yet, or
// an old cached copy from before the font was self-hosted), an unregistered
// family counts as an available *system* font, so fonts.load() resolves
// and fonts.check() returns true while every icon would still paint as
// its name. Both report success for the one case that most needs
// catching, which is why this script measures a real ligature instead.
//
// Shared by index.html and login.html as a blocking <script src> in
// <head>: that still runs before the first paint, and inline script is
// off the table under the Content-Security-Policy (web/security.go).
document.documentElement.classList.add("icons-loading");
(function () {
  var root = document.documentElement;
  var FONT = '24px "Material Symbols Outlined"';

  // With the font, "settings" is a single ~24px glyph; without it the word
  // runs 60px+. Under 2em means the ligature actually substituted.
  function ligaturesRender() {
    var probe = document.createElement("span");
    probe.textContent = "settings";
    // Inline styles rather than the .material-symbols-outlined class, so
    // this measures the font itself and not whether app.css has landed.
    probe.style.cssText =
      "position:absolute;top:0;left:-9999px;visibility:hidden;white-space:nowrap;" +
      "font-family:'Material Symbols Outlined';font-size:24px;line-height:1;";
    document.body.appendChild(probe);
    var width = probe.getBoundingClientRect().width;
    document.body.removeChild(probe);
    return width > 0 && width < 48;
  }

  // Called from several places and deliberately idempotent, so a font that
  // turns up late still upgrades the page out of the fallback.
  function settle() {
    if (!document.body) {
      document.addEventListener("DOMContentLoaded", settle);
      return;
    }
    // Put the fallback class on before taking icons-loading off, or the
    // raw names paint for a frame in between.
    root.classList.toggle("icons-unavailable", !ligaturesRender());
    root.classList.remove("icons-loading");
  }

  if (window.FontFace && document.fonts) {
    document.fonts.load(FONT).then(settle, settle);
    document.fonts.ready.then(settle, settle);
    // Backstop: neither promise is guaranteed to settle on every browser.
    setTimeout(settle, 3000);
  } else {
    settle();
  }
})();
