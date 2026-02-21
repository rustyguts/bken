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
	// v4 — audit log
	`CREATE TABLE IF NOT EXISTS audit_log (
		id           INTEGER PRIMARY KEY AUTOINCREMENT,
		actor_id     INTEGER NOT NULL,
		actor_name   TEXT NOT NULL,
		action       TEXT NOT NULL,
		target       TEXT NOT NULL DEFAULT '',
		details_json TEXT NOT NULL DEFAULT '{}',
		created_at   INTEGER NOT NULL DEFAULT (unixepoch())
	)`,
	// v5 — bans
	`CREATE TABLE IF NOT EXISTS bans (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		pubkey     TEXT NOT NULL DEFAULT '',
		ip         TEXT NOT NULL DEFAULT '',
		reason     TEXT NOT NULL DEFAULT '',
		banned_by  TEXT NOT NULL DEFAULT '',
		duration_s INTEGER NOT NULL DEFAULT 0,
		created_at INTEGER NOT NULL DEFAULT (unixepoch())
	)`,
	// v6 — user roles
	`CREATE TABLE IF NOT EXISTS user_roles (
		pubkey TEXT PRIMARY KEY,
		role   TEXT NOT NULL DEFAULT 'USER'
	)`,
	// v7 — announcements
	`CREATE TABLE IF NOT EXISTS announcements (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		content    TEXT NOT NULL,
		created_by TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL DEFAULT (unixepoch())
	)`,
	// v8 — channel slow_mode
	`ALTER TABLE channels ADD COLUMN slow_mode_seconds INTEGER NOT NULL DEFAULT 0`,
	// v9 — indexes for performance
	`CREATE INDEX IF NOT EXISTS idx_audit_log_created ON audit_log(created_at)`,
	// v10 — enable WAL mode
	`PRAGMA journal_mode=WAL`,
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
	// Allow multiple read connections but serialise writes.
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)

	// Enable WAL mode for concurrent readers.
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		log.Printf("[store] WAL mode: %v (non-fatal)", err)
	}
	// Busy timeout to avoid SQLITE_BUSY on concurrent access.
	if _, err := db.Exec(`PRAGMA busy_timeout=5000`); err != nil {
		log.Printf("[store] busy_timeout: %v (non-fatal)", err)
	}

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

// ---------------------------------------------------------------------------
// Audit Log
// ---------------------------------------------------------------------------

// AuditEntry represents one row in the audit_log table.
type AuditEntry struct {
	ID          int64
	ActorID     int
	ActorName   string
	Action      string
	Target      string
	DetailsJSON string
	CreatedAt   int64
}

// InsertAuditLog records an admin action in the audit log.
// If the table exceeds maxAuditEntries rows, the oldest entries are purged.
func (s *Store) InsertAuditLog(actorID int, actorName, action, target, detailsJSON string) error {
	if detailsJSON == "" {
		detailsJSON = "{}"
	}
	_, err := s.db.Exec(
		`INSERT INTO audit_log(actor_id, actor_name, action, target, details_json) VALUES(?,?,?,?,?)`,
		actorID, actorName, action, target, detailsJSON,
	)
	if err != nil {
		return err
	}
	// Auto-purge oldest entries beyond 10,000.
	_, err = s.db.Exec(`DELETE FROM audit_log WHERE id NOT IN (SELECT id FROM audit_log ORDER BY id DESC LIMIT 10000)`)
	return err
}

