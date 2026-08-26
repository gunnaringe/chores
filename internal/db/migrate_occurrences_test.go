package db

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// priorSchema mirrors the schema as it stood immediately before
// task_occurrences replaced task_completions: tasks already had the
// repeat_mode columns, and completions still hung off tasks with an
// ON DELETE CASCADE. Seeding a database in exactly that shape is the only
// way to prove Open() carries a real deployment's history forward.
const priorSchema = `
CREATE TABLE families (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    created_at TEXT NOT NULL,
    dashboard_key TEXT
);
CREATE TABLE users (
    id TEXT PRIMARY KEY,
    family_id TEXT NOT NULL REFERENCES families(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('parent', 'child')),
    created_at TEXT NOT NULL,
    auth_subject TEXT,
    email TEXT NOT NULL DEFAULT ''
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
    icon_type TEXT NOT NULL DEFAULT '',
    icon_value TEXT NOT NULL DEFAULT '',
    repeat_mode TEXT NOT NULL DEFAULT 'cron',
    days_of_week TEXT NOT NULL DEFAULT '',
    repeat_interval_weeks INTEGER NOT NULL DEFAULT 1,
    start_date TEXT NOT NULL DEFAULT ''
);
CREATE TABLE task_assignments (
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    child_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (task_id, child_id)
);
CREATE TABLE task_completions (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    child_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    family_id TEXT NOT NULL REFERENCES families(id) ON DELETE CASCADE,
    due_date TEXT NOT NULL,
    amount_cents INTEGER NOT NULL,
    completed_at TEXT NOT NULL,
    UNIQUE (task_id, child_id, due_date)
);
CREATE TABLE payouts (
    id TEXT PRIMARY KEY,
    child_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    family_id TEXT NOT NULL REFERENCES families(id) ON DELETE CASCADE,
    amount_cents INTEGER NOT NULL,
    full_payout INTEGER NOT NULL DEFAULT 0,
    note TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);
CREATE TABLE invitations (
    id TEXT PRIMARY KEY,
    family_id TEXT NOT NULL REFERENCES families(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    email TEXT NOT NULL DEFAULT '',
    token TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    accepted_at TEXT
);
CREATE TABLE app_settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE push_subscriptions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    family_id TEXT NOT NULL REFERENCES families(id) ON DELETE CASCADE,
    endpoint TEXT NOT NULL UNIQUE,
    p256dh TEXT NOT NULL,
    auth TEXT NOT NULL,
    created_at TEXT NOT NULL
);

INSERT INTO families (id, name, created_at) VALUES ('fam1', 'Sortland', '2024-01-01T00:00:00Z');
INSERT INTO users (id, family_id, name, role, created_at) VALUES
    ('mom', 'fam1', 'Mom', 'parent', '2024-01-01T00:00:00Z'),
    ('kid', 'fam1', 'Kid', 'child', '2024-01-01T00:00:00Z');
INSERT INTO tasks (id, family_id, title, description, price_cents, schedule, created_at,
                   icon_type, icon_value, repeat_mode, days_of_week, start_date) VALUES
    ('t1', 'fam1', 'Take out the rubbish', 'Both bins', 2500, '', '2024-01-01T00:00:00Z',
     'emoji', 'X', 'weekly', '1,4', '2024-01-01'),
    ('t2', 'fam1', 'Walk the dog', '', 1000, '0 0 * * *', '2024-01-01T00:00:00Z',
     '', '', 'cron', '', '');
INSERT INTO task_assignments (task_id, child_id) VALUES ('t1', 'kid'), ('t2', 'kid');
INSERT INTO task_completions (id, task_id, child_id, family_id, due_date, amount_cents, completed_at) VALUES
    ('c1', 't1', 'kid', 'fam1', '2024-01-04', 2500, '2024-01-04T18:00:00Z'),
    ('c2', 't1', 'kid', 'fam1', '2024-01-08', 2000, '2024-01-08T18:00:00Z'),
    ('c3', 't2', 'kid', 'fam1', '2024-01-08', 1000, '2024-01-08T19:00:00Z');
INSERT INTO payouts (id, child_id, family_id, amount_cents, full_payout, note, created_at) VALUES
    ('p1', 'kid', 'fam1', 3000, 0, 'pocket money', '2024-01-09T10:00:00Z');
`

func seedPriorSchema(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "prior.db")
	seed, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open seed db: %v", err)
	}
	if _, err := seed.Exec(priorSchema); err != nil {
		t.Fatalf("apply prior schema: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("close seed db: %v", err)
	}
	return path
}

func sumEarned(t *testing.T, conn *sql.DB, childID string) int64 {
	t.Helper()
	var earned int64
	if err := conn.QueryRow(`
		SELECT COALESCE(SUM(amount_cents), 0) FROM task_occurrences
		WHERE child_id = ? AND completed_at IS NOT NULL`, childID).Scan(&earned); err != nil {
		t.Fatalf("sum earnings: %v", err)
	}
	return earned
}

