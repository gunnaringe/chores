package web

import (
	"encoding/json"
	"strings"
	"testing"
)

// Guards material-symbols-i18n.json against drifting from the icon list:
// scripts/update-material-symbols.sh can drop or rename an icon, and an
// entry for a name that no longer exists would silently never match.
func TestMaterialSymbolsI18nNamesExist(t *testing.T) {
	var list []string
	raw, err := FS.ReadFile("material-symbols.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, e := range list {
		names[strings.SplitN(e, " ", 2)[0]] = true
	}

	raw, err = FS.ReadFile("material-symbols-i18n.json")
	if err != nil {
		t.Fatal(err)
	}
	var words map[string][]string
	if err := json.Unmarshal(raw, &words); err != nil {
		t.Fatal(err)
	}
	for name, perLang := range words {
		if !names[name] {
			t.Errorf("%q is not in material-symbols.json (renamed or removed upstream?)", name)
		}
		// [nb, nn extras, sv] — see loadLocalIconWords in app.js.
		if len(perLang) != 3 || perLang[0] == "" || perLang[2] == "" {
			t.Errorf("%q: want [nb, nn extras, sv] with nb and sv filled in, got %q", name, perLang)
		}
	}
}
