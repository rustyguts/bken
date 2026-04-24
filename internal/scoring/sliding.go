package scoring

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/rustyguts/bken/internal/config"
)

// Fixed-window fallback scorer. Ports cli/src/bken/scoring.py Window + NMS.
// Used when density scoring is disabled or for A/B comparisons.

const slidingWindowSec = 30.0
const slidingStepSec = 5.0
const slidingMinGap = 15.0

type slidingWindow struct {
	start float64
	end   float64
	laugh float64
	shout float64
	cheer float64
	gun   float64
	loud  float64
	talk  float64
	kw    int
	spk   float64
}

func (w *slidingWindow) score() float64 {
	return weights["Shout"]*w.shout +
		weights["Laughter"]*w.laugh +
		weights["Cheer"]*w.cheer +
		weights["kw"]*float64(w.kw) +
		weights["LoudSpike"]*w.loud +
		weights["talk_base"]*w.talk +
		weights["talk_dens"]*w.spk +
		weights["Gunfire"]*w.gun
}

// SlidingScore writes candidate_clip rows from a fixed-window scan. Same
// write semantics as Score: source='auto', preserves manual rows, IoU-match
// to keep user_rating and LLM metadata.
func SlidingScore(ctx context.Context, cfg *config.Config, conn *sql.DB, videoID int64) error {
	var durationS sql.NullFloat64
	if err := conn.QueryRowContext(ctx, "SELECT duration_s FROM video WHERE id=?", videoID).Scan(&durationS); err != nil {
		return fmt.Errorf("load video %d: %w", videoID, err)
	}
	if !durationS.Valid || durationS.Float64 <= 0 {
		return nil
	}
	events, err := loadEvents(ctx, conn, videoID)
	if err != nil {
		return err
	}
	segs, err := loadSegments(ctx, conn, videoID)
	if err != nil {
		return err
	}

	windows := buildSlidingWindows(durationS.Float64, events, segs)

	// Rank + NMS with a time-gap guard on top of IoU to match Python's _non_max_suppress.
	sort.SliceStable(windows, func(i, j int) bool { return windows[i].score() > windows[j].score() })
	var kept []slidingWindow
	for _, w := range windows {
		ok := true
		for _, k := range kept {
			if overlap(w.start-slidingMinGap, w.end+slidingMinGap, k.start, k.end) > 0 {
				ok = false
				break
			}
		}
		if ok {
			kept = append(kept, w)
			if len(kept) >= cfg.TopNCandidates {
				break
			}
		}
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].start < kept[j].start })

	final := make([]Segment, 0, len(kept))
	for _, w := range kept {
		sc := w.score()
		if sc <= 0.01 {
			continue
		}
		final = append(final, Segment{
			Start: w.start,
			End:   w.end,
			Score: sc,
			Features: map[string]any{
				"laugh": w.laugh, "shout": w.shout, "cheer": w.cheer,
				"gun": w.gun, "loud": w.loud, "talk": w.talk,
				"kw": w.kw, "spk": w.spk,
			},
		})
	}

	existing, err := loadExistingAuto(ctx, conn, videoID)
	if err != nil {
		return err
	}
	matched, unmatchedNew, unmatchedOld := matchExisting(final, existing, matchIoU)
	for _, m := range matched {
		featJSON, _ := json.Marshal(m.newSeg.Features)
		if _, err := conn.ExecContext(ctx, `UPDATE candidate_clip
                SET start_s=?, end_s=?, score=?, features=?, transcript=?, active=1
                WHERE id=?`,
			m.newSeg.Start, m.newSeg.End, m.newSeg.Score, string(featJSON),
			transcriptForRegion(segs, m.newSeg.Start, m.newSeg.End), m.old.id); err != nil {
			return err
		}
	}
	for _, n := range unmatchedNew {
		featJSON, _ := json.Marshal(n.Features)
		if _, err := conn.ExecContext(ctx, `INSERT INTO candidate_clip
                (video_id, start_s, end_s, score, features, transcript, active, source)
                VALUES (?, ?, ?, ?, ?, ?, 1, 'auto')`,
			videoID, n.Start, n.End, n.Score, string(featJSON),
			transcriptForRegion(segs, n.Start, n.End)); err != nil {
			return err
		}
	}
	for _, o := range unmatchedOld {
		if o.active == 1 {
			if _, err := conn.ExecContext(ctx, "UPDATE candidate_clip SET active=0 WHERE id=?", o.id); err != nil {
				return err
			}
		}
	}

	if _, err := conn.ExecContext(ctx,
		"UPDATE video SET score_done=1, updated_at=datetime('now') WHERE id=?", videoID); err != nil {
		return err
	}
	return nil
}

func buildSlidingWindows(durationS float64, events []audioEvent, segs []transcriptSeg) []slidingWindow {
	if durationS < slidingWindowSec {
		return nil
	}
	var out []slidingWindow
	for start := 0.0; start+slidingWindowSec <= durationS; start += slidingStepSec {
		end := start + slidingWindowSec
		w := slidingWindow{start: start, end: end}
		for _, ev := range events {
			if ev.End < start || ev.Start > end {
				continue
			}
			switch ev.Label {
			case "Laughter":
				w.laugh += ev.Score
			case "Shout":
				w.shout += ev.Score
			case "Cheer":
				w.cheer += ev.Score
			case "Gunfire":
				w.gun += ev.Score
			case "LoudSpike":
				w.loud += ev.Score
			}
		}
		var textParts []string
		for _, s := range segs {
			if s.End < start || s.Start > end {
				continue
			}
			if strings.TrimSpace(s.Text) != "" {
				w.talk += 1
				textParts = append(textParts, s.Text)
			}
		}
		joined := strings.Join(textParts, " ")
		if joined != "" {
			w.spk = float64(len(joined)) / slidingWindowSec
			w.kw = len(keywordRE.FindAllString(joined, -1))
		}
		out = append(out, w)
	}
	return out
}

func overlap(a0, a1, b0, b1 float64) float64 {
	hi := a1
	if b1 < hi {
		hi = b1
	}
	lo := a0
	if b0 > lo {
		lo = b0
	}
	d := hi - lo
	if d < 0 {
		return 0
	}
	return d
}
