// Package thumbs renders JPG thumbnails for clips and videos. Port of
// cli/src/bken/thumbs.py. Frames are cached to disk under cfg.ThumbsDir.
package thumbs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/rustyguts/bken/internal/config"
	"github.com/rustyguts/bken/internal/ffmpeg"
)

// ForClip returns the on-disk path to a clip thumbnail, rendering if needed.
// Pulls the frame from the source video at the clip's midpoint.
func ForClip(ctx context.Context, cfg *config.Config, conn *sql.DB, clipID int64) (string, error) {
	var (
		thumbPath sql.NullString
		videoID   int64
		startS    float64
		endS      float64
		videoPath string
		duration  sql.NullFloat64
	)
	row := conn.QueryRowContext(ctx, `SELECT c.thumb_path, c.video_id, c.start_s, c.end_s,
               v.path, v.duration_s
          FROM candidate_clip c
          JOIN video v ON v.id = c.video_id
         WHERE c.id = ?`, clipID)
	if err := row.Scan(&thumbPath, &videoID, &startS, &endS, &videoPath, &duration); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("clip id=%d not found", clipID)
		}
		return "", err
	}

	if thumbPath.Valid && thumbPath.String != "" {
		existing := cfg.ResolveData(thumbPath.String)
		if st, err := os.Stat(existing); err == nil && st.Size() > 0 {
			return existing, nil
		}
	}

	if err := os.MkdirAll(cfg.ThumbsDir, 0o755); err != nil {
		return "", err
	}
	out := filepath.Join(cfg.ThumbsDir, fmt.Sprintf("%d.jpg", clipID))

	mid := (startS + endS) / 2
	if mid < 0 {
		mid = 0
	}
	if duration.Valid && duration.Float64 > 0 && mid > duration.Float64-0.1 {
		mid = duration.Float64 - 0.1
	}

	if err := renderFrame(ctx, videoPath, mid, out); err != nil {
		return "", err
	}

	stored := out
	if rel, err := filepath.Rel(cfg.Data, out); err == nil && len(rel) > 0 && rel[0] != '.' {
		stored = rel
	}
	if _, err := conn.ExecContext(ctx,
		"UPDATE candidate_clip SET thumb_path = ? WHERE id = ?", stored, clipID); err != nil {
		return "", err
	}
	return out, nil
}

// ForVideo renders a thumbnail at ~10% of the video's duration. The Nuxt
// `/video_thumb/:id` route reads the cached file.
func ForVideo(ctx context.Context, cfg *config.Config, conn *sql.DB, videoID int64) (string, error) {
	var (
		videoPath string
		duration  sql.NullFloat64
	)
	row := conn.QueryRowContext(ctx,
		"SELECT path, duration_s FROM video WHERE id = ?", videoID)
	if err := row.Scan(&videoPath, &duration); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("video id=%d not found", videoID)
		}
		return "", err
	}

	if err := os.MkdirAll(cfg.ThumbsDir, 0o755); err != nil {
		return "", err
	}
	out := filepath.Join(cfg.ThumbsDir, fmt.Sprintf("video_%d.jpg", videoID))
	if st, err := os.Stat(out); err == nil && st.Size() > 0 {
		return out, nil
	}

	seek := 1.0
	if duration.Valid && duration.Float64 > 0 {
		seek = duration.Float64 * 0.10
		if seek > duration.Float64-0.1 {
			seek = duration.Float64 - 0.1
		}
		if seek < 0 {
			seek = 0
		}
	}
	if err := renderFrame(ctx, videoPath, seek, out); err != nil {
		return "", err
	}
	return out, nil
}

func renderFrame(ctx context.Context, src string, seekS float64, out string) error {
	tmp := out + ".partial"
	// Gaming captures are frequently mis-tagged as SMPTE-170M on content
	// that's actually BT.709; override via setparams, then convert to
	// BT.601 for JPEG YCbCr.
	vf := "setparams=color_primaries=bt709:color_trc=bt709:colorspace=bt709:range=tv," +
		"scale=640:-2:flags=lanczos:in_color_matrix=bt709:out_color_matrix=bt601," +
		"format=yuvj420p"
	if err := ffmpeg.Run(ctx,
		"-ss", strconv.FormatFloat(seekS, 'f', 3, 64),
		"-i", src,
		"-frames:v", "1",
		"-vf", vf,
		"-q:v", "4",
		tmp,
	); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, out)
}
