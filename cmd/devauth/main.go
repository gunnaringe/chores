// Command devauth is a tiny standalone OAuth2 identity provider for local
// development and testing: it mimics Auth0's specific endpoint shape
// (/authorize, /oauth/token, /userinfo, /v2/logout) closely enough that
// cmd/chores's AUTH0_DOMAIN can just point at it instead of a real tenant.
// It always logs in as one canned, configurable identity, with no login UI
// at all — the whole point is a zero-friction stand-in for local testing,
// not a realistic auth experience.
//
// Run it alongside chores, e.g.:
//
//	go run ./cmd/devauth -client-id=devclient -client-secret=devsecret
//	AUTH0_DOMAIN=http://localhost:9999 AUTH0_CLIENT_ID=devclient \
//	  AUTH0_CLIENT_SECRET=devsecret AUTH0_CALLBACK_URL=http://localhost:8080/auth/callback \
//	  go run ./cmd/chores
package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type identity struct {
	Sub   string
	Name  string
	Email string
}

type config struct {
	clientID     string
	clientSecret string
	identity     identity
}

const (
	codeTTL  = time.Minute
	tokenTTL = time.Hour
)

// store is a single in-memory map of issued codes/tokens to their expiry,
// guarded by a mutex — this is a throwaway single-process test tool, no
// persistence needed. There's only ever one canned identity, so a code or
// token existing (and unexpired) is all that matters; nothing needs to map
// to a specific identity value.
type store struct {
	mu     sync.Mutex
	codes  map[string]time.Time
	tokens map[string]time.Time
}

func newStore() *store {
	return &store{codes: map[string]time.Time{}, tokens: map[string]time.Time{}}
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// sweepLocked drops expired entries from both maps. Callers must hold mu.
func (s *store) sweepLocked() {
	now := time.Now()
	for k, exp := range s.codes {
		if now.After(exp) {
			delete(s.codes, k)
		}
	}
	for k, exp := range s.tokens {
		if now.After(exp) {
			delete(s.tokens, k)
		}
	}
}

func (s *store) issueCode() (string, error) {
	code, err := randomToken()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	s.codes[code] = time.Now().Add(codeTTL)
	return code, nil
}

// consumeCode reports whether code was valid and unexpired, deleting it
// either way (single-use).
func (s *store) consumeCode(code string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.codes[code]
	delete(s.codes, code)
	return ok && time.Now().Before(exp)
}

func (s *store) issueToken() (string, error) {
	tok, err := randomToken()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	s.tokens[tok] = time.Now().Add(tokenTTL)
	return tok, nil
}

func (s *store) validToken(tok string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.tokens[tok]
	return ok && time.Now().Before(exp)
}

// handleAuthorize is GET /authorize. There's no login UI — client_id is
// checked (a mismatch is a loud plain-text error, since a misconfigured
// client shouldn't silently appear to work) and the browser is redirected
// straight back to redirect_uri with a fresh code, as if a real user
// instantly logged in and approved the request.
func handleAuthorize(cfg config, st *store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("client_id") != cfg.clientID {
			http.Error(w, "devauth: unknown client_id", http.StatusBadRequest)
			return
		}
		redirectURI := q.Get("redirect_uri")
		if redirectURI == "" {
			http.Error(w, "devauth: missing redirect_uri", http.StatusBadRequest)
			return
		}
		if rt := q.Get("response_type"); rt != "" && rt != "code" {
			http.Error(w, "devauth: only response_type=code is supported", http.StatusBadRequest)
			return
		}
		dest, err := url.Parse(redirectURI)
		if err != nil {
			http.Error(w, "devauth: invalid redirect_uri", http.StatusBadRequest)
			return
		}
		code, err := st.issueCode()
		if err != nil {
			http.Error(w, "devauth: failed to issue code", http.StatusInternalServerError)
			return
		}
		dq := dest.Query()
		dq.Set("code", code)
		if state := q.Get("state"); state != "" {
			dq.Set("state", state)
		}
		dest.RawQuery = dq.Encode()
		http.Redirect(w, r, dest.String(), http.StatusFound)
	}
}

