package web

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/rustyguts/bken/internal/queue"
)

// sortOption is one entry in the `/api/clips?sort=` whitelist.
type sortOption struct {
	key    string
	order  string
	label  string
}

var clipSortOptions = []sortOption{
	{"llm_rank", "c.llm_rank ASC NULLS LAST, c.score DESC", "Best (LLM rank)"},
	{"recorded_desc", "v.recorded_at DESC, c.start_s ASC", "Newest recording"},
	{"recorded_asc", "v.recorded_at ASC, c.start_s ASC", "Oldest recording"},
	{"rating_desc", "c.user_rating DESC NULLS LAST, c.score DESC", "Highest user rating"},
	{"rating_asc", "c.user_rating ASC NULLS LAST, c.score DESC", "Lowest user rating"},
	{"score_desc", "c.score DESC", "Heuristic score"},
}

const clipDefaultSort = "llm_rank"

func (d *Deps) listClips(c echo.Context) error {
	ctx := c.Request().Context()
	q := c.QueryParams()

	game := q.Get("game")
	sortKey := q.Get("sort")
	var order string
	for _, so := range clipSortOptions {
		if so.key == sortKey {
			order = so.order
			break
		}
	}
	if order == "" {
		sortKey = clipDefaultSort
		for _, so := range clipSortOptions {
			if so.key == sortKey {
				order = so.order
				break
			}
		}
	}
	minRating := parseIntClamp(q.Get("min_rating"), 0, 0, 5)

	wheres := []string{"c.clip_path IS NOT NULL", "COALESCE(c.active, 1) = 1"}
	args := []any{}
	if game != "" {
		wheres = append(wheres, "v.game = ?")
		args = append(args, game)
	}
	if minRating > 0 {
		wheres = append(wheres, "COALESCE(c.user_rating, 0) >= ?")
		args = append(args, minRating)
	}

	sqlText := fmt.Sprintf("%s WHERE %s ORDER BY %s",
		ClipSelect, strings.Join(wheres, " AND "), order)
	rows, err := queryClipRows(ctx, d.DB, sqlText, args...)
	if err != nil {
		return err
	}
	clips, err := EnrichClipsWithVision(ctx, d.DB, rows)
	if err != nil {
		return err
	}

	opts := make([]map[string]string, 0, len(clipSortOptions))
	for _, so := range clipSortOptions {
		opts = append(opts, map[string]string{"key": so.key, "label": so.label})
	}

	return c.JSON(http.StatusOK, echo.Map{
		"clips":         clips,
		"sort_options":  opts,
		"default_sort":  clipDefaultSort,
	})
}

