package db

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// oldUsersSchema mirrors the users table as it existed before auth_subject
// and email were added, so we can verify Open() upgrades a database created
// by an older build of the app without losing existing rows.
const oldUsersSchema = `
CREATE TABLE families (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE TABLE users (
    id TEXT PRIMARY KEY,
    family_id TEXT NOT NULL REFERENCES families(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('parent', 'child')),
    created_at TEXT NOT NULL
);
`

func TestOpen_MigratesPreExistingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")

	seed, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open seed db: %v", err)
	}
	if _, err := seed.Exec(oldUsersSchema); err != nil {
		t.Fatalf("apply old schema: %v", err)
	}
	if _, err := seed.Exec(
		`INSERT INTO families (id, name, created_at) VALUES ('fam1', 'Old Family', '2024-01-01T00:00:00Z')`,
	); err != nil {
		t.Fatalf("seed family: %v", err)
	}
	if _, err := seed.Exec(
		`INSERT INTO users (id, family_id, name, role, created_at) VALUES ('u1', 'fam1', 'Mom', 'parent', '2024-01-01T00:00:00Z')`,
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("close seed db: %v", err)
	}

	conn, err := Open(path)
	if err != nil {
		t.Fatalf("Open on pre-existing database: %v", err)
	}
	defer conn.Close()

	var name string
	var email string
	var authSubject sql.NullString
	if err := conn.QueryRow(`SELECT name, email, auth_subject FROM users WHERE id = 'u1'`).Scan(&name, &email, &authSubject); err != nil {
		t.Fatalf("query migrated row: %v", err)
	}
	if name != "Mom" {
		t.Fatalf("expected existing row to survive migration with name %q, got %q", "Mom", name)
	}
	if email != "" {
		t.Fatalf("expected email to default to empty string, got %q", email)
	}
	if authSubject.Valid {
		t.Fatalf("expected auth_subject to be NULL for a pre-existing row, got %q", authSubject.String)
	}

	// The unique index must actually be enforced post-migration.
	if _, err := conn.Exec(
		`INSERT INTO users (id, family_id, name, role, created_at, auth_subject) VALUES ('u2', 'fam1', 'Dad', 'parent', '2024-01-01T00:00:00Z', 'sub-1')`,
	); err != nil {
		t.Fatalf("insert with auth_subject: %v", err)
	}
	if _, err := conn.Exec(
		`INSERT INTO users (id, family_id, name, role, created_at, auth_subject) VALUES ('u3', 'fam1', 'Other', 'parent', '2024-01-01T00:00:00Z', 'sub-1')`,
	); err == nil {
		t.Fatal("expected a duplicate auth_subject insert to fail the unique index")
	}
}
