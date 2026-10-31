// Privacy page bootstrap. A file of its own because the
// Content-Security-Policy (web/security.go) forbids inline script.
document.getElementById("back-app-name").textContent = t("app.name");
function showPolicy(lang) {
  document.querySelectorAll("[data-policy-lang]").forEach(function (a) {
    a.hidden = a.dataset.policyLang !== lang;
  });
  document.documentElement.lang = lang;
  document.title = document.querySelector('[data-policy-lang="' + lang + '"] h1').textContent;
}
showPolicy(getLang() === "nb" || getLang() === "nn" ? "nb" : "en");
document.querySelectorAll("[data-show-lang]").forEach(function (link) {
  link.addEventListener("click", function (e) {
    e.preventDefault();
    showPolicy(link.dataset.showLang);
  });
});
