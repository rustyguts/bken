// Package store provides persistent server state backed by an embedded SQLite
// database. It owns the database lifecycle and exposes a minimal API used by
// the rest of the server.
//
// Migration design: SQL statements are kept in the [migrations] slice as
// ordered strings. Each is applied exactly once; the applied version is
// tracked in the schema_migrations table. To add a migration, append a new
// string — never edit or reorder existing entries.
package store

import (
	"database/sql"
	"fmt"
	"log"

	_ "modernc.org/sqlite"
)

// migrations holds the ordered list of DDL/DML statements that bring the
// schema up to date. Index i corresponds to version i+1.
var migrations = []string{
	// v1 — settings key/value store
	`CREATE TABLE IF NOT EXISTS settings (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`,
	// v2 — channels
	`CREATE TABLE IF NOT EXISTS channels (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		name       TEXT NOT NULL UNIQUE,
		position   INTEGER NOT NULL DEFAULT 0,
		created_at INTEGER NOT NULL DEFAULT (unixepoch())
	)`,
	// v3 — uploaded files
	`CREATE TABLE IF NOT EXISTS files (
		id           INTEGER PRIMARY KEY AUTOINCREMENT,
		name         TEXT NOT NULL,
		size         INTEGER NOT NULL,
		content_type TEXT NOT NULL,
		disk_path    TEXT NOT NULL,
		created_at   INTEGER NOT NULL DEFAULT (unixepoch())
	)`,
}

// Store wraps a SQLite database and exposes server-state operations.
type Store struct {
	db *sql.DB
}

// New opens (or creates) the SQLite database at path and applies any pending
// migrations. Use ":memory:" for ephemeral in-process storage (tests).
func New(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	// SQLite is single-writer; cap to one connection to prevent SQLITE_BUSY.
	db.SetMaxOpenConns(1)

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// Close releases the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// migrate creates the schema_migrations table (if absent) and applies any
// migrations whose version number exceeds the current maximum.
func (s *Store) migrate() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	var current int
	if err := s.db.QueryRow(
		`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`,
	).Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	for i, stmt := range migrations {
		v := i + 1
		if v <= current {
			continue
		}
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("migration %d: %w", v, err)
		}
		if _, err := s.db.Exec(
			`INSERT INTO schema_migrations(version) VALUES(?)`, v,
		); err != nil {
			return fmt.Errorf("record migration %d: %w", v, err)
		}
		log.Printf("[store] applied migration v%d", v)
	}
	return nil
}

// GetSetting returns the value stored under key. The second return value is
// false when the key does not exist; an error is only returned for real I/O
// failures.
func (s *Store) GetSetting(key string) (string, bool, error) {
	var val string
	err := s.db.QueryRow(
		`SELECT value FROM settings WHERE key = ?`, key,
	).Scan(&val)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return val, true, nil
}

// SetSetting upserts key → value in the settings table.
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO settings(key, value) VALUES(?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	return err
}

// Channel represents a named voice channel stored in the database.
type Channel struct {
	ID       int64
	Name     string
	Position int
}

// GetChannels returns all channels ordered by position then id.
func (s *Store) GetChannels() ([]Channel, error) {
	rows, err := s.db.Query(
		`SELECT id, name, position FROM channels ORDER BY position ASC, id ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var channels []Channel
	for rows.Next() {
		var ch Channel
		if err := rows.Scan(&ch.ID, &ch.Name, &ch.Position); err != nil {
			return nil, err
		}
		channels = append(channels, ch)
	}
	return channels, rows.Err()
}

// CreateChannel inserts a new channel with the given name and returns its id.
// Returns an error if a channel with that name already exists.
func (s *Store) CreateChannel(name string) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO channels(name) VALUES(?)`, name,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// RenameChannel updates the name of the channel with the given id.
// Returns sql.ErrNoRows if no such channel exists.
func (s *Store) RenameChannel(id int64, name string) error {
	res, err := s.db.Exec(
		`UPDATE channels SET name = ? WHERE id = ?`, name, id,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteChannel removes the channel with the given id.
// Returns sql.ErrNoRows if no such channel exists.
func (s *Store) DeleteChannel(id int64) error {
	res, err := s.db.Exec(`DELETE FROM channels WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ChannelCount returns the number of channels currently stored.
func (s *Store) ChannelCount() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM channels`).Scan(&n)
	return n, err
}

// File represents an uploaded file stored on disk.
type File struct {
	ID          int64
	Name        string
	Size        int64
	ContentType string
	DiskPath    string
}

// CreateFile inserts a file record and returns its id.
func (s *Store) CreateFile(name, contentType, diskPath string, size int64) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO files(name, size, content_type, disk_path) VALUES(?, ?, ?, ?)`,
		name, size, contentType, diskPath,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetFile returns the file record with the given id.
// Returns sql.ErrNoRows if no such file exists.
func (s *Store) GetFile(id int64) (File, error) {
	var f File
	err := s.db.QueryRow(
		`SELECT id, name, size, content_type, disk_path FROM files WHERE id = ?`, id,
	).Scan(&f.ID, &f.Name, &f.Size, &f.ContentType, &f.DiskPath)
	return f, err
}
