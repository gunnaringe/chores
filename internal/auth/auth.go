// Package auth gates access to the app behind an Auth0 login when
// configured, or lets every request through when running in local-testing
// mode. It intentionally does not know anything about families or users —
// it only answers "is somebody logged in, and as which login identity" for
// the app as a whole. Binding a login identity to a specific family member
// is the server package's job (see internal/server); this package just
// makes that identity available via context.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

type Mode string

const (
	ModeDisabled Mode = "disabled"
	ModeAuth0    Mode = "auth0"
)

type Config struct {
	Mode         Mode
	Domain       string
	ClientID     string
	ClientSecret string
	CallbackURL  string
}

// Identity is the logged-in person's login-provider profile. Sub is the
// stable identifier (Auth0's "subject") that should be used for binding —
// unlike Email, it never changes and is never reused across accounts.
type Identity struct {
	Sub   string
	Name  string
	Email string
}

type session struct {
	Identity
	ExpiresAt time.Time
}

const (
	sessionCookieName  = "chores_session"
	stateCookieName    = "chores_oauth_state"
	returnToCookieName = "chores_oauth_return_to"
	sessionTTL         = 7 * 24 * time.Hour
	stateTTL           = 10 * time.Minute
)

// Manager implements both modes. In ModeDisabled every handler is a no-op
// or pass-through, so callers can wire it up unconditionally.
type Manager struct {
	mode     Mode
	domain   string
	oauthCfg oauth2.Config

	mu       sync.Mutex
	sessions map[string]session
}

func NewManager(cfg Config) (*Manager, error) {
	m := &Manager{
		mode:     cfg.Mode,
		domain:   cfg.Domain,
		sessions: map[string]session{},
	}
	if cfg.Mode != ModeAuth0 {
		return m, nil
	}
	if cfg.Domain == "" || cfg.ClientID == "" || cfg.ClientSecret == "" || cfg.CallbackURL == "" {
		return nil, errors.New("auth0 mode requires a domain, client id, client secret and callback url")
	}
	m.oauthCfg = oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.CallbackURL,
		Scopes:       []string{"openid", "profile", "email"},
		Endpoint: oauth2.Endpoint{
			AuthURL:  fmt.Sprintf("https://%s/authorize", cfg.Domain),
			TokenURL: fmt.Sprintf("https://%s/oauth/token", cfg.Domain),
		},
	}
	return m, nil
}

func (m *Manager) Mode() Mode { return m.mode }

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func isSecure(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

// safeReturnTo only allows same-origin relative paths, so a crafted
// ?returnTo= can't be used to redirect a login through an external site.
func safeReturnTo(path string) string {
	if path == "" || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return "/"
	}
	return path
}

// ---- identity context -----------------------------------------------------

type ctxKey struct{}

// FromContext returns the caller's login identity, as attached by
// RequireAuth or RequirePage. It reports false in ModeDisabled, or for any
// request that didn't pass through one of those middlewares.
func FromContext(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(ctxKey{}).(Identity)
	return id, ok
}

// NewContextWithIdentity attaches id to ctx the same way RequireAuth and
// RequirePage do. Exposed for tests that need to exercise identity-aware
// server code without going through a real HTTP request.
func NewContextWithIdentity(ctx context.Context, id Identity) context.Context {
	return withIdentity(ctx, id)
}

func withIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// ---- session store -----------------------------------------------------

func (m *Manager) sessionFromRequest(r *http.Request) (session, bool) {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return session{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[c.Value]
	if !ok {
		return session{}, false
	}
	if time.Now().After(s.ExpiresAt) {
		delete(m.sessions, c.Value)
		return session{}, false
	}
	return s, true
}

func (m *Manager) createSession(w http.ResponseWriter, r *http.Request, id Identity) error {
	token, err := randomToken()
	if err != nil {
		return err
	}
	s := session{Identity: id, ExpiresAt: time.Now().Add(sessionTTL)}

	m.mu.Lock()
	m.sessions[token] = s
	for k, v := range m.sessions {
		if time.Now().After(v.ExpiresAt) {
			delete(m.sessions, k)
		}
	}
	m.mu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecure(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  s.ExpiresAt,
	})
	return nil
}

