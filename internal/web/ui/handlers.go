package ui

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/rustyguts/bken/internal/config"
	"github.com/rustyguts/bken/internal/library"
	"github.com/rustyguts/bken/internal/queue"
)

// Deps is the subset of runtime state the UI layer needs. Kept separate
// from internal/web.Deps so the import graph stays acyclic.
type Deps struct {
	Cfg *config.Config
	DB  *sql.DB
}

// Register wires the UI routes onto an Echo instance.
func (d *Deps) Register(e *echo.Echo) {
	e.GET("/", d.dashboard)
	e.GET("/video/:id", d.videoEditor)
	e.GET("/libraries", d.libraries)
	e.POST("/libraries", d.librariesCreate)
	e.GET("/jobs", d.jobs)
	e.GET("/sse/jobs", d.sseJobs)
	e.GET("/sse/video/:id/stages", d.sseVideoStages)
	e.GET("/static/*", echo.WrapHandler(http.StripPrefix("/static/", http.FileServer(http.FS(StaticFS)))))
}

// ---- dashboard ----

type videoRow struct {
	ID          int64
	Name        string
	Path        string
	Game        string
	DurationS   float64
	Width       int
	Height      int
	Missing     bool
	ThumbURL    string
	MomentCount int
	Stages      []stageFlag
}

type stageFlag struct {
	Name string
	Done bool
}

type dashboardData struct {
	Title     string
	Heading   string
	Nav       string
	Videos    []videoRow
	Libraries []library.Library
	Games     []string
	Filters   dashboardFilters
	Error     string
}

type dashboardFilters struct {
	Q         string
	LibraryID int64
	Game      string
	Missing   bool
}

func (d *Deps) dashboard(c echo.Context) error {
	ctx := c.Request().Context()
	f := dashboardFilters{
		Q:       strings.TrimSpace(c.QueryParam("q")),
		Game:    strings.TrimSpace(c.QueryParam("game")),
		Missing: c.QueryParam("missing") == "1",
	}
	if lid := c.QueryParam("library_id"); lid != "" {
		f.LibraryID, _ = strconv.ParseInt(lid, 10, 64)
	}

	videos, err := d.queryVideos(ctx, f)
	data := dashboardData{Title: "Assets", Heading: "Assets", Nav: "dashboard", Filters: f, Videos: videos}
	if err != nil {
		data.Error = err.Error()
	}
	libs, _ := library.List(ctx, d.DB)
	data.Libraries = libs
	data.Games, _ = d.queryGames(ctx)
	return render(c, "index", data)
}

