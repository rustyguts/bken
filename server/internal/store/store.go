package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// ErrBlobNotFound is returned when no blob metadata exists for an ID.
var ErrBlobNotFound = errors.New("blob metadata not found")

// BlobMetadata stores metadata about a binary blob on disk.
type BlobMetadata struct {
	ID           string
	Kind         string
	OriginalName string
	ContentType  string
	DiskName     string
	SizeBytes    int64
	CreatedAt    time.Time
}

// Store persists server state in SQLite.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) a SQLite database and runs migrations.
func Open(path string) (*Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("database path is required")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	st := &Store{db: db}
	if err := st.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	slog.Info("sqlite store opened", "path", path)
	return st, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		return fmt.Errorf("enable foreign keys: %w", err)
	}

	const schema = `
CREATE TABLE IF NOT EXISTS blobs (
	id TEXT PRIMARY KEY,
	kind TEXT NOT NULL,
	original_name TEXT NOT NULL,
	content_type TEXT NOT NULL,
	disk_name TEXT NOT NULL UNIQUE,
	size_bytes INTEGER NOT NULL CHECK(size_bytes >= 0),
	created_at_unix_ms INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_blobs_created_at ON blobs(created_at_unix_ms);

CREATE TABLE IF NOT EXISTS messages (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	server_id TEXT NOT NULL,
	channel_id TEXT NOT NULL,
	user_id TEXT NOT NULL,
	username TEXT NOT NULL,
	message TEXT NOT NULL,
	ts INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_messages_channel ON messages(server_id, channel_id, ts);

CREATE TABLE IF NOT EXISTS reactions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	msg_id INTEGER NOT NULL,
	user_id TEXT NOT NULL,
	emoji TEXT NOT NULL,
	created_at_unix_ms INTEGER NOT NULL,
	UNIQUE(msg_id, user_id, emoji)
);
CREATE INDEX IF NOT EXISTS idx_reactions_msg ON reactions(msg_id);
`

	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("run sqlite migrations: %w", err)
	}

	// Add file columns to messages (idempotent — ignore errors for already-existing columns).
	for _, stmt := range []string{
		`ALTER TABLE messages ADD COLUMN file_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE messages ADD COLUMN file_name TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE messages ADD COLUMN file_size INTEGER NOT NULL DEFAULT 0`,
	} {
		_, _ = s.db.ExecContext(ctx, stmt)
	}

	slog.Debug("sqlite migrations applied")
	return nil
}

// CreateBlob creates one blob metadata row.
func (s *Store) CreateBlob(ctx context.Context, meta BlobMetadata) error {
	if strings.TrimSpace(meta.ID) == "" {
		return fmt.Errorf("blob id is required")
	}
	if strings.TrimSpace(meta.Kind) == "" {
		return fmt.Errorf("blob kind is required")
	}
	if strings.TrimSpace(meta.OriginalName) == "" {
		return fmt.Errorf("blob original name is required")
	}
	if strings.TrimSpace(meta.ContentType) == "" {
		return fmt.Errorf("blob content type is required")
	}
	if strings.TrimSpace(meta.DiskName) == "" {
		return fmt.Errorf("blob disk name is required")
	}
	if meta.SizeBytes < 0 {
		return fmt.Errorf("blob size must be non-negative")
	}
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = time.Now().UTC()
	}

	const q = `
INSERT INTO blobs (
	id, kind, original_name, content_type, disk_name, size_bytes, created_at_unix_ms
) VALUES (?, ?, ?, ?, ?, ?, ?)
`
	_, err := s.db.ExecContext(
		ctx,
		q,
		meta.ID,
		meta.Kind,
		meta.OriginalName,
		meta.ContentType,
		meta.DiskName,
		meta.SizeBytes,
		meta.CreatedAt.UnixMilli(),
	)
	if err != nil {
		return fmt.Errorf("insert blob metadata: %w", err)
	}
	slog.Debug("blob metadata created", "blob_id", meta.ID, "size", meta.SizeBytes)
	return nil
}

// MessageRow is a persisted chat message.
type MessageRow struct {
	ID        int64
	ServerID  string
	ChannelID string
	UserID    string
	Username  string
	Message   string
	TS        int64
	FileID    string
	FileName  string
	FileSize  int64
}

