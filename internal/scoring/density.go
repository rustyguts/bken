package scoring

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/rustyguts/bken/internal/config"
)

// Weights mirror density.WEIGHTS exactly. Tune in one place.
var weights = map[string]float64{
	"Laughter":  4.0,
	"Shout":     6.0,
	"Cheer":     3.0,
	"Gunfire":   -1.0,
	"LoudSpike": 0.5,

	"talk_base": 0.08,
	"talk_dens": 0.02,
	"kw":        2.0,
}

// Keywords/phrases that bracket reaction moments in gaming chatter.
var keywords = []string{
	"oh my god", "oh my gosh", "oh fuck", "what the fuck", "wtf", "no way",
	"dude", "bro", "did you see that", "did you just", "what just happened",
	"fucking hell", "holy shit", "holy fuck", "get rekt", "let's go", "lets go",
	"are you kidding", "you idiot", "you bastard", "shut up", "dammit",
	"god damn", "goddamn", "oh no", "nooo", "why", "seriously",
	"what the hell", "incredible", "amazing",
}

var keywordRE = regexp.MustCompile("(?i)" + joinEscaped(keywords))

func joinEscaped(xs []string) string {
	parts := make([]string, len(xs))
	for i, s := range xs {
		parts[i] = regexp.QuoteMeta(s)
	}
	return strings.Join(parts, "|")
}

// posInf is a sentinel for percentile() when signal is all zero.
var posInf = math.Inf(1)

// nmsIoU matches density.py multi_scale_segments call site in scoring.py which
// passes the default 0.5. We use 0.3 per task brief — tighter NMS produces
// more distinct moments on dense videos. This is the only meaningful
// tuning-constant delta from Python.
const nmsIoU = 0.3

const matchIoU = 0.5 // density.match_existing default

type audioEvent struct {
	Start float64
	End   float64
	Label string
	Score float64
}

type transcriptSeg struct {
	Start float64
	End   float64
	Text  string
}

// buildDensity rasterizes events + transcript onto a 1 Hz signal I[t], then
// gaussian-smooths with the given sigma. Reflect padding matches scipy's
// default ``reflect`` mode for gaussian_filter1d.
func buildDensity(durationS float64, events []audioEvent, segs []transcriptSeg, sigma float64) []float32 {
	n := int(durationS) + 1
	if n < 1 {
		n = 1
	}
	I := make([]float32, n)

	for _, ev := range events {
		w, ok := weights[ev.Label]
		if !ok {
			continue
		}
		s := int(ev.Start)
		if s < 0 {
			s = 0
		}
		e := int(ev.End) + 1
		if e > n {
			e = n
		}
		if e <= s {
			continue
		}
		delta := float32(w * ev.Score)
		for i := s; i < e; i++ {
			I[i] += delta
		}
	}

	for _, seg := range segs {
		s := int(seg.Start)
		if s < 0 {
			s = 0
		}
		e := int(seg.End) + 1
		if e > n {
			e = n
		}
		if e <= s {
			continue
		}
		text := strings.TrimSpace(seg.Text)
		span := e - s
		if span < 1 {
			span = 1
		}
		base := weights["talk_base"] + weights["talk_dens"]*float64(len(text))/float64(span)
		b32 := float32(base)
		for i := s; i < e; i++ {
			I[i] += b32
		}
		if text != "" {
			matches := keywordRE.FindAllString(text, -1)
			if len(matches) > 0 {
				inc := float32(weights["kw"] * float64(len(matches)) / float64(span))
				for i := s; i < e; i++ {
					I[i] += inc
				}
			}
		}
	}

	if sigma > 0 {
		I = gaussianSmooth(I, sigma)
	}
	return I
}