func (d *Deps) queryGames(ctx context.Context) ([]string, error) {
	rows, err := d.DB.QueryContext(ctx,
		`SELECT DISTINCT game FROM video WHERE game IS NOT NULL AND game != '' ORDER BY game`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (d *Deps) queryVideos(ctx context.Context, f dashboardFilters) ([]videoRow, error) {
	var (
		clauses []string
		args    []any
	)
	if !f.Missing {
		clauses = append(clauses, "v.missing = 0")
	}
	if f.Q != "" {
		clauses = append(clauses, "v.path LIKE ?")
		args = append(args, "%"+f.Q+"%")
	}
	if f.LibraryID > 0 {
		clauses = append(clauses, "v.library_id = ?")
		args = append(args, f.LibraryID)
	}
	if f.Game != "" {
		clauses = append(clauses, "v.game = ?")
		args = append(args, f.Game)
	}
	where := ""
	if len(clauses) > 0 {
		where = "WHERE " + strings.Join(clauses, " AND ")
	}
	q := `SELECT v.id, v.path, v.game, v.duration_s, v.width, v.height, v.missing,
	        v.audio_done, v.transcribe_done, v.events_done, v.vision_done, v.score_done, v.rank_done,
	        COALESCE((SELECT COUNT(*) FROM candidate_clip c WHERE c.video_id = v.id AND c.active = 1), 0) AS moments
	      FROM video v ` + where + ` ORDER BY v.updated_at DESC LIMIT 500`

	rows, err := d.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []videoRow
	for rows.Next() {
		var (
			id                                                             int64
			path                                                           string
			game                                                           sql.NullString
			duration                                                       sql.NullFloat64
			width, height                                                  sql.NullInt64
			missing                                                        int
			audioDone, trDone, evtDone, visDone, scoreDone, rankDone, mct  int
		)
		if err := rows.Scan(&id, &path, &game, &duration, &width, &height, &missing,
			&audioDone, &trDone, &evtDone, &visDone, &scoreDone, &rankDone, &mct); err != nil {
			return nil, err
		}
		vr := videoRow{
			ID:          id,
			Path:        path,
			Name:        filepath.Base(path),
			Game:        game.String,
			DurationS:   duration.Float64,
			Width:       int(width.Int64),
			Height:      int(height.Int64),
			Missing:     missing == 1,
			ThumbURL:    fmt.Sprintf("/video_thumb/%d", id),
			MomentCount: mct,
			Stages: []stageFlag{
				{"audio", audioDone == 1},
				{"asr", trDone == 1},
				{"events", evtDone == 1},
				{"vision", visDone == 1},
				{"score", scoreDone == 1},
				{"rank", rankDone == 1},
			},
		}
		out = append(out, vr)
	}
	return out, rows.Err()
}

// ---- video editor ----

type videoEditorData struct {
	Title   string
	Heading string
	Nav     string
	Video   videoRow
	Moments []momentRow
	Stages  []pipelineStage
}

// pipelineStage is one row in the per-video pipeline status panel.
// State is "ok" (green), "fail" (red), or "unknown" (grey).
type pipelineStage struct {
	Key     string
	Label   string
	State   string
	Detail  string
	Trigger string // reprocess from_stage value, empty = not triggerable
}

type momentRow struct {
	ID     int64
	Title  string
	StartS float64
	EndS   float64
	Score  float64
	Reason string
	Source string
	Rating int
}

func (d *Deps) videoEditor(c echo.Context) error {
	ctx := c.Request().Context()
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid id")
	}

	var (
		path          string
		game          sql.NullString
		duration      sql.NullFloat64
		width, height sql.NullInt64
		missing       int
		audioDone, trDone, evtDone, visDone, scoreDone, rankDone int
	)
	err = d.DB.QueryRowContext(ctx,
		`SELECT path, game, duration_s, width, height, missing,
		        audio_done, transcribe_done, events_done, vision_done, score_done, rank_done
		 FROM video WHERE id = ?`, id).
		Scan(&path, &game, &duration, &width, &height, &missing,
			&audioDone, &trDone, &evtDone, &visDone, &scoreDone, &rankDone)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "video not found")
		}
		return err
	}
	vr := videoRow{
		ID: id, Path: path, Name: filepath.Base(path), Game: game.String,
		DurationS: duration.Float64, Width: int(width.Int64), Height: int(height.Int64),
		Missing: missing == 1,
	}

	moments, err := d.queryMoments(ctx, id)
	if err != nil {
		return err
	}

	stages := d.pipelineStages(ctx, id, path, missing,
		audioDone, trDone, evtDone, visDone, scoreDone, rankDone, len(moments))

	return render(c, "video", videoEditorData{
		Title: vr.Name, Heading: "Editor", Nav: "dashboard",
		Video: vr, Moments: moments, Stages: stages,
	})
}

