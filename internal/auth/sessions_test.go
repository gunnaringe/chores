package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gunnaringe/chores/internal/db"
)

func newSQLStore(t *testing.T) *SQLSessionStore {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return NewSQLSessionStore(conn, nil)
}

func newManagerWithStore(t *testing.T, store SessionStore) *Manager {
	t.Helper()
	cfg := validConfig()
	cfg.Sessions = store
	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

func login(t *testing.T, m *Manager, id Identity) *http.Cookie {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := m.createSession(rec, httptest.NewRequest(http.MethodGet, "/", nil), id); err != nil {
		t.Fatalf("createSession: %v", err)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			return c
		}
	}
	t.Fatal("no session cookie set")
	return nil
}

// The point of the SQL store: a new Manager — what a restarted process
// builds — still recognises a session cookie issued before the restart.
func TestSQLSessionStore_SurvivesRestart(t *testing.T) {
	store := newSQLStore(t)
	cookie := login(t, newManagerWithStore(t, store), Identity{Sub: "auth0|abc", Name: "Ann", Email: "ann@example.com"})

	restarted := newManagerWithStore(t, store)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(cookie)
	s, ok := restarted.sessionFromRequest(req)
	if !ok {
		t.Fatal("session lost across restart")
	}
	if s.Sub != "auth0|abc" || s.Name != "Ann" || s.Email != "ann@example.com" {
		t.Fatalf("identity not round-tripped: %+v", s.Identity)
	}

	// Logout removes it for good.
	restarted.clearSession(httptest.NewRecorder(), req)
	if _, ok := restarted.sessionFromRequest(req); ok {
		t.Fatal("session still valid after logout")
	}
}

func TestSQLSessionStore_StoresOnlyAHash(t *testing.T) {
	store := newSQLStore(t)
	cookie := login(t, newManagerWithStore(t, store), Identity{Sub: "auth0|abc"})

	var n int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE token_hash = ?`, cookie.Value).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("raw session token stored in the database")
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE token_hash = ?`, hashSessionToken(cookie.Value)).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected one hashed session row, got %d", n)
	}
}

func TestSQLSessionStore_ExpiredSessionsRejectedAndSwept(t *testing.T) {
	store := newSQLStore(t)
	ctx := context.Background()
	if err := store.Put(ctx, "old", session{Identity: Identity{Sub: "auth0|old"}, ExpiresAt: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	m := newManagerWithStore(t, store)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "old"})
	if _, ok := m.sessionFromRequest(req); ok {
		t.Fatal("expired session accepted")
	}

	login(t, m, Identity{Sub: "auth0|new"}) // a login sweeps expired rows
	if _, ok, _ := store.Get(ctx, "old"); ok {
		t.Fatal("expired session not swept on the next login")
	}
}
