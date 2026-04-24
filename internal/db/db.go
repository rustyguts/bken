// Package db wraps sqlite with schema init + forward-only migrations,
// matching the Python layer one-for-one. Uses modernc.org/sqlite (pure Go,
// no CGo toolchain required to build bken).
package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Open opens the SQLite database, applies schema + migrations, enables WAL.
// The path's parent dir is created if missing.
func Open(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir db parent: %w", err)
	}
	// `_pragma` tokens run at connection time; matches Python's post-connect pragmas.
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(30000)", path)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// WAL + busy_timeout mean a single writer is fine; keep SetMaxOpenConns high
	// enough for Echo handlers to read concurrently.
	conn.SetMaxOpenConns(16)
	if err := Init(conn); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

// Init runs the base schema then migrations. Idempotent.
func Init(conn *sql.DB) error {
	if _, err := conn.Exec(Schema); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	if err := migrate(conn); err != nil {
		return err
	}
	if _, err := conn.Exec(PostMigrationIndexes); err != nil {
		return fmt.Errorf("apply post-migration indexes: %w", err)
	}
	return nil
}

func migrate(conn *sql.DB) error {
	for _, m := range Migrations {
		has, err := hasColumn(conn, m.Table, m.Column)
		if err != nil {
			return err
		}
		if has {
			continue
		}
		if _, err := conn.Exec(m.DDL); err != nil {
			return fmt.Errorf("migrate %s.%s: %w", m.Table, m.Column, err)
		}
	}
	return nil
}

func hasColumn(conn *sql.DB, table, column string) (bool, error) {
	rows, err := conn.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid       int
			name      string
			ctype     string
			notnull   int
			dfltValue sql.NullString
			pk        int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

// Touch bumps video.updated_at.
func Touch(conn *sql.DB, videoID int64) error {
	_, err := conn.Exec("UPDATE video SET updated_at = datetime('now') WHERE id = ?", videoID)
	return err
}