// pipelineStages builds the per-stage status list shown below the player.
// "ok" = done + artifacts; "fail" = last job of that stage failed;
// "running" = stage currently executing; "unknown" = not yet attempted.
func (d *Deps) pipelineStages(ctx context.Context, videoID int64, videoPath string, missing int,
	audio, tr, evt, vis, score, rk int, momentCount int) []pipelineStage {

	running := d.runningStages(ctx, videoID)

	// Ingest: is the source file present on disk?
	ingestState, ingestDetail := "unknown", ""
	if missing == 1 {
		ingestState, ingestDetail = "fail", "source file missing on disk"
	} else if videoPath != "" {
		if _, err := os.Stat(videoPath); err == nil {
			ingestState, ingestDetail = "ok", filepath.Base(videoPath)
		} else {
			ingestState, ingestDetail = "fail", "path not accessible"
		}
	}

	// Per-stage: latest job of that type + flag.
	latestErr := d.latestStageErrors(ctx, videoID)

	stateFor := func(done int, jobType string) (string, string) {
		if running[jobType] {
			return "running", "in progress"
		}
		if done == 1 {
			return "ok", ""
		}
		if err, ok := latestErr[jobType]; ok && err != "" {
			return "fail", err
		}
		return "unknown", ""
	}

	counts := d.rowCounts(ctx, videoID)

	audioState, audioDetail := stateFor(audio, queue.TypeExtractAudio)
	if audioState == "ok" {
		audioDetail = "16 kHz mono WAV cached"
	}
	trState, trDetail := stateFor(tr, queue.TypeTranscribe)
	if trState == "ok" {
		trDetail = fmt.Sprintf("%d segments", counts["transcript"])
	}
	evtState, evtDetail := stateFor(evt, queue.TypeDetectEvents)
	if evtState == "ok" {
		evtDetail = fmt.Sprintf("%d audio events", counts["events"])
	}
	visState, visDetail := stateFor(vis, queue.TypeDetectVision)
	if visState == "ok" {
		visDetail = fmt.Sprintf("%d frames / %d OCR / %d detections",
			counts["vision_frames"], counts["vision_ocr"], counts["vision_detections"])
	}
	scoreState, scoreDetail := stateFor(score, queue.TypeScore)
	if scoreState == "ok" {
		scoreDetail = fmt.Sprintf("%d candidate moments", momentCount)
	}

	// Clip stage: how many active candidates have clip_path on disk.
	clipState, clipDetail := "unknown", ""
	if counts["clips_with_path"] > 0 || counts["clips_total"] > 0 {
		if counts["clips_total"] > 0 && counts["clips_with_path"] == counts["clips_total"] {
			clipState, clipDetail = "ok", fmt.Sprintf("%d / %d cut", counts["clips_with_path"], counts["clips_total"])
		} else if counts["clips_with_path"] > 0 {
			clipState, clipDetail = "partial", fmt.Sprintf("%d / %d cut", counts["clips_with_path"], counts["clips_total"])
		} else {
			clipState, clipDetail = "unknown", fmt.Sprintf("0 / %d cut", counts["clips_total"])
		}
	}
	if err, ok := latestErr[queue.TypeCreateClip]; ok && clipState != "ok" && err != "" {
		clipState, clipDetail = "fail", err
	}

	rankState, rankDetail := stateFor(rk, queue.TypeRank)
	if rankState == "ok" {
		rankDetail = fmt.Sprintf("%d ranked", counts["clips_ranked"])
	}

	return []pipelineStage{
		{Key: "ingest", Label: "Ingest", State: ingestState, Detail: ingestDetail, Trigger: ""},
		{Key: "audio", Label: "Extract audio", State: audioState, Detail: audioDetail, Trigger: "audio"},
		{Key: "transcribe", Label: "Transcribe (ASR)", State: trState, Detail: trDetail, Trigger: "transcribe"},
		{Key: "events", Label: "Audio events (PANNs)", State: evtState, Detail: evtDetail, Trigger: "events"},
		{Key: "vision", Label: "Vision (OCR + detect)", State: visState, Detail: visDetail, Trigger: "vision"},
		{Key: "score", Label: "Score moments", State: scoreState, Detail: scoreDetail, Trigger: "score"},
		{Key: "clip", Label: "Cut clips", State: clipState, Detail: clipDetail, Trigger: ""},
		{Key: "rank", Label: "Rank (LLM)", State: rankState, Detail: rankDetail, Trigger: "rank"},
	}
}

