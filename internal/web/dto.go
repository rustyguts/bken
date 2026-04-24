// Package web wires the Echo HTTP server that ports the Nuxt API + routes.
//
// DTOs and SQL here are intentionally hand-rolled so the JSON shape stays
// byte-for-byte compatible with `app/server/utils/db.ts::ClipDto`; the Nuxt
// frontend is the consumer.
package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// ClipSelect joins candidate_clip with video for every clip-returning handler.
// Kept as a string so the caller can append WHERE/ORDER/LIMIT freely.
const ClipSelect = `
SELECT c.id, c.video_id, c.start_s, c.end_s, c.score,
       c.features, c.transcript, c.llm_rank, c.llm_title, c.llm_desc,
       c.llm_contents, c.llm_tags, c.clip_path, c.thumb_path, c.user_rating,
       c.active, c.title, c.source,
       v.game, v.recorded_at, v.path AS video_path
  FROM candidate_clip c
  JOIN video v ON v.id = c.video_id
`

// ClipRow is the struct form of one CLIP_SELECT row.
type ClipRow struct {
	ID          int64
	VideoID     int64
	StartS      float64
	EndS        float64
	Score       float64
	Features    sql.NullString
	Transcript  sql.NullString
	LLMRank     sql.NullInt64
	LLMTitle    sql.NullString
	LLMDesc     sql.NullString
	LLMContents sql.NullString
	LLMTags     sql.NullString
	ClipPath    sql.NullString
	ThumbPath   sql.NullString
	UserRating  sql.NullInt64
	Active      sql.NullInt64
	Title       sql.NullString
	Source      string
	Game        sql.NullString
	RecordedAt  sql.NullString
	VideoPath   sql.NullString
}

// DetectionDto is one label + count pair used for top-detection summaries.
type DetectionDto struct {
	Label string `json:"label"`
	Count int64  `json:"count"`
}

// ClipDto is the JSON contract for `/api/clips`, `/api/search/*`, and rate
// responses. Field order matches `app/server/utils/db.ts::ClipDto`.
type ClipDto struct {
	ID            int64          `json:"id"`
	VideoID       int64          `json:"video_id"`
	StartS        float64        `json:"start_s"`
	EndS          float64        `json:"end_s"`
	Duration      float64        `json:"duration"`
	Score         float64        `json:"score"`
	LLMRank       *int64         `json:"llm_rank"`
	Title         string         `json:"title"`
	Reason        string         `json:"reason"`
	Contents      string         `json:"contents"`
	Transcript    string         `json:"transcript"`
	Tags          []string       `json:"tags"`
	Rating        *int64         `json:"rating"`
	Game          string         `json:"game"`
	RecordedAt    string         `json:"recorded_at"`
	VideoName     string         `json:"video_name"`
	ThumbURL      string         `json:"thumb_url"`
	VideoURL      string         `json:"video_url"`
	DownloadURL   string         `json:"download_url"`
	ClipName      string         `json:"clip_name"`
	OCRText       string         `json:"ocr_text"`
	TopDetections []DetectionDto `json:"top_detections"`
	CaptionSample string         `json:"caption_sample"`
}

func scanClipRow(rs interface {
	Scan(dest ...any) error
}) (ClipRow, error) {
	var r ClipRow
	err := rs.Scan(
		&r.ID, &r.VideoID, &r.StartS, &r.EndS, &r.Score,
		&r.Features, &r.Transcript, &r.LLMRank, &r.LLMTitle, &r.LLMDesc,
		&r.LLMContents, &r.LLMTags, &r.ClipPath, &r.ThumbPath, &r.UserRating,
		&r.Active, &r.Title, &r.Source,
		&r.Game, &r.RecordedAt, &r.VideoPath,
	)
	return r, err
}

