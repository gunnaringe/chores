// Command dbaudit reports on a chores database without modifying it.
//
// It exists for one job in particular: checking a copy of production
// before and after a schema migration, so "the migration was lossless"
// is something you verified rather than something you assumed. Every
// figure it prints is one that has to match across the upgrade — most of
// all the per-child balances, which are real money owed to real children.
//
// It opens the file read-only and never calls db.Open, which would apply
// the schema and run migrations. Point it at a copy regardless.
//
// Usage:
//
//	go run ./cmd/dbaudit path/to/chores.db
//
// NOTE: SQLite keeps recent writes in a -wal sidecar file. A database
// copied without its -wal can be missing nearly everything while still
// opening cleanly, so copy both, or run
// `PRAGMA wal_checkpoint(TRUNCATE)` before copying.
package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	_ "modernc.org/sqlite"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: dbaudit <path-to-database>")
		os.Exit(2)
	}
	path := os.Args[1]
	if err := run(path); err != nil {
		fmt.Fprintf(os.Stderr, "dbaudit: %v\n", err)
		os.Exit(1)
	}
}

func run(path string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	if _, err := os.Stat(path + "-wal"); err == nil {
		fmt.Printf("note: %s exists, so this database has uncheckpointed writes.\n"+
			"      Both files must be copied together or the copy is incomplete.\n\n",
			filepath.Base(path)+"-wal")
	}

	// mode=ro so an audit can never be the thing that changes the data.
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&_pragma=foreign_keys(0)")
	if err != nil {
		return err
	}
	defer db.Close()

	schema, err := detectSchema(db)
	if err != nil {
		return err
	}
	fmt.Printf("schema: %s\n\n", schema.name)

	if err := reportCounts(db, schema); err != nil {
		return err
	}
	if err := reportHazards(db, schema); err != nil {
		return err
	}
	return reportBalances(db, schema)
}

// schemaShape captures the one thing that differs between the pre- and
// post-migration layouts: what the occurrence table is called, and whether
// it can hold rows that aren't completions.
type schemaShape struct {
	name           string
	table          string
	completedOnly  bool // true before task_occurrences, where every row was a completion
	completedWhere string
}

func detectSchema(db *sql.DB) (schemaShape, error) {
	occurrences, err := tableExists(db, "task_occurrences")
	if err != nil {
		return schemaShape{}, err
	}
	if occurrences {
		return schemaShape{
			name:           "current (task_occurrences)",
			table:          "task_occurrences",
			completedWhere: " AND completed_at IS NOT NULL",
		}, nil
	}
	completions, err := tableExists(db, "task_completions")
	if err != nil {
		return schemaShape{}, err
	}
	if completions {
		return schemaShape{
			name:          "pre-migration (task_completions)",
			table:         "task_completions",
			completedOnly: true,
		}, nil
	}
	return schemaShape{}, fmt.Errorf("neither task_occurrences nor task_completions found; is this a chores database?")
}