// GetAuditLog returns audit log entries, most recent first, with optional action filter.
// Pass action="" to return all actions. Limit controls max rows returned.
func (s *Store) GetAuditLog(action string, limit int) ([]AuditEntry, error) {
	var rows *sql.Rows
	var err error
	if action != "" {
		rows, err = s.db.Query(
			`SELECT id, actor_id, actor_name, action, target, details_json, created_at FROM audit_log WHERE action = ? ORDER BY id DESC LIMIT ?`,
			action, limit,
		)
	} else {
		rows, err = s.db.Query(
			`SELECT id, actor_id, actor_name, action, target, details_json, created_at FROM audit_log ORDER BY id DESC LIMIT ?`,
			limit,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.ActorID, &e.ActorName, &e.Action, &e.Target, &e.DetailsJSON, &e.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// AuditLogCount returns the number of entries in the audit log.
func (s *Store) AuditLogCount() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM audit_log`).Scan(&n)
	return n, err
}

// ---------------------------------------------------------------------------
// Bans
// ---------------------------------------------------------------------------

// Ban represents a row in the bans table.
type Ban struct {
	ID        int64
	Pubkey    string
	IP        string
	Reason    string
	BannedBy  string
	DurationS int // 0 = permanent
	CreatedAt int64
}

// InsertBan records a ban. DurationS=0 means permanent.
func (s *Store) InsertBan(pubkey, ip, reason, bannedBy string, durationS int) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO bans(pubkey, ip, reason, banned_by, duration_s) VALUES(?,?,?,?,?)`,
		pubkey, ip, reason, bannedBy, durationS,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetBans returns all bans ordered by most recent first.
func (s *Store) GetBans() ([]Ban, error) {
	rows, err := s.db.Query(
		`SELECT id, pubkey, ip, reason, banned_by, duration_s, created_at FROM bans ORDER BY id DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bans []Ban
	for rows.Next() {
		var b Ban
		if err := rows.Scan(&b.ID, &b.Pubkey, &b.IP, &b.Reason, &b.BannedBy, &b.DurationS, &b.CreatedAt); err != nil {
			return nil, err
		}
		bans = append(bans, b)
	}
	return bans, rows.Err()
}

// DeleteBan removes a ban by ID.
func (s *Store) DeleteBan(id int64) error {
	res, err := s.db.Exec(`DELETE FROM bans WHERE id = ?`, id)
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

// IsIPBanned checks if the given IP is banned (considering temp ban expiry).
func (s *Store) IsIPBanned(ip string) (bool, string, error) {
	var reason string
	err := s.db.QueryRow(
		`SELECT reason FROM bans WHERE ip = ? AND (duration_s = 0 OR created_at + duration_s > unixepoch()) LIMIT 1`,
		ip,
	).Scan(&reason)
	if err == sql.ErrNoRows {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	return true, reason, nil
}

// IsUserBanned checks if the given pubkey/username is banned (considering temp ban expiry).
func (s *Store) IsUserBanned(pubkey string) (bool, string, error) {
	var reason string
	err := s.db.QueryRow(
		`SELECT reason FROM bans WHERE pubkey = ? AND (duration_s = 0 OR created_at + duration_s > unixepoch()) LIMIT 1`,
		pubkey,
	).Scan(&reason)
	if err == sql.ErrNoRows {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	return true, reason, nil
}

// PurgeExpiredBans removes bans that have passed their duration.
func (s *Store) PurgeExpiredBans() (int64, error) {
	res, err := s.db.Exec(`DELETE FROM bans WHERE duration_s > 0 AND created_at + duration_s <= unixepoch()`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ---------------------------------------------------------------------------
// User Roles
// ---------------------------------------------------------------------------

// SetUserRole upserts a role for the given pubkey.
func (s *Store) SetUserRole(pubkey, role string) error {
	_, err := s.db.Exec(
		`INSERT INTO user_roles(pubkey, role) VALUES(?,?)
		 ON CONFLICT(pubkey) DO UPDATE SET role = excluded.role`,
		pubkey, role,
	)
	return err
}

// GetUserRole returns the role for a pubkey. Returns "USER" if not set.
func (s *Store) GetUserRole(pubkey string) (string, error) {
	var role string
	err := s.db.QueryRow(`SELECT role FROM user_roles WHERE pubkey = ?`, pubkey).Scan(&role)
	if err == sql.ErrNoRows {
		return "USER", nil
	}
	return role, err
}

// ---------------------------------------------------------------------------
// Announcements
// ---------------------------------------------------------------------------

// Announcement represents a row in the announcements table.
type Announcement struct {
	ID        int64
	Content   string
	CreatedBy string
	CreatedAt int64
}

// InsertAnnouncement creates an announcement. Only the latest 1 is considered active.
func (s *Store) InsertAnnouncement(content, createdBy string) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO announcements(content, created_by) VALUES(?,?)`,
		content, createdBy,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetLatestAnnouncement returns the most recent announcement, or sql.ErrNoRows if none.
func (s *Store) GetLatestAnnouncement() (Announcement, error) {
	var a Announcement
	err := s.db.QueryRow(
		`SELECT id, content, created_by, created_at FROM announcements ORDER BY id DESC LIMIT 1`,
	).Scan(&a.ID, &a.Content, &a.CreatedBy, &a.CreatedAt)
	return a, err
}

// ---------------------------------------------------------------------------
// Channel slow mode
// ---------------------------------------------------------------------------

// SetChannelSlowMode sets the slow mode cooldown in seconds for a channel.
func (s *Store) SetChannelSlowMode(channelID int64, seconds int) error {
	res, err := s.db.Exec(`UPDATE channels SET slow_mode_seconds = ? WHERE id = ?`, seconds, channelID)
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

// GetChannelSlowMode returns the slow mode seconds for a channel.
func (s *Store) GetChannelSlowMode(channelID int64) (int, error) {
	var secs int
	err := s.db.QueryRow(`SELECT slow_mode_seconds FROM channels WHERE id = ?`, channelID).Scan(&secs)
	return secs, err
}

// ---------------------------------------------------------------------------
// SQLite optimization
// ---------------------------------------------------------------------------

// Optimize runs PRAGMA optimize for SQLite query planner statistics.
func (s *Store) Optimize() error {
	_, err := s.db.Exec(`PRAGMA optimize`)
	return err
}

// ---------------------------------------------------------------------------
// CLI helpers
// ---------------------------------------------------------------------------

// GetAllSettings returns all key/value pairs from the settings table.
func (s *Store) GetAllSettings() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT key, value FROM settings ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	settings := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		settings[k] = v
	}
	return settings, rows.Err()
}

// Backup creates a copy of the database at the given path using SQLite's
// backup API through VACUUM INTO.
func (s *Store) Backup(destPath string) error {
	_, err := s.db.Exec(`VACUUM INTO ?`, destPath)
	return err
}
