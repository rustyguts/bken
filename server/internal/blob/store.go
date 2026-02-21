package blob

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bken/server/internal/store"
)

const defaultContentType = "application/octet-stream"

// Store coordinates blob bytes on disk with metadata in sqlite.
type Store struct {
	rootDir string
	meta    *store.Store
}

// PutInput contains the data required to write one blob.
type PutInput struct {
	Kind         string
	OriginalName string
	ContentType  string
	Reader       io.Reader
}

// OpenResult is a blob metadata + opened file stream tuple.
type OpenResult struct {
	Metadata store.BlobMetadata
	File     *os.File
}

// NewStore creates a blob store rooted at rootDir.
func NewStore(rootDir string, meta *store.Store) (*Store, error) {
	rootDir = strings.TrimSpace(rootDir)
	if rootDir == "" {
		return nil, fmt.Errorf("blob root directory is required")
	}
	if meta == nil {
		return nil, fmt.Errorf("sqlite metadata store is required")
	}
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		return nil, fmt.Errorf("create blob directory: %w", err)
	}
	slog.Debug("blob store initialized", "dir", rootDir)
	return &Store{rootDir: rootDir, meta: meta}, nil
}

// Put writes bytes to disk as an opaque UUID-named blob and stores metadata in sqlite.
func (s *Store) Put(ctx context.Context, input PutInput) (store.BlobMetadata, error) {
	if input.Reader == nil {
		return store.BlobMetadata{}, fmt.Errorf("blob reader is required")
	}
	kind := strings.TrimSpace(input.Kind)
	if kind == "" {
		kind = "blob"
	}
	originalName := strings.TrimSpace(input.OriginalName)
	if originalName == "" {
		return store.BlobMetadata{}, fmt.Errorf("blob original name is required")
	}
	contentType := strings.TrimSpace(input.ContentType)
	if contentType == "" {
		contentType = defaultContentType
	}

	id, err := newUUID()
	if err != nil {
		return store.BlobMetadata{}, fmt.Errorf("generate blob id: %w", err)
	}

	tempFile, err := os.CreateTemp(s.rootDir, ".blob-write-*")
	if err != nil {
		return store.BlobMetadata{}, fmt.Errorf("create temp blob file: %w", err)
	}
	tempPath := tempFile.Name()

	size, copyErr := io.Copy(tempFile, input.Reader)
	closeErr := tempFile.Close()
	if copyErr != nil {
		_ = os.Remove(tempPath)
		return store.BlobMetadata{}, fmt.Errorf("write blob bytes: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tempPath)
		return store.BlobMetadata{}, fmt.Errorf("close blob file: %w", closeErr)
	}

	finalPath := filepath.Join(s.rootDir, id)
	if err := os.Rename(tempPath, finalPath); err != nil {
		_ = os.Remove(tempPath)
		return store.BlobMetadata{}, fmt.Errorf("move blob into place: %w", err)
	}

	meta := store.BlobMetadata{
		ID:           id,
		Kind:         kind,
		OriginalName: originalName,
		ContentType:  contentType,
		DiskName:     id,
		SizeBytes:    size,
		CreatedAt:    time.Now().UTC(),
	}
	if err := s.meta.CreateBlob(ctx, meta); err != nil {
		_ = os.Remove(finalPath)
		return store.BlobMetadata{}, fmt.Errorf("persist blob metadata: %w", err)
	}

	slog.Info("blob stored", "blob_id", id, "name", originalName, "size", size, "content_type", contentType)
	return meta, nil
}

// Open resolves blob metadata in sqlite and opens its corresponding on-disk blob.
func (s *Store) Open(ctx context.Context, id string) (OpenResult, error) {
	meta, err := s.meta.BlobByID(ctx, id)
	if err != nil {
		return OpenResult{}, err
	}

	path := filepath.Join(s.rootDir, meta.DiskName)
	f, err := os.Open(path)
	if err != nil {
		slog.Error("blob file open failed", "blob_id", id, "path", path, "err", err)
		return OpenResult{}, fmt.Errorf("open blob file: %w", err)
	}

	slog.Debug("blob opened", "blob_id", id, "size", meta.SizeBytes)
	return OpenResult{Metadata: meta, File: f}, nil
}

func newUUID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}

	// Set version 4 and variant bits per RFC 4122.
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80

	return fmt.Sprintf(
		"%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",
		raw[0], raw[1], raw[2], raw[3],
		raw[4], raw[5],
		raw[6], raw[7],
		raw[8], raw[9],
		raw[10], raw[11], raw[12], raw[13], raw[14], raw[15],
	), nil
}
