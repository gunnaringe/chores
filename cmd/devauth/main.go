// Command devauth is a tiny standalone OAuth2 identity provider for local
// development and testing: it mimics Auth0's specific endpoint shape
// (/authorize, /oauth/token, /userinfo, /v2/logout) closely enough that
// cmd/chores's AUTH0_DOMAIN can just point at it instead of a real tenant.
// It logs in as one of a small set of canned, configurable identities, with
// no real login UI — the whole point is a zero-friction stand-in for local
// testing, not a realistic auth experience. By default it offers two —
// a test parent and a test child — since testing an invite flow (a parent
// inviting a child, the child logging in with their own separate identity
// to accept it) needs two distinct logins, not just one. With exactly one
// identity configured, /authorize skips straight to it with no picker at
// all, same as if there were only ever one.
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
	"fmt"
	"html"
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
	identities   []identity
}

const (
	codeTTL  = time.Minute
	tokenTTL = time.Hour
)

// identityFlag implements flag.Value for a repeatable -identity flag, each
// occurrence in "sub|name|email" form.
type identityFlag struct{ values []identity }

func (f *identityFlag) String() string {
	parts := make([]string, len(f.values))
	for i, id := range f.values {
		parts[i] = id.Sub + "|" + id.Name + "|" + id.Email
	}
	return strings.Join(parts, ",")
}

func (f *identityFlag) Set(s string) error {
	parts := strings.SplitN(s, "|", 3)
	if len(parts) != 3 {
		return fmt.Errorf("want sub|name|email, got %q", s)
	}
	f.values = append(f.values, identity{Sub: parts[0], Name: parts[1], Email: parts[2]})
	return nil
}

// codeEntry/tokenEntry pair an issued code/token with which of the
// configured identities it resolves to, plus its expiry.
type codeEntry struct {
	identity identity
	expires  time.Time
}
type tokenEntry struct {
	identity identity
	expires  time.Time
}

// store is a single in-memory map of issued codes/tokens, guarded by a
// mutex — this is a throwaway single-process test tool, no persistence
// needed.
type store struct {
	mu     sync.Mutex
	codes  map[string]codeEntry
	tokens map[string]tokenEntry
}

func newStore() *store {
	return &store{codes: map[string]codeEntry{}, tokens: map[string]tokenEntry{}}
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
	for k, e := range s.codes {
		if now.After(e.expires) {
			delete(s.codes, k)
		}
	}
	for k, e := range s.tokens {
		if now.After(e.expires) {
			delete(s.tokens, k)
		}
	}
}

func (s *store) issueCode(id identity) (string, error) {
	code, err := randomToken()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	s.codes[code] = codeEntry{identity: id, expires: time.Now().Add(codeTTL)}
	return code, nil
}

// consumeCode returns the identity a valid, unexpired code was issued for,
// deleting it either way (single-use).
func (s *store) consumeCode(code string) (identity, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.codes[code]
	delete(s.codes, code)
	if !ok || time.Now().After(e.expires) {
		return identity{}, false
	}
	return e.identity, true
}

func (s *store) issueToken(id identity) (string, error) {
	tok, err := randomToken()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	s.tokens[tok] = tokenEntry{identity: id, expires: time.Now().Add(tokenTTL)}
	return tok, nil
}

func (s *store) lookupToken(tok string) (identity, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.tokens[tok]
	if !ok || time.Now().After(e.expires) {
		return identity{}, false
	}
	return e.identity, true
}

// handleAuthorize is GET /authorize. client_id is checked (a mismatch is a
// loud plain-text error, since a misconfigured client shouldn't silently
// appear to work). With exactly one identity configured, the browser is
// redirected straight back to redirect_uri with a fresh code, as if a real
// user instantly logged in and approved the request — no picker at all.
// With more than one, an explicit choice is needed: either an "identity="
// param already naming one (so a picker link, or a scripted test, can
// resolve directly), or — if neither — a minimal page listing them as
// plain links, each just resubmitting this same request with "identity="
// added.
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

		chosen, ok := resolveIdentity(cfg.identities, q.Get("identity"))
		if !ok {
			http.Error(w, "devauth: unknown identity", http.StatusBadRequest)
			return
		}
		if chosen == nil {
			renderIdentityPicker(w, cfg.identities, r.URL)
			return
		}

		dest, err := url.Parse(redirectURI)
		if err != nil {
			http.Error(w, "devauth: invalid redirect_uri", http.StatusBadRequest)
			return
		}
		code, err := st.issueCode(*chosen)
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

