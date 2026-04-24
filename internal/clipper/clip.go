// Package clipper cuts candidate_clip rows out of their source videos with
// ffmpeg. Port of cli/src/bken/clipper.py (with boundary-snap logic lifted
// from cli/src/bken/density.py).
package clipper

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/rustyguts/bken/internal/config"
	"github.com/rustyguts/bken/internal/ffmpeg"
)

// Create cuts one candidate_clip to {cfg.ClipsDir}/{clipID}.mp4 and writes
// the relative stored path back to candidate_clip.clip_path.
func Create(ctx context.Context, cfg *config.Config, conn *sql.DB, clipID int64) error {
	var (
		videoID    int64
		startS     float64
		endS       float64
		videoPath  string
		durationNS sql.NullFloat64
	)
	row := conn.QueryRowContext(ctx, `SELECT c.video_id, c.start_s, c.end_s,
               v.path, v.duration_s
          FROM candidate_clip c
          JOIN video v ON v.id = c.video_id
         WHERE c.id = ?`, clipID)
	if err := row.Scan(&videoID, &startS, &endS, &videoPath, &durationNS); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("clip id=%d not found", clipID)
		}
		return err
	}
	duration := 0.0
	if durationNS.Valid {
		duration = durationNS.Float64
	}

	s, e := padAndClamp(startS, endS, duration, cfg)

	// Snap edges to transcript silence gaps — prevents clips from starting
	// mid-word when a gap lies within ClipSnapWindow of an edge.
	if cfg.ClipSnapWindow > 0 {
		gaps, err := transcriptGaps(ctx, conn, videoID)
		if err == nil && len(gaps) > 0 {
			s, e = snapEdges(s, e, gaps, cfg.ClipSnapWindow)
		}
	}

	if err := os.MkdirAll(cfg.ClipsDir, 0o755); err != nil {
		return err
	}
	out := filepath.Join(cfg.ClipsDir, fmt.Sprintf("%d.mp4", clipID))
	// `.partial.mp4` (not `.mp4.partial`) so ffmpeg can infer the muxer from
	// the extension. Renamed to the final path after successful encode.
	tmp := filepath.Join(cfg.ClipsDir, fmt.Sprintf("%d.partial.mp4", clipID))

	args := []string{
		"-ss", strconv.FormatFloat(s, 'f', 3, 64),
		"-i", videoPath,
		"-t", strconv.FormatFloat(e-s, 'f', 3, 64),
	}
	args = append(args, encodeArgs(cfg)...)
	args = append(args,
		"-c:a", "aac", "-b:a", cfg.ClipAudioBitrate,
		"-movflags", "+faststart",
		"-f", "mp4",
		tmp,
	)

	if err := ffmpeg.Run(ctx, args...); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, out); err != nil {
		return err
	}

	stored := out
	if rel, err := filepath.Rel(cfg.Data, out); err == nil && !startsWith(rel, "..") {
		stored = rel
	}

	_, err := conn.ExecContext(ctx,
		"UPDATE candidate_clip SET clip_path = ? WHERE id = ?", stored, clipID)
	return err
}

func padAndClamp(start, end, duration float64, cfg *config.Config) (float64, float64) {
	s := start - cfg.ClipPadBefore
	if s < 0 {
		s = 0
	}
	e := end + cfg.ClipPadAfter
	if duration > 0 && e > duration {
		e = duration
	}
	length := e - s
	if length < cfg.ClipMinSeconds {
		need := cfg.ClipMinSeconds - length
		s -= need / 2
		e += need / 2
		if s < 0 {
			e -= s
			s = 0
		}
		if duration > 0 && e > duration {
			s -= (e - duration)
			if s < 0 {
				s = 0
			}
			e = duration
		}
	}
	if (e - s) > cfg.ClipMaxSeconds {
		mid := (s + e) / 2
		s = mid - cfg.ClipMaxSeconds/2
		e = mid + cfg.ClipMaxSeconds/2
	}
	return s, e
}

// encodeArgs mirrors Python clipper._video_encode_args: codec-specific knobs
// so libopenh264 (CRF-less) falls back to bitrate mode, etc.
func encodeArgs(cfg *config.Config) []string {
	args := []string{"-c:v", cfg.ClipVideoCodec}
	switch cfg.ClipVideoCodec {
	case "libsvtav1":
		args = append(args,
			"-preset", cfg.ClipVideoPreset,
			"-crf", strconv.Itoa(cfg.ClipVideoCRF),
			"-pix_fmt", "yuv420p",
			"-g", "120",
		)
	case "libaom-av1":
		args = append(args,
			"-crf", strconv.Itoa(cfg.ClipVideoCRF),
			"-cpu-used", cfg.ClipVideoPreset,
			"-row-mt", "1",
			"-pix_fmt", "yuv420p",
			"-b:v", "0",
		)
	case "libvpx-vp9":
		args = append(args,
			"-crf", strconv.Itoa(cfg.ClipVideoCRF),
			"-b:v", "0",
			"-deadline", "good",
			"-cpu-used", cfg.ClipVideoPreset,
			"-row-mt", "1",
			"-pix_fmt", "yuv420p",
		)
	case "libopenh264":
		args = append(args,
			"-b:v", cfg.ClipVideoBitrate,
			"-profile:v", "high",
			"-pix_fmt", "yuv420p",
		)
	case "h264_nvenc":
		args = append(args,
			"-preset", "p7",
			"-rc", "vbr",
			"-cq", strconv.Itoa(cfg.ClipVideoCRF),
			"-b:v", "0",
			"-profile:v", "high",
			"-pix_fmt", "yuv420p",
		)
	case "av1_nvenc":
		args = append(args,
			"-preset", "p7",
			"-rc", "vbr",
			"-cq", strconv.Itoa(cfg.ClipVideoCRF),
			"-b:v", "0",
			"-pix_fmt", "yuv420p",
		)
	default:
		args = append(args,
			"-b:v", cfg.ClipVideoBitrate,
			"-pix_fmt", "yuv420p",
		)
	}
	return args
}

// transcriptGaps returns timestamps (seconds) at the midpoint of every
// silence gap (>=0.2s) between consecutive transcript segments for a video.
func transcriptGaps(ctx context.Context, conn *sql.DB, videoID int64) ([]float64, error) {
	rows, err := conn.QueryContext(ctx,
		"SELECT start_s, end_s FROM transcript_segment WHERE video_id = ? ORDER BY start_s",
		videoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type seg struct{ s, e float64 }
	var segs []seg
	for rows.Next() {
		var s seg
		if err := rows.Scan(&s.s, &s.e); err != nil {
			return nil, err
		}
		segs = append(segs, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var gaps []float64
	for i := 1; i < len(segs); i++ {
		if segs[i].s-segs[i-1].e >= 0.2 {
			gaps = append(gaps, (segs[i-1].e+segs[i].s)/2)
		}
	}
	sort.Float64s(gaps)
	return gaps, nil
}

func snapEdges(s, e float64, gaps []float64, window float64) (float64, float64) {
	snap := func(t float64) float64 {
		best := t
		bestD := window + 1
		for _, g := range gaps {
			d := g - t
			if d < 0 {
				d = -d
			}
			if d <= window && d < bestD {
				best = g
				bestD = d
			}
		}
		return best
	}
	ns := snap(s)
	ne := snap(e)
	if ne <= ns {
		return s, e
	}
	return ns, ne
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
