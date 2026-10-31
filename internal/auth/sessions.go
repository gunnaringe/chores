package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// SessionStore keeps logged-in sessions, keyed by the random token in the
// session cookie.
type SessionStore interface {
	Get(ctx context.Context, token string) (session, bool, error)
	Put(ctx context.Context, token string, s session) error
	Delete(ctx context.Context, token string) error
}

// memorySessionStore is what a Manager gets when Config.Sessions is nil.
// It forgets every session on restart, so it's only meant for tests and
// throwaway runs; cmd/chores always passes a SQLSessionStore.
type memorySessionStore struct {
	mu       sync.Mutex
	sessions map[string]session
}

func newMemorySessionStore() *memorySessionStore {
	return &memorySessionStore{sessions: map[string]session{}}
}

func (m *memorySessionStore) Get(_ context.Context, token string) (session, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[token]
	return s, ok, nil
}

func (m *memorySessionStore) Put(_ context.Context, token string, s session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for k, v := range m.sessions {
		if now.After(v.ExpiresAt) {
			delete(m.sessions, k)
		}
	}
	m.sessions[token] = s
	return nil
}

func (m *memorySessionStore) Delete(_ context.Context, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, token)
	return nil
}

// SQLSessionStore keeps sessions in the app's own database, so a deploy or
// restart no longer logs every family out. Only a SHA-256 of the cookie
// token is stored, the same as personal access tokens: a copy of the
// database (a backup, say) can't be replayed as a live session.
type SQLSessionStore struct {
	db *sql.DB // writer
	ro *sql.DB // reads; may be the same pool
}

// NewSQLSessionStore stores sessions in the sessions table that db.Open
// creates. reader may be nil to read through db.
func NewSQLSessionStore(db, reader *sql.DB) *SQLSessionStore {
	if reader == nil {
		reader = db
	}
	return &SQLSessionStore{db: db, ro: reader}
}

func hashSessionToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Fixed-width UTC, so that expires_at compares correctly as text in SQL.
const sessionTimeLayout = "2006-01-02T15:04:05Z"

func (s *SQLSessionStore) Get(ctx context.Context, token string) (session, bool, error) {
	var sess session
	var expires string
	err := s.ro.QueryRowContext(ctx,
		`SELECT auth_subject, identity_name, identity_email, expires_at FROM sessions WHERE token_hash = ?`,
		hashSessionToken(token),
	).Scan(&sess.Sub, &sess.Name, &sess.Email, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return session{}, false, nil
	}
	if err != nil {
		return session{}, false, err
	}
	if sess.ExpiresAt, err = time.Parse(sessionTimeLayout, expires); err != nil {
		return session{}, false, fmt.Errorf("parse session expiry: %w", err)
	}
	return sess, true, nil
}

func (s *SQLSessionStore) Put(ctx context.Context, token string, sess session) error {
	now := time.Now().UTC()
	// Expired rows are swept on login rather than by a background job:
	// logins are rare enough that this costs nothing, and frequent enough
	// that the table never grows past one session-TTL's worth of logins.
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, now.Format(sessionTimeLayout)); err != nil {
		return fmt.Errorf("sweep expired sessions: %w", err)
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, auth_subject, identity_name, identity_email, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		hashSessionToken(token), sess.Sub, sess.Name, sess.Email,
		now.Format(sessionTimeLayout), sess.ExpiresAt.UTC().Format(sessionTimeLayout),
	)
	return err
}

func (s *SQLSessionStore) Delete(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hashSessionToken(token))
	return err
}
