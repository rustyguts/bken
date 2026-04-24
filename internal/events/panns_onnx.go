//go:build onnx

// Package events detects audio events (laughter, shouts, gunfire, cheers)
// via a PANNs CNN14 ONNX model plus an RMS-energy spike detector.
//
// Build with `-tags onnx`. Requires libonnxruntime.so reachable at runtime
// (set BKEN_ONNXRUNTIME to override the default /usr/local/lib/onnxruntime.so)
// and the exported model at {cfg.Models}/panns_cnn14.onnx — run
// `python scripts/export_onnx.py --models ./models` to produce it.
package events

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"

	"github.com/go-audio/wav"
	ort "github.com/yalue/onnxruntime_go"
	"gonum.org/v1/gonum/dsp/fourier"

	"github.com/rustyguts/bken/internal/config"
)

// PANNs CNN14 mel config. Matches panns-inference defaults: 32 kHz,
// n_fft=1024, hop=320, 64 mels, 50 Hz-14 kHz band.
const (
	pannsSR      = 32000
	nFFT         = 1024
	hopSize      = 320
	nMels        = 64
	melFMin      = 50.0
	melFMax      = 14000.0
	windowSec    = 10.0
	windowFrames = 501 // (windowSec * pannsSR - nFFT) / hopSize + 1 -> 500.5, matches export
	hopWindowSec = 1.0
)

// Events we care about. Groups map to AudioSet indices via the labels file
// order (class_labels_indices.csv from AudioSet).
var eventGroups = map[string][]string{
	"Laughter": {"Laughter", "Giggle", "Belly laugh", "Baby laughter"},
	"Shout":    {"Shout", "Yell", "Screaming", "Children shouting"},
	"Cheer":    {"Cheering", "Crowd", "Clapping"},
	"Gunfire":  {"Gunshot, gunfire", "Machine gun", "Fusillade", "Cap gun", "Artillery fire"},
}

var eventThresholds = map[string]float64{
	"Laughter": 0.05,
	"Shout":    0.03,
	"Cheer":    0.05,
	"Gunfire":  0.10,
}

const (
	minEventDuration = 1.0
	maxGapMerge      = 1.5
)

// ErrModelMissing is returned when the ONNX file is absent.
var ErrModelMissing = errors.New("panns_cnn14.onnx missing; run scripts/export_onnx.py first")

func modelPath(cfg *config.Config) string {
	return filepath.Join(cfg.Models, "panns_cnn14.onnx")
}

