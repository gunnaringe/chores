// Package db opens the SQLite database and applies the schema.
package db

import (
	"database/sql"
	_ "embed"
	"fmt"

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
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_auth_subject ON users(auth_subject)`); err != nil {
		return fmt.Errorf("create idx_users_auth_subject: %w", err)
	}
	return nil
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
