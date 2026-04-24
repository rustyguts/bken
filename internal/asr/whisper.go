// Package asr does speech-to-text by posting the extracted WAV to a Whisper
// HTTP service. We use onerahmet/openai-whisper-asr-webservice (faster-whisper
// under the hood), which is a single container, CPU or GPU capable, and
// returns segment-level JSON with timestamps. No CGo build tags, no native
// library dependency in the bken binary.
package asr

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rustyguts/bken/internal/config"
)

type segment struct {
	Start      float64 `json:"start"`
	End        float64 `json:"end"`
	Text       string  `json:"text"`
	AvgLogprob float64 `json:"avg_logprob"`
	NoSpeech   float64 `json:"no_speech_prob"`
}

type asrResponse struct {
	Text     string    `json:"text"`
	Segments []segment `json:"segments"`
	Language string    `json:"language"`
}

// Transcribe uploads the cached WAV for videoID to the ASR service and
// persists segments to transcript_segment.
func Transcribe(ctx context.Context, cfg *config.Config, conn *sql.DB, videoID int64, force bool) error {
	var transcribeDone int
	row := conn.QueryRowContext(ctx, "SELECT transcribe_done FROM video WHERE id = ?", videoID)
	if err := row.Scan(&transcribeDone); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("video id=%d not found", videoID)
		}
		return err
	}
	if transcribeDone == 1 && !force {
		return nil
	}

	wavPath := filepath.Join(cfg.AudioDir, fmt.Sprintf("%d.wav", videoID))
	if _, err := os.Stat(wavPath); err != nil {
		return fmt.Errorf("wav missing at %s — run extract-audio first", wavPath)
	}

	resp, err := postASR(ctx, cfg, wavPath)
	if err != nil {
		return err
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, "DELETE FROM transcript_segment WHERE video_id = ?", videoID); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO transcript_segment
		(video_id, start_s, end_s, text, avg_logprob, no_speech)
		VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, s := range resp.Segments {
		if _, err := stmt.ExecContext(ctx,
			videoID,
			s.Start,
			s.End,
			strings.TrimSpace(s.Text),
			s.AvgLogprob,
			s.NoSpeech,
		); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE video SET transcribe_done = 1, updated_at = datetime('now') WHERE id = ?",
		videoID); err != nil {
		return err
	}
	return tx.Commit()
}

// TranscribeAll iterates every video with audio_done=1 needing transcription.
func TranscribeAll(ctx context.Context, cfg *config.Config, conn *sql.DB, force bool) error {
	var q string
	if force {
		q = "SELECT id FROM video WHERE audio_done = 1 ORDER BY id"
	} else {
		q = "SELECT id FROM video WHERE audio_done = 1 AND transcribe_done = 0 ORDER BY id"
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
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if err := Transcribe(ctx, cfg, conn, id, force); err != nil {
			fmt.Fprintf(os.Stderr, "transcribe id=%d: %v\n", id, err)
		}
	}
	return nil
}

// postASR uploads the WAV to /asr and returns parsed segments. Uses a multi-
// hour timeout because large WAVs on CPU can take a while.
func postASR(ctx context.Context, cfg *config.Config, wavPath string) (*asrResponse, error) {
	base := strings.TrimRight(cfg.ASRBaseURL, "/")
	if base == "" {
		base = "http://whisper:9000"
	}

	f, err := os.Open(wavPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	body := &bytes.Buffer{}
	mp := multipart.NewWriter(body)
	part, err := mp.CreateFormFile("audio_file", filepath.Base(wavPath))
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(part, f); err != nil {
		return nil, err
	}
	if err := mp.Close(); err != nil {
		return nil, err
	}

	q := url.Values{}
	q.Set("encode", "true")
	q.Set("task", "transcribe")
	q.Set("output", "json")
	if cfg.ASRLanguage != "" {
		q.Set("language", cfg.ASRLanguage)
	}
	q.Set("word_timestamps", "false")
	q.Set("vad_filter", "true")

	req, err := http.NewRequestWithContext(ctx, "POST", base+"/asr?"+q.Encode(), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mp.FormDataContentType())

	client := &http.Client{Timeout: 2 * time.Hour}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("asr request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("asr %d: %s", resp.StatusCode, truncate(string(raw), 400))
	}

	var out asrResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("parse asr json: %w (raw=%q)", err, truncate(string(raw), 400))
	}
	return &out, nil
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n]
	}
	return s
}
