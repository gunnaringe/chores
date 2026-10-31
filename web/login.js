// Welcome page (login.html) bootstrap. A file of its own because the
// Content-Security-Policy (web/security.go) forbids inline script.
(function () {
  const text = (id, key) => {
    document.getElementById(id).textContent = t(key);
  };

  applyAppName();
  text("app-title", "app.name");
  text("login-tagline", "login.tagline");
  text("login-btn", "login.button");
  text("login-btn-2", "login.button");
  text("dashboard-link", "login.dashboardLink");
  text("how-heading", "login.howHeading");
  text("extras-heading", "login.extrasHeading");
  text("extras-body", "login.extrasBody");
  text("integrations-heading", "login.integrationsHeading");
  text("ha-body", "login.integrationHaBody");
  text("kindle-link", "login.integrationKindleLink");
  text("kindle-body", "login.integrationKindleBody");
  text("cta-heading", "login.ctaHeading");
  text("about-heading", "login.aboutHeading");
  text("source-link", "login.sourceLink");
  text("license-link", "login.licenseLink");
  text("privacy-link", "login.privacyLink");
  text("hosting", "login.hosting");
  text("tech-stack-heading", "login.techStackHeading");
  text("built-with", "login.builtWith");
  text("disclaimer", "login.disclaimer");

  const techStack = document.getElementById("tech-stack");
  for (const key of ["login.techStack1", "login.techStack2", "login.techStack3", "login.techStack4", "login.techStack5", "login.techStack6"]) {
    const li = document.createElement("li");
    li.textContent = t(key);
    techStack.appendChild(li);
  }

  const STEPS = [
    { icon: "edit_note", title: "login.step1Title", body: "login.step1Body" },
    { icon: "check_circle", title: "login.step2Title", body: "login.step2Body" },
    { icon: "savings", title: "login.step3Title", body: "login.step3Body" },
  ];
  const steps = document.getElementById("steps");
  for (const step of STEPS) {
    const li = document.createElement("li");
    li.className = "step";

    const icon = document.createElement("span");
    icon.className = "step-icon material-symbols-outlined";
    icon.textContent = step.icon;

    const body = document.createElement("div");
    const h3 = document.createElement("h3");
    h3.textContent = t(step.title);
    const p = document.createElement("p");
    p.textContent = t(step.body);
    p.style.margin = "0";
    body.append(h3, p);

    li.append(icon, body);
    steps.appendChild(li);
  }

  // Screenshots exist per language, since every word in them is UI text.
  // Only English and Bokmål have a set (regenerate them with
  // .claude/skills/verify-ui/welcome-shots.js). Nynorsk readers get the
  // Bokmål set, everyone else English — picked here rather than by
  // requesting a missing file and falling back, which cost a 404 per
  // screenshot on every visit. The onerror fallback stays as a safety net.
  const SHOTS = [
    { file: "today", caption: "login.shotToday" },
    { file: "tasks", caption: "login.shotTasks" },
    { file: "balance", caption: "login.shotBalance" },
  ];
  const lang = getLang();
  const shotLang = { en: "en", nb: "nb", nn: "nb" }[lang] || "en";
  const shots = document.getElementById("shots");
  for (const shot of SHOTS) {
    const figure = document.createElement("figure");
    figure.className = "shot";

    const img = document.createElement("img");
    img.src = `/screenshots/${shot.file}-${shotLang}.webp`;
    img.alt = t(shot.caption);
    img.loading = "lazy";
    img.decoding = "async";
    img.width = 390;
    img.height = 720;
    img.onerror = () => {
      img.onerror = null;
      img.src = `/screenshots/${shot.file}-en.webp`;
    };

    const caption = document.createElement("figcaption");
    caption.textContent = t(shot.caption);

    figure.append(img, caption);
    shots.appendChild(figure);
  }

  for (const id of ["login-btn", "login-btn-2"]) {
    document.getElementById(id).addEventListener("click", () => {
      location.href = "/auth/login";
    });
  }

  const switcher = document.getElementById("lang-switcher");
  switcher.setAttribute("aria-label", t("lang.label"));
  switcher.innerHTML = window.LANGUAGES.map(
    (l) => `<option value="${l.code}"${l.code === getLang() ? " selected" : ""}>${l.label}</option>`
  ).join("");
  switcher.addEventListener("change", (e) => {
    setLang(e.target.value);
    location.reload();
  });

  if ("serviceWorker" in navigator) {
    window.addEventListener("load", () => navigator.serviceWorker.register("/sw.js"));
  }
})();