// InsertMessage persists a chat message and returns the assigned ID.
func (s *Store) InsertMessage(ctx context.Context, serverID, channelID, userID, username, message string, ts int64, fileID, fileName string, fileSize int64) (int64, error) {
	const q = `INSERT INTO messages (server_id, channel_id, user_id, username, message, ts, file_id, file_name, file_size) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	result, err := s.db.ExecContext(ctx, q, serverID, channelID, userID, username, message, ts, fileID, fileName, fileSize)
	if err != nil {
		return 0, fmt.Errorf("insert message: %w", err)
	}
	id, _ := result.LastInsertId()
	slog.Debug("message persisted", "msg_id", id, "server_id", serverID, "channel_id", channelID, "user_id", userID)
	return id, nil
}

// GetMessages returns the most recent messages for a channel, ordered oldest first.
func (s *Store) GetMessages(ctx context.Context, serverID, channelID string, limit int) ([]MessageRow, error) {
	if limit <= 0 {
		limit = 50
	}
	const q = `
SELECT id, server_id, channel_id, user_id, username, message, ts, file_id, file_name, file_size
FROM messages
WHERE server_id = ? AND channel_id = ?
ORDER BY ts DESC, id DESC
LIMIT ?
`
	rows, err := s.db.QueryContext(ctx, q, serverID, channelID, limit)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	defer rows.Close()

	var msgs []MessageRow
	for rows.Next() {
		var m MessageRow
		if err := rows.Scan(&m.ID, &m.ServerID, &m.ChannelID, &m.UserID, &m.Username, &m.Message, &m.TS, &m.FileID, &m.FileName, &m.FileSize); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		msgs = append(msgs, m)
	}
	// Reverse to oldest-first order.
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	slog.Debug("messages loaded", "server_id", serverID, "channel_id", channelID, "count", len(msgs))
	return msgs, rows.Err()
}

// ReactionRow is a single reaction record.
type ReactionRow struct {
	MsgID  int64
	UserID string
	Emoji  string
}

// AddReaction persists a reaction (idempotent — duplicate is ignored).
func (s *Store) AddReaction(ctx context.Context, msgID int64, userID, emoji string) error {
	const q = `INSERT OR IGNORE INTO reactions (msg_id, user_id, emoji, created_at_unix_ms) VALUES (?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, q, msgID, userID, emoji, time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("insert reaction: %w", err)
	}
	return nil
}

// RemoveReaction deletes a reaction.
func (s *Store) RemoveReaction(ctx context.Context, msgID int64, userID, emoji string) error {
	const q = `DELETE FROM reactions WHERE msg_id = ? AND user_id = ? AND emoji = ?`
	_, err := s.db.ExecContext(ctx, q, msgID, userID, emoji)
	if err != nil {
		return fmt.Errorf("delete reaction: %w", err)
	}
	return nil
}

// GetReactionsForMessages returns reactions grouped by message ID.
func (s *Store) GetReactionsForMessages(ctx context.Context, msgIDs []int64) (map[int64][]ReactionRow, error) {
	if len(msgIDs) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(msgIDs))
	args := make([]any, len(msgIDs))
	for i, id := range msgIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	q := `SELECT msg_id, user_id, emoji FROM reactions WHERE msg_id IN (` + strings.Join(placeholders, ",") + `) ORDER BY id`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query reactions: %w", err)
	}
	defer rows.Close()

	result := make(map[int64][]ReactionRow)
	for rows.Next() {
		var r ReactionRow
		if err := rows.Scan(&r.MsgID, &r.UserID, &r.Emoji); err != nil {
			return nil, fmt.Errorf("scan reaction: %w", err)
		}
		result[r.MsgID] = append(result[r.MsgID], r)
	}
	return result, rows.Err()
}

// BlobByID returns blob metadata by UUID.
func (s *Store) BlobByID(ctx context.Context, id string) (BlobMetadata, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return BlobMetadata{}, fmt.Errorf("blob id is required")
	}

	const q = `
SELECT id, kind, original_name, content_type, disk_name, size_bytes, created_at_unix_ms
FROM blobs
WHERE id = ?
`

	var (
		meta           BlobMetadata
		createdAtUnixM int64
	)
	err := s.db.QueryRowContext(ctx, q, id).Scan(
		&meta.ID,
		&meta.Kind,
		&meta.OriginalName,
		&meta.ContentType,
		&meta.DiskName,
		&meta.SizeBytes,
		&createdAtUnixM,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			slog.Debug("blob not found", "blob_id", id)
			return BlobMetadata{}, ErrBlobNotFound
		}
		return BlobMetadata{}, fmt.Errorf("query blob metadata: %w", err)
	}

	meta.CreatedAt = time.UnixMilli(createdAtUnixM).UTC()
	slog.Debug("blob loaded", "blob_id", id, "size", meta.SizeBytes)
	return meta, nil
}