// resolveIdentity picks which configured identity /authorize should use:
// requestedSub (if non-empty) must name one exactly (ok=false if it
// doesn't); otherwise a single configured identity is used automatically,
// or nil is returned (still ok) to signal "show the picker" when there's
// more than one and none was named.
func resolveIdentity(identities []identity, requestedSub string) (*identity, bool) {
	if requestedSub != "" {
		for i := range identities {
			if identities[i].Sub == requestedSub {
				return &identities[i], true
			}
		}
		return nil, false
	}
	if len(identities) == 1 {
		return &identities[0], true
	}
	return nil, true
}

func renderIdentityPicker(w http.ResponseWriter, identities []identity, original *url.URL) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `<!doctype html><meta charset="utf-8"><title>devauth</title>`+
		`<body style="font-family:sans-serif;max-width:420px;margin:80px auto;line-height:1.6;">`+
		`<h1>devauth</h1><p>Choose which test identity to log in as:</p><ul>`)
	for _, id := range identities {
		q := original.Query()
		q.Set("identity", id.Sub)
		link := *original
		link.RawQuery = q.Encode()
		fmt.Fprintf(w, `<li><a href="%s">%s</a> — %s</li>`,
			html.EscapeString(link.String()), html.EscapeString(id.Name), html.EscapeString(id.Email))
	}
	fmt.Fprint(w, `</ul></body>`)
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
		id, ok := st.consumeCode(r.FormValue("code"))
		if !ok {
			writeJSONError(w, http.StatusBadRequest, "invalid_grant")
			return
		}
		token, err := st.issueToken(id)
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

// handleUserinfo is GET /userinfo — resolves the bearer token back to
// whichever identity was chosen at /authorize time.
func handleUserinfo(st *store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		token, ok := strings.CutPrefix(auth, "Bearer ")
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "invalid_token")
			return
		}
		id, ok := st.lookupToken(token)
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "invalid_token")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"sub":   id.Sub,
			"name":  id.Name,
			"email": id.Email,
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
	var identities identityFlag
	flag.Var(&identities, "identity", `a canned test identity as "sub|name|email" (repeatable; defaults to a test parent + a test child if none given, so both roles can log in)`)
	flag.Parse()

	if *clientID == "" || *clientSecret == "" {
		log.Fatal("devauth: -client-id and -client-secret are required")
	}
	if len(identities.values) == 0 {
		identities.values = []identity{
			{Sub: "devauth|local-parent", Name: "Test Parent", Email: "parent@example.com"},
			{Sub: "devauth|local-child", Name: "Test Child", Email: "child@example.com"},
		}
	}

	cfg := config{
		clientID:     *clientID,
		clientSecret: *clientSecret,
		identities:   identities.values,
	}
	st := newStore()

	mux := http.NewServeMux()
	mux.HandleFunc("/authorize", handleAuthorize(cfg, st))
	mux.HandleFunc("/oauth/token", handleToken(cfg, st))
	mux.HandleFunc("/userinfo", handleUserinfo(st))
	mux.HandleFunc("/v2/logout", handleLogout())

	log.Printf("devauth: listening on %s", *addr)
	for _, id := range cfg.identities {
		log.Printf("devauth: identity available: sub=%s name=%q email=%s", id.Sub, id.Name, id.Email)
	}
	if len(cfg.identities) > 1 {
		log.Printf("devauth: more than one identity configured — /authorize will show a picker each login")
	}
	log.Printf("devauth: point chores at this with:")
	log.Printf("  AUTH0_DOMAIN=http://localhost%s AUTH0_CLIENT_ID=%s AUTH0_CLIENT_SECRET=%s AUTH0_CALLBACK_URL=http://localhost:8080/auth/callback", *addr, cfg.clientID, cfg.clientSecret)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatalf("devauth: server error: %v", err)
	}
}