func (d *Deps) listGames(c echo.Context) error {
	ctx := c.Request().Context()
	rows, err := d.DB.QueryContext(ctx, `
		SELECT v.game AS game,
		       COUNT(c.id) AS n,
		       AVG(c.user_rating) AS avg_rating,
		       SUM(CASE WHEN c.user_rating IS NOT NULL THEN 1 ELSE 0 END) AS rated
		  FROM video v
		  LEFT JOIN candidate_clip c
		         ON c.video_id = v.id
		        AND c.clip_path IS NOT NULL
		        AND COALESCE(c.active, 1) = 1
		 WHERE v.game IS NOT NULL
		 GROUP BY v.game
		 ORDER BY n DESC, v.game ASC`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type gameRow struct {
		Game      string   `json:"game"`
		N         int64    `json:"n"`
		AvgRating *float64 `json:"avg_rating"`
		Rated     int64    `json:"rated"`
	}
	out := []gameRow{}
	for rows.Next() {
		var g gameRow
		var avg sql.NullFloat64
		if err := rows.Scan(&g.Game, &g.N, &avg, &g.Rated); err != nil {
			return err
		}
		if avg.Valid {
			v := avg.Float64
			g.AvgRating = &v
		}
		out = append(out, g)
	}
	return c.JSON(http.StatusOK, echo.Map{"games": out})
}

func (d *Deps) listJobs(c echo.Context) error {
	ctx := c.Request().Context()
	// Tolerate fresh DBs that never ran a job by handling the table-missing
	// case the same way the Nuxt handler did.
	var exists int
	if err := d.DB.QueryRowContext(ctx,
		`SELECT 1 FROM sqlite_master WHERE type='table' AND name='job'`).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return c.JSON(http.StatusOK, echo.Map{"jobs": []any{}})
		}
		return err
	}

	rows, err := d.DB.QueryContext(ctx, `
		SELECT j.id, j.video_id, j.library_id, j.type, j.status, j.target_id,
		       j.error_message, j.created_at, j.updated_at,
		       v.path, v.game, l.name, j.progress
		  FROM job j
		  LEFT JOIN video v ON v.id = j.video_id
		  LEFT JOIN library l ON l.id = j.library_id
		 ORDER BY j.created_at DESC
		 LIMIT 100`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type jobDto struct {
		ID           int64   `json:"id"`
		VideoID      *int64  `json:"video_id"`
		LibraryID    *int64  `json:"library_id"`
		VideoName    *string `json:"video_name"`
		LibraryName  *string `json:"library_name"`
		Game         *string `json:"game"`
		Type         string  `json:"type"`
		Status       string  `json:"status"`
		TargetID     *int64  `json:"target_id"`
		Progress     int     `json:"progress"`
		ErrorMessage *string `json:"error_message"`
		CreatedAt    string  `json:"created_at"`
		UpdatedAt    string  `json:"updated_at"`
	}
	out := []jobDto{}
	for rows.Next() {
		var (
			id                               int64
			vid, lid, tgt                    sql.NullInt64
			typ, status, created, updated    string
			errMsg, vpath, game, libName     sql.NullString
			progress                         int
		)
		if err := rows.Scan(&id, &vid, &lid, &typ, &status, &tgt,
			&errMsg, &created, &updated, &vpath, &game, &libName, &progress); err != nil {
			return err
		}
		j := jobDto{
			ID: id, Type: typ, Status: status, Progress: progress,
			CreatedAt: created, UpdatedAt: updated,
		}
		if vid.Valid {
			v := vid.Int64
			j.VideoID = &v
		}
		if lid.Valid {
			v := lid.Int64
			j.LibraryID = &v
		}
		if tgt.Valid {
			v := tgt.Int64
			j.TargetID = &v
		}
		if errMsg.Valid {
			v := errMsg.String
			j.ErrorMessage = &v
		}
		if vpath.Valid {
			v := filepath.Base(vpath.String)
			j.VideoName = &v
		}
		if game.Valid {
			v := game.String
			j.Game = &v
		}
		if libName.Valid {
			v := libName.String
			j.LibraryName = &v
		}
		out = append(out, j)
	}
	return c.JSON(http.StatusOK, echo.Map{"jobs": out})
}