// handleToken is POST /oauth/token. golang.org/x/oauth2's Exchange, as used
// unmodified by cmd/chores's auth.Manager, tries HTTP Basic client auth
// first and only falls back to form-body client_id/client_secret on error
// — so both must be accepted here for the very first request to succeed.
func handleToken(cfg config, st *store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "devauth: method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "devauth: invalid form body", http.StatusBadRequest)
			return
		}
		clientID, clientSecret, ok := r.BasicAuth()
		if !ok {
			clientID = r.FormValue("client_id")
			clientSecret = r.FormValue("client_secret")
		}
		if clientID != cfg.clientID || clientSecret != cfg.clientSecret {
			writeJSONError(w, http.StatusUnauthorized, "invalid_client")
			return
		}
		if r.FormValue("grant_type") != "authorization_code" {
			writeJSONError(w, http.StatusBadRequest, "unsupported_grant_type")
			return
		}
		if !st.consumeCode(r.FormValue("code")) {
			writeJSONError(w, http.StatusBadRequest, "invalid_grant")
			return
		}
		token, err := st.issueToken()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "server_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"access_token": token,
			"token_type":   "Bearer",
			"expires_in":   int(tokenTTL.Seconds()),
		})
	}
}

// handleUserinfo is GET /userinfo — the one canned identity for any
// unexpired bearer token.
func handleUserinfo(cfg config, st *store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		token, ok := strings.CutPrefix(auth, "Bearer ")
		if !ok || !st.validToken(token) {
			writeJSONError(w, http.StatusUnauthorized, "invalid_token")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"sub":   cfg.identity.Sub,
			"name":  cfg.identity.Name,
			"email": cfg.identity.Email,
		})
	}
}

// handleLogout is GET /v2/logout — devauth has no real session to clear,
// so this just redirects straight back, mirroring Auth0's own contract.
func handleLogout() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		returnTo := r.URL.Query().Get("returnTo")
		if returnTo == "" {
			returnTo = "/"
		}
		http.Redirect(w, r, returnTo, http.StatusFound)
	}
}

func writeJSON(w http.ResponseWriter, status int, body map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]any{"error": code})
}

func main() {
	addr := flag.String("addr", ":9999", "address to listen on")
	clientID := flag.String("client-id", "", "client id chores must be configured with (required)")
	clientSecret := flag.String("client-secret", "", "client secret chores must be configured with (required)")
	sub := flag.String("sub", "devauth|local-test", "canned identity's stable subject id")
	name := flag.String("name", "Test Parent", "canned identity's display name")
	email := flag.String("email", "test@example.com", "canned identity's email")
	flag.Parse()

	if *clientID == "" || *clientSecret == "" {
		log.Fatal("devauth: -client-id and -client-secret are required")
	}

	cfg := config{
		clientID:     *clientID,
		clientSecret: *clientSecret,
		identity:     identity{Sub: *sub, Name: *name, Email: *email},
	}
	st := newStore()

	mux := http.NewServeMux()
	mux.HandleFunc("/authorize", handleAuthorize(cfg, st))
	mux.HandleFunc("/oauth/token", handleToken(cfg, st))
	mux.HandleFunc("/userinfo", handleUserinfo(cfg, st))
	mux.HandleFunc("/v2/logout", handleLogout())

	log.Printf("devauth: listening on %s", *addr)
	log.Printf("devauth: client_id=%s canned identity: sub=%s name=%q email=%s", cfg.clientID, cfg.identity.Sub, cfg.identity.Name, cfg.identity.Email)
	log.Printf("devauth: point chores at this with:")
	log.Printf("  AUTH0_DOMAIN=http://localhost%s AUTH0_CLIENT_ID=%s AUTH0_CLIENT_SECRET=%s AUTH0_CALLBACK_URL=http://localhost:8080/auth/callback", *addr, cfg.clientID, cfg.clientSecret)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatalf("devauth: server error: %v", err)
	}
}
