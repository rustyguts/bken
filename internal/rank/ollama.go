// Package rank summarizes + ranks candidate clips via a local LLM served by
// Ollama. Talks the OpenAI-compatible /v1/chat/completions endpoint so we
// can swap models without code changes. Default model is Qwen2.5 1.5B
// Instruct (Q4) — ~1 GB on disk, ~1.5 GB resident at inference, CPU-friendly.
package rank

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/rustyguts/bken/internal/config"
)

// systemPrompt is copied from cli/src/bken/ranker.py so Python and Go runs
// produce comparable output.
const systemPrompt = `You curate a personal gaming-clip archive. For each candidate moment ` +
	`you see (audio transcript + visual signals), decide whether it's a ` +
	`memorable, funny, or exciting moment and summarize it.

Output ONLY a single JSON array, no prose, no markdown fences. One object per ` +
	`candidate clip, in the same order as the input. Required keys per object:
- "clip_id": integer, echoed from the input
- "rank": integer 1..N, 1 = best (N = number of candidates)
- "title": short punchy title, <= 8 words
- "desc": one sentence explaining why it's memorable (or not)
- "contents": 2-3 sentences narrating what actually happens
- "tags": array of 1-5 short tags like "laugh", "crash", "callout", "death", "kill"`

type rankResponse struct {
	ClipID   int64    `json:"clip_id"`
	Rank     int      `json:"rank"`
	Title    string   `json:"title"`
	Desc     string   `json:"desc"`
	Contents string   `json:"contents"`
	Tags     []string `json:"tags"`
}

type candidate struct {
	ID         int64
	VideoID    int64
	StartS     float64
	EndS       float64
	Score      float64
	Features   sql.NullString
	Transcript sql.NullString
}

