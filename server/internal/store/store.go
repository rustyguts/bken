package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
`

	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("run sqlite migrations: %w", err)
	}
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
	return nil
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
			return BlobMetadata{}, ErrBlobNotFound
		}
		return BlobMetadata{}, fmt.Errorf("query blob metadata: %w", err)
	}

	meta.CreatedAt = time.UnixMilli(createdAtUnixM).UTC()
	return meta, nil
}