// gaussianSmooth convolves I with a 1D Gaussian. `reflect` boundary so edge
// values aren't pulled toward zero. Matches scipy.ndimage.gaussian_filter1d
// defaults (truncate=4.0).
func gaussianSmooth(I []float32, sigma float64) []float32 {
	if sigma <= 0 || len(I) == 0 {
		return I
	}
	// scipy uses radius = int(truncate * sigma + 0.5), truncate=4.0
	radius := int(4.0*sigma + 0.5)
	if radius < 1 {
		radius = 1
	}
	k := make([]float64, 2*radius+1)
	var sum float64
	inv2s2 := 1.0 / (2.0 * sigma * sigma)
	for i := -radius; i <= radius; i++ {
		v := math.Exp(-float64(i*i) * inv2s2)
		k[i+radius] = v
		sum += v
	}
	for i := range k {
		k[i] /= sum
	}

	n := len(I)
	out := make([]float32, n)
	for t := 0; t < n; t++ {
		var acc float64
		for j := -radius; j <= radius; j++ {
			idx := reflectIndex(t+j, n)
			acc += k[j+radius] * float64(I[idx])
		}
		out[t] = float32(acc)
	}
	return out
}

// reflectIndex mirrors scipy's `reflect` mode: d c b a | a b c d | d c b a.
func reflectIndex(i, n int) int {
	if n == 1 {
		return 0
	}
	period := 2 * n
	// map into [0, period)
	i = i % period
	if i < 0 {
		i += period
	}
	if i >= n {
		i = period - 1 - i
	}
	return i
}

// hysteresisSegments returns [start, end) ranges where I >= low, gated by a
// peak >= high, and bridging sub-threshold dips shorter than gapTol seconds.
func hysteresisSegments(I []float32, high, low float64, gapTol, minLen, maxLen int) [][2]int {
	if low <= 0 || high <= 0 || len(I) == 0 {
		return nil
	}
	n := len(I)
	aboveLow := make([]bool, n)
	for i, v := range I {
		aboveLow[i] = float64(v) >= low
	}
	if gapTol > 1 {
		aboveLow = binaryClosing(aboveLow, gapTol)
	}

	var out [][2]int
	inR := false
	start := 0
	for t := 0; t < n; t++ {
		if aboveLow[t] && !inR {
			inR = true
			start = t
		} else if !aboveLow[t] && inR {
			inR = false
			if maxIn(I, start, t) >= float32(high) {
				s, e := clampRegion(start, t, I, minLen, maxLen)
				out = append(out, [2]int{s, e})
			}
		}
	}
	if inR && maxIn(I, start, n) >= float32(high) {
		s, e := clampRegion(start, n, I, minLen, maxLen)
		out = append(out, [2]int{s, e})
	}
	return out
}

// binaryClosing = dilation then erosion with a flat structuring element of
// width `w`. Bridges `<= w` runs of false inside a run of true. Python passes
// `structure=np.ones(w)`; scipy's default origin is 0 (centered).
func binaryClosing(a []bool, w int) []bool {
	if w <= 1 || len(a) == 0 {
		return a
	}
	// Flat SE of size w → radius; scipy handles even sizes by biasing left.
	radL := w / 2
	radR := w - 1 - radL
	n := len(a)
	// Dilation.
	dil := make([]bool, n)
	for i := 0; i < n; i++ {
		lo := i - radL
		if lo < 0 {
			lo = 0
		}
		hi := i + radR
		if hi >= n {
			hi = n - 1
		}
		for j := lo; j <= hi; j++ {
			if a[j] {
				dil[i] = true
				break
			}
		}
	}
	// Erosion.
	out := make([]bool, n)
	for i := 0; i < n; i++ {
		lo := i - radL
		if lo < 0 {
			lo = 0
		}
		hi := i + radR
		if hi >= n {
			hi = n - 1
		}
		keep := true
		for j := lo; j <= hi; j++ {
			if !dil[j] {
				keep = false
				break
			}
		}
		out[i] = keep
	}
	return out
}

func maxIn(I []float32, s, e int) float32 {
	if s >= e || s < 0 || e > len(I) {
		return 0
	}
	m := I[s]
	for i := s + 1; i < e; i++ {
		if I[i] > m {
			m = I[i]
		}
	}
	return m
}

