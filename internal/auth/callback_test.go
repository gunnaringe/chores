package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeProvider serves just enough of an OAuth2 provider for CallbackHandler:
// a token endpoint that accepts any code, and a userinfo endpoint returning
// whatever profile the test gives it.
func fakeProvider(t *testing.T, profile map[string]string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"access_token": "at", "token_type": "Bearer"})
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(profile)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func callback(t *testing.T, provider *httptest.Server) *httptest.ResponseRecorder {
	t.Helper()
	m, err := NewManager(Config{Domain: provider.URL, ClientID: "id", ClientSecret: "secret"})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	r := httptest.NewRequest(http.MethodGet, "/auth/callback?state=s&code=c", nil)
	r.AddCookie(&http.Cookie{Name: stateCookieName, Value: "s"})
	w := httptest.NewRecorder()
	m.CallbackHandler(w, r)
	return w
}

func sessionCookieSet(w *httptest.ResponseRecorder) bool {
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookieName && c.Value != "" {
			return true
		}
	}
	return false
}

func TestCallback_CreatesSession(t *testing.T) {
	w := callback(t, fakeProvider(t, map[string]string{"sub": "auth0|someone", "name": "Someone"}))
	if w.Code != http.StatusFound || !sessionCookieSet(w) {
		t.Fatalf("expected a redirect with a session cookie, got %d", w.Code)
	}
}

// Every membership check matches on sub, so a profile without one must not
// become a session — all such logins would be the same person.
func TestCallback_RejectsProfileWithoutSub(t *testing.T) {
	w := callback(t, fakeProvider(t, map[string]string{"name": "Nobody"}))
	if w.Code != http.StatusBadGateway || sessionCookieSet(w) {
		t.Fatalf("expected a 502 without a session cookie, got %d", w.Code)
	}
}