// Detect runs PANNs + RMS on one video's audio and persists events.
func Detect(ctx context.Context, cfg *config.Config, conn *sql.DB, videoID int64, force bool) error {
	var eventsDone int
	if err := conn.QueryRowContext(ctx, "SELECT events_done FROM video WHERE id = ?", videoID).Scan(&eventsDone); err != nil {
		return err
	}
	if eventsDone == 1 && !force {
		return nil
	}

	mp := modelPath(cfg)
	if _, err := os.Stat(mp); err != nil {
		return fmt.Errorf("%w: %s", ErrModelMissing, mp)
	}

	wavPath := filepath.Join(cfg.AudioDir, fmt.Sprintf("%d.wav", videoID))
	samples16k, err := loadWAVFloat32(wavPath)
	if err != nil {
		return err
	}
	// Input WAV is 16 kHz (matches audio.go); upsample to PANNs' 32 kHz by
	// simple linear interpolation — good enough for SED peak detection.
	samples := resampleLinear(samples16k, 16000, pannsSR)

	if err := ensureORT(); err != nil {
		return err
	}

	labels, err := loadAudioSetLabels(cfg)
	if err != nil {
		return err
	}
	groupIdx := map[string][]int{}
	for g, names := range eventGroups {
		for _, n := range names {
			if idx, ok := labels[n]; ok {
				groupIdx[g] = append(groupIdx[g], idx)
			}
		}
	}

	sess, err := ort.NewDynamicAdvancedSession(mp,
		[]string{"input"}, []string{"output"}, nil)
	if err != nil {
		return fmt.Errorf("onnx session: %w", err)
	}
	defer sess.Destroy()

	// Slide windows of windowSec with hopWindowSec stride.
	winSamples := int(windowSec * pannsSR)
	hopSamples := int(hopWindowSec * pannsSR)
	if len(samples) < winSamples {
		pad := make([]float32, winSamples-len(samples))
		samples = append(samples, pad...)
	}
	var starts []int
	for s := 0; s+winSamples <= len(samples); s += hopSamples {
		starts = append(starts, s)
	}

	groupPeaks := map[string][]float64{}
	for g := range eventGroups {
		groupPeaks[g] = make([]float64, len(starts))
	}

	for i, s := range starts {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		mel := melSpectrogram(samples[s : s+winSamples])
		// Model input shape (1, 64, T). Flatten row-major.
		flat := make([]float32, nMels*windowFrames)
		for m := 0; m < nMels; m++ {
			row := mel[m]
			if len(row) < windowFrames {
				// pad
				for t := 0; t < len(row); t++ {
					flat[m*windowFrames+t] = float32(row[t])
				}
			} else {
				for t := 0; t < windowFrames; t++ {
					flat[m*windowFrames+t] = float32(row[t])
				}
			}
		}
		in, err := ort.NewTensor(ort.NewShape(1, nMels, windowFrames), flat)
		if err != nil {
			return err
		}
		outputs := []ort.Value{nil}
		if err := sess.Run([]ort.Value{in}, outputs); err != nil {
			in.Destroy()
			return fmt.Errorf("onnx run: %w", err)
		}
		probs := outputs[0].(*ort.Tensor[float32]).GetData()
		in.Destroy()
		for g, idxs := range groupIdx {
			peak := 0.0
			for _, idx := range idxs {
				if idx < len(probs) {
					if v := float64(probs[idx]); v > peak {
						peak = v
					}
				}
			}
			groupPeaks[g][i] = peak
		}
		outputs[0].Destroy()
	}

	var allEvents []audioEvent
	for g, peaks := range groupPeaks {
		evs := windowsToEvents(peaks, starts, pannsSR, windowSec, eventThresholds[g])
		for _, e := range evs {
			allEvents = append(allEvents, audioEvent{label: g, start: e.start, end: e.end, score: e.score})
		}
	}
	for _, e := range rmsSpikeEvents(samples, pannsSR) {
		allEvents = append(allEvents, audioEvent{label: "LoudSpike", start: e.start, end: e.end, score: e.score})
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM audio_event WHERE video_id = ?", videoID); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx,
		"INSERT INTO audio_event (video_id, start_s, end_s, label, score) VALUES (?,?,?,?,?)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, e := range allEvents {
		if _, err := stmt.ExecContext(ctx, videoID, e.start, e.end, e.label, e.score); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE video SET events_done=1, updated_at=datetime('now') WHERE id=?", videoID); err != nil {
		return err
	}
	return tx.Commit()
}

// DetectAll iterates every audio_done=1 video.
func DetectAll(ctx context.Context, cfg *config.Config, conn *sql.DB, force bool) error {
	var q string
	if force {
		q = "SELECT id FROM video WHERE audio_done = 1 ORDER BY id"
	} else {
		q = "SELECT id FROM video WHERE audio_done = 1 AND events_done = 0 ORDER BY id"
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
			fmt.Fprintf(os.Stderr, "events id=%d: %v\n", id, err)
		}
	}
	return nil
}

type audioEvent struct {
	label      string
	start, end float64
	score      float64
}

type timeRange struct {
	start, end, score float64
}

func windowsToEvents(peaks []float64, startsSamples []int, sr int, winLen, threshold float64) []timeRange {
	var fires []timeRange
	for i, p := range peaks {
		if p > threshold {
			s := float64(startsSamples[i]) / float64(sr)
			fires = append(fires, timeRange{s, s + winLen, p})
		}
	}
	// Drop very short events (< minEventDuration) unless they'd leave nothing.
	var filtered []timeRange
	for _, e := range fires {
		if e.end-e.start >= minEventDuration {
			filtered = append(filtered, e)
		}
	}
	if len(filtered) == 0 {
		filtered = fires
	}
	return mergeClose(filtered)
}

func mergeClose(events []timeRange) []timeRange {
	if len(events) == 0 {
		return events
	}
	sort.Slice(events, func(i, j int) bool { return events[i].start < events[j].start })
	merged := []timeRange{events[0]}
	for _, e := range events[1:] {
		last := &merged[len(merged)-1]
		if e.start-last.end <= maxGapMerge {
			if e.end > last.end {
				last.end = e.end
			}
			if e.score > last.score {
				last.score = e.score
			}
		} else {
			merged = append(merged, e)
		}
	}
	return merged
}