func (d *Deps) listVideos(c echo.Context) error {
	ctx := c.Request().Context()
	q := c.QueryParams()
	showMissing := q.Get("missing") == "1" || q.Get("missing") == "true"

	var sb strings.Builder
	sb.WriteString(`SELECT v.id, v.path, v.game, v.duration_s, v.width, v.height,
		v.recorded_at, v.missing, v.library_id,
		v.audio_done, v.transcribe_done, v.events_done, v.vision_done, v.score_done, v.rank_done,
		v.created_at, v.updated_at,
		(SELECT COUNT(*) FROM candidate_clip c WHERE c.video_id=v.id AND c.active=1) AS moment_count,
		(SELECT COUNT(*) FROM candidate_clip c WHERE c.video_id=v.id AND c.active=1 AND c.clip_path IS NOT NULL) AS clip_count
		FROM video v WHERE 1=1`)

	args := []any{}
	if !showMissing {
		sb.WriteString(" AND v.missing = 0")
	}
	if libID := q.Get("library_id"); libID != "" {
		if n, err := strconv.ParseInt(libID, 10, 64); err == nil {
			sb.WriteString(" AND v.library_id = ?")
			args = append(args, n)
		}
	}
	if qs := q.Get("q"); qs != "" {
		sb.WriteString(" AND v.path LIKE ?")
		args = append(args, "%"+qs+"%")
	}
	if game := q.Get("game"); game != "" {
		sb.WriteString(" AND v.game = ?")
		args = append(args, game)
	}
	if from := q.Get("date_from"); from != "" {
		sb.WriteString(" AND v.recorded_at >= ?")
		args = append(args, from)
	}
	if to := q.Get("date_to"); to != "" {
		sb.WriteString(" AND v.recorded_at <= ?")
		args = append(args, to+"T23:59:59")
	}
	if mm := q.Get("min_moments"); mm != "" {
		if n, err := strconv.ParseInt(mm, 10, 64); err == nil {
			sb.WriteString(" AND (SELECT COUNT(*) FROM candidate_clip c WHERE c.video_id=v.id AND c.active=1) >= ?")
			args = append(args, n)
		}
	}

	allowed := map[string]string{
		"recorded_at":  "v.recorded_at",
		"duration_s":   "v.duration_s",
		"moment_count": "moment_count",
		"name":         "v.path",
		"created_at":   "v.created_at",
	}
	sort := q.Get("sort")
	col, ok := allowed[sort]
	if !ok {
		col = "v.recorded_at"
	}
	order := "DESC"
	if q.Get("order") == "asc" {
		order = "ASC"
	}
	sb.WriteString(" ORDER BY " + col + " " + order)

	rows, err := d.DB.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	type status struct {
		Audio      bool `json:"audio"`
		Transcribe bool `json:"transcribe"`
		Events     bool `json:"events"`
		Vision     bool `json:"vision"`
		Score      bool `json:"score"`
		Rank       bool `json:"rank"`
	}
	type videoDto struct {
		ID          int64    `json:"id"`
		Path        string   `json:"path"`
		Name        string   `json:"name"`
		Game        *string  `json:"game"`
		DurationS   *float64 `json:"duration_s"`
		Width       *int64   `json:"width"`
		Height      *int64   `json:"height"`
		RecordedAt  *string  `json:"recorded_at"`
		Missing     bool     `json:"missing"`
		LibraryID   *int64   `json:"library_id"`
		Status      status   `json:"status"`
		MomentCount int64    `json:"moment_count"`
		ClipCount   int64    `json:"clip_count"`
		ThumbURL    string   `json:"thumb_url"`
		CreatedAt   string   `json:"created_at"`
		UpdatedAt   string   `json:"updated_at"`
	}
	out := []videoDto{}
	for rows.Next() {
		var (
			id                                                                int64
			path                                                              string
			game, recorded                                                    sql.NullString
			dur                                                               sql.NullFloat64
			width, height, libID                                              sql.NullInt64
			missing, audio, transcribe, ev, vis, score, rnk                   int64
			created, updated                                                  string
			momentCount, clipCount                                            int64
		)
		if err := rows.Scan(&id, &path, &game, &dur, &width, &height,
			&recorded, &missing, &libID,
			&audio, &transcribe, &ev, &vis, &score, &rnk,
			&created, &updated, &momentCount, &clipCount); err != nil {
			return err
		}
		v := videoDto{
			ID: id, Path: path, Name: filepath.Base(path),
			Missing:     missing == 1,
			Status:      status{audio == 1, transcribe == 1, ev == 1, vis == 1, score == 1, rnk == 1},
			MomentCount: momentCount,
			ClipCount:   clipCount,
			ThumbURL:    fmt.Sprintf("/video_thumb/%d.jpg", id),
			CreatedAt:   created, UpdatedAt: updated,
		}
		if game.Valid {
			v.Game = &game.String
		}
		if dur.Valid {
			v.DurationS = &dur.Float64
		}
		if width.Valid {
			v.Width = &width.Int64
		}
		if height.Valid {
			v.Height = &height.Int64
		}
		if recorded.Valid {
			v.RecordedAt = &recorded.String
		}
		if libID.Valid {
			v.LibraryID = &libID.Int64
		}
		out = append(out, v)
	}
	return c.JSON(http.StatusOK, echo.Map{"videos": out})
}

func (d *Deps) getVideo(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := parsePositive(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid video id")
	}
	row := d.DB.QueryRowContext(ctx, `SELECT id, path, game, duration_s, width, height, fps,
		video_codec, audio_codec, recorded_at,
		audio_done, transcribe_done, events_done, vision_done, score_done, rank_done,
		created_at, updated_at
		FROM video WHERE id=?`, id)
	var (
		vid                                             int64
		path                                            string
		game, vcodec, acodec, recorded                  sql.NullString
		dur, fps                                        sql.NullFloat64
		width, height                                   sql.NullInt64
		audio, transcribe, ev, vis, score, rnk          int64
		created, updated                                string
	)
	if err := row.Scan(&vid, &path, &game, &dur, &width, &height, &fps,
		&vcodec, &acodec, &recorded,
		&audio, &transcribe, &ev, &vis, &score, &rnk, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "video not found")
		}
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{
		"id":           vid,
		"path":         path,
		"name":         filepath.Base(path),
		"game":         nullableString(game),
		"duration_s":   nullableFloat(dur),
		"width":        nullableInt(width),
		"height":       nullableInt(height),
		"fps":          nullableFloat(fps),
		"video_codec":  nullableString(vcodec),
		"audio_codec":  nullableString(acodec),
		"recorded_at":  nullableString(recorded),
		"status": echo.Map{
			"audio":      audio == 1,
			"transcribe": transcribe == 1,
			"events":     ev == 1,
			"vision":     vis == 1,
			"score":      score == 1,
			"rank":       rnk == 1,
		},
		"created_at": created,
		"updated_at": updated,
	})
}

