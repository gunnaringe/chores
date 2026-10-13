package web

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMaterialSymbolsHandler(t *testing.T) {
	for _, gz := range []bool{false, true} {
		req := httptest.NewRequest("GET", "/material-symbols.json", nil)
		if gz {
			req.Header.Set("Accept-Encoding", "gzip, deflate, br")
		}
		rec := httptest.NewRecorder()
		MaterialSymbolsHandler(rec, req)

		var body io.Reader = rec.Body
		if gz {
			if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
				t.Fatalf("Content-Encoding = %q, want gzip", got)
			}
			zr, err := gzip.NewReader(rec.Body)
			if err != nil {
				t.Fatal(err)
			}
			body = zr
		} else if got := rec.Header().Get("Content-Encoding"); got != "" {
			t.Fatalf("Content-Encoding = %q without Accept-Encoding", got)
		}

		var entries []string
		if err := json.NewDecoder(body).Decode(&entries); err != nil {
			t.Fatalf("gzip=%v: %v", gz, err)
		}
		found := false
		for _, e := range entries {
			if strings.HasPrefix(e, "delete ") && strings.Contains(e, " trash") {
				found = true
			}
		}
		if !found {
			t.Fatalf("gzip=%v: no \"delete\" entry tagged \"trash\" among %d entries", gz, len(entries))
		}
	}
}

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
