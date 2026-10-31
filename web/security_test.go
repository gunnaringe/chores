package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeaders(t *testing.T) {
	h := SecurityHeaders(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	csp := rec.Header().Get("Content-Security-Policy")
	for _, want := range []string{"script-src 'self';", "frame-ancestors 'none'", "object-src 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP %q lacks %q", csp, want)
		}
	}
	if strings.Contains(csp, "script-src 'self' 'unsafe-inline'") {
		t.Error("CSP must not allow inline script")
	}
	if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options = %q, want DENY", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", got)
	}
	if rec.Header().Get("Strict-Transport-Security") != "" {
		t.Error("HSTS sent over plain HTTP")
	}

	// The kiosk dashboard may be framed (e.g. a Home Assistant panel).
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dashboard", nil))
	if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "frame-ancestors *") {
		t.Error("/dashboard should be frameable")
	}
	if rec.Header().Get("X-Frame-Options") != "" {
		t.Error("/dashboard should not send X-Frame-Options")
	}

	// HSTS behind a TLS-terminating proxy.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Strict-Transport-Security") == "" {
		t.Error("no HSTS behind an https proxy")
	}
}

func TestErrorPageHasNoInlineScript(t *testing.T) {
	rec := httptest.NewRecorder()
	RenderErrorPage(rec, http.StatusNotFound, "nope")
	body := rec.Body.String()
	if strings.Contains(body, "<script>") || strings.Contains(body, " onclick=") {
		t.Fatal("error page carries inline script, which the CSP blocks")
	}
}

// The CSP forbids inline script, so any inline <script> block or on*=
// attribute that creeps back into a page silently stops working in the
// browser. Catch it here instead.
func TestPagesHaveNoInlineScript(t *testing.T) {
	for _, name := range []string{"index.html", "login.html", "privacy.html", "app.js"} {
		b, err := FS.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		if strings.HasSuffix(name, ".html") && strings.Contains(src, "<script>") {
			t.Errorf("%s has an inline <script> block; move it to a .js file", name)
		}
		for _, attr := range []string{" onclick=", " onerror=", " onload=", " onchange=", " oninput=", " onsubmit="} {
			if strings.Contains(src, attr) {
				t.Errorf("%s uses an inline%s handler; use addEventListener", name, attr)
			}
		}
	}
}