func (d *Deps) listVideoMoments(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := parsePositive(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid video id")
	}
	rows, err := d.DB.QueryContext(ctx, `SELECT id, video_id, start_s, end_s, score,
		title, llm_rank, llm_title, llm_desc, llm_contents, llm_tags,
		transcript, clip_path, clip_stale, thumb_path, user_rating, source, features
		FROM candidate_clip
		WHERE video_id = ? AND active = 1
		ORDER BY start_s ASC`, id)
	if err != nil {
		return err
	}
	defer rows.Close()
	type momentDto struct {
		ID          int64   `json:"id"`
		VideoID     int64   `json:"video_id"`
		StartS      float64 `json:"start_s"`
		EndS        float64 `json:"end_s"`
		Duration    float64 `json:"duration"`
		Score       float64 `json:"score"`
		Title       string  `json:"title"`
		LLMRank     *int64  `json:"llm_rank"`
		LLMTitle    *string `json:"llm_title"`
		LLMDesc     *string `json:"llm_desc"`
		LLMContents *string `json:"llm_contents"`
		LLMTags     *string `json:"llm_tags"`
		Transcript  *string `json:"transcript"`
		ClipPath    *string `json:"clip_path"`
		ClipStale   bool    `json:"clip_stale"`
		ThumbPath   *string `json:"thumb_path"`
		UserRating  *int64  `json:"user_rating"`
		Source      string  `json:"source"`
		Features    *string `json:"features"`
	}
	out := []momentDto{}
	for rows.Next() {
		var (
			mid, vid                         int64
			startS, endS, score              float64
			title, llmTitle, llmDesc         sql.NullString
			llmContents, llmTags, transcript sql.NullString
			clipPath, thumbPath, features    sql.NullString
			llmRank, userRating              sql.NullInt64
			clipStale                        int64
			source                           string
		)
		if err := rows.Scan(&mid, &vid, &startS, &endS, &score,
			&title, &llmRank, &llmTitle, &llmDesc, &llmContents, &llmTags,
			&transcript, &clipPath, &clipStale, &thumbPath, &userRating, &source, &features); err != nil {
			return err
		}
		t := title.String
		if t == "" {
			t = llmTitle.String
		}
		if t == "" {
			mins := int(startS) / 60
			secs := int(startS) % 60
			t = fmt.Sprintf("Moment @ %d:%02d", mins, secs)
		}
		m := momentDto{
			ID: mid, VideoID: vid,
			StartS: startS, EndS: endS, Duration: endS - startS,
			Score: score, Title: t,
			ClipStale: clipStale == 1,
			Source:    source,
		}
		if llmRank.Valid {
			m.LLMRank = &llmRank.Int64
		}
		if userRating.Valid {
			m.UserRating = &userRating.Int64
		}
		if llmTitle.Valid {
			m.LLMTitle = &llmTitle.String
		}
		if llmDesc.Valid {
			m.LLMDesc = &llmDesc.String
		}
		if llmContents.Valid {
			m.LLMContents = &llmContents.String
		}
		if llmTags.Valid {
			m.LLMTags = &llmTags.String
		}
		if transcript.Valid {
			m.Transcript = &transcript.String
		}
		if clipPath.Valid {
			m.ClipPath = &clipPath.String
		}
		if thumbPath.Valid {
			m.ThumbPath = &thumbPath.String
		}
		if features.Valid {
			m.Features = &features.String
		}
		out = append(out, m)
	}
	return c.JSON(http.StatusOK, echo.Map{"moments": out})
}

// reprocessBody is the JSON body for POST /api/videos/:id/reprocess. The
// Nuxt side accepts `from_stage` or `stage`; we accept both.
type reprocessBody struct {
	FromStage string `json:"from_stage"`
	Stage     string `json:"stage"`
}

var validReprocessStages = map[string]bool{
	"full": true, "audio": true, "transcribe": true, "events": true,
	"vision": true, "score": true, "rank": true,
}

// stageResetColumns maps a reprocess entry stage to every `*_done` column
// that must be cleared. Matches cli/src/bken/cli.py::reprocess-video. `full`
// is an alias for `audio` (full pipeline from scratch).
var stageResetColumns = map[string][]string{
	"full":       {"audio_done", "transcribe_done", "events_done", "vision_done", "score_done", "rank_done"},
	"audio":      {"audio_done", "transcribe_done", "events_done", "vision_done", "score_done", "rank_done"},
	"transcribe": {"transcribe_done", "events_done", "vision_done", "score_done", "rank_done"},
	"events":     {"events_done", "score_done", "rank_done"},
	"vision":     {"vision_done", "score_done", "rank_done"},
	"score":      {"score_done", "rank_done"},
	"rank":       {"rank_done"},
}

