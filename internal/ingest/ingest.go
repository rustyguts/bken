// Package ingest walks filesystem roots, probes video files with ffprobe,
// and upserts rows in the `video` table. Port of cli/src/bken/ingest.py.
package ingest

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/rustyguts/bken/internal/config"
	"github.com/rustyguts/bken/internal/ffmpeg"
)

// First-16-MB SHA1: enough to disambiguate files that collided on size+mtime,
// without paying the cost of a full hash of multi-GB archive videos.
const hashPrefixBytes = 16 * 1024 * 1024

var videoExts = map[string]bool{
	".mp4":  true,
	".mkv":  true,
	".mov":  true,
	".webm": true,
	".avi":  true,
	".flv":  true,
	".m4v":  true,
}

// Rusty's recorder writes "YYYY-MM-DD_HH-MM-SS" or "YYYY-MM-DD HH-MM-SS".
var filenameDTRe = regexp.MustCompile(`(\d{4})-(\d{2})-(\d{2})[ _](\d{2})-(\d{2})-(\d{2})`)

// Scan walks each root recursively, ingesting every video file. workers<=1
// disables the pool (useful for tests). If libraryID is non-nil, each row is
// tagged with it and videos in that library whose path no longer exists are
// marked missing=1.
func Scan(ctx context.Context, cfg *config.Config, conn *sql.DB, roots []string, workers int, libraryID *int64) error {
	if err := cfg.EnsureDirs(); err != nil {
		return err
	}

	var paths []string
	for _, root := range roots {
		ps, err := collectPaths(root)
		if err != nil {
			return fmt.Errorf("collect %s: %w", root, err)
		}
		paths = append(paths, ps...)
	}

	if len(paths) == 0 {
		return markMissing(ctx, conn, libraryID, roots)
	}

	if workers < 1 {
		workers = 1
	}

	// Guard DB writes: modernc.org/sqlite + WAL tolerates multiple readers,
	// but we still serialize writes to avoid SQLITE_BUSY churn.
	var dbMu sync.Mutex

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(workers)
	for _, p := range paths {
		p := p
		g.Go(func() error {
			if gctx.Err() != nil {
				return gctx.Err()
			}
			if err := ingestOne(gctx, conn, &dbMu, p, libraryID); err != nil {
				// Log + continue: a single bad file shouldn't abort the scan.
				fmt.Fprintf(os.Stderr, "ingest %s: %v\n", p, err)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}

	return markMissing(ctx, conn, libraryID, roots)
}

func collectPaths(root string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		if videoExts[strings.ToLower(filepath.Ext(root))] {
			return []string{root}, nil
		}
		return nil, nil
	}
	var out []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			return nil
		}
		if videoExts[strings.ToLower(filepath.Ext(path))] {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}

func ingestOne(ctx context.Context, conn *sql.DB, mu *sync.Mutex, path string, libraryID *int64) error {
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	size := st.Size()
	mtime := float64(st.ModTime().UnixNano()) / 1e9

	var existingID sql.NullInt64
	var existingSize sql.NullInt64
	var existingMtime sql.NullFloat64
	mu.Lock()
	row := conn.QueryRowContext(ctx, "SELECT id, size_bytes, mtime FROM video WHERE path = ?", path)
	err = row.Scan(&existingID, &existingSize, &existingMtime)
	mu.Unlock()
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	unchanged := existingID.Valid &&
		existingSize.Int64 == size &&
		absFloat(existingMtime.Float64-mtime) < 1.0

	if unchanged {
		if libraryID != nil {
			mu.Lock()
			_, err = conn.ExecContext(ctx,
				"UPDATE video SET missing = 0, library_id = ?, updated_at = datetime('now') WHERE id = ?",
				*libraryID, existingID.Int64)
			mu.Unlock()
			return err
		}
		return nil
	}

	probe, err := ffmpeg.Ffprobe(ctx, path)
	if err != nil {
		return err
	}

	sha1p, err := sha1Prefix(path, hashPrefixBytes)
	if err != nil {
		return err
	}
	sha1f, err := sha1Full(path)
	if err != nil {
		return err
	}
	game := gameFromPath(path)
	recordedAt := parseRecordedAt(path, st.ModTime())

	mu.Lock()
	defer mu.Unlock()
	if existingID.Valid {
		_, err = conn.ExecContext(ctx, `UPDATE video SET
                size_bytes=?, mtime=?, sha1_prefix=?, sha1_full=?,
                duration_s=?, width=?, height=?, fps=?,
                video_codec=?, audio_codec=?, audio_channels=?, audio_rate=?,
                game=?, recorded_at=?, library_id=?, missing=0,
                audio_done=0, transcribe_done=0, events_done=0, score_done=0, rank_done=0,
                updated_at=datetime('now')
               WHERE id=?`,
			size, mtime, sha1p, sha1f,
			nullFloat(probe.DurationS), nullInt(probe.Width), nullInt(probe.Height), nullFloat(probe.FPS),
			nullStr(probe.VideoCodec), nullStr(probe.AudioCodec),
			nullInt(probe.AudioChannels), nullInt(probe.AudioRate),
			game, recordedAt, nullInt64Ptr(libraryID),
			existingID.Int64)
		return err
	}
	_, err = conn.ExecContext(ctx, `INSERT INTO video (
            path, size_bytes, mtime, sha1_prefix, sha1_full, library_id,
            duration_s, width, height, fps,
            video_codec, audio_codec, audio_channels, audio_rate,
            game, recorded_at
           ) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		path, size, mtime, sha1p, sha1f, nullInt64Ptr(libraryID),
		nullFloat(probe.DurationS), nullInt(probe.Width), nullInt(probe.Height), nullFloat(probe.FPS),
		nullStr(probe.VideoCodec), nullStr(probe.AudioCodec),
		nullInt(probe.AudioChannels), nullInt(probe.AudioRate),
		game, recordedAt)
	return err
}

// markMissing flags DB rows (scoped to libraryID when non-nil) whose files
// disappeared. No-op when libraryID is nil to avoid nuking unrelated rows.
func markMissing(ctx context.Context, conn *sql.DB, libraryID *int64, roots []string) error {
	if libraryID == nil {
		return nil
	}
	rows, err := conn.QueryContext(ctx, "SELECT id, path FROM video WHERE library_id = ?", *libraryID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var missing []int64
	for rows.Next() {
		var id int64
		var path string
		if err := rows.Scan(&id, &path); err != nil {
			return err
		}
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			missing = append(missing, id)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range missing {
		if _, err := conn.ExecContext(ctx,
			"UPDATE video SET missing = 1, updated_at = datetime('now') WHERE id = ?", id); err != nil {
			return err
		}
	}
	return nil
}

func sha1Prefix(path string, n int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.CopyN(h, f, n); err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func sha1Full(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func gameFromPath(path string) string {
	parent := filepath.Base(filepath.Dir(path))
	return strings.TrimSpace(strings.ReplaceAll(parent, "_", " "))
}

func parseRecordedAt(path string, mtime time.Time) string {
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	m := filenameDTRe.FindStringSubmatch(stem)
	if m != nil {
		y, _ := atoi(m[1])
		mo, _ := atoi(m[2])
		d, _ := atoi(m[3])
		h, _ := atoi(m[4])
		mi, _ := atoi(m[5])
		se, _ := atoi(m[6])
		if t, err := makeTime(y, mo, d, h, mi, se); err == nil {
			return t.Format("2006-01-02T15:04:05")
		}
	}
	return mtime.UTC().Format(time.RFC3339)
}

func makeTime(y, mo, d, h, mi, se int) (time.Time, error) {
	t := time.Date(y, time.Month(mo), d, h, mi, se, 0, time.Local)
	if t.Year() != y || int(t.Month()) != mo || t.Day() != d ||
		t.Hour() != h || t.Minute() != mi || t.Second() != se {
		return time.Time{}, fmt.Errorf("invalid datetime")
	}
	return t, nil
}

func atoi(s string) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("bad digit")
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

func absFloat(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func nullFloat(v float64) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullStr(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func nullInt64Ptr(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}
