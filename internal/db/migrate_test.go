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

	// auth_subject is intentionally not unique: the same login can be bound
	// to more than one user row (e.g. a child who's a member of two
	// families), so a second row with the same auth_subject must succeed.
	if _, err := conn.Exec(
		`INSERT INTO families (id, name, created_at) VALUES ('fam2', 'Other Family', '2024-01-01T00:00:00Z')`,
	); err != nil {
		t.Fatalf("seed second family: %v", err)
	}
	if _, err := conn.Exec(
		`INSERT INTO users (id, family_id, name, role, created_at, auth_subject) VALUES ('u2', 'fam1', 'Dad', 'parent', '2024-01-01T00:00:00Z', 'sub-1')`,
	); err != nil {
		t.Fatalf("insert with auth_subject: %v", err)
	}
	if _, err := conn.Exec(
		`INSERT INTO users (id, family_id, name, role, created_at, auth_subject) VALUES ('u3', 'fam2', 'Other', 'child', '2024-01-01T00:00:00Z', 'sub-1')`,
	); err != nil {
		t.Fatalf("expected a second row with the same auth_subject to be allowed, got: %v", err)
	}
}

// oldTasksSchema mirrors the tasks table as it existed with a single bare
// "icon" column, before it was split into icon_type/icon_value to support
// both emoji and Font Awesome icons.
const oldTasksSchema = `
CREATE TABLE families (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE TABLE tasks (
    id TEXT PRIMARY KEY,
    family_id TEXT NOT NULL REFERENCES families(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    price_cents INTEGER NOT NULL,
    schedule TEXT NOT NULL,
    active INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    icon TEXT NOT NULL DEFAULT ''
);
`

func TestOpen_MigratesTaskIconColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old-icon.db")

	seed, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open seed db: %v", err)
	}
	if _, err := seed.Exec(oldTasksSchema); err != nil {
		t.Fatalf("apply old schema: %v", err)
	}
	if _, err := seed.Exec(
		`INSERT INTO families (id, name, created_at) VALUES ('fam1', 'Old Family', '2024-01-01T00:00:00Z')`,
	); err != nil {
		t.Fatalf("seed family: %v", err)
	}
	if _, err := seed.Exec(
		`INSERT INTO tasks (id, family_id, title, price_cents, schedule, created_at, icon)
		 VALUES ('t1', 'fam1', 'Dishes', 100, '0 0 * * *', '2024-01-01T00:00:00Z', '🧹')`,
	); err != nil {
		t.Fatalf("seed task with icon: %v", err)
	}
	if _, err := seed.Exec(
		`INSERT INTO tasks (id, family_id, title, price_cents, schedule, created_at, icon)
		 VALUES ('t2', 'fam1', 'No icon task', 100, '0 0 * * *', '2024-01-01T00:00:00Z', '')`,
	); err != nil {
		t.Fatalf("seed task without icon: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("close seed db: %v", err)
	}

	conn, err := Open(path)
	if err != nil {
		t.Fatalf("Open on pre-existing database: %v", err)
	}
	defer conn.Close()

	var iconType, iconValue string
	if err := conn.QueryRow(`SELECT icon_type, icon_value FROM tasks WHERE id = 't1'`).Scan(&iconType, &iconValue); err != nil {
		t.Fatalf("query migrated task: %v", err)
	}
	if iconType != "emoji" || iconValue != "🧹" {
		t.Fatalf("expected the old icon to migrate to (emoji, 🧹), got (%q, %q)", iconType, iconValue)
	}

	if err := conn.QueryRow(`SELECT icon_type, icon_value FROM tasks WHERE id = 't2'`).Scan(&iconType, &iconValue); err != nil {
		t.Fatalf("query migrated task without icon: %v", err)
	}
	if iconType != "" || iconValue != "" {
		t.Fatalf("expected a task with no icon to stay empty, got (%q, %q)", iconType, iconValue)
	}
}

