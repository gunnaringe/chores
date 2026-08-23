// Chores — minimal i18n helper shared by the app and the login page.
// The app's own name ("Chores") is a brand and stays untranslated.

window.APP_NAME = "Chores";

window.LANGUAGES = [
  { code: "en", label: "English" },
  { code: "nb", label: "Norsk" },
];

const TRANSLATIONS = {
  en: {
    "familyPicker.subtitle": "Pick or create a family to get started.",
    "familyPicker.open": "Open",
    "familyPicker.noFamilies": "No families yet.",
    "familyPicker.createHeading": "Create a family",
    "familyPicker.nameRequired": "Family name is required",

    "onboarding.subtitle": "Create your family to get started, or open the invite link a family member sent you.",
    "onboarding.heading": "Create your family",
    "onboarding.yourNameLabel": "Your name",
    "onboarding.yourNamePlaceholder": "e.g. Mom",

    "family.nameLabel": "Family name",
    "family.namePlaceholder": "e.g. The Smiths",
    "family.createBtn": "Create family",

    "userPicker.switchFamily": "Switch family",
    "userPicker.whoIsUsing": "Who's using the app right now?",
    "userPicker.noMembers": "No family members yet — add one below.",
    "userPicker.continue": "Continue",

    "addUser.heading": "Add a family member",
    "addUser.nameLabel": "Name",
    "addUser.namePlaceholder": "Name",
    "addUser.roleLabel": "Role",
    "addUser.add": "Add",
    "addUser.nameRequired": "Name is required",

    "role.parent": "Parent",
    "role.child": "Child",

    "topbar.switchUser": "Switch user",

    "tabs.home": "Home",
    "tabs.tasks": "Tasks",
    "tabs.accounting": "Accounting",
    "tabs.family": "Family",

    "childTasks.heading": "Today's tasks",
    "childTasks.empty": "No tasks scheduled for today.",
    "childTasks.markDone": "Mark done",
    "childTasks.markNotDone": "Mark not done",

    "taskList.heading": "Tasks",
    "taskList.empty": "No tasks yet — add one below.",
    "taskList.everyDay": "Every day",
    "taskList.inactive": "inactive",
    "taskList.deactivate": "Deactivate",
    "taskList.activate": "Activate",
    "taskList.delete": "Delete",
    "taskList.confirmDelete": 'Delete task "{title}"?',
    "taskList.notDueToday": "not due today",

    "addTask.heading": "Add a task",
    "addTask.titleLabel": "Title",
    "addTask.titlePlaceholder": "e.g. Do the dishes",
    "addTask.descLabel": "Description (optional)",
    "addTask.priceLabel": "Price (kr)",
    "addTask.repeatsOn": "Repeats on",
    "addTask.assignLabel": "Assign to",
    "addTask.selectAll": "Select all",
    "addTask.noChildren": "Add a child in the Family tab before creating tasks.",
    "addTask.addBtn": "Add task",
    "addTask.titleRequired": "Title is required",
    "addTask.pricePositive": "Price must be a positive number",
    "addTask.childRequired": "Assign the task to at least one child",

    "days.sun": "Sun",
    "days.mon": "Mon",
    "days.tue": "Tue",
    "days.wed": "Wed",
    "days.thu": "Thu",
    "days.fri": "Fri",
    "days.sat": "Sat",

    "accounting.noChildren": "No children in this family yet.",
    "accounting.last7Days": "Last 7 days",
    "accounting.balanceOwed": "Balance owed",
    "accounting.payoutHeading": "Pay out",
    "accounting.amountLabel": "Amount (kr) — leave as full balance or enter a partial amount",
    "accounting.noteLabel": "Note (optional)",
    "accounting.payFull": "Pay full balance",
    "accounting.payPartial": "Pay entered amount",
    "accounting.amountPositive": "Enter an amount greater than zero",
    "accounting.historyHeading": "Payout history",
    "accounting.noPayouts": "No payouts yet.",
    "accounting.full": "full",
    "accounting.partial": "partial",

    "familyTab.heading": "Family members",
    "familyTab.you": "you",
    "familyTab.invitePending": "invite pending",

    "invitations.pendingHeading": "Pending invitations",
    "invitations.none": "No pending invitations.",
    "invitations.revoke": "Revoke",
    "invitations.inviteHeading": "Invite a family member",
    "invitations.inviteDesc": "Creates a one-time link. Whoever opens it (after logging in with their own Auth0 account) joins this family as the role you pick below.",
    "invitations.theirNameLabel": "Their name",
    "invitations.theirNamePlaceholder": "e.g. Dad, or Kid",
    "invitations.roleLabel": "Role",
    "invitations.theirEmailLabel": "Their email (optional, just for your reference)",
    "invitations.createBtn": "Create invite link",
    "invitations.shareLabel": "Share this link with them (shown once)",
    "invitations.nameRequired": "Name is required",

    "auth.signedInAs": "Signed in as {name}",
    "auth.logout": "Log out",

    "login.subtitle": "Please log in to continue.",
    "login.button": "Log in",

    "lang.label": "Language",
  },
  nb: {
    "familyPicker.subtitle": "Velg eller opprett en familie for å komme i gang.",
    "familyPicker.open": "Åpne",
    "familyPicker.noFamilies": "Ingen familier ennå.",
    "familyPicker.createHeading": "Opprett en familie",
    "familyPicker.nameRequired": "Familienavn er påkrevd",

    "onboarding.subtitle": "Opprett familien din for å komme i gang, eller åpne invitasjonslenken et familiemedlem sendte deg.",
    "onboarding.heading": "Opprett familien din",
    "onboarding.yourNameLabel": "Ditt navn",
    "onboarding.yourNamePlaceholder": "f.eks. Mamma",

    "family.nameLabel": "Familienavn",
    "family.namePlaceholder": "f.eks. Familien Hansen",
    "family.createBtn": "Opprett familie",

    "userPicker.switchFamily": "Bytt familie",
    "userPicker.whoIsUsing": "Hvem bruker appen nå?",
    "userPicker.noMembers": "Ingen familiemedlemmer ennå — legg til en under.",
    "userPicker.continue": "Fortsett",

    "addUser.heading": "Legg til familiemedlem",
    "addUser.nameLabel": "Navn",
    "addUser.namePlaceholder": "Navn",
    "addUser.roleLabel": "Rolle",
    "addUser.add": "Legg til",
    "addUser.nameRequired": "Navn er påkrevd",

    "role.parent": "Forelder",
    "role.child": "Barn",

    "topbar.switchUser": "Bytt bruker",

    "tabs.home": "Hjem",
    "tabs.tasks": "Oppgaver",
    "tabs.accounting": "Regnskap",
    "tabs.family": "Familie",

    "childTasks.heading": "Dagens oppgaver",
    "childTasks.empty": "Ingen oppgaver planlagt i dag.",
    "childTasks.markDone": "Merk som utført",
    "childTasks.markNotDone": "Merk som ikke utført",

    "taskList.heading": "Oppgaver",
    "taskList.empty": "Ingen oppgaver ennå — legg til en under.",
    "taskList.everyDay": "Hver dag",
    "taskList.inactive": "inaktiv",
    "taskList.deactivate": "Deaktiver",
    "taskList.activate": "Aktiver",
    "taskList.delete": "Slett",
    "taskList.confirmDelete": 'Slette oppgaven "{title}"?',
    "taskList.notDueToday": "ikke i dag",

    "addTask.heading": "Legg til oppgave",
    "addTask.titleLabel": "Tittel",
    "addTask.titlePlaceholder": "f.eks. Vaske opp",
    "addTask.descLabel": "Beskrivelse (valgfritt)",
    "addTask.priceLabel": "Pris (kr)",
    "addTask.repeatsOn": "Gjentas på",
    "addTask.assignLabel": "Tildel til",
    "addTask.selectAll": "Velg alle",
    "addTask.noChildren": "Legg til et barn under Familie før du kan lage oppgaver.",
    "addTask.addBtn": "Legg til oppgave",
    "addTask.titleRequired": "Tittel er påkrevd",
    "addTask.pricePositive": "Prisen må være et positivt tall",
    "addTask.childRequired": "Tildel oppgaven til minst ett barn",

    "days.sun": "Søn",
    "days.mon": "Man",
    "days.tue": "Tir",
    "days.wed": "Ons",
    "days.thu": "Tor",
    "days.fri": "Fre",
    "days.sat": "Lør",

    "accounting.noChildren": "Ingen barn i denne familien ennå.",
    "accounting.last7Days": "Siste 7 dager",
    "accounting.balanceOwed": "Utestående saldo",
    "accounting.payoutHeading": "Utbetal",
    "accounting.amountLabel": "Beløp (kr) — behold hele saldoen eller angi et delbeløp",
    "accounting.noteLabel": "Notat (valgfritt)",
    "accounting.payFull": "Utbetal hele saldoen",
    "accounting.payPartial": "Utbetal angitt beløp",
    "accounting.amountPositive": "Angi et beløp større enn null",
    "accounting.historyHeading": "Utbetalingshistorikk",
    "accounting.noPayouts": "Ingen utbetalinger ennå.",
    "accounting.full": "hel",
    "accounting.partial": "del",

    "familyTab.heading": "Familiemedlemmer",
    "familyTab.you": "deg",
    "familyTab.invitePending": "invitasjon venter",

    "invitations.pendingHeading": "Ventende invitasjoner",
    "invitations.none": "Ingen ventende invitasjoner.",
    "invitations.revoke": "Trekk tilbake",
    "invitations.inviteHeading": "Inviter et familiemedlem",
    "invitations.inviteDesc": "Oppretter en engangslenke. Den som åpner den (etter å ha logget inn med sin egen Auth0-konto) blir med i denne familien med rollen du velger under.",
    "invitations.theirNameLabel": "Navnet deres",
    "invitations.theirNamePlaceholder": "f.eks. Pappa, eller Barn",
    "invitations.roleLabel": "Rolle",
    "invitations.theirEmailLabel": "E-posten deres (valgfritt, kun til din egen referanse)",
    "invitations.createBtn": "Opprett invitasjonslenke",
    "invitations.shareLabel": "Del denne lenken med dem (vises kun én gang)",
    "invitations.nameRequired": "Navn er påkrevd",

    "auth.signedInAs": "Innlogget som {name}",
    "auth.logout": "Logg ut",

    "login.subtitle": "Logg inn for å fortsette.",
    "login.button": "Logg inn",

    "lang.label": "Språk",
  },
};

function getLang() {
  const stored = localStorage.getItem("chores.lang");
  if (stored && TRANSLATIONS[stored]) return stored;
  const nav = (navigator.language || "en").toLowerCase();
  if (nav.startsWith("nb") || nav.startsWith("no") || nav.startsWith("nn")) return "nb";
  return "en";
}

function setLang(lang) {
  if (TRANSLATIONS[lang]) localStorage.setItem("chores.lang", lang);
}

function localeTag(lang) {
  return (lang || getLang()) === "nb" ? "nb-NO" : "en-US";
}

function t(key, vars) {
  const lang = getLang();
  let str = (TRANSLATIONS[lang] && TRANSLATIONS[lang][key]) || TRANSLATIONS.en[key] || key;
  if (vars) {
    for (const k of Object.keys(vars)) {
      str = str.replace(new RegExp(`\\{${k}\\}`, "g"), vars[k]);
    }
  }
  return str;
}

window.t = t;
window.getLang = getLang;
window.setLang = setLang;
window.localeTag = localeTag;