// Rank ranks candidate clips for one video.
func Rank(ctx context.Context, cfg *config.Config, conn *sql.DB, videoID int64) error {
	rows, err := conn.QueryContext(ctx,
		`SELECT id, video_id, start_s, end_s, score, features, transcript
		 FROM candidate_clip WHERE video_id=? AND active=1 ORDER BY score DESC`,
		videoID)
	if err != nil {
		return err
	}
	var cands []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.ID, &c.VideoID, &c.StartS, &c.EndS, &c.Score, &c.Features, &c.Transcript); err != nil {
			rows.Close()
			return err
		}
		cands = append(cands, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(cands) == 0 {
		_, err := conn.ExecContext(ctx,
			"UPDATE video SET rank_done=1, updated_at=datetime('now') WHERE id=?", videoID)
		return err
	}

	userPrompt := buildPrompt(ctx, conn, cands)

	raw, err := callOllama(ctx, cfg, systemPrompt, userPrompt)
	if err != nil {
		return err
	}

	parsed, err := extractJSONArray([]byte(raw))
	if err != nil {
		return fmt.Errorf("parse llm output: %w (raw=%q)", err, truncate(raw, 400))
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	byID := map[int64]rankResponse{}
	for _, r := range parsed {
		byID[r.ClipID] = r
	}
	for heuristicRank, c := range cands {
		r, ok := byID[c.ID]
		rank := heuristicRank + 1
		if ok && r.Rank > 0 {
			rank = r.Rank
		}
		tagJSON, _ := json.Marshal(r.Tags)
		if _, err := tx.ExecContext(ctx,
			`UPDATE candidate_clip
			 SET llm_rank=?, llm_title=?, llm_desc=?, llm_contents=?, llm_tags=?
			 WHERE id=?`,
			rank,
			truncate(r.Title, 200),
			truncate(r.Desc, 1000),
			truncate(r.Contents, 2000),
			string(tagJSON),
			c.ID,
		); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE video SET rank_done=1, updated_at=datetime('now') WHERE id=?", videoID); err != nil {
		return err
	}
	return tx.Commit()
}

// RankAll ranks every video with score_done=1 and (rank_done=0 or force).
func RankAll(ctx context.Context, cfg *config.Config, conn *sql.DB, force bool) error {
	var q string
	if force {
		q = "SELECT id FROM video WHERE score_done = 1 ORDER BY id"
	} else {
		q = "SELECT id FROM video WHERE score_done = 1 AND rank_done = 0 ORDER BY id"
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
		if err := Rank(ctx, cfg, conn, id); err != nil {
			fmt.Fprintf(os.Stderr, "rank id=%d: %v\n", id, err)
		}
	}
	return nil
}

func buildPrompt(ctx context.Context, conn *sql.DB, cands []candidate) string {
	var b strings.Builder
	b.WriteString("Candidates:\n\n")
	for _, c := range cands {
		ocrTexts := visionOCRFor(ctx, conn, c.VideoID, c.StartS, c.EndS)
		caption := visionCaptionFor(ctx, conn, c.VideoID, c.StartS, c.EndS)
		fmt.Fprintf(&b, "clip_id=%d @ %.0f-%.0fs (%.0fs), heuristic_score=%.2f\n",
			c.ID, c.StartS, c.EndS, c.EndS-c.StartS, c.Score)
		if c.Features.Valid && strings.TrimSpace(c.Features.String) != "" {
			fmt.Fprintf(&b, "  signals: %s\n", strings.TrimSpace(c.Features.String))
		}
		if len(ocrTexts) > 0 {
			fmt.Fprintf(&b, "  on-screen text: %s\n", strings.Join(ocrTexts, ", "))
		}
		if caption != "" {
			if len(caption) > 240 {
				caption = caption[:240]
			}
			fmt.Fprintf(&b, "  peak visual: %s\n", caption)
		}
		tr := ""
		if c.Transcript.Valid {
			tr = strings.TrimSpace(c.Transcript.String)
		}
		if tr == "" {
			tr = "(no speech)"
		}
		fmt.Fprintf(&b, "  transcript: %s\n\n", tr)
	}
	b.WriteString("Return the JSON array now.")
	return b.String()
}

// chatRequest / chatResponse mirror Ollama's OpenAI-compatible chat endpoint.
// (github.com/ollama/ollama/blob/main/docs/openai.md)
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Stream      bool          `json:"stream"`
	Temperature float64       `json:"temperature"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// callOllama posts to /v1/chat/completions and returns the assistant message.
// Retries once on transient failure (e.g. model being pulled on first run).
func callOllama(ctx context.Context, cfg *config.Config, system, user string) (string, error) {
	base := strings.TrimRight(cfg.LLMBaseURL, "/")
	if base == "" {
		base = "http://127.0.0.1:11434"
	}
	model := cfg.LLMModel
	if model == "" {
		model = "qwen2.5:1.5b-instruct"
	}

	body, _ := json.Marshal(chatRequest{
		Model:       model,
		Temperature: 0.2,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	})

	url := base + "/v1/chat/completions"
	// Ollama may pull the model on first call; bump timeout accordingly.
	client := &http.Client{Timeout: 10 * time.Minute}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound && attempt == 0 {
			// Model missing — ask Ollama to pull it and retry once.
			if err := pullModel(ctx, base, model); err != nil {
				return "", fmt.Errorf("pull %s: %w", model, err)
			}
			continue
		}
		if resp.StatusCode/100 != 2 {
			return "", fmt.Errorf("ollama %d: %s", resp.StatusCode, truncate(string(raw), 400))
		}
		var cr chatResponse
		if err := json.Unmarshal(raw, &cr); err != nil {
			return "", fmt.Errorf("unmarshal ollama: %w (raw=%q)", err, truncate(string(raw), 400))
		}
		if cr.Error != nil {
			return "", fmt.Errorf("ollama error: %s", cr.Error.Message)
		}
		if len(cr.Choices) == 0 {
			return "", fmt.Errorf("ollama returned no choices")
		}
		return cr.Choices[0].Message.Content, nil
	}
	return "", fmt.Errorf("ollama call failed: %w", lastErr)
}

// pullModel calls Ollama's native /api/pull endpoint to download a model.
// Streams newline-delimited JSON progress lines; we drain to completion.
func pullModel(ctx context.Context, base, model string) error {
	body, _ := json.Marshal(map[string]any{"name": model, "stream": false})
	ctxPull, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctxPull, "POST", base+"/api/pull", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

func visionOCRFor(ctx context.Context, conn *sql.DB, videoID int64, start, end float64) []string {
	rows, err := conn.QueryContext(ctx,
		`SELECT text, text_upper FROM vision_ocr
		 WHERE video_id=? AND ts_s BETWEEN ? AND ?
		 ORDER BY CASE WHEN text_upper LIKE '%WASTED%' OR text_upper LIKE '%DIED%' OR text_upper LIKE '%VICTORY%' THEN 0 ELSE 1 END,
		          area_frac DESC
		 LIMIT 10`,
		videoID, start, end)
	if err != nil {
		return nil
	}
	defer rows.Close()
	seen := map[string]bool{}
	var out []string
	for rows.Next() {
		var t, tu string
		if err := rows.Scan(&t, &tu); err != nil {
			return out
		}
		key := strings.TrimSpace(tu)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, strings.TrimSpace(t))
	}
	return out
}

func visionCaptionFor(ctx context.Context, conn *sql.DB, videoID int64, start, end float64) string {
	peak := (start + end) / 2
	var cap sql.NullString
	err := conn.QueryRowContext(ctx,
		`SELECT caption FROM vision_frame
		 WHERE video_id=? AND ts_s BETWEEN ? AND ?
		 ORDER BY ABS(ts_s - ?) LIMIT 1`,
		videoID, start, end, peak).Scan(&cap)
	if err != nil || !cap.Valid {
		return ""
	}
	return cap.String
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n]
	}
	return s
}

// extractJSONArray finds the first balanced [...] in the response, tolerating
// markdown fences and prose wrappers some small models emit.
func extractJSONArray(data []byte) ([]rankResponse, error) {
	s := string(data)
	s = strings.ReplaceAll(s, "```json", "")
	s = strings.ReplaceAll(s, "```", "")
	start := strings.Index(s, "[")
	if start < 0 {
		return nil, fmt.Errorf("no JSON array in response: %q", truncate(s, 200))
	}
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(s); i++ {
		ch := s[i]
		if inStr {
			if esc {
				esc = false
			} else if ch == '\\' {
				esc = true
			} else if ch == '"' {
				inStr = false
			}
			continue
		}
		switch ch {
		case '"':
			inStr = true
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				var out []rankResponse
				if err := json.Unmarshal([]byte(s[start:i+1]), &out); err != nil {
					return nil, err
				}
				return out, nil
			}
		}
	}
	return nil, fmt.Errorf("unbalanced JSON array")
}