// rmsSpikeEvents mirrors events.py _rms_spike_events: 400 ms window / 100 ms
// hop, threshold = median(dB) + 2·std.
func rmsSpikeEvents(y []float32, sr int) []timeRange {
	hop := int(float64(sr) * 0.1)
	frame := int(float64(sr) * 0.4)
	if frame <= 0 || len(y) < frame {
		return nil
	}
	var rms []float64
	for i := 0; i+frame <= len(y); i += hop {
		var sum float64
		for _, v := range y[i : i+frame] {
			sum += float64(v) * float64(v)
		}
		rms = append(rms, math.Sqrt(sum/float64(frame)))
	}
	dbVals := make([]float64, len(rms))
	for i, r := range rms {
		dbVals[i] = 20 * math.Log10(r+1e-8)
	}
	med := median(dbVals)
	std := stddev(dbVals, med)
	threshold := med + 2*std

	var events []timeRange
	i := 0
	for i < len(dbVals) {
		if dbVals[i] <= threshold {
			i++
			continue
		}
		j := i
		peak := dbVals[i]
		for j < len(dbVals) && dbVals[j] > threshold {
			if dbVals[j] > peak {
				peak = dbVals[j]
			}
			j++
		}
		start := float64(i*hop) / float64(sr)
		end := float64(j*hop) / float64(sr)
		if end-start >= 0.3 {
			events = append(events, timeRange{start, end, (peak - med) / (std + 1e-6)})
		}
		i = j
	}
	return mergeClose(events)
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	cp := make([]float64, len(xs))
	copy(cp, xs)
	sort.Float64s(cp)
	n := len(cp)
	if n%2 == 1 {
		return cp[n/2]
	}
	return (cp[n/2-1] + cp[n/2]) / 2
}

func stddev(xs []float64, mean float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var s float64
	for _, v := range xs {
		d := v - mean
		s += d * d
	}
	return math.Sqrt(s / float64(len(xs)))
}

func loadWAVFloat32(path string) ([]float32, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec := wav.NewDecoder(f)
	buf, err := dec.FullPCMBuffer()
	if err != nil {
		return nil, err
	}
	out := make([]float32, len(buf.Data))
	scale := float32(1) / 32768.0
	for i, s := range buf.Data {
		out[i] = float32(s) * scale
	}
	return out, nil
}

// resampleLinear upsamples or downsamples by linear interpolation — fine for
// PANNs inference where we care about peak probabilities, not fidelity.
func resampleLinear(in []float32, srIn, srOut int) []float32 {
	if srIn == srOut || len(in) == 0 {
		return in
	}
	ratio := float64(srIn) / float64(srOut)
	outLen := int(float64(len(in)) / ratio)
	out := make([]float32, outLen)
	for i := 0; i < outLen; i++ {
		x := float64(i) * ratio
		i0 := int(x)
		if i0 >= len(in)-1 {
			out[i] = in[len(in)-1]
			continue
		}
		f := float32(x - float64(i0))
		out[i] = in[i0]*(1-f) + in[i0+1]*f
	}
	return out
}

// hann returns a length-n Hann window.
func hann(n int) []float64 {
	w := make([]float64, n)
	if n == 1 {
		w[0] = 1
		return w
	}
	for i := 0; i < n; i++ {
		w[i] = 0.5 * (1 - math.Cos(2*math.Pi*float64(i)/float64(n-1)))
	}
	return w
}

// melFilterbank returns an (nMels, 1+nFFT/2) filterbank (HTK = false, Slaney
// norm, to match librosa defaults used by panns-inference).
func melFilterbank(sr, nFFT, nMels int, fMin, fMax float64) [][]float64 {
	nBins := 1 + nFFT/2
	// mel scale (Slaney / librosa)
	hz2mel := func(hz float64) float64 {
		// Slaney: below 1000 Hz linear, above logarithmic.
		fMinHz := 0.0
		fSp := 200.0 / 3
		minLogHz := 1000.0
		minLogMel := (minLogHz - fMinHz) / fSp
		logstep := math.Log(6.4) / 27.0
		if hz < minLogHz {
			return (hz - fMinHz) / fSp
		}
		return minLogMel + math.Log(hz/minLogHz)/logstep
	}
	mel2hz := func(mel float64) float64 {
		fMinHz := 0.0
		fSp := 200.0 / 3
		minLogHz := 1000.0
		minLogMel := (minLogHz - fMinHz) / fSp
		logstep := math.Log(6.4) / 27.0
		if mel < minLogMel {
			return fMinHz + fSp*mel
		}
		return minLogHz * math.Exp(logstep*(mel-minLogMel))
	}
	mMin := hz2mel(fMin)
	mMax := hz2mel(fMax)
	mels := make([]float64, nMels+2)
	for i := range mels {
		mels[i] = mMin + (mMax-mMin)*float64(i)/float64(nMels+1)
	}
	freqs := make([]float64, len(mels))
	for i, m := range mels {
		freqs[i] = mel2hz(m)
	}
	fftFreqs := make([]float64, nBins)
	for i := 0; i < nBins; i++ {
		fftFreqs[i] = float64(sr) * float64(i) / float64(nFFT)
	}
	fb := make([][]float64, nMels)
	for m := 0; m < nMels; m++ {
		fb[m] = make([]float64, nBins)
		lower := freqs[m]
		center := freqs[m+1]
		upper := freqs[m+2]
		for k := 0; k < nBins; k++ {
			f := fftFreqs[k]
			if f < lower || f > upper {
				continue
			}
			if f <= center {
				fb[m][k] = (f - lower) / (center - lower)
			} else {
				fb[m][k] = (upper - f) / (upper - center)
			}
		}
		// Slaney normalization: area = 2 / (upper - lower)
		norm := 2.0 / (upper - lower)
		for k := range fb[m] {
			fb[m][k] *= norm
		}
	}
	return fb
}

