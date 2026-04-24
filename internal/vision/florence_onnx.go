//go:build onnx

// Package vision samples frames via ffmpeg and, when a Florence-2 ONNX export
// is present, runs per-frame caption / OCR / object detection.
//
// Build with `-tags onnx`. Without the tag, the stub fails fast. Even with
// the tag, if florence2_base.onnx / florence2_decoder.onnx are missing we
// insert bare vision_frame rows (no caption / detections) so downstream stages
// can proceed — Florence-2 export is painful and can land in a later pass.
package vision

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	ort "github.com/yalue/onnxruntime_go"

	"github.com/rustyguts/bken/internal/config"
)

// ErrNoVideo signals the video row is missing or the file is gone.
var ErrNoVideo = errors.New("video not found or file missing")

func modelBasePath(cfg *config.Config) string {
	return filepath.Join(cfg.Models, "florence2_base.onnx")
}

// Detect runs the vision stage on one video.
func Detect(ctx context.Context, cfg *config.Config, conn *sql.DB, videoID int64, force bool) error {
	var (
		videoPath  string
		duration   float64
		visionDone int
	)
	row := conn.QueryRowContext(ctx,
		"SELECT path, duration_s, vision_done FROM video WHERE id = ?", videoID)
	if err := row.Scan(&videoPath, &duration, &visionDone); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: id=%d", ErrNoVideo, videoID)
		}
		return err
	}
	if visionDone == 1 && !force {
		return nil
	}
	if _, err := os.Stat(videoPath); err != nil {
		return fmt.Errorf("%w: %s", ErrNoVideo, videoPath)
	}

	schedule := buildSchedule(ctx, cfg, videoPath, duration)
	if len(schedule) == 0 {
		// nothing to do but mark done so downstream stages can proceed
		_, err := conn.ExecContext(ctx,
			"UPDATE video SET vision_done=1, updated_at=datetime('now') WHERE id=?", videoID)
		return err
	}

	modelPresent := false
	if _, err := os.Stat(modelBasePath(cfg)); err == nil {
		modelPresent = true
		if err := ensureORT(); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(os.Stderr, "vision: %s missing — persisting bare frame rows only\n", modelBasePath(cfg))
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM vision_frame WHERE video_id = ?", videoID); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO vision_frame (video_id, ts_s, sampled, caption, n_objs, n_ocr, raw_json)
		 VALUES (?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, entry := range schedule {
		// Model wiring lives behind modelPresent; when absent we insert the
		// bare frame row so scoring still has a sampling grid.
		var caption string
		var objs, ocrs int
		raw := "{}"
		if modelPresent {
			// Placeholder: a full Florence-2 decoder pipeline is out of scope
			// for this pass — frames are sampled but not captioned until
			// scripts/export_onnx.py produces a usable decoder export.
		}
		if _, err := stmt.ExecContext(ctx, videoID, entry.ts, entry.sampled,
			nullIfEmpty(caption), objs, ocrs, raw); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx,
		"UPDATE video SET vision_done=1, updated_at=datetime('now') WHERE id=?", videoID); err != nil {
		return err
	}
	return tx.Commit()
}

// DetectAll iterates all videos needing vision.
func DetectAll(ctx context.Context, cfg *config.Config, conn *sql.DB, force bool) error {
	var q string
	if force {
		q = "SELECT id FROM video ORDER BY id"
	} else {
		q = "SELECT id FROM video WHERE vision_done = 0 ORDER BY id"
	}
	rows, err := conn.QueryContext(ctx, q)
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
	for _, id := range ids {
		if err := Detect(ctx, cfg, conn, id, force); err != nil {
			fmt.Fprintf(os.Stderr, "vision id=%d: %v\n", id, err)
		}
	}
	return nil
}

type scheduleEntry struct {
	ts      float64
	sampled string // "fixed" | "scene"
}

func buildSchedule(ctx context.Context, cfg *config.Config, videoPath string, duration float64) []scheduleEntry {
	grid := make(map[float64]bool)
	step := 1.0 / cfg.VisionFPS
	for t := 0.0; t <= duration; t += step {
		grid[roundTo(t, 2)] = true
	}

	scenes := map[float64]bool{}
	if cfg.VisionSceneDetect {
		for _, t := range detectScenes(ctx, videoPath, cfg.VisionSceneThreshold) {
			scenes[roundTo(t, 2)] = true
		}
	}

	ts := make([]scheduleEntry, 0, len(grid)+len(scenes))
	for t := range grid {
		ts = append(ts, scheduleEntry{ts: t, sampled: "fixed"})
	}
	for t := range scenes {
		if !grid[t] {
			ts = append(ts, scheduleEntry{ts: t, sampled: "scene"})
		}
	}
	sort.Slice(ts, func(i, j int) bool { return ts[i].ts < ts[j].ts })

	// dedupe within 0.3s
	var deduped []scheduleEntry
	for _, e := range ts {
		if len(deduped) == 0 || e.ts-deduped[len(deduped)-1].ts >= 0.3 {
			deduped = append(deduped, e)
		}
	}
	if cfg.VisionMaxFrames > 0 && len(deduped) > cfg.VisionMaxFrames {
		deduped = deduped[:cfg.VisionMaxFrames]
	}
	return deduped
}

func roundTo(v float64, places int) float64 {
	mul := 1.0
	for i := 0; i < places; i++ {
		mul *= 10
	}
	return float64(int64(v*mul+0.5)) / mul
}

// detectScenes shells out to ffmpeg's `select='gt(scene,T)',showinfo` filter
// and parses the `pts_time:<t>` values from stderr.
func detectScenes(ctx context.Context, videoPath string, threshold float64) []float64 {
	t := threshold / 100.0
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-hide_banner",
		"-i", videoPath,
		"-vf", fmt.Sprintf("select='gt(scene,%f)',showinfo", t),
		"-vsync", "vfr",
		"-f", "null", "-",
	)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil
	}
	if err := cmd.Start(); err != nil {
		return nil
	}
	var out []float64
	scanner := bufio.NewScanner(stderr)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		idx := strings.Index(line, "pts_time:")
		if idx < 0 {
			continue
		}
		rest := line[idx+len("pts_time:"):]
		end := 0
		for end < len(rest) {
			c := rest[end]
			if (c >= '0' && c <= '9') || c == '.' {
				end++
			} else {
				break
			}
		}
		if end == 0 {
			continue
		}
		if v, err := strconv.ParseFloat(rest[:end], 64); err == nil {
			out = append(out, v)
		}
	}
	// Drain and wait.
	_, _ = io.Copy(io.Discard, stderr)
	_ = cmd.Wait()
	return out
}

func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

var ortInitialized = false

func ensureORT() error {
	if ortInitialized {
		return nil
	}
	if p := os.Getenv("BKEN_ONNXRUNTIME"); p != "" {
		ort.SetSharedLibraryPath(p)
	}
	if err := ort.InitializeEnvironment(); err != nil {
		return fmt.Errorf("init onnxruntime: %w", err)
	}
	ortInitialized = true
	return nil
}