func (d *Deps) reprocessVideo(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := parsePositive(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid video id")
	}
	var body reprocessBody
	_ = c.Bind(&body)
	stage := body.FromStage
	if stage == "" {
		stage = body.Stage
	}
	if stage == "" {
		stage = "score"
	}
	if !validReprocessStages[stage] {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid stage: "+stage)
	}

	// Clear the flags before enqueueing so the reprocess handler sees a
	// clean slate when it fans out stage tasks.
	cols := stageResetColumns[stage]
	sets := make([]string, 0, len(cols))
	for _, col := range cols {
		sets = append(sets, col+"=0")
	}
	if _, err := d.DB.ExecContext(ctx,
		"UPDATE video SET "+strings.Join(sets, ", ")+" WHERE id=?", id); err != nil {
		return err
	}

	jobID, err := queue.Enqueue(ctx, d.Client, d.DB, queue.TypeReprocess,
		queue.ReprocessPayload{VideoID: id, FromStage: stage})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{
		"ok":         true,
		"video_id":   id,
		"from_stage": stage,
		"job_id":     jobID,
	})
}

type createMomentBody struct {
	VideoID int64   `json:"video_id"`
	StartS  float64 `json:"start_s"`
	EndS    float64 `json:"end_s"`
	Title   string  `json:"title"`
}

func (d *Deps) createMoment(c echo.Context) error {
	ctx := c.Request().Context()
	var b createMomentBody
	if err := c.Bind(&b); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if b.VideoID <= 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid video_id")
	}
	if b.EndS <= b.StartS {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid start/end times")
	}
	title := strings.TrimSpace(b.Title)
	var titleArg any
	if title == "" {
		titleArg = nil
	} else {
		titleArg = title
	}
	res, err := d.DB.ExecContext(ctx, `INSERT INTO candidate_clip
		(video_id, start_s, end_s, score, features, transcript, active, title, source)
		VALUES (?, ?, ?, 0, '{}', '', 1, ?, 'manual')`,
		b.VideoID, b.StartS, b.EndS, titleArg)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	return c.JSON(http.StatusOK, echo.Map{"ok": true, "id": id})
}

type patchMomentBody struct {
	Title  *string  `json:"title"`
	StartS *float64 `json:"start_s"`
	EndS   *float64 `json:"end_s"`
}

func (d *Deps) patchMoment(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := parsePositive(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid moment id")
	}
	var b patchMomentBody
	if err := c.Bind(&b); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	sets := []string{}
	args := []any{}
	if b.Title != nil {
		t := strings.TrimSpace(*b.Title)
		sets = append(sets, "title = ?")
		if t == "" {
			args = append(args, nil)
		} else {
			args = append(args, t)
		}
	}
	if b.StartS != nil {
		sets = append(sets, "start_s = ?")
		args = append(args, *b.StartS)
	}
	if b.EndS != nil {
		sets = append(sets, "end_s = ?")
		args = append(args, *b.EndS)
	}
	if b.StartS != nil || b.EndS != nil {
		// Mark the existing clip stale — the UI hides Download/Share until
		// the user re-runs create_clip.
		sets = append(sets, "clip_stale = CASE WHEN clip_path IS NOT NULL THEN 1 ELSE 0 END")
	}
	if len(sets) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "no fields to update")
	}
	args = append(args, id)
	if _, err := d.DB.ExecContext(ctx,
		"UPDATE candidate_clip SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{"ok": true})
}

func (d *Deps) deleteMoment(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := parsePositive(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid moment id")
	}
	var clipPath sql.NullString
	if err := d.DB.QueryRowContext(ctx,
		"SELECT clip_path FROM candidate_clip WHERE id = ?", id).Scan(&clipPath); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "moment not found")
		}
		return err
	}
	if clipPath.Valid && clipPath.String != "" {
		resolved := d.Cfg.ResolveData(clipPath.String)
		_ = os.Remove(resolved)
	}
	if _, err := d.DB.ExecContext(ctx,
		"DELETE FROM candidate_clip WHERE id = ?", id); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{"ok": true})
}

func (d *Deps) createMomentClip(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := parsePositive(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid moment id")
	}
	var exists int
	if err := d.DB.QueryRowContext(ctx,
		"SELECT 1 FROM candidate_clip WHERE id = ?", id).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "moment not found")
		}
		return err
	}
	// Clear the stale flag optimistically, same as the Nuxt handler.
	if _, err := d.DB.ExecContext(ctx,
		"UPDATE candidate_clip SET clip_stale = 0 WHERE id = ?", id); err != nil {
		return err
	}
	jobID, err := queue.Enqueue(ctx, d.Client, d.DB, queue.TypeCreateClip,
		queue.CreateClipPayload{ClipID: id})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{
		"ok":         true,
		"moment_id":  id,
		"job_id":     jobID,
	})
}