// melSpectrogram computes log-power mel spectrogram of `samples`.
// Returns shape (nMels, nFrames).
func melSpectrogram(samples []float32) [][]float64 {
	win := hann(nFFT)
	fft := fourier.NewFFT(nFFT)
	fb := melFilterbank(pannsSR, nFFT, nMels, melFMin, melFMax)

	// Centered-frame count matches librosa default (center=True).
	nFrames := (len(samples)-nFFT)/hopSize + 1
	if nFrames < 1 {
		nFrames = 1
	}
	// Precompute columns: power spectrum per frame.
	mel := make([][]float64, nMels)
	for m := range mel {
		mel[m] = make([]float64, nFrames)
	}
	buf := make([]float64, nFFT)
	for f := 0; f < nFrames; f++ {
		off := f * hopSize
		for i := 0; i < nFFT; i++ {
			if off+i < len(samples) {
				buf[i] = float64(samples[off+i]) * win[i]
			} else {
				buf[i] = 0
			}
		}
		coeffs := fft.Coefficients(nil, buf)
		// power = |X|^2
		for m := 0; m < nMels; m++ {
			var sum float64
			for k, c := range coeffs {
				p := real(c)*real(c) + imag(c)*imag(c)
				sum += p * fb[m][k]
			}
			mel[m][f] = math.Log(sum + 1e-10)
		}
	}
	return mel
}

// loadAudioSetLabels reads class_labels_indices.csv if present next to the
// model. Expected format: index,mid,display_name (standard AudioSet layout).
// If the file is missing, we fall back to a hard-coded subset that covers
// the labels we actually care about — values are the indices from AudioSet.
func loadAudioSetLabels(cfg *config.Config) (map[string]int, error) {
	path := filepath.Join(cfg.Models, "class_labels_indices.csv")
	if _, err := os.Stat(path); err == nil {
		return parseAudioSetCSV(path)
	}
	// Fallback map of display_name → AudioSet class index (subset).
	// These indices come from class_labels_indices.csv (AudioSet ontology).
	return map[string]int{
		"Laughter":          16,
		"Baby laughter":     17,
		"Giggle":            18,
		"Belly laugh":       20,
		"Shout":             24,
		"Yell":              26,
		"Children shouting": 27,
		"Screaming":         28,
		"Cheering":          39,
		"Crowd":             37,
		"Clapping":          63,
		"Gunshot, gunfire":  427,
		"Machine gun":       428,
		"Fusillade":         429,
		"Cap gun":           431,
		"Artillery fire":    430,
	}, nil
}

// parseAudioSetCSV parses class_labels_indices.csv (index,mid,display_name).
func parseAudioSetCSV(path string) (map[string]int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := map[string]int{}
	start := 0
	line := 0
	for i := 0; i <= len(data); i++ {
		if i == len(data) || data[i] == '\n' {
			row := string(data[start:i])
			start = i + 1
			line++
			if line == 1 || row == "" {
				continue
			}
			// Split on commas, respecting a single quoted field for display_name.
			idxEnd := indexByte(row, ',')
			if idxEnd < 0 {
				continue
			}
			midEnd := indexByteFrom(row, ',', idxEnd+1)
			if midEnd < 0 {
				continue
			}
			idx := parseInt(row[:idxEnd])
			name := row[midEnd+1:]
			name = trimQuotes(name)
			out[name] = idx
		}
	}
	return out, nil
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func indexByteFrom(s string, b byte, from int) int {
	for i := from; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func parseInt(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return n
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func trimQuotes(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
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
