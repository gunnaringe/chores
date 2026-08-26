// Chores — minimal i18n helper shared by the app and the welcome page.
//
// The app's name is itself translated: it's "Chores" in English and
// "Ukelønn" in Norwegian, including the name an installed PWA gets. Use
// appName() rather than a literal anywhere the name is shown.
//
// The same two names exist in Go, in web/manifest.go, which serves the
// localized web app manifest — keep them in step.

window.LANGUAGES = [
  { code: "en", label: "English" },
  { code: "nb", label: "Norsk" },
];

const TRANSLATIONS = {
  en: {
    "app.name": "Chores",

    "familyPicker.nameRequired": "Family name is required",

    "onboarding.subtitle": "Create your family to get started, or join one with an invite code.",
    "onboarding.heading": "Create your family",
    "onboarding.yourNameLabel": "Your name",
    "onboarding.yourNamePlaceholder": "e.g. Mom",
    "onboarding.joinHeading": "Have an invite code?",
    "onboarding.joinCodeLabel": "Invite code",
    "onboarding.joinBtn": "Join family",
    "onboarding.joinCodeRequired": "Enter an invite code",

    "family.nameLabel": "Family name",
    "family.namePlaceholder": "e.g. The Smiths",
    "family.createBtn": "Create family",
    "family.renameHeading": "Family name",
    "family.renameSave": "Save",
    "family.renameRequired": "Family name is required",

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

    "topbar.settings": "Settings",
    "topbar.viewingAs": "You're viewing the app as {name}",
    "topbar.switchBack": "Switch back",

    "common.close": "Close",
    "common.dismiss": "Dismiss",

    "settings.accountSection": "Account",
    "settings.familySection": "This family",
    "settings.householdsSection": "Other families",
    "settings.notificationsHeading": "Notifications",
    "settings.notificationsDesc": "Get a notification on this device whenever someone in the family completes a task.",
    "settings.notificationsEnable": "Enable notifications",
    "settings.notificationsDisable": "Disable notifications",
    "settings.notificationsEnabledOnDevice": "Notifications are enabled on this device.",
    "settings.notificationsChecking": "Checking notification status…",
    "settings.notificationsUnsupported": "Notifications aren't supported on this browser or device.",
    "settings.notificationsUnavailable": "The server doesn't have push notifications configured.",
    "settings.notificationsDenied": "Notifications are blocked for this site — enable them in your browser's site settings to use this.",

    "dashboard.enterKeyPrompt": "Enter the dashboard key for your family to continue.",
    "dashboard.keyLabel": "Dashboard key",
    "dashboard.unlock": "Unlock",
    "dashboard.keyRequired": "Enter a dashboard key",
    "dashboard.invalidKey": "That dashboard key isn't valid.",
    "dashboard.settingsHeading": "Dashboard",
    "dashboard.settingsDesc": "Set up a key-protected view of today's tasks for every child, meant for a shared screen or tablet — no login needed there, just the key. Anyone with the key can view it and mark tasks done, but can't do anything else in the app.",
    "dashboard.loading": "Loading…",
    "dashboard.urlLabel": "Dashboard URL",
    "dashboard.setup": "Set up dashboard",
    "dashboard.regenerate": "Regenerate key",
    "dashboard.disable": "Disable dashboard",

    "tabs.today": "Today",
    "tabs.tasks": "Tasks",
    "tabs.accounting": "Balance",
    "tabs.history": "History",

    "history.todayHeading": "Today",
    "history.yesterdayHeading": "Yesterday",
    "history.thisWeekHeading": "Earlier this week",
    "history.laterHeading": "Later",
    "history.empty": "Nothing here.",
    "history.loading": "Loading…",
    "history.searchPlaceholder": "Search by task or name…",
    "history.searchResultsHeading": "Search results",
    "history.loadMore": "Load more",
    "history.confirmDelete": "Confirm delete",
    "history.showIncomplete": "Show not-completed tasks",
    "history.notCompletedBadge": "Not completed",
    "history.markComplete": "Mark completed",
    "history.markIncomplete": "Mark not completed",
    "history.confirmMarkComplete": "Confirm mark completed",
    "history.confirmMarkIncomplete": "Confirm mark not completed",

    "today.progress": "{done} of {total} done",
    "today.mustDo": "Must do",
    "today.canDo": "Can do",

    "childTasks.heading": "Today's tasks",
    "childTasks.empty": "No tasks scheduled for today.",
    "childTasks.markDone": "Mark done",
    "childTasks.markNotDone": "Mark not done",

    "taskList.heading": "Tasks",
    "taskList.empty": "No tasks yet — add one below.",
    "taskList.everyDay": "Every day",
    "taskList.paused": "paused",
    "taskList.pause": "Pause",
    "taskList.resume": "Resume",
    "taskList.edit": "Edit",
    "taskList.editHeading": "Edit task",
    "taskList.saveChanges": "Save changes",
    "taskList.cancel": "Cancel",
    "taskList.delete": "Delete",
    "taskList.notDueToday": "not due today",
    "taskList.onceOn": "Once, on {date}",
    "taskList.everyNWeeks": "{days}, every {n} weeks",
    "taskList.mandatory": "Mandatory",
    "taskList.optional": "Optional",

    "addTask.heading": "Add a task",
    "addTask.titleLabel": "Title",
    "addTask.titlePlaceholder": "e.g. Do the dishes",
    "addTask.descLabel": "Description (optional)",
    "addTask.priceLabel": "Price (kr)",
    "addTask.classificationLabel": "Classification",
    "addTask.repeatLabel": "Repeat",
    "addTask.repeatOnce": "Once",
    "addTask.repeatWeekly": "Weekly",
    "addTask.repeatCron": "Cron",
    "addTask.onceDateLabel": "Date",
    "addTask.onceDateRequired": "Pick a date",
    "addTask.everyNWeeksLabel": "Every ___ week(s)",
    "addTask.daysRequired": "Pick at least one day",
    "addTask.intervalPositive": "Must repeat at least every 1 week",
    "addTask.cronLabel": "Cron expression",
    "addTask.cronHint": "Standard 5-field cron: minute hour day-of-month month day-of-week.",
    "addTask.cronRequired": "Enter a cron expression",
    "addTask.iconLabel": "Icon (optional)",
    "addTask.iconSearchPlaceholder": "Search icons…",
    "addTask.iconNoResults": "No matching icons",
    "addTask.iconClear": "Clear",
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
    "accounting.earnedToday": "Earned today",
    "accounting.earnedThisWeek": "Earned this week",
    "accounting.balanceOwed": "Balance owed",
    "accounting.payoutHeading": "Pay out",
    "accounting.amountLabel": "Amount (kr)",
    "accounting.amountHint": "Leave as the full balance, or enter a partial amount.",
    "accounting.noteLabel": "Note (optional)",
    "accounting.payFull": "Pay full balance",
    "accounting.payPartial": "Pay entered amount",
    "accounting.amountPositive": "Enter an amount greater than zero",
    "accounting.amountExceedsBalance": "Enter an amount no higher than the balance owed",
    "accounting.historyHeading": "Payout history",
    "accounting.noPayouts": "No payouts yet.",
    "accounting.full": "full",
    "accounting.partial": "partial",

    "familyTab.switchFamilyLabel": "Switch family",
    "familyTab.switchUserLabel": "Switch family member",
    "familyTab.heading": "Family members",
    "familyTab.you": "you",
    "familyTab.invitePending": "invite pending",
    "familyTab.remove": "Remove",
    "familyTab.removeWord": "remove",
    "familyTab.removeConfirmHint": 'This also deletes {name}\'s task history and payouts — it can\'t be undone. Type "{word}" to confirm.',
    "familyTab.leave": "Leave family",
    "familyTab.leaveWord": "leave",
    "familyTab.leaveConfirmHint": 'You\'ll no longer be a member of {family}. Type "{word}" to confirm.',
    "familyTab.typeToConfirmPlaceholder": 'Type "{word}"',
    "familyTab.dangerZoneHeading": "Danger zone",
    "familyTab.deleteFamilyDesc": "Permanently deletes {family} and everything in it — every task, completion, payout, and family member. This can't be undone.",
    "familyTab.deleteFamilyButton": "Delete family",
    "familyTab.deleteWord": "delete",
    "familyTab.deleteConfirmHint": 'Type "{word}" to confirm.',
    "familyTab.renameLabel": "Your name",
    "familyTab.save": "Save",
    "familyTab.createHeading": "Create a new family",
    "familyTab.createDesc": "Start another family from scratch, with you as its first parent.",
    "familyTab.joinHeading": "Join a family",
    "familyTab.joinDesc": "Enter a code someone shared with you to join their family too.",
    "familyTab.joinCodeLabel": "Invite code",
    "familyTab.joinBtn": "Join",
    "familyTab.joinCodeRequired": "Enter an invite code",

    "invitations.revoke": "Revoke",
    "invitations.revokeWord": "revoke",
    "invitations.revokeConfirmHint": 'This deletes the invite for {name} — their link and code will stop working. Type "{word}" to confirm.',
    "invitations.inviteDesc": "Creates a one-time code so they can log in with their own account — open their row above to see the invite link until they use it.",
    "invitations.linkLabel": "Invite link — opening it logs them in and accepts automatically",
    "invitations.codeLabel": "Invite code — share it with them",

    "auth.logout": "Log out",

    "login.tagline": "Family chores and allowance, in one place.",
    "login.button": "Log in",
    "login.howHeading": "How it works",
    "login.step1Title": "Parents set the chores",
    "login.step1Body": "Add a chore, set what it pays, and pick who it's for. Repeat it daily, weekly, or just once — and mark it a must-do or an optional extra.",
    "login.step2Title": "Kids tick them off",
    "login.step2Body": "Everyone gets today's list on their own phone, split into Must do and Can do. One tap marks a chore done.",
    "login.step3Title": "The balance adds up",
    "login.step3Body": "Earnings collect into a running balance. Pay out all of it or part of it whenever suits — every completed chore stays in the history.",
    "login.extrasHeading": "Also in the box",
    "login.extrasBody": "A key-protected screen for the kitchen wall that needs no login, push notifications when someone finishes a chore, and an install to the home screen that behaves like a real app. English and Norwegian throughout.",
    "login.shotToday": "Today, split into Must do and Can do",
    "login.shotTasks": "Chores, with what each one pays",
    "login.shotBalance": "The balance, and paying it out",
    "login.ctaHeading": "Ready?",
    "login.aboutHeading": "About",
    "login.sourceLink": "Source on GitHub",
    "login.hosting": "Runs on Fly.io in Stockholm. Data lives in a SQLite database.",
    "login.builtWith": "Built with Claude, on a personal Claude Pro account.",
    "login.disclaimer": "This is a hobby project, made for my own use. No guarantees are given about its reliability — use it at your own risk.",

    "lang.label": "Language",
  },
  nb: {
    "app.name": "Ukelønn",

    "familyPicker.nameRequired": "Familienavn er påkrevd",

    "onboarding.subtitle": "Opprett familien din for å komme i gang, eller bli med i en med en invitasjonskode.",
    "onboarding.heading": "Opprett familien din",
    "onboarding.yourNameLabel": "Ditt navn",
    "onboarding.yourNamePlaceholder": "f.eks. Mamma",
    "onboarding.joinHeading": "Har du en invitasjonskode?",
    "onboarding.joinCodeLabel": "Invitasjonskode",
    "onboarding.joinBtn": "Bli med i familien",
    "onboarding.joinCodeRequired": "Angi en invitasjonskode",

    "family.nameLabel": "Familienavn",
    "family.namePlaceholder": "f.eks. Familien Hansen",
    "family.createBtn": "Opprett familie",
    "family.renameHeading": "Familienavn",
    "family.renameSave": "Lagre",
    "family.renameRequired": "Familienavn er påkrevd",

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

    "topbar.settings": "Innstillinger",
    "topbar.viewingAs": "Du ser appen som {name}",
    "topbar.switchBack": "Bytt tilbake",

    "common.close": "Lukk",
    "common.dismiss": "Lukk",

    "settings.accountSection": "Konto",
    "settings.familySection": "Denne familien",
    "settings.householdsSection": "Andre familier",
    "settings.notificationsHeading": "Varsler",
    "settings.notificationsDesc": "Få et varsel på denne enheten når noen i familien fullfører en oppgave.",
    "settings.notificationsEnable": "Slå på varsler",
    "settings.notificationsDisable": "Slå av varsler",
    "settings.notificationsEnabledOnDevice": "Varsler er slått på for denne enheten.",
    "settings.notificationsChecking": "Sjekker varselstatus…",
    "settings.notificationsUnsupported": "Varsler støttes ikke i denne nettleseren eller på denne enheten.",
    "settings.notificationsUnavailable": "Serveren har ikke satt opp push-varsler.",
    "settings.notificationsDenied": "Varsler er blokkert for dette nettstedet — slå dem på i nettleserens nettstedsinnstillinger for å bruke dette.",

    "dashboard.enterKeyPrompt": "Skriv inn dashbord-nøkkelen for familien din for å fortsette.",
    "dashboard.keyLabel": "Dashbord-nøkkel",
    "dashboard.unlock": "Lås opp",
    "dashboard.keyRequired": "Skriv inn en dashbord-nøkkel",
    "dashboard.invalidKey": "Den dashbord-nøkkelen er ikke gyldig.",
    "dashboard.settingsHeading": "Dashbord",
    "dashboard.settingsDesc": "Sett opp en nøkkelbeskyttet visning av dagens oppgaver for alle barna, laget for en delt skjerm eller nettbrett — ingen innlogging der, bare nøkkelen. Alle med nøkkelen kan se den og merke oppgaver som utført, men kan ikke gjøre noe annet i appen.",
    "dashboard.loading": "Laster…",
    "dashboard.urlLabel": "Dashbord-URL",
    "dashboard.setup": "Sett opp dashbord",
    "dashboard.regenerate": "Generer ny nøkkel",
    "dashboard.disable": "Slå av dashbord",

    "tabs.today": "I dag",
    "tabs.tasks": "Oppgaver",
    "tabs.accounting": "Saldo",
    "tabs.history": "Historikk",

    "history.todayHeading": "I dag",
    "history.yesterdayHeading": "I går",
    "history.thisWeekHeading": "Tidligere denne uken",
    "history.laterHeading": "Tidligere",
    "history.empty": "Ingenting her.",
    "history.loading": "Laster…",
    "history.searchPlaceholder": "Søk etter oppgave eller navn…",
    "history.searchResultsHeading": "Søkeresultater",
    "history.loadMore": "Last inn mer",
    "history.confirmDelete": "Bekreft sletting",
    "history.showIncomplete": "Vis ikke fullførte oppgaver",
    "history.notCompletedBadge": "Ikke fullført",
    "history.markComplete": "Merk som fullført",
    "history.markIncomplete": "Merk som ikke fullført",
    "history.confirmMarkComplete": "Bekreft merking som fullført",
    "history.confirmMarkIncomplete": "Bekreft merking som ikke fullført",

    "today.progress": "{done} av {total} gjort",
    "today.mustDo": "Må gjøre",
    "today.canDo": "Kan gjøre",

    "childTasks.heading": "Dagens oppgaver",
    "childTasks.empty": "Ingen oppgaver planlagt i dag.",
    "childTasks.markDone": "Merk som utført",
    "childTasks.markNotDone": "Merk som ikke utført",

    "taskList.heading": "Oppgaver",
    "taskList.empty": "Ingen oppgaver ennå — legg til en under.",
    "taskList.everyDay": "Hver dag",
    "taskList.paused": "pause",
    "taskList.pause": "Pause",
    "taskList.resume": "Gjenoppta",
    "taskList.edit": "Rediger",
    "taskList.editHeading": "Rediger oppgave",
    "taskList.saveChanges": "Lagre endringer",
    "taskList.cancel": "Avbryt",
    "taskList.delete": "Slett",
    "taskList.notDueToday": "ikke i dag",
    "taskList.onceOn": "Én gang, {date}",
    "taskList.everyNWeeks": "{days}, hver {n}. uke",
    "taskList.mandatory": "Obligatorisk",
    "taskList.optional": "Valgfri",

    "addTask.heading": "Legg til oppgave",
    "addTask.titleLabel": "Tittel",
    "addTask.titlePlaceholder": "f.eks. Vaske opp",
    "addTask.descLabel": "Beskrivelse (valgfritt)",
    "addTask.priceLabel": "Pris (kr)",
    "addTask.classificationLabel": "Klassifisering",
    "addTask.repeatLabel": "Gjentakelse",
    "addTask.repeatOnce": "Én gang",
    "addTask.repeatWeekly": "Ukentlig",
    "addTask.repeatCron": "Cron",
    "addTask.onceDateLabel": "Dato",
    "addTask.onceDateRequired": "Velg en dato",
    "addTask.everyNWeeksLabel": "Hver ___ uke(r)",
    "addTask.daysRequired": "Velg minst én dag",
    "addTask.intervalPositive": "Må gjentas minst hver uke",
    "addTask.cronLabel": "Cron-uttrykk",
    "addTask.cronHint": "Standard 5-felts cron: minutt time dag-i-måned måned ukedag.",
    "addTask.cronRequired": "Skriv inn et cron-uttrykk",
    "addTask.iconLabel": "Ikon (valgfritt)",
    "addTask.iconSearchPlaceholder": "Søk etter ikoner…",
    "addTask.iconNoResults": "Ingen ikoner funnet",
    "addTask.iconClear": "Fjern",
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
    "accounting.earnedToday": "Opptjent i dag",
    "accounting.earnedThisWeek": "Opptjent denne uken",
    "accounting.balanceOwed": "Utestående saldo",
    "accounting.payoutHeading": "Utbetal",
    "accounting.amountLabel": "Beløp (kr)",
    "accounting.amountHint": "Behold hele saldoen, eller angi et delbeløp.",
    "accounting.noteLabel": "Notat (valgfritt)",
    "accounting.payFull": "Utbetal hele saldoen",
    "accounting.payPartial": "Utbetal angitt beløp",
    "accounting.amountPositive": "Angi et beløp større enn null",
    "accounting.amountExceedsBalance": "Angi et beløp som ikke er høyere enn utestående saldo",
    "accounting.historyHeading": "Utbetalingshistorikk",
    "accounting.noPayouts": "Ingen utbetalinger ennå.",
    "accounting.full": "hel",
    "accounting.partial": "del",

    "familyTab.switchFamilyLabel": "Bytt familie",
    "familyTab.switchUserLabel": "Bytt familiemedlem",
    "familyTab.heading": "Familiemedlemmer",
    "familyTab.you": "deg",
    "familyTab.invitePending": "invitasjon venter",
    "familyTab.remove": "Fjern",
    "familyTab.removeWord": "fjern",
    "familyTab.removeConfirmHint": 'Dette sletter også {name} sin oppgavehistorikk og utbetalinger — dette kan ikke angres. Skriv «{word}» for å bekrefte.',
    "familyTab.leave": "Forlat familien",
    "familyTab.leaveWord": "forlat",
    "familyTab.leaveConfirmHint": 'Du vil ikke lenger være medlem av {family}. Skriv «{word}» for å bekrefte.',
    "familyTab.typeToConfirmPlaceholder": 'Skriv «{word}»',
    "familyTab.dangerZoneHeading": "Faresone",
    "familyTab.deleteFamilyDesc": "Sletter {family} og alt i den for godt — alle oppgaver, fullføringer, utbetalinger og familiemedlemmer. Dette kan ikke angres.",
    "familyTab.deleteFamilyButton": "Slett familien",
    "familyTab.deleteWord": "slett",
    "familyTab.deleteConfirmHint": 'Skriv «{word}» for å bekrefte.',
    "familyTab.renameLabel": "Ditt navn",
    "familyTab.save": "Lagre",
    "familyTab.createHeading": "Opprett en ny familie",
    "familyTab.createDesc": "Start en ny familie fra bunnen av, med deg som dens første forelder.",
    "familyTab.joinHeading": "Bli med i en familie",
    "familyTab.joinDesc": "Angi en kode noen har delt med deg for å også bli med i familien deres.",
    "familyTab.joinCodeLabel": "Invitasjonskode",
    "familyTab.joinBtn": "Bli med",
    "familyTab.joinCodeRequired": "Angi en invitasjonskode",

    "invitations.revoke": "Trekk tilbake",
    "invitations.revokeWord": "opphev",
    "invitations.revokeConfirmHint": 'Dette sletter invitasjonen til {name} — lenken og koden slutter å virke. Skriv «{word}» for å bekrefte.',
    "invitations.inviteDesc": "Oppretter en engangskode så de kan logge inn med sin egen konto — åpne raden deres over for å se invitasjonslenken til de bruker den.",
    "invitations.linkLabel": "Invitasjonslenke — å åpne den logger dem inn og godtar automatisk",
    "invitations.codeLabel": "Invitasjonskode — del den med dem",

    "auth.logout": "Logg ut",

    "login.tagline": "Husarbeid og ukelønn på ett sted.",
    "login.button": "Logg inn",
    "login.howHeading": "Slik fungerer det",
    "login.step1Title": "Foreldre setter opp oppgavene",
    "login.step1Body": "Legg til en oppgave, sett hva den er verdt, og velg hvem den gjelder. Gjenta den daglig, ukentlig eller bare én gang — og merk den som må-gjøre eller valgfri ekstrajobb.",
    "login.step2Title": "Barna huker dem av",
    "login.step2Body": "Alle får dagens liste på sin egen telefon, delt i Må gjøre og Kan gjøre. Ett trykk markerer en oppgave som gjort.",
    "login.step3Title": "Saldoen vokser",
    "login.step3Body": "Det opptjente samles i en løpende saldo. Betal ut alt eller deler av det når det passer — alle fullførte oppgaver blir liggende i historikken.",
    "login.extrasHeading": "Dette er også med",
    "login.extrasBody": "En nøkkelbeskyttet skjerm til kjøkkenveggen som ikke krever innlogging, varsler når noen fullfører en oppgave, og installasjon på hjemskjermen som oppfører seg som en ekte app. Norsk og engelsk gjennomgående.",
    "login.shotToday": "I dag, delt i Må gjøre og Kan gjøre",
    "login.shotTasks": "Oppgaver, med hva hver enkelt er verdt",
    "login.shotBalance": "Saldoen, og utbetaling av den",
    "login.ctaHeading": "Klar?",
    "login.aboutHeading": "Om",
    "login.sourceLink": "Kildekode på GitHub",
    "login.hosting": "Kjører på Fly.io i Stockholm. Dataene ligger i en SQLite-database.",
    "login.builtWith": "Laget med Claude, på en personlig Claude Pro-konto.",
    "login.disclaimer": "Dette er et hobbyprosjekt laget til eget bruk. Det gis ingen garantier for appens pålitelighet — bruk den på eget ansvar.",

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

function appName() {
  return t("app.name");
}

// Stamps the current language's app name onto everything outside the page
// body that carries it: the tab title, the iOS add-to-home-screen name, and
// the manifest a PWA install reads its name from. The manifest href is
// rewritten rather than the file being static, because the installed app's
// name has to follow the language the user actually picked — browsers
// re-read the manifest when that href changes.
function applyAppName() {
  document.title = appName();

  const appleTitle = document.querySelector('meta[name="apple-mobile-web-app-title"]');
  if (appleTitle) appleTitle.setAttribute("content", appName());

  const manifest = document.querySelector('link[rel="manifest"]');
  if (manifest) manifest.setAttribute("href", `/manifest.webmanifest?lang=${encodeURIComponent(getLang())}`);
}

window.t = t;
window.appName = appName;
window.applyAppName = applyAppName;
window.getLang = getLang;
window.setLang = setLang;
window.localeTag = localeTag;