func clampRegion(start, end int, I []float32, minLen, maxLen int) (int, int) {
	n := len(I)
	length := end - start
	if length < minLen {
		need := minLen - length
		left := need / 2
		right := need - left
		newS := start - left
		if newS < 0 {
			newS = 0
		}
		newE := end + right
		if newE > n {
			newE = n
		}
		if newS == 0 {
			newE = newS + minLen
			if newE > n {
				newE = n
			}
		}
		if newE == n {
			newS = newE - minLen
			if newS < 0 {
				newS = 0
			}
		}
		start, end = newS, newE
	}
	if (end - start) > maxLen {
		// center window on peak
		peak := start
		pv := I[start]
		for i := start + 1; i < end; i++ {
			if I[i] > pv {
				pv = I[i]
				peak = i
			}
		}
		start = peak - maxLen/2
		if start < 0 {
			start = 0
		}
		end = start + maxLen
		if end > n {
			end = n
		}
		if (end - start) < maxLen {
			start = end - maxLen
			if start < 0 {
				start = 0
			}
		}
	}
	return start, end
}

// multiScaleSegments runs hysteresis at each (high_pct, low_pct) scale, pools,
// and applies NMS. Region score is sum of I over the region (rewards sustained density).
func multiScaleSegments(I []float32, cfg *config.Config) []Segment {
	minLen := int(cfg.ClipMinSeconds)
	maxLen := int(cfg.ClipMaxSeconds)
	gapTol := cfg.DensityGapTolerance

	var pooled []Segment
	for _, sc := range cfg.DensityScales {
		high := percentilePos(I, sc.HighPct)
		low := percentilePos(I, sc.LowPct)
		if math.IsInf(high, 0) || math.IsInf(low, 0) {
			continue
		}
		for _, rng := range hysteresisSegments(I, high, low, gapTol, minLen, maxLen) {
			s, e := rng[0], rng[1]
			var sum float64
			var peak float32
			for i := s; i < e; i++ {
				sum += float64(I[i])
				if I[i] > peak {
					peak = I[i]
				}
			}
			mean := 0.0
			if e > s {
				mean = sum / float64(e-s)
			}
			pooled = append(pooled, Segment{
				Start: float64(s),
				End:   float64(e),
				Score: sum,
				Features: map[string]any{
					"scale_high_pct": sc.HighPct,
					"scale_low_pct":  sc.LowPct,
					"length_s":       e - s,
					"peak":           float64(peak),
					"mean":           mean,
				},
			})
		}
	}

	kept := nms(pooled, nmsIoU)
	if len(kept) > cfg.TopNCandidates {
		kept = kept[:cfg.TopNCandidates]
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].Start < kept[j].Start })
	return kept
}

// snapToSilence shifts each edge toward the nearest inter-segment gap, up to
// windowS seconds. No-op when no gap is close enough.
func snapToSilence(r Segment, segs []transcriptSeg, windowS float64) Segment {
	if windowS <= 0 || len(segs) < 2 {
		return r
	}
	sorted := make([]transcriptSeg, len(segs))
	copy(sorted, segs)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Start < sorted[j].Start })
	var gaps []float64
	for i := 0; i+1 < len(sorted); i++ {
		if sorted[i+1].Start-sorted[i].End >= 0.2 {
			gaps = append(gaps, (sorted[i].End+sorted[i+1].Start)/2)
		}
	}
	if len(gaps) == 0 {
		return r
	}
	nearest := func(t float64) (float64, bool) {
		bestD := windowS + 1
		var best float64
		found := false
		for _, g := range gaps {
			d := g - t
			if d < 0 {
				d = -d
			}
			if d <= windowS && d < bestD {
				best = g
				bestD = d
				found = true
			}
		}
		return best, found
	}
	s := r.Start
	if v, ok := nearest(r.Start); ok {
		s = v
	}
	e := r.End
	if v, ok := nearest(r.End); ok {
		e = v
	}
	if e <= s {
		return r
	}
	return Segment{Start: s, End: e, Score: r.Score, Features: r.Features}
}

// ──────────────────────────────────────────────────────────────────────────
// DB I/O
// ──────────────────────────────────────────────────────────────────────────

