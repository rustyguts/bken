// Package audio extracts a 16 kHz mono WAV per video. Port of
// cli/src/bken/audio.py. Whisper and audio-event models want mono 16 kHz,
// so we decode once and cache on disk.
package audio

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/rustyguts/bken/internal/config"
	"github.com/rustyguts/bken/internal/ffmpeg"
)

// PathFor returns the cached WAV path for a video.
func PathFor(cfg *config.Config, videoID int64) string {
	return filepath.Join(cfg.AudioDir, fmt.Sprintf("%d.wav", videoID))
}

// Extract decodes a single video's audio to the cached WAV location.
// Skips work when the file exists and audio_done=1, unless `force`.
func Extract(ctx context.Context, cfg *config.Config, conn *sql.DB, videoID int64, force bool) error {
	var path string
	var audioDone int
	row := conn.QueryRowContext(ctx, "SELECT path, audio_done FROM video WHERE id = ?", videoID)
	if err := row.Scan(&path, &audioDone); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("video id=%d not found", videoID)
		}
		return err
	}

	out := PathFor(cfg, videoID)
	if !force && audioDone == 1 {
		if st, err := os.Stat(out); err == nil && st.Size() > 0 {
			return nil
		}
	}

	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}

	if err := ffmpeg.Run(ctx,
		"-i", path,
		"-vn",
		"-ac", strconv.Itoa(cfg.AudioChannels),
		"-ar", strconv.Itoa(cfg.AudioSampleRate),
		"-c:a", "pcm_s16le",
		out,
	); err != nil {
		// Remove partial output so a retry starts clean.
		if _, statErr := os.Stat(out); statErr == nil {
			_ = os.Remove(out)
		}
		return err
	}

	_, err := conn.ExecContext(ctx,
		"UPDATE video SET audio_done=1, updated_at=datetime('now') WHERE id=?",
		videoID)
	return err
}

// ExtractAll extracts audio for every video whose audio_done=0 (or all if
// force). `workers` controls parallel ffmpeg processes.
func ExtractAll(ctx context.Context, cfg *config.Config, conn *sql.DB, workers int, force bool) error {
	if err := cfg.EnsureDirs(); err != nil {
		return err
	}

	var query string
	if force {
		query = "SELECT id FROM video ORDER BY id"
	} else {
		query = "SELECT id FROM video WHERE audio_done = 0 ORDER BY id"
	}
	rows, err := conn.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	if len(ids) == 0 {
		return nil
	}
	if workers < 1 {
		workers = 1
	}

	var mu sync.Mutex // serialize stderr log writes
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(workers)
	for _, id := range ids {
		id := id
		g.Go(func() error {
			if gctx.Err() != nil {
				return gctx.Err()
			}
			if err := Extract(gctx, cfg, conn, id, force); err != nil {
				mu.Lock()
				fmt.Fprintf(os.Stderr, "audio extract id=%d: %v\n", id, err)
				mu.Unlock()
			}
			return nil
		})
	}
	return g.Wait()
}

// Exists reports whether the cached WAV is present.
func Exists(cfg *config.Config, videoID int64) bool {
	st, err := os.Stat(PathFor(cfg, videoID))
	if errors.Is(err, fs.ErrNotExist) {
		return false
	}
	return err == nil && st.Size() > 0
}
