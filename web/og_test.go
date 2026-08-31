package web

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseHostLanguages(t *testing.T) {
	hosts, err := ParseHostLanguages(" ukepenger.example=nb , Vekepengar.Example=nn ")
	if err != nil {
		t.Fatalf("ParseHostLanguages: %v", err)
	}
	// Hostnames are case-insensitive, so the map is keyed lowercased.
	want := map[string]string{"ukepenger.example": "nb", "vekepengar.example": "nn"}
	for host, lang := range want {
		if hosts[host] != lang {
			t.Errorf("host %q = %q, want %q", host, hosts[host], lang)
		}
	}

	if hosts, err := ParseHostLanguages(""); err != nil || len(hosts) != 0 {
		t.Errorf("empty spec = %v, %v; want empty map and no error", hosts, err)
	}

	// A typo must not degrade to "English for everyone", which nothing in a
	// browser would ever reveal.
	for _, spec := range []string{"ukepenger.example", "ukepenger.example=xx", "=nb", "ukepenger.example="} {
		if _, err := ParseHostLanguages(spec); err == nil {
			t.Errorf("ParseHostLanguages(%q) succeeded, want error", spec)
		}
	}
}

// render returns the login page as served for a given host and query.
func render(t *testing.T, hostLangs map[string]string, host, query string) string {
	t.Helper()
	pages, err := NewPages(hostLangs)
	if err != nil {
		t.Fatalf("NewPages: %v", err)
	}
	r := httptest.NewRequest("GET", "http://"+host+"/"+query, nil)
	r.Host = host
	w := httptest.NewRecorder()
	pages.Render(w, r, "login.html")
	return w.Body.String()
}

func TestRenderStampsDefaultLanguage(t *testing.T) {
	hosts := map[string]string{"ukepenger.example": "nb"}

	// getLang() in i18n.js reads data-default-lang; lang is the document's
	// own declared language.
	body := render(t, hosts, "ukepenger.example", "")
	if !strings.Contains(body, `<html lang="nb" data-default-lang="nb">`) {
		t.Error("nb host did not stamp its default language onto <html>")
	}
	if strings.Contains(body, langPlaceholder) {
		t.Error("the default <html lang=\"en\"> survived on a mapped host")
	}
	if body := render(t, hosts, "chores.example", ""); !strings.Contains(body, `<html lang="en" data-default-lang="en">`) {
		t.Error("unmapped host did not stamp English")
	}
}

func TestRenderLocalizesByHost(t *testing.T) {
	hosts := map[string]string{"ukepenger.example": "nb"}

	body := render(t, hosts, "ukepenger.example", "")
	for _, want := range []string{
		`<meta property="og:title" content="Ukepenger" />`,
		`<meta property="og:description" content="Husarbeid og ukepenger på ett sted." />`,
		`<meta property="og:locale" content="nb_NO" />`,
		// Each hostname must advertise itself: Facebook caches a scrape
		// against the og:url it is told, so a shared canonical would show
		// one domain's card for both.
		`<meta property="og:url" content="http://ukepenger.example/" />`,
		`<meta property="og:image" content="http://ukepenger.example/icons/og-image-nb.png" />`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("nb host is missing:\n%s", want)
		}
	}
	if strings.Contains(body, ogPlaceholder) {
		t.Error("placeholder survived into the served page")
	}

	// An unlisted host falls back to English rather than erroring.
	body = render(t, hosts, "chores.example", "")
	if !strings.Contains(body, `<meta property="og:title" content="Chores" />`) {
		t.Error("unmapped host did not fall back to English")
	}
	if !strings.Contains(body, `<meta property="og:url" content="http://chores.example/" />`) {
		t.Error("unmapped host did not reflect its own origin")
	}
}

func TestRenderLangQueryOverridesHost(t *testing.T) {
	hosts := map[string]string{"ukepenger.example": "nb"}

	if body := render(t, hosts, "chores.example", "?lang=sv"); !strings.Contains(body, `content="Veckopeng"`) {
		t.Error("?lang= did not override the host mapping")
	}
	// An unknown ?lang= falls through to the host's own language rather
	// than dropping to English.
	if body := render(t, hosts, "ukepenger.example", "?lang=xx"); !strings.Contains(body, `content="Ukepenger"`) {
		t.Error("unknown ?lang= did not fall back to the host language")
	}
}

func TestRenderEscapesHost(t *testing.T) {
	// Host is caller-controlled, so it must never be able to close an
	// attribute and open a tag.
	body := render(t, nil, `evil.example/"><script>alert(1)</script>`, "")
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Errorf("host was reflected unescaped:\n%s", body)
	}
}

func TestRenderHostPortIsIgnored(t *testing.T) {
	// Local dev serves on a port; the mapping is keyed on the bare host.
	body := render(t, map[string]string{"ukepenger.example": "nb"}, "ukepenger.example:8080", "")
	if !strings.Contains(body, `content="Ukepenger"`) {
		t.Error("host:port did not match the configured hostname")
	}
	if !strings.Contains(body, `<meta property="og:url" content="http://ukepenger.example:8080/" />`) {
		t.Error("og:url dropped the port it was served on")
	}
}
