// Package library manages registered library directories and their
// scheduled rescans. Port of cli/src/bken/library.py.
package library

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"time"

	"github.com/rustyguts/bken/internal/config"
)

// Library is the row shape returned to callers — matches the DB columns.
type Library struct {
	ID                  int64
	Name                string
	Path                string
	ScanIntervalMinutes int
	LastScanAt          sql.NullString
	NextScanAt          sql.NullString
	Active              bool
	CreatedAt           sql.NullString
}

// IngestFn is injected by callers so this package doesn't depend on
// internal/ingest (which depends on config + ffmpeg).
type IngestFn func(ctx context.Context, roots []string, libraryID *int64) error

// Add registers a new library. If the path is already registered, returns
// the existing id. scanIntervalMin <= 0 falls back to 60 minutes.
func Add(ctx context.Context, conn *sql.DB, name, path string, scanIntervalMin int) (int64, error) {
	if scanIntervalMin <= 0 {
		scanIntervalMin = 60
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return 0, fmt.Errorf("resolve path: %w", err)
	}
	if name == "" {
		name = filepath.Base(abs)
	}
	var existing int64
	err = conn.QueryRowContext(ctx, "SELECT id FROM library WHERE path=?", abs).Scan(&existing)
	if err == nil {
		return existing, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}
	next := time.Now().UTC().Format("2006-01-02 15:04:05")
	res, err := conn.ExecContext(ctx,
		`INSERT INTO library (name, path, scan_interval_minutes, next_scan_at, active)
         VALUES (?, ?, ?, ?, 1)`,
		name, abs, scanIntervalMin, next)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// List returns every library row.
func List(ctx context.Context, conn *sql.DB) ([]Library, error) {
	rows, err := conn.QueryContext(ctx, `SELECT id, name, path, scan_interval_minutes,
        last_scan_at, next_scan_at, active, created_at FROM library ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLibraries(rows)
}

// Get returns a single library row by id, or sql.ErrNoRows.
func Get(ctx context.Context, conn *sql.DB, id int64) (*Library, error) {
	row := conn.QueryRowContext(ctx, `SELECT id, name, path, scan_interval_minutes,
        last_scan_at, next_scan_at, active, created_at FROM library WHERE id=?`, id)
	var l Library
	var active int64
	if err := row.Scan(&l.ID, &l.Name, &l.Path, &l.ScanIntervalMinutes,
		&l.LastScanAt, &l.NextScanAt, &active, &l.CreatedAt); err != nil {
		return nil, err
	}
	l.Active = active == 1
	return &l, nil
}

// Sync scans a library by invoking the provided ingest function, then bumps
// last_scan_at and next_scan_at.
func Sync(ctx context.Context, cfg *config.Config, conn *sql.DB, libraryID int64, ingestFn IngestFn) error {
	lib, err := Get(ctx, conn, libraryID)
	if err != nil {
		return err
	}
	if err := ingestFn(ctx, []string{lib.Path}, &libraryID); err != nil {
		return err
	}
	now := time.Now().UTC()
	next := now.Add(time.Duration(lib.ScanIntervalMinutes) * time.Minute)
	_, err = conn.ExecContext(ctx,
		`UPDATE library SET last_scan_at=?, next_scan_at=? WHERE id=?`,
		now.Format("2006-01-02 15:04:05"),
		next.Format("2006-01-02 15:04:05"),
		libraryID)
	return err
}

// DueForScan returns active libraries whose next_scan_at is in the past or
// null.
func DueForScan(ctx context.Context, conn *sql.DB) ([]Library, error) {
	rows, err := conn.QueryContext(ctx, `SELECT id, name, path, scan_interval_minutes,
        last_scan_at, next_scan_at, active, created_at FROM library
        WHERE active=1 AND (next_scan_at IS NULL OR next_scan_at <= datetime('now'))
        ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLibraries(rows)
}

// Delete hard-deletes a library row. ON DELETE SET NULL on video.library_id
// keeps the video rows but unlinks them.
func Delete(ctx context.Context, conn *sql.DB, id int64) error {
	_, err := conn.ExecContext(ctx, "DELETE FROM library WHERE id=?", id)
	return err
}

func scanLibraries(rows *sql.Rows) ([]Library, error) {
	var out []Library
	for rows.Next() {
		var l Library
		var active int64
		if err := rows.Scan(&l.ID, &l.Name, &l.Path, &l.ScanIntervalMinutes,
			&l.LastScanAt, &l.NextScanAt, &active, &l.CreatedAt); err != nil {
			return nil, err
		}
		l.Active = active == 1
		out = append(out, l)
	}
	return out, rows.Err()
}