// latestStageErrors returns the error_message of the most recent failing
// job for each stage type. `reprocess` jobs carry stage info as a prefix on
// the error (`"transcribe: asr: ..."`) — unpack that into the per-stage map
// so the status panel can render a red badge for the stage that broke.
func (d *Deps) latestStageErrors(ctx context.Context, videoID int64) map[string]string {
	out := map[string]string{}
	rows, err := d.DB.QueryContext(ctx,
		`SELECT type, status, COALESCE(error_message, ''), updated_at
		 FROM job WHERE video_id=? ORDER BY updated_at DESC`, videoID)
	if err != nil {
		return out
	}
	defer rows.Close()

	stageByPrefix := map[string]string{
		"audio":      queue.TypeExtractAudio,
		"transcribe": queue.TypeTranscribe,
		"events":     queue.TypeDetectEvents,
		"vision":     queue.TypeDetectVision,
		"score":      queue.TypeScore,
		"clip":       queue.TypeCreateClip,
		"rank":       queue.TypeRank,
	}
	seen := map[string]bool{}
	for rows.Next() {
		var typ, status, msg, updated string
		if err := rows.Scan(&typ, &status, &msg, &updated); err != nil {
			return out
		}
		if status != "failed" || msg == "" {
			continue
		}
		if typ == queue.TypeReprocess {
			// Split "<stage>: <rest>" — record under the matching stage type
			// only if we haven't already seen a direct job for that stage.
			idx := strings.Index(msg, ":")
			if idx > 0 {
				prefix := strings.TrimSpace(msg[:idx])
				if tkn, ok := stageByPrefix[prefix]; ok && !seen[tkn] {
					out[tkn] = strings.TrimSpace(msg[idx+1:])
					seen[tkn] = true
				}
			}
			continue
		}
		if seen[typ] {
			continue
		}
		seen[typ] = true
		out[typ] = msg
	}
	return out
}

// runningStages scans the job table for the given video and returns a set of
// asynq task types (queue.TypeXxx) that are currently `pending` or `running`.
// Reprocess jobs map onto all stages downstream of their entry point so the
// panel lights up multiple spinners while a full pipeline runs.
func (d *Deps) runningStages(ctx context.Context, videoID int64) map[string]bool {
	out := map[string]bool{}
	rows, err := d.DB.QueryContext(ctx,
		`SELECT type, status FROM job
		 WHERE video_id=? AND status IN ('pending','running')
		 ORDER BY updated_at DESC`, videoID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var typ, status string
		if err := rows.Scan(&typ, &status); err != nil {
			return out
		}
		if typ == queue.TypeReprocess {
			// Unknown which stage is in-flight from the row alone. Mark all
			// stages running so the user sees activity; the next tick will
			// narrow it down once per-stage logs land (we surface running =
			// "something in this video is processing").
			for _, s := range []string{
				queue.TypeExtractAudio, queue.TypeTranscribe, queue.TypeDetectEvents,
				queue.TypeDetectVision, queue.TypeScore, queue.TypeCreateClip, queue.TypeRank,
			} {
				out[s] = true
			}
			continue
		}
		out[typ] = true
	}
	return out
}

// rowCounts batches the count queries the stage panel needs.
func (d *Deps) rowCounts(ctx context.Context, videoID int64) map[string]int {
	out := map[string]int{}
	q := func(sql string) int {
		var n int
		_ = d.DB.QueryRowContext(ctx, sql, videoID).Scan(&n)
		return n
	}
	out["transcript"] = q(`SELECT COUNT(*) FROM transcript_segment WHERE video_id=?`)
	out["events"] = q(`SELECT COUNT(*) FROM audio_event WHERE video_id=?`)
	out["vision_frames"] = q(`SELECT COUNT(*) FROM vision_frame WHERE video_id=?`)
	out["vision_ocr"] = q(`SELECT COUNT(*) FROM vision_ocr WHERE video_id=?`)
	out["vision_detections"] = q(`SELECT COUNT(*) FROM vision_detection WHERE video_id=?`)
	out["clips_total"] = q(`SELECT COUNT(*) FROM candidate_clip WHERE video_id=? AND active=1`)
	out["clips_with_path"] = q(`SELECT COUNT(*) FROM candidate_clip WHERE video_id=? AND active=1 AND clip_path IS NOT NULL AND clip_path != ''`)
	out["clips_ranked"] = q(`SELECT COUNT(*) FROM candidate_clip WHERE video_id=? AND active=1 AND llm_rank IS NOT NULL`)
	return out
}