// The migration's whole reason for existing: a real family's completion
// history has to come through it intact, since those rows are the earnings
// behind payouts already made.
func TestOpen_MigratesCompletionsToOccurrences(t *testing.T) {
	path := seedPriorSchema(t)

	conn, err := Open(path)
	if err != nil {
		t.Fatalf("Open on pre-existing database: %v", err)
	}
	defer conn.Close()

	var count int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM task_occurrences`).Scan(&count); err != nil {
		t.Fatalf("count occurrences: %v", err)
	}
	if count != 3 {
		t.Fatalf("migrated %d occurrences, want 3", count)
	}

	// Every completion becomes a completed occurrence, keeping the amount
	// recorded at the time rather than the task's current price.
	var title, description, iconType, iconValue, completedAt string
	var amount int64
	if err := conn.QueryRow(`
		SELECT title, description, icon_type, icon_value, amount_cents, completed_at
		FROM task_occurrences WHERE id = 'c2'`,
	).Scan(&title, &description, &iconType, &iconValue, &amount, &completedAt); err != nil {
		t.Fatalf("read migrated occurrence: %v", err)
	}
	if title != "Take out the rubbish" || description != "Both bins" {
		t.Errorf("snapshot = %q/%q, want the task's title and description", title, description)
	}
	if iconType != "emoji" || iconValue != "X" {
		t.Errorf("icon snapshot = %q/%q, want emoji/X", iconType, iconValue)
	}
	// c2 was completed at 2000 while the task now costs 2500. The recorded
	// amount is the one that must survive.
	if amount != 2000 {
		t.Errorf("amount_cents = %d, want the 2000 recorded at completion", amount)
	}
	if completedAt != "2024-01-08T18:00:00Z" {
		t.Errorf("completed_at = %q, want it carried over unchanged", completedAt)
	}

	// The balance the family sees must be identical either side of the
	// migration: 5500 earned, 3000 paid out.
	var paidOut int64
	if err := conn.QueryRow(
		`SELECT COALESCE(SUM(amount_cents), 0) FROM payouts WHERE child_id = 'kid'`).Scan(&paidOut); err != nil {
		t.Fatalf("sum payouts: %v", err)
	}
	if earned := sumEarned(t, conn, "kid"); earned != 5500 || paidOut != 3000 {
		t.Errorf("earned/paid = %d/%d, want 5500/3000", earned, paidOut)
	}

	// The old table is retired, not destroyed.
	var backup int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM ` + completionsBackupTable).Scan(&backup); err != nil {
		t.Fatalf("read backup table: %v", err)
	}
	if backup != 3 {
		t.Errorf("backup holds %d rows, want the original 3", backup)
	}
}

// Deleting a task used to cascade its completions away, taking the earnings
// behind already-made payouts with them and driving the balance negative.
// Occurrence rows have no foreign key to tasks, so removing the task row
// now leaves history — and the balance — untouched.
func TestOpen_DeletingATaskNoLongerErasesHistory(t *testing.T) {
	path := seedPriorSchema(t)
	conn, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Exec(`DELETE FROM tasks WHERE id = 't1'`); err != nil {
		t.Fatalf("delete task: %v", err)
	}

	if earned := sumEarned(t, conn, "kid"); earned != 5500 {
		t.Fatalf("earnings dropped to %d after deleting a task; want 5500 kept", earned)
	}

	// And an occurrence still says what it said, with no task left to join to.
	var title string
	if err := conn.QueryRow(`SELECT title FROM task_occurrences WHERE id = 'c1'`).Scan(&title); err != nil {
		t.Fatalf("read orphaned occurrence: %v", err)
	}
	if title != "Take out the rubbish" {
		t.Errorf("title = %q, want the snapshot to survive its task", title)
	}
}

// Open() runs on every start, so the migration has to be safe to re-run.
func TestOpen_CompletionsMigrationIsIdempotent(t *testing.T) {
	path := seedPriorSchema(t)
	for i := 1; i <= 3; i++ {
		conn, err := Open(path)
		if err != nil {
			t.Fatalf("Open #%d: %v", i, err)
		}
		var count int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM task_occurrences`).Scan(&count); err != nil {
			t.Fatalf("count after Open #%d: %v", i, err)
		}
		if count != 3 {
			t.Fatalf("after Open #%d there are %d occurrences, want 3", i, count)
		}
		conn.Close()
	}
}

// A child leaving the family is still meant to take their history with
// them — that cascade is deliberate, and dropping the task_id foreign key
// must not have quietly disarmed it too.
func TestOpen_RemovingAChildStillCascadesTheirOccurrences(t *testing.T) {
	path := seedPriorSchema(t)
	conn, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Exec(`DELETE FROM users WHERE id = 'kid'`); err != nil {
		t.Fatalf("remove child: %v", err)
	}
	var count int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM task_occurrences`).Scan(&count); err != nil {
		t.Fatalf("count occurrences: %v", err)
	}
	if count != 0 {
		t.Errorf("%d occurrences survived the child's removal, want 0", count)
	}
}