// TestOpen_MigratesTaskRepeatColumns verifies that a task created before
// repeat_mode/days_of_week/repeat_interval_weeks/start_date existed keeps
// working exactly as before: it lands in 'cron' mode using its existing
// schedule string, rather than losing its schedule or needing reinterpretation.
func TestOpen_MigratesTaskRepeatColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old-repeat.db")

	seed, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open seed db: %v", err)
	}
	if _, err := seed.Exec(oldTasksSchema); err != nil {
		t.Fatalf("apply old schema: %v", err)
	}
	if _, err := seed.Exec(
		`INSERT INTO families (id, name, created_at) VALUES ('fam1', 'Old Family', '2024-01-01T00:00:00Z')`,
	); err != nil {
		t.Fatalf("seed family: %v", err)
	}
	if _, err := seed.Exec(
		`INSERT INTO tasks (id, family_id, title, price_cents, schedule, created_at, icon)
		 VALUES ('t1', 'fam1', 'Dishes', 100, '0 0 * * 1,3,5', '2024-01-01T00:00:00Z', '')`,
	); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("close seed db: %v", err)
	}

	conn, err := Open(path)
	if err != nil {
		t.Fatalf("Open on pre-existing database: %v", err)
	}
	defer conn.Close()

	var repeatMode, schedule, daysOfWeek, startDate string
	var intervalWeeks int
	if err := conn.QueryRow(
		`SELECT repeat_mode, schedule, days_of_week, repeat_interval_weeks, start_date FROM tasks WHERE id = 't1'`,
	).Scan(&repeatMode, &schedule, &daysOfWeek, &intervalWeeks, &startDate); err != nil {
		t.Fatalf("query migrated task: %v", err)
	}
	if repeatMode != "cron" {
		t.Fatalf("expected a pre-existing task to migrate to repeat_mode 'cron', got %q", repeatMode)
	}
	if schedule != "0 0 * * 1,3,5" {
		t.Fatalf("expected the original schedule to survive migration untouched, got %q", schedule)
	}
	if daysOfWeek != "" || startDate != "" {
		t.Fatalf("expected days_of_week and start_date to stay empty for a cron task, got (%q, %q)", daysOfWeek, startDate)
	}
	if intervalWeeks != 1 {
		t.Fatalf("expected repeat_interval_weeks to default to 1, got %d", intervalWeeks)
	}
}

// TestOpen_MigratesTaskClassificationColumn verifies that a task created
// before the classification column existed defaults to 'mandatory', keeping
// its prior behavior (there was no "optional" concept before this column).
func TestOpen_MigratesTaskClassificationColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old-classification.db")

	seed, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open seed db: %v", err)
	}
	if _, err := seed.Exec(oldTasksSchema); err != nil {
		t.Fatalf("apply old schema: %v", err)
	}
	if _, err := seed.Exec(
		`INSERT INTO families (id, name, created_at) VALUES ('fam1', 'Old Family', '2024-01-01T00:00:00Z')`,
	); err != nil {
		t.Fatalf("seed family: %v", err)
	}
	if _, err := seed.Exec(
		`INSERT INTO tasks (id, family_id, title, price_cents, schedule, created_at)
		 VALUES ('t1', 'fam1', 'Dishes', 100, '0 0 * * *', '2024-01-01T00:00:00Z')`,
	); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("close seed db: %v", err)
	}

	conn, err := Open(path)
	if err != nil {
		t.Fatalf("Open on pre-existing database: %v", err)
	}
	defer conn.Close()

	var classification string
	if err := conn.QueryRow(`SELECT classification FROM tasks WHERE id = 't1'`).Scan(&classification); err != nil {
		t.Fatalf("query migrated task: %v", err)
	}
	if classification != "mandatory" {
		t.Fatalf("expected a pre-existing task to migrate to classification 'mandatory', got %q", classification)
	}
}

func TestOpen_MigratesDashboardKeyColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old-dashboard.db")

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
	if err := seed.Close(); err != nil {
		t.Fatalf("close seed db: %v", err)
	}

	conn, err := Open(path)
	if err != nil {
		t.Fatalf("Open on pre-existing database: %v", err)
	}
	defer conn.Close()

	var key sql.NullString
	if err := conn.QueryRow(`SELECT dashboard_key FROM families WHERE id = 'fam1'`).Scan(&key); err != nil {
		t.Fatalf("query migrated family: %v", err)
	}
	if key.Valid {
		t.Fatalf("expected dashboard_key to default to NULL, got %q", key.String)
	}

	if _, err := conn.Exec(
		`INSERT INTO families (id, name, created_at, dashboard_key) VALUES ('fam2', 'Family Two', '2024-01-01T00:00:00Z', 'secret-key')`,
	); err != nil {
		t.Fatalf("insert family with a dashboard_key: %v", err)
	}
	if _, err := conn.Exec(
		`INSERT INTO families (id, name, created_at, dashboard_key) VALUES ('fam3', 'Family Three', '2024-01-01T00:00:00Z', 'secret-key')`,
	); err == nil {
		t.Fatal("expected a duplicate dashboard_key across families to be rejected")
	}
}