func (d *Deps) rateClip(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := parsePositive(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid clip id")
	}
	rating := parseIntClamp(c.QueryParam("rating"), 0, 0, 5)

	var exists int
	if err := d.DB.QueryRowContext(ctx,
		"SELECT 1 FROM candidate_clip WHERE id=?", id).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "clip not found")
		}
		return err
	}
	var val any
	if rating == 0 {
		val = nil
	} else {
		val = rating
	}
	if _, err := d.DB.ExecContext(ctx,
		"UPDATE candidate_clip SET user_rating=? WHERE id=?", val, id); err != nil {
		return err
	}
	row := d.DB.QueryRowContext(ctx, ClipSelect+" WHERE c.id=?", id)
	r, err := scanClipRow(row)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, ClipToDto(r))
}

func (d *Deps) clipVision(c echo.Context) error {
	ctx := c.Request().Context()
	clipID, err := parsePositive(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid clip id")
	}
	var (
		videoID       int64
		startS, endS  float64
	)
	if err := d.DB.QueryRowContext(ctx,
		"SELECT video_id, start_s, end_s FROM candidate_clip WHERE id = ?", clipID,
	).Scan(&videoID, &startS, &endS); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "Clip not found")
		}
		return err
	}

	frameRows, err := d.DB.QueryContext(ctx,
		`SELECT id, ts_s, sampled, caption FROM vision_frame
		 WHERE video_id = ? AND ts_s BETWEEN ? AND ? ORDER BY ts_s`,
		videoID, startS, endS)
	if err != nil {
		return err
	}
	type frameRec struct {
		ID      int64
		TsS     float64
		Sampled string
		Caption sql.NullString
	}
	var frames []frameRec
	for frameRows.Next() {
		var f frameRec
		if err := frameRows.Scan(&f.ID, &f.TsS, &f.Sampled, &f.Caption); err != nil {
			frameRows.Close()
			return err
		}
		frames = append(frames, f)
	}
	frameRows.Close()

	if len(frames) == 0 {
		return c.JSON(http.StatusOK, echo.Map{"frames": []any{}})
	}

	ids := make([]any, len(frames))
	for i, f := range frames {
		ids[i] = f.ID
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]

	type ocrItem struct {
		Text    string   `json:"text"`
		Conf    *float64 `json:"conf"`
		BboxX1  float64  `json:"bbox_x1"`
		BboxY1  float64  `json:"bbox_y1"`
		BboxX2  float64  `json:"bbox_x2"`
		BboxY2  float64  `json:"bbox_y2"`
		AreaFrac *float64 `json:"area_frac"`
	}
	ocrByFrame := make(map[int64][]ocrItem)
	ocrRows, err := d.DB.QueryContext(ctx,
		fmt.Sprintf(`SELECT frame_id, text, conf, bbox_x1, bbox_y1, bbox_x2, bbox_y2, area_frac
			FROM vision_ocr WHERE frame_id IN (%s) ORDER BY frame_id, conf DESC`, placeholders),
		ids...)
	if err != nil {
		return err
	}
	for ocrRows.Next() {
		var fid int64
		var it ocrItem
		var conf, area sql.NullFloat64
		if err := ocrRows.Scan(&fid, &it.Text, &conf, &it.BboxX1, &it.BboxY1, &it.BboxX2, &it.BboxY2, &area); err != nil {
			ocrRows.Close()
			return err
		}
		if conf.Valid {
			it.Conf = &conf.Float64
		}
		if area.Valid {
			it.AreaFrac = &area.Float64
		}
		ocrByFrame[fid] = append(ocrByFrame[fid], it)
	}
	ocrRows.Close()

	type detItem struct {
		Label    string   `json:"label"`
		Conf     float64  `json:"conf"`
		BboxX1   float64  `json:"bbox_x1"`
		BboxY1   float64  `json:"bbox_y1"`
		BboxX2   float64  `json:"bbox_x2"`
		BboxY2   float64  `json:"bbox_y2"`
		AreaFrac *float64 `json:"area_frac"`
	}
	detByFrame := make(map[int64][]detItem)
	detRows, err := d.DB.QueryContext(ctx,
		fmt.Sprintf(`SELECT frame_id, label, conf, bbox_x1, bbox_y1, bbox_x2, bbox_y2, area_frac
			FROM vision_detection WHERE frame_id IN (%s) ORDER BY frame_id, conf DESC`, placeholders),
		ids...)
	if err != nil {
		return err
	}
	for detRows.Next() {
		var fid int64
		var it detItem
		var area sql.NullFloat64
		if err := detRows.Scan(&fid, &it.Label, &it.Conf, &it.BboxX1, &it.BboxY1, &it.BboxX2, &it.BboxY2, &area); err != nil {
			detRows.Close()
			return err
		}
		if area.Valid {
			it.AreaFrac = &area.Float64
		}
		detByFrame[fid] = append(detByFrame[fid], it)
	}
	detRows.Close()

	type frameDto struct {
		TsS        float64   `json:"ts_s"`
		Sampled    string    `json:"sampled"`
		Caption    *string   `json:"caption"`
		OCR        []ocrItem `json:"ocr"`
		Detections []detItem `json:"detections"`
	}
	out := make([]frameDto, 0, len(frames))
	for _, f := range frames {
		fd := frameDto{TsS: f.TsS, Sampled: f.Sampled,
			OCR: ocrByFrame[f.ID], Detections: detByFrame[f.ID]}
		if fd.OCR == nil {
			fd.OCR = []ocrItem{}
		}
		if fd.Detections == nil {
			fd.Detections = []detItem{}
		}
		if f.Caption.Valid {
			fd.Caption = &f.Caption.String
		}
		out = append(out, fd)
	}
	return c.JSON(http.StatusOK, echo.Map{"frames": out})
}