type existingClip struct {
	id          int64
	start       float64
	end         float64
	userRating  sql.NullInt64
	llmTitle    sql.NullString
	llmDesc     sql.NullString
	llmContents sql.NullString
	llmTags     sql.NullString
	llmRank     sql.NullInt64
	title       sql.NullString
	active      int64
}

// Score computes density-based candidates for videoID and writes them into
// candidate_clip. Never touches rows with source='manual'. Applies IoU-based
// preservation of user_rating + LLM metadata. Sets score_done=1 on success.
func Score(ctx context.Context, cfg *config.Config, conn *sql.DB, videoID int64) error {
	var durationS sql.NullFloat64
	if err := conn.QueryRowContext(ctx,
		"SELECT duration_s FROM video WHERE id=?", videoID,
	).Scan(&durationS); err != nil {
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
	if len(events) == 0 && len(segs) == 0 {
		// Valid state (e.g. silent video); set score_done=1 and finish.
		if _, err := conn.ExecContext(ctx,
			"UPDATE video SET score_done=1, updated_at=datetime('now') WHERE id=?", videoID); err != nil {
			return err
		}
		return nil
	}

	I := buildDensity(durationS.Float64, events, segs, cfg.DensitySmoothSigma)
	regions := multiScaleSegments(I, cfg)

	final := make([]Segment, 0, len(regions))
	for _, r := range regions {
		snapped := snapToSilence(r, segs, cfg.ClipSnapWindow)
		if snapped.Score > 0.01 {
			final = append(final, snapped)
		}
	}
	for i := range final {
		final[i].Features["transcript_chars"] = len(transcriptForRegion(segs, final[i].Start, final[i].End))
	}

	existing, err := loadExistingAuto(ctx, conn, videoID)
	if err != nil {
		return err
	}
	matched, unmatchedNew, unmatchedOld := matchExisting(final, existing, matchIoU)

	prevActive := map[int64]bool{}
	for _, e := range existing {
		if e.active == 1 {
			prevActive[e.id] = true
		}
	}
	newActive := map[int64]bool{}

	for _, m := range matched {
		newR, oldR := m.newSeg, m.old
		transcript := transcriptForRegion(segs, newR.Start, newR.End)
		featJSON, _ := json.Marshal(newR.Features)
		if _, err := conn.ExecContext(ctx, `UPDATE candidate_clip
                SET start_s=?, end_s=?, score=?, features=?, transcript=?, active=1
                WHERE id=?`,
			newR.Start, newR.End, newR.Score, string(featJSON), transcript, oldR.id); err != nil {
			return err
		}
		newActive[oldR.id] = true
	}
	for _, n := range unmatchedNew {
		transcript := transcriptForRegion(segs, n.Start, n.End)
		featJSON, _ := json.Marshal(n.Features)
		res, err := conn.ExecContext(ctx, `INSERT INTO candidate_clip
                (video_id, start_s, end_s, score, features, transcript, active, source)
                VALUES (?, ?, ?, ?, ?, ?, 1, 'auto')`,
			videoID, n.Start, n.End, n.Score, string(featJSON), transcript)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		newActive[id] = true
	}
	for _, o := range unmatchedOld {
		if o.active == 1 {
			if _, err := conn.ExecContext(ctx,
				"UPDATE candidate_clip SET active=0 WHERE id=?", o.id); err != nil {
				return err
			}
		}
	}

	changed := len(prevActive) != len(newActive)
	if !changed {
		for id := range prevActive {
			if !newActive[id] {
				changed = true
				break
			}
		}
	}
	if changed && len(newActive) > 0 {
		if _, err := conn.ExecContext(ctx,
			"UPDATE candidate_clip SET llm_rank=NULL WHERE video_id=? AND active=1", videoID); err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx,
			"UPDATE video SET rank_done=0 WHERE id=?", videoID); err != nil {
			return err
		}
	}

	if _, err := conn.ExecContext(ctx,
		"UPDATE video SET score_done=1, updated_at=datetime('now') WHERE id=?", videoID); err != nil {
		return err
	}
	return nil
}

