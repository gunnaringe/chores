// Package server implements the ChoresService Connect API on top of SQLite.
package server

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/gunnaringe/chores/gen/chores/v1/choresv1connect"
)

type Server struct {
	choresv1connect.UnimplementedChoresServiceHandler
	// db is the single write connection (see db.Open); every write and
	// every transaction goes through it. ro is the read-only pool from
	// db.OpenReader for plain reads outside a transaction, so a list RPC
	// never waits for someone else's write to finish.
	db *sql.DB
	ro *sql.DB

	// VAPID keypair used to sign Web Push messages. Empty when key setup
	// failed (see ensureVAPIDKeys), in which case push notifications are
	// silently unavailable rather than fatal to the rest of the app.
	vapidPublicKey  string
	vapidPrivateKey string

	// pushClient delivers Web Push messages; see pushsafety.go for why it
	// is not http.DefaultClient.
	pushClient *http.Client
}

// New builds the service on a write connection and a read pool. reader may
// be nil, in which case reads share the write connection.
func New(db, reader *sql.DB) *Server {
	if reader == nil {
		reader = db
	}
	s := &Server{db: db, ro: reader, pushClient: newPushHTTPClient()}
	if err := s.ensureVAPIDKeys(); err != nil {
		log.Printf("push notifications disabled: %v", err)
	}
	return s
}

// querier is the subset of *sql.DB that *sql.Tx also satisfies, so a helper
// can run either standalone or as part of a transaction.
//
// This matters more than it looks: the connection pool is capped at one
// connection (see db.Open), so a transaction holds the *only* connection
// for its lifetime. A helper that reached for s.db while a transaction was
// open would block until the request's context expired rather than fail,
// and one that read through s.ro would not see the transaction's own
// uncommitted writes — which is why anything callable from inside a
// transaction takes a querier instead.
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// inTx runs fn inside a transaction, rolling back on error or panic.
//
// Authorization and validation are deliberately done by the caller, before
// this is entered: they only read, they don't need to be atomic with the
// write, and keeping them out keeps the transaction — and therefore the
// hold on the single connection — as short as possible.
func (s *Server) inTx(ctx context.Context, fn func(q querier) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func newID() string {
	return uuid.NewString()
}

// Timestamps are stored as fixed-width RFC3339 UTC strings so that
// lexicographic ordering matches chronological ordering and comparisons
// (e.g. "completed_at >= ?") work directly in SQL.

// nowUTC is the server's only clock. A variable so tests can move it (see
// TestMain in server_test.go) — anything here that needs "now" goes through
// it rather than calling time.Now() directly.
var nowUTC = func() time.Time {
	return time.Now().UTC()
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func parseTime(s string) (time.Time, error) {
	return time.Parse(time.RFC3339, s)
}

func timestampPB(t time.Time) *timestamppb.Timestamp {
	return timestamppb.New(t)
}