func (d *Deps) searchObject(c echo.Context) error {
	ctx := c.Request().Context()
	label := strings.ToLower(c.QueryParam("label"))
	if label == "" {
		// Fallback: Nuxt doc said `q` in the header comment, but the code
		// reads `label`. Accept both to match operator intent.
		label = strings.ToLower(c.QueryParam("q"))
	}
	if label == "" {
		return c.JSON(http.StatusOK, echo.Map{"clips": []any{}})
	}
	minCount := parseIntClamp(c.QueryParam("min_count"), 1, 1, 1_000_000)

	sqlText := ClipSelect + `
	JOIN (
	  SELECT c.id AS clip_id, COUNT(*) AS cnt
	  FROM candidate_clip c
	  JOIN vision_detection d ON d.video_id = c.video_id
	    AND d.ts_s BETWEEN c.start_s AND c.end_s
	  WHERE c.clip_path IS NOT NULL
	    AND COALESCE(c.active, 1) = 1
	    AND LOWER(d.label) = ?
	  GROUP BY c.id
	  HAVING cnt >= ?
	) matches ON matches.clip_id = c.id
	ORDER BY c.score DESC`
	rows, err := queryClipRows(ctx, d.DB, sqlText, label, minCount)
	if err != nil {
		return err
	}
	clips, err := EnrichClipsWithVision(ctx, d.DB, rows)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{"clips": clips})
}

func (d *Deps) searchOCR(c echo.Context) error {
	ctx := c.Request().Context()
	q := strings.ToUpper(c.QueryParam("q"))
	if q == "" {
		return c.JSON(http.StatusOK, echo.Map{"clips": []any{}})
	}
	sqlText := ClipSelect + `
	JOIN vision_ocr o ON o.video_id = c.video_id
	  AND o.ts_s BETWEEN c.start_s AND c.end_s
	WHERE c.clip_path IS NOT NULL
	  AND COALESCE(c.active, 1) = 1
	  AND o.text_upper LIKE ?
	GROUP BY c.id
	ORDER BY c.score DESC`
	rows, err := queryClipRows(ctx, d.DB, sqlText, "%"+q+"%")
	if err != nil {
		return err
	}
	clips, err := EnrichClipsWithVision(ctx, d.DB, rows)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{"clips": clips})
}