// queryClipRows runs `sql` with args, returns the materialised rows.
func queryClipRows(ctx context.Context, conn *sql.DB, sqlText string, args ...any) ([]ClipRow, error) {
	rows, err := conn.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ClipRow
	for rows.Next() {
		r, err := scanClipRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// clipToDtoBase fills everything except the vision-derived fields. Mirrors
// `_clipToDtoBase` in db.ts.
func clipToDtoBase(row ClipRow) ClipDto {
	var tags []string
	if row.LLMTags.Valid && row.LLMTags.String != "" {
		// Silently ignore malformed JSON — same as the Nuxt side.
		_ = json.Unmarshal([]byte(row.LLMTags.String), &tags)
	}
	if tags == nil {
		tags = []string{}
	}

	title := row.LLMTitle.String
	if title == "" {
		mins := int(row.StartS) / 60
		secs := int(row.StartS) % 60
		title = fmt.Sprintf("Clip @ %d:%02d", mins, secs)
	}

	game := row.Game.String
	if game == "" {
		game = "Unknown"
	}

	videoName := ""
	if row.VideoPath.Valid && row.VideoPath.String != "" {
		base := filepath.Base(row.VideoPath.String)
		ext := filepath.Ext(base)
		videoName = strings.TrimSuffix(base, ext)
	}

	clipName := ""
	if row.ClipPath.Valid && row.ClipPath.String != "" {
		clipName = filepath.Base(row.ClipPath.String)
	}

	var llmRank *int64
	if row.LLMRank.Valid {
		v := row.LLMRank.Int64
		llmRank = &v
	}
	var rating *int64
	if row.UserRating.Valid {
		v := row.UserRating.Int64
		rating = &v
	}

	return ClipDto{
		ID:            row.ID,
		VideoID:       row.VideoID,
		StartS:        row.StartS,
		EndS:          row.EndS,
		Duration:      row.EndS - row.StartS,
		Score:         row.Score,
		LLMRank:       llmRank,
		Title:         title,
		Reason:        row.LLMDesc.String,
		Contents:      row.LLMContents.String,
		Transcript:    row.Transcript.String,
		Tags:          tags,
		Rating:        rating,
		Game:          game,
		RecordedAt:    row.RecordedAt.String,
		VideoName:     videoName,
		ThumbURL:      fmt.Sprintf("/thumb/%d.jpg", row.ID),
		VideoURL:      fmt.Sprintf("/clip/%d.mp4", row.ID),
		DownloadURL:   fmt.Sprintf("/clip/%d.mp4?download=1", row.ID),
		ClipName:      clipName,
		OCRText:       "",
		TopDetections: []DetectionDto{},
		CaptionSample: "",
	}
}

// ClipToDto returns the bare DTO without vision enrichment, used by /api/rate.
func ClipToDto(row ClipRow) ClipDto {
	return clipToDtoBase(row)
}

// EnrichClipsWithVision attaches OCR text, top detections, and caption per
// clip. Three batched queries, N+M+K rows total. Matches the `IN (?,?,?)`
// expansion the Nuxt code uses.
func EnrichClipsWithVision(ctx context.Context, conn *sql.DB, rows []ClipRow) ([]ClipDto, error) {
	if len(rows) == 0 {
		return []ClipDto{}, nil
	}
	ids := make([]any, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]

	ocrMap := make(map[int64][]string)
	ocrSQL := fmt.Sprintf(`SELECT DISTINCT c.id, o.text
	   FROM candidate_clip c
	   JOIN vision_ocr o ON o.video_id = c.video_id
	     AND o.ts_s BETWEEN c.start_s AND c.end_s
	  WHERE c.id IN (%s)
	  ORDER BY c.id, o.area_frac DESC`, placeholders)
	ocrRows, err := conn.QueryContext(ctx, ocrSQL, ids...)
	if err != nil {
		return nil, err
	}
	for ocrRows.Next() {
		var id int64
		var text string
		if err := ocrRows.Scan(&id, &text); err != nil {
			ocrRows.Close()
			return nil, err
		}
		if len(ocrMap[id]) < 10 {
			ocrMap[id] = append(ocrMap[id], text)
		}
	}
	ocrRows.Close()
	if err := ocrRows.Err(); err != nil {
		return nil, err
	}

	capMap := make(map[int64]string)
	capSQL := fmt.Sprintf(`SELECT c.id, f.caption
	   FROM candidate_clip c
	   JOIN vision_frame f ON f.video_id = c.video_id
	     AND f.ts_s BETWEEN c.start_s AND c.end_s
	  WHERE c.id IN (%s)
	  GROUP BY c.id
	  HAVING ABS(f.ts_s - (c.start_s + c.end_s)/2) = MIN(ABS(f.ts_s - (c.start_s + c.end_s)/2))`,
		placeholders)
	capRows, err := conn.QueryContext(ctx, capSQL, ids...)
	if err != nil {
		return nil, err
	}
	for capRows.Next() {
		var id int64
		var cap sql.NullString
		if err := capRows.Scan(&id, &cap); err != nil {
			capRows.Close()
			return nil, err
		}
		if cap.Valid && cap.String != "" {
			capMap[id] = cap.String
		}
	}
	capRows.Close()
	if err := capRows.Err(); err != nil {
		return nil, err
	}

	detMap := make(map[int64][]DetectionDto)
	detSQL := fmt.Sprintf(`SELECT c.id, d.label, COUNT(*) AS cnt
	   FROM candidate_clip c
	   JOIN vision_detection d ON d.video_id = c.video_id
	     AND d.ts_s BETWEEN c.start_s AND c.end_s
	  WHERE c.id IN (%s)
	  GROUP BY c.id, d.label
	  ORDER BY c.id, cnt DESC`, placeholders)
	detRows, err := conn.QueryContext(ctx, detSQL, ids...)
	if err != nil {
		return nil, err
	}
	for detRows.Next() {
		var id int64
		var label string
		var cnt int64
		if err := detRows.Scan(&id, &label, &cnt); err != nil {
			detRows.Close()
			return nil, err
		}
		if len(detMap[id]) < 5 {
			detMap[id] = append(detMap[id], DetectionDto{Label: label, Count: cnt})
		}
	}
	detRows.Close()
	if err := detRows.Err(); err != nil {
		return nil, err
	}

	out := make([]ClipDto, 0, len(rows))
	for _, r := range rows {
		dto := clipToDtoBase(r)
		dto.OCRText = strings.Join(ocrMap[r.ID], ", ")
		if list, ok := detMap[r.ID]; ok {
			dto.TopDetections = list
		}
		dto.CaptionSample = capMap[r.ID]
		out = append(out, dto)
	}
	return out, nil
}
