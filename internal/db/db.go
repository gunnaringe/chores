// Package db opens the SQLite database and applies the schema.
package db

import (
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

func Open(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)", path)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	conn.SetMaxOpenConns(1)

	if _, err := conn.Exec(schema); err != nil {
		conn.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	if err := migrate(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrate schema: %w", err)
	}
	return conn, nil
}

// migrate adds columns/indexes introduced after a table's initial
// CREATE TABLE IF NOT EXISTS, so databases created by older builds of the
// app pick them up too. SQLite has no "ADD COLUMN IF NOT EXISTS", so each
// addition is guarded by checking PRAGMA table_info first.
func migrate(db *sql.DB) error {
	familyCols, err := columnSet(db, "families")
	if err != nil {
		return err
	}
	if !familyCols["dashboard_key"] {
		if _, err := db.Exec(`ALTER TABLE families ADD COLUMN dashboard_key TEXT`); err != nil {
			return fmt.Errorf("add families.dashboard_key: %w", err)
		}
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_families_dashboard_key ON families(dashboard_key)`); err != nil {
		return fmt.Errorf("create idx_families_dashboard_key: %w", err)
	}

	userCols, err := columnSet(db, "users")
	if err != nil {
		return err
	}
	if !userCols["auth_subject"] {
		if _, err := db.Exec(`ALTER TABLE users ADD COLUMN auth_subject TEXT`); err != nil {
			return fmt.Errorf("add users.auth_subject: %w", err)
		}
	}
	if !userCols["email"] {
		if _, err := db.Exec(`ALTER TABLE users ADD COLUMN email TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add users.email: %w", err)
		}
	}
	if err := relaxAuthSubjectUniqueness(db); err != nil {
		return fmt.Errorf("relax auth_subject uniqueness: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_users_auth_subject ON users(auth_subject)`); err != nil {
		return fmt.Errorf("create idx_users_auth_subject: %w", err)
	}

	taskCols, err := columnSet(db, "tasks")
	if err != nil {
		return err
	}
	if !taskCols["icon_type"] {
		if _, err := db.Exec(`ALTER TABLE tasks ADD COLUMN icon_type TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add tasks.icon_type: %w", err)
		}
	}
	if !taskCols["icon_value"] {
		if _, err := db.Exec(`ALTER TABLE tasks ADD COLUMN icon_value TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add tasks.icon_value: %w", err)
		}
	}
	// Superseded by icon_type/icon_value (which distinguish emoji from
	// Font Awesome icons), but a database from that brief window has an
	// "icon" column holding a bare emoji string. Bring it forward once;
	// harmless to leave the old column in place afterwards.
	if taskCols["icon"] {
		if _, err := db.Exec(`
			UPDATE tasks SET icon_type = 'emoji', icon_value = icon
			WHERE icon_type = '' AND icon != ''
		`); err != nil {
			return fmt.Errorf("backfill tasks.icon_type/icon_value: %w", err)
		}
	}

	if !taskCols["repeat_mode"] {
		// Existing tasks keep working exactly as before: 'cron' mode
		// interprets the existing `schedule` column the same way IsDue/
		// DatesBetween always have, with no data loss or reinterpretation.
		if _, err := db.Exec(`ALTER TABLE tasks ADD COLUMN repeat_mode TEXT NOT NULL DEFAULT 'cron'`); err != nil {
			return fmt.Errorf("add tasks.repeat_mode: %w", err)
		}
	}
	if !taskCols["days_of_week"] {
		if _, err := db.Exec(`ALTER TABLE tasks ADD COLUMN days_of_week TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add tasks.days_of_week: %w", err)
		}
	}
	if !taskCols["repeat_interval_weeks"] {
		if _, err := db.Exec(`ALTER TABLE tasks ADD COLUMN repeat_interval_weeks INTEGER NOT NULL DEFAULT 1`); err != nil {
			return fmt.Errorf("add tasks.repeat_interval_weeks: %w", err)
		}
	}
	if !taskCols["start_date"] {
		if _, err := db.Exec(`ALTER TABLE tasks ADD COLUMN start_date TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add tasks.start_date: %w", err)
		}
	}
	if !taskCols["classification"] {
		// Existing tasks default to 'mandatory', matching how they've always
		// been treated (there was no "optional" concept before this column).
		if _, err := db.Exec(`ALTER TABLE tasks ADD COLUMN classification TEXT NOT NULL DEFAULT 'mandatory'`); err != nil {
			return fmt.Errorf("add tasks.classification: %w", err)
		}
	}

	// Tasks created before per-child assignment existed have no rows in
	// task_assignments; treat them as assigned to every child in their
	// family so they don't silently disappear from everyone's view. Once a
	// task has at least one assignment this is a no-op for it forever after.
	if _, err := db.Exec(`
		INSERT INTO task_assignments (task_id, child_id)
		SELECT t.id, u.id
		FROM tasks t
		JOIN users u ON u.family_id = t.family_id AND u.role = 'child'
		WHERE NOT EXISTS (SELECT 1 FROM task_assignments ta WHERE ta.task_id = t.id)
	`); err != nil {
		return fmt.Errorf("backfill task_assignments: %w", err)
	}
	return nil
}

// relaxAuthSubjectUniqueness drops the uniqueness constraint on
// users.auth_subject, so one login can be bound to more than one user row
// (e.g. a child who's a member of two families). This constraint was
// enforced two different ways depending on when a database was created:
//
//   - A database created after auth_subject was added to schema.sql got it
//     as an inline "auth_subject TEXT UNIQUE" column constraint, which
//     SQLite has no ALTER TABLE support for removing directly — the table
//     has to be rebuilt.
//   - A database from before that got a separately created
//     "CREATE UNIQUE INDEX idx_users_auth_subject" instead (see below),
//     which a plain DROP INDEX handles.
//
// Both branches are idempotent: once relaxed, later calls find nothing to
// do.
func relaxAuthSubjectUniqueness(db *sql.DB) error {
	if _, err := db.Exec(`DROP INDEX IF EXISTS idx_users_auth_subject`); err != nil {
		return err
	}

	var tableSQL string
	err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'users'`).Scan(&tableSQL)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // schema.sql hasn't created the table yet somehow; nothing to relax
	}
	if err != nil {
		return err
	}
	if !strings.Contains(tableSQL, "UNIQUE") {
		return nil // already a plain column, either from a fresh db or a prior run of this migration
	}

	if _, err := db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	defer db.Exec(`PRAGMA foreign_keys = ON`)

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Rebuilt under a temporary name and swapped in via DROP+RENAME (rather
	// than renaming the live "users" table out of the way first) so that
	// other tables' "REFERENCES users(id)" clauses — which SQLite resolves
	// by name, not by a fixed pointer captured at CREATE TABLE time — never
	// stop pointing at a table that exists. This is SQLite's own documented
	// pattern for schema changes on a table with foreign key dependents.
	stmts := []string{
		`CREATE TABLE users_new (
			id TEXT PRIMARY KEY,
			family_id TEXT NOT NULL REFERENCES families(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			role TEXT NOT NULL CHECK (role IN ('parent', 'child')),
			created_at TEXT NOT NULL,
			auth_subject TEXT,
			email TEXT NOT NULL DEFAULT ''
		)`,
		`INSERT INTO users_new (id, family_id, name, role, created_at, auth_subject, email)
		 SELECT id, family_id, name, role, created_at, auth_subject, email FROM users`,
		`DROP TABLE users`,
		`ALTER TABLE users_new RENAME TO users`,
		`CREATE INDEX IF NOT EXISTS idx_users_family ON users(family_id)`,
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("rebuild users table: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	rows, err := db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		return errors.New("foreign_key_check found inconsistencies after rebuilding users table")
	}
	return rows.Err()
}

func columnSet(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	set := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			return nil, err
		}
		set[name] = true
	}
	return set, rows.Err()
}