func (d *Deps) listLibraries(c echo.Context) error {
	ctx := c.Request().Context()
	rows, err := d.DB.QueryContext(ctx, `
		SELECT l.id, l.name, l.path, l.scan_interval_minutes,
		       l.last_scan_at, l.next_scan_at, l.active, l.created_at,
		       COUNT(v.id) AS video_count,
		       SUM(CASE WHEN v.missing = 1 THEN 1 ELSE 0 END) AS missing_count
		  FROM library l
		  LEFT JOIN video v ON v.library_id = l.id
		 GROUP BY l.id
		 ORDER BY l.name`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type libDto struct {
		ID                  int64   `json:"id"`
		Name                string  `json:"name"`
		Path                string  `json:"path"`
		ScanIntervalMinutes int64   `json:"scan_interval_minutes"`
		LastScanAt          *string `json:"last_scan_at"`
		NextScanAt          *string `json:"next_scan_at"`
		Active              bool    `json:"active"`
		VideoCount          int64   `json:"video_count"`
		MissingCount        int64   `json:"missing_count"`
		CreatedAt           *string `json:"created_at"`
	}
	out := []libDto{}
	for rows.Next() {
		var l libDto
		var lastScan, nextScan, created sql.NullString
		var active int64
		var missing sql.NullInt64
		if err := rows.Scan(&l.ID, &l.Name, &l.Path, &l.ScanIntervalMinutes,
			&lastScan, &nextScan, &active, &created, &l.VideoCount, &missing); err != nil {
			return err
		}
		l.Active = active == 1
		if missing.Valid {
			l.MissingCount = missing.Int64
		}
		if lastScan.Valid {
			l.LastScanAt = &lastScan.String
		}
		if nextScan.Valid {
			l.NextScanAt = &nextScan.String
		}
		if created.Valid {
			l.CreatedAt = &created.String
		}
		out = append(out, l)
	}
	return c.JSON(http.StatusOK, echo.Map{"libraries": out})
}

type createLibraryBody struct {
	Name                string `json:"name"`
	Path                string `json:"path"`
	ScanIntervalMinutes *int   `json:"scan_interval_minutes"`
}

func (d *Deps) createLibrary(c echo.Context) error {
	ctx := c.Request().Context()
	var b createLibraryBody
	if err := c.Bind(&b); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if strings.TrimSpace(b.Path) == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "path is required")
	}
	st, err := os.Stat(b.Path)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "path does not exist or is not accessible")
	}
	if !st.IsDir() {
		return echo.NewHTTPError(http.StatusBadRequest, "path must be a directory")
	}
	abs, err := filepath.Abs(b.Path)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var existing int64
	if err := d.DB.QueryRowContext(ctx,
		"SELECT id FROM library WHERE path = ?", abs).Scan(&existing); err == nil {
		return echo.NewHTTPError(http.StatusConflict, "library already exists for this path")
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	interval := 60
	if b.ScanIntervalMinutes != nil && *b.ScanIntervalMinutes > 0 {
		interval = *b.ScanIntervalMinutes
	}
	name := strings.TrimSpace(b.Name)
	if name == "" {
		name = filepath.Base(abs)
	}
	res, err := d.DB.ExecContext(ctx,
		"INSERT INTO library (name, path, scan_interval_minutes) VALUES (?, ?, ?)",
		name, abs, interval)
	if err != nil {
		return err
	}
	libID, _ := res.LastInsertId()

	if _, err := queue.Enqueue(ctx, d.Client, d.DB, queue.TypeLibrarySync,
		queue.LibrarySyncPayload{LibraryID: libID}); err != nil {
		// Persist the library even if enqueue fails; caller can retry sync.
		c.Logger().Warnf("enqueue initial library_sync for %d: %v", libID, err)
	}
	return c.JSON(http.StatusOK, echo.Map{"ok": true, "id": libID})
}

func (d *Deps) deleteLibrary(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := parsePositive(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid library id")
	}
	var name string
	if err := d.DB.QueryRowContext(ctx,
		"SELECT name FROM library WHERE id = ?", id).Scan(&name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "library not found")
		}
		return err
	}
	if _, err := d.DB.ExecContext(ctx,
		"UPDATE library SET active = 0 WHERE id = ?", id); err != nil {
		return err
	}
	if _, err := d.DB.ExecContext(ctx,
		`UPDATE video SET missing = 1, updated_at = datetime('now') WHERE library_id = ?`, id); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{"ok": true, "id": id, "name": name})
}

func (d *Deps) syncLibrary(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := parsePositive(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid library id")
	}
	var exists int
	if err := d.DB.QueryRowContext(ctx,
		"SELECT 1 FROM library WHERE id = ?", id).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "library not found")
		}
		return err
	}
	jobID, err := queue.Enqueue(ctx, d.Client, d.DB, queue.TypeLibrarySync,
		queue.LibrarySyncPayload{LibraryID: id})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, echo.Map{
		"ok":         true,
		"library_id": id,
		"job_id":     jobID,
	})
}

// ----- helpers -----

func parsePositive(raw string) (int64, error) {
	// Strip `.mp4`/`.jpg` suffixes for media routes that share the parser.
	s := raw
	if i := strings.LastIndex(s, "."); i > 0 {
		s = s[:i]
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid id: %q", raw)
	}
	return n, nil
}

func parseIntClamp(raw string, def, lo, hi int) int {
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}

func nullableString(ns sql.NullString) any {
	if !ns.Valid {
		return nil
	}
	return ns.String
}

func nullableFloat(nf sql.NullFloat64) any {
	if !nf.Valid {
		return nil
	}
	return nf.Float64
}

func nullableInt(ni sql.NullInt64) any {
	if !ni.Valid {
		return nil
	}
	return ni.Int64
}