func reportCounts(db *sql.DB, s schemaShape) error {
	fmt.Println("-- row counts --")
	tables := []string{"families", "users", "tasks", "task_assignments", s.table,
		"payouts", "invitations", "push_subscriptions", "app_settings"}
	if backup, _ := tableExists(db, "task_completions_backup_v1"); backup {
		tables = append(tables, "task_completions_backup_v1")
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, t := range tables {
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + t).Scan(&n); err != nil {
			fmt.Fprintf(w, "  %s\t<%v>\n", t, err)
			continue
		}
		fmt.Fprintf(w, "  %s\t%d\n", t, n)
	}
	if !s.completedOnly {
		var completed, due int
		db.QueryRow(`SELECT COUNT(*) FROM task_occurrences WHERE completed_at IS NOT NULL`).Scan(&completed)
		db.QueryRow(`SELECT COUNT(*) FROM task_occurrences WHERE completed_at IS NULL`).Scan(&due)
		fmt.Fprintf(w, "    of which completed\t%d\n", completed)
		fmt.Fprintf(w, "    of which due, not done\t%d\n", due)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Println()
	return nil
}

func reportHazards(db *sql.DB, s schemaShape) error {
	fmt.Println("-- data hazards --")
	checks := []struct {
		label string
		query string
	}{
		{"occurrences with no matching child",
			`SELECT COUNT(*) FROM ` + s.table + ` o LEFT JOIN users u ON u.id = o.child_id WHERE u.id IS NULL`},
		{"occurrences with no matching family",
			`SELECT COUNT(*) FROM ` + s.table + ` o LEFT JOIN families f ON f.id = o.family_id WHERE f.id IS NULL`},
		{"occurrences with a malformed due_date",
			`SELECT COUNT(*) FROM ` + s.table + ` WHERE due_date NOT GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'`},
		{"duplicate (task, child, due_date) triples",
			`SELECT COALESCE(SUM(c-1), 0) FROM (SELECT COUNT(*) c FROM ` + s.table + ` GROUP BY task_id, child_id, due_date HAVING c > 1)`},
		{"occurrences with a non-positive amount",
			`SELECT COUNT(*) FROM ` + s.table + ` WHERE amount_cents <= 0` + s.completedWhere},
		{"tasks with an unknown repeat_mode",
			`SELECT COUNT(*) FROM tasks WHERE repeat_mode NOT IN ('once', 'weekly', 'cron')`},
		{"weekly tasks with no days_of_week",
			`SELECT COUNT(*) FROM tasks WHERE repeat_mode = 'weekly' AND days_of_week = ''`},
		{"cron tasks with an empty schedule",
			`SELECT COUNT(*) FROM tasks WHERE repeat_mode = 'cron' AND schedule = ''`},
		{"once/weekly tasks with no start_date",
			`SELECT COUNT(*) FROM tasks WHERE repeat_mode IN ('once', 'weekly') AND start_date = ''`},
		{"tasks assigned to nobody",
			`SELECT COUNT(*) FROM tasks t WHERE NOT EXISTS (SELECT 1 FROM task_assignments a WHERE a.task_id = t.id)`},
		{"children whose payouts exceed their earnings",
			`SELECT COUNT(*) FROM users u WHERE u.role = 'child'
			   AND COALESCE((SELECT SUM(amount_cents) FROM payouts WHERE child_id = u.id), 0)
			     > COALESCE((SELECT SUM(amount_cents) FROM ` + s.table + ` WHERE child_id = u.id` + s.completedWhere + `), 0)`},
	}
	if s.completedOnly {
		// Only meaningful before the migration: this is the cascade that
		// made deleting a task erase the earnings behind real payouts.
		checks = append(checks, struct{ label, query string }{
			"completions whose task row is gone",
			`SELECT COUNT(*) FROM task_completions c LEFT JOIN tasks t ON t.id = c.task_id WHERE t.id IS NULL`,
		})
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, c := range checks {
		var n int
		if err := db.QueryRow(c.query).Scan(&n); err != nil {
			fmt.Fprintf(w, "  %s\t<%v>\n", c.label, err)
			continue
		}
		mark := "ok"
		if n > 0 {
			mark = "<-- NEEDS ATTENTION"
		}
		fmt.Fprintf(w, "  %s\t%d\t%s\n", c.label, n, mark)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Println()
	return nil
}

// reportBalances is the part that matters most across a migration: these
// figures are money owed, and every one of them must be identical before
// and after.
func reportBalances(db *sql.DB, s schemaShape) error {
	fmt.Println("-- per-child balances (these MUST match across a migration) --")
	rows, err := db.Query(`
		SELECT f.name, u.name,
		       COALESCE((SELECT SUM(amount_cents) FROM ` + s.table + ` WHERE child_id = u.id` + s.completedWhere + `), 0),
		       COALESCE((SELECT SUM(amount_cents) FROM payouts WHERE child_id = u.id), 0)
		FROM users u JOIN families f ON f.id = u.family_id
		WHERE u.role = 'child'
		ORDER BY f.name, u.name`)
	if err != nil {
		return err
	}
	defer rows.Close()

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  FAMILY\tCHILD\tEARNED\tPAID OUT\tBALANCE")
	var totalEarned, totalPaid int64
	for rows.Next() {
		var family, child string
		var earned, paid int64
		if err := rows.Scan(&family, &child, &earned, &paid); err != nil {
			return err
		}
		totalEarned += earned
		totalPaid += paid
		flag := ""
		if earned-paid < 0 {
			flag = "\t<-- NEGATIVE"
		}
		fmt.Fprintf(w, "  %s\t%s\t%d\t%d\t%d%s\n", family, child, earned, paid, earned-paid, flag)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	fmt.Fprintf(w, "  \tTOTAL\t%d\t%d\t%d\n", totalEarned, totalPaid, totalEarned-totalPaid)
	return w.Flush()
}

func tableExists(db *sql.DB, name string) (bool, error) {
	var found string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&found)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