func (d *Deps) queryMoments(ctx context.Context, videoID int64) ([]momentRow, error) {
	rows, err := d.DB.QueryContext(ctx,
		`SELECT id, COALESCE(title, llm_title, ''), start_s, end_s, score,
		        COALESCE(llm_desc, ''), COALESCE(source, 'auto'), COALESCE(user_rating, 0)
		 FROM candidate_clip WHERE video_id = ? AND active = 1 ORDER BY start_s`, videoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []momentRow
	for rows.Next() {
		var m momentRow
		if err := rows.Scan(&m.ID, &m.Title, &m.StartS, &m.EndS, &m.Score, &m.Reason, &m.Source, &m.Rating); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// sseVideoStages streams pipeline stage HTML for one video. Ticks every
// 1.5s; client swaps #stage-panel via Datastar element-patch. Lightweight:
// ~8 COUNT queries per tick against the small SQLite.
func (d *Deps) sseVideoStages(c echo.Context) error {
	videoID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid id")
	}

	w := c.Response()
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	ctx := c.Request().Context()
	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()

	// Cap stream lifetime so a stale tab can't hog the browser's 6 connection-
	// per-origin slots forever. EventSource auto-reconnects on close, so the
	// UI still sees continuous updates.
	deadline := time.After(30 * time.Second)

	if err := d.writeStagePatch(c, videoID); err != nil {
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-deadline:
			return nil
		case <-ticker.C:
			if err := d.writeStagePatch(c, videoID); err != nil {
				return nil
			}
		}
	}
}

func (d *Deps) writeStagePatch(c echo.Context, videoID int64) error {
	ctx := c.Request().Context()
	// Reload the video row each tick — flags flip as stages finish.
	var (
		path                                                     string
		missing                                                  int
		audio, tr, evt, vis, score, rk                            int
	)
	if err := d.DB.QueryRowContext(ctx,
		`SELECT path, missing, audio_done, transcribe_done, events_done,
		        vision_done, score_done, rank_done
		 FROM video WHERE id=?`, videoID).Scan(
		&path, &missing, &audio, &tr, &evt, &vis, &score, &rk); err != nil {
		return err
	}
	momentCount := 0
	_ = d.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM candidate_clip WHERE video_id=? AND active=1`, videoID).
		Scan(&momentCount)

	stages := d.pipelineStages(ctx, videoID, path, missing, audio, tr, evt, vis, score, rk, momentCount)

	data := struct {
		Video  videoRow
		Stages []pipelineStage
	}{
		Video:  videoRow{ID: videoID},
		Stages: stages,
	}

	t, err := PartialTemplate("stage_panel")
	if err != nil {
		return err
	}
	var body strings.Builder
	if err := t.ExecuteTemplate(&body, "stage_panel", data); err != nil {
		return err
	}

	// Datastar element-patch: prefix each body line with `data: elements `.
	var sse strings.Builder
	sse.WriteString("event: datastar-patch-elements\n")
	for line := range strings.SplitSeq(body.String(), "\n") {
		sse.WriteString("data: elements ")
		sse.WriteString(line)
		sse.WriteByte('\n')
	}
	sse.WriteByte('\n')
	if _, err := c.Response().Write([]byte(sse.String())); err != nil {
		return err
	}
	c.Response().Flush()
	return nil
}

// ---- libraries ----

type librariesData struct {
	Title     string
	Heading   string
	Nav       string
	Libraries []library.Library
}

func (d *Deps) libraries(c echo.Context) error {
	libs, err := library.List(c.Request().Context(), d.DB)
	if err != nil {
		return err
	}
	return render(c, "libraries", librariesData{
		Title: "Libraries", Heading: "Libraries", Nav: "libraries", Libraries: libs,
	})
}

func (d *Deps) librariesCreate(c echo.Context) error {
	name := strings.TrimSpace(c.FormValue("name"))
	path := strings.TrimSpace(c.FormValue("path"))
	if name == "" || path == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name + path required")
	}
	interval, _ := strconv.Atoi(c.FormValue("scan_interval_minutes"))
	if _, err := library.Add(c.Request().Context(), d.DB, name, path, interval); err != nil {
		return err
	}
	return c.Redirect(http.StatusSeeOther, "/libraries")
}

// ---- jobs ----

// jobRow flattens sql.Null* fields so templates print values, not struct dumps.
type jobRow struct {
	ID           int64
	Type         string
	Status       string
	VideoID      int64
	TargetID     int64
	LibraryID    int64
	Progress     int
	ErrorMessage string
	CreatedAt    string
	UpdatedAt    string
}

func toJobRows(jobs []queue.Job) []jobRow {
	out := make([]jobRow, 0, len(jobs))
	for _, j := range jobs {
		r := jobRow{
			ID: j.ID, Type: j.Type, Status: j.Status,
			Progress: j.Progress, CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt,
		}
		if j.VideoID.Valid {
			r.VideoID = j.VideoID.Int64
		}
		if j.TargetID.Valid {
			r.TargetID = j.TargetID.Int64
		}
		if j.LibraryID.Valid {
			r.LibraryID = j.LibraryID.Int64
		}
		if j.ErrorMessage.Valid {
			r.ErrorMessage = j.ErrorMessage.String
		}
		out = append(out, r)
	}
	return out
}

type jobsData struct {
	Title   string
	Heading string
	Nav     string
	Jobs    []jobRow
}

func (d *Deps) jobs(c echo.Context) error {
	jobs, err := queue.ListJobs(d.DB, 100)
	if err != nil {
		return err
	}
	return render(c, "jobs", jobsData{Title: "Jobs", Heading: "Jobs", Nav: "jobs", Jobs: toJobRows(jobs)})
}

// sseJobs streams job table patches every 2s. Uses the Datastar SSE
// element-patch protocol — the client-side datastar runtime swaps #jobs-body
// when it receives a `datastar-patch-elements` event.
func (d *Deps) sseJobs(c echo.Context) error {
	w := c.Response()
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	ctx := c.Request().Context()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	deadline := time.After(30 * time.Second)
	if err := d.writeJobsPatch(c); err != nil {
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-deadline:
			return nil
		case <-ticker.C:
			if err := d.writeJobsPatch(c); err != nil {
				return nil
			}
		}
	}
}

func (d *Deps) writeJobsPatch(c echo.Context) error {
	jobs, err := queue.ListJobs(d.DB, 100)
	if err != nil {
		return err
	}
	var body strings.Builder
	body.WriteString(`<tbody id="jobs-body">`)
	if len(jobs) == 0 {
		body.WriteString(`<tr><td colspan="8" class="text-center text-base-content/60 py-6">No jobs yet.</td></tr>`)
	} else {
		t, err := PartialTemplate("job_row")
		if err != nil {
			return err
		}
		for _, j := range toJobRows(jobs) {
			if err := t.ExecuteTemplate(&body, "job_row", j); err != nil {
				return err
			}
		}
	}
	body.WriteString(`</tbody>`)
	// Datastar v1 element-patch format: one SSE event, `event: datastar-patch-elements`,
	// data lines starting with `elements `.
	payload := "event: datastar-patch-elements\n"
	for _, line := range strings.Split(body.String(), "\n") {
		payload += "data: elements " + line + "\n"
	}
	payload += "\n"
	if _, err := c.Response().Write([]byte(payload)); err != nil {
		return err
	}
	c.Response().Flush()
	return nil
}

// ---- render helper ----

func render(c echo.Context, name string, data any) error {
	var buf strings.Builder
	if err := Render(&buf, name, data); err != nil {
		return err
	}
	return c.HTML(http.StatusOK, buf.String())
}