func (m *Manager) clearSession(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookieName); err == nil {
		m.mu.Lock()
		delete(m.sessions, c.Value)
		m.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecure(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// ---- login / callback / logout -----------------------------------------------------

// LoginHandler starts the Auth0 login. An optional ?returnTo=/some/path
// (same-origin only) sends the browser there after a successful login
// instead of "/" — used by the invite-accept flow to resume after login.
func (m *Manager) LoginHandler(w http.ResponseWriter, r *http.Request) {
	if m.mode != ModeAuth0 {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	state, err := randomToken()
	if err != nil {
		http.Error(w, "failed to start login", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    state,
		Path:     "/auth",
		HttpOnly: true,
		Secure:   isSecure(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(stateTTL),
	})

	returnTo := safeReturnTo(r.URL.Query().Get("returnTo"))
	if returnTo != "/" {
		http.SetCookie(w, &http.Cookie{
			Name:     returnToCookieName,
			Value:    returnTo,
			Path:     "/auth",
			HttpOnly: true,
			Secure:   isSecure(r),
			SameSite: http.SameSiteLaxMode,
			Expires:  time.Now().Add(stateTTL),
		})
	}

	http.Redirect(w, r, m.oauthCfg.AuthCodeURL(state), http.StatusFound)
}

type userInfo struct {
	Sub   string `json:"sub"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

func (m *Manager) fetchUserInfo(ctx context.Context, token *oauth2.Token) (*userInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("https://%s/userinfo", m.domain), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("userinfo request failed: %s: %s", resp.Status, body)
	}

	var info userInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decode userinfo: %w", err)
	}
	return &info, nil
}

func (m *Manager) CallbackHandler(w http.ResponseWriter, r *http.Request) {
	if m.mode != ModeAuth0 {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	stateCookie, err := r.Cookie(stateCookieName)
	if err != nil || stateCookie.Value == "" || stateCookie.Value != r.URL.Query().Get("state") {
		http.Error(w, "invalid login state, please try logging in again", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    "",
		Path:     "/auth",
		HttpOnly: true,
		Secure:   isSecure(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})

	returnTo := "/"
	if c, err := r.Cookie(returnToCookieName); err == nil {
		returnTo = safeReturnTo(c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     returnToCookieName,
		Value:    "",
		Path:     "/auth",
		HttpOnly: true,
		Secure:   isSecure(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})

	if errParam := r.URL.Query().Get("error"); errParam != "" {
		http.Error(w, "login failed: "+errParam+": "+r.URL.Query().Get("error_description"), http.StatusBadGateway)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing authorization code", http.StatusBadRequest)
		return
	}

	token, err := m.oauthCfg.Exchange(r.Context(), code)
	if err != nil {
		log.Printf("auth0 token exchange failed: %v", err)
		http.Error(w, "login failed", http.StatusBadGateway)
		return
	}

	info, err := m.fetchUserInfo(r.Context(), token)
	if err != nil {
		log.Printf("auth0 userinfo fetch failed: %v", err)
		http.Error(w, "login failed", http.StatusBadGateway)
		return
	}

	if err := m.createSession(w, r, Identity{Sub: info.Sub, Name: info.Name, Email: info.Email}); err != nil {
		log.Printf("create session failed: %v", err)
		http.Error(w, "login failed", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, returnTo, http.StatusFound)
}

func (m *Manager) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	if m.mode != ModeAuth0 {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	m.clearSession(w, r)

	scheme := "http"
	if isSecure(r) {
		scheme = "https"
	}
	returnTo := fmt.Sprintf("%s://%s/", scheme, r.Host)

	logoutURL := fmt.Sprintf("https://%s/v2/logout?client_id=%s&returnTo=%s",
		m.domain, url.QueryEscape(m.oauthCfg.ClientID), url.QueryEscape(returnTo))
	http.Redirect(w, r, logoutURL, http.StatusFound)
}

// MeHandler reports the current login state as plain JSON for the frontend
// (it deliberately isn't part of the Connect API, since it must work even
// while unauthenticated).
func (m *Manager) MeHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if m.mode != ModeAuth0 {
		json.NewEncoder(w).Encode(map[string]any{
			"mode":          string(ModeDisabled),
			"authenticated": true,
		})
		return
	}

	s, ok := m.sessionFromRequest(r)
	if !ok {
		json.NewEncoder(w).Encode(map[string]any{
			"mode":          string(ModeAuth0),
			"authenticated": false,
		})
		return
	}
	json.NewEncoder(w).Encode(map[string]any{
		"mode":          string(ModeAuth0),
		"authenticated": true,
		"name":          s.Name,
		"email":         s.Email,
	})
}

// RequireAuth protects the API: unauthenticated requests get a 401 instead
// of reaching the RPC layer. Authenticated requests carry their Identity in
// context (see FromContext). A no-op in ModeDisabled.
func (m *Manager) RequireAuth(next http.Handler) http.Handler {
	if m.mode != ModeAuth0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, ok := m.sessionFromRequest(r)
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":"unauthenticated","message":"login required"}`))
			return
		}
		next.ServeHTTP(w, r.WithContext(withIdentity(r.Context(), s.Identity)))
	})
}

// RequirePage protects a browser-navigated (non-API) route: unauthenticated
// requests are redirected through login instead of getting a bare 401, and
// sent back to the original URL afterwards. Used by the invite-accept page.
// A no-op in ModeDisabled.
func (m *Manager) RequirePage(next http.Handler) http.Handler {
	if m.mode != ModeAuth0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, ok := m.sessionFromRequest(r)
		if !ok {
			returnTo := r.URL.Path
			if r.URL.RawQuery != "" {
				returnTo += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, "/auth/login?returnTo="+url.QueryEscape(returnTo), http.StatusFound)
			return
		}
		next.ServeHTTP(w, r.WithContext(withIdentity(r.Context(), s.Identity)))
	})
}

// Gate protects the web UI's root document: an unauthenticated visit to "/"
// gets loginPage instead of the app shell. Every other path (e.g. app.js,
// app.css) is passed through untouched since those files carry no data and
// gating them would break the login page's own styling/scripts. A no-op in
// ModeDisabled.
func (m *Manager) Gate(protected, loginPage http.Handler) http.Handler {
	if m.mode != ModeAuth0 {
		return protected
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			protected.ServeHTTP(w, r)
			return
		}
		if s, ok := m.sessionFromRequest(r); ok {
			protected.ServeHTTP(w, r.WithContext(withIdentity(r.Context(), s.Identity)))
			return
		}
		loginPage.ServeHTTP(w, r)
	})
}