func loadEvents(ctx context.Context, conn *sql.DB, videoID int64) ([]audioEvent, error) {
	rows, err := conn.QueryContext(ctx,
		"SELECT start_s, end_s, label, score FROM audio_event WHERE video_id=? ORDER BY start_s",
		videoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []audioEvent
	for rows.Next() {
		var e audioEvent
		if err := rows.Scan(&e.Start, &e.End, &e.Label, &e.Score); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func loadSegments(ctx context.Context, conn *sql.DB, videoID int64) ([]transcriptSeg, error) {
	rows, err := conn.QueryContext(ctx,
		"SELECT start_s, end_s, text FROM transcript_segment WHERE video_id=? ORDER BY start_s",
		videoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []transcriptSeg
	for rows.Next() {
		var s transcriptSeg
		if err := rows.Scan(&s.Start, &s.End, &s.Text); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func loadExistingAuto(ctx context.Context, conn *sql.DB, videoID int64) ([]existingClip, error) {
	rows, err := conn.QueryContext(ctx,
		`SELECT id, start_s, end_s, user_rating, llm_title, llm_desc, llm_contents, llm_tags, llm_rank, title, active
         FROM candidate_clip WHERE video_id=? AND source='auto'`, videoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []existingClip
	for rows.Next() {
		var c existingClip
		if err := rows.Scan(&c.id, &c.start, &c.end, &c.userRating,
			&c.llmTitle, &c.llmDesc, &c.llmContents, &c.llmTags, &c.llmRank,
			&c.title, &c.active); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func transcriptForRegion(segs []transcriptSeg, start, end float64) string {
	var parts []string
	for _, s := range segs {
		if s.End < start || s.Start > end {
			continue
		}
		txt := strings.TrimSpace(s.Text)
		if txt != "" {
			parts = append(parts, txt)
		}
	}
	out := strings.Join(parts, " ")
	if len(out) > 4000 {
		out = out[:4000]
	}
	return out
}

type matchPair struct {
	newSeg Segment
	old    existingClip
}

// matchExisting: greedy IoU match between new regions and existing rows.
// Identical contract to density.match_existing: returns matched pairs, new
// regions that didn't match, and existing rows that didn't match.
func matchExisting(newSegs []Segment, existing []existingClip, threshold float64) ([]matchPair, []Segment, []existingClip) {
	ranked := make([]Segment, len(newSegs))
	copy(ranked, newSegs)
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].Score > ranked[j].Score })
	available := make([]existingClip, len(existing))
	copy(available, existing)

	var matched []matchPair
	var unmatchedNew []Segment
	for _, n := range ranked {
		bestIdx := -1
		bestIoU := threshold
		for i, row := range available {
			v := iou(n.Start, n.End, row.start, row.end)
			if v < bestIoU {
				continue
			}
			if v > bestIoU {
				bestIdx = i
				bestIoU = v
				continue
			}
			// Tie: prefer rated row, then titled row, then smallest id.
			if bestIdx < 0 {
				bestIdx = i
				continue
			}
			if preferRow(row, available[bestIdx]) {
				bestIdx = i
			}
		}
		if bestIdx >= 0 {
			matched = append(matched, matchPair{newSeg: n, old: available[bestIdx]})
			available = append(available[:bestIdx], available[bestIdx+1:]...)
		} else {
			unmatchedNew = append(unmatchedNew, n)
		}
	}
	return matched, unmatchedNew, available
}

// preferRow reproduces density.py tie-break: rated > titled > smallest-id.
// Returns true if a is preferred over b.
func preferRow(a, b existingClip) bool {
	aRated := a.userRating.Valid
	bRated := b.userRating.Valid
	if aRated != bRated {
		return aRated
	}
	aTitle := a.llmTitle.Valid && a.llmTitle.String != ""
	bTitle := b.llmTitle.Valid && b.llmTitle.String != ""
	if aTitle != bTitle {
		return aTitle
	}
	return a.id < b.id
}
