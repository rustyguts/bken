package cli

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/rustyguts/bken/internal/asr"
	"github.com/rustyguts/bken/internal/audio"
	"github.com/rustyguts/bken/internal/clipper"
	"github.com/rustyguts/bken/internal/config"
	"github.com/rustyguts/bken/internal/db"
	"github.com/rustyguts/bken/internal/events"
	"github.com/rustyguts/bken/internal/ingest"
	"github.com/rustyguts/bken/internal/library"
	"github.com/rustyguts/bken/internal/queue"
	"github.com/rustyguts/bken/internal/rank"
	"github.com/rustyguts/bken/internal/scoring"
	"github.com/rustyguts/bken/internal/vision"
	"github.com/rustyguts/bken/internal/web"
)

// Stub subcommands; real implementations land in subsequent tasks. Each is
// wired through appCtx so we don't re-parse config per subcommand.

func newInitDBCmd(ctx *appCtx) *cobra.Command {
	return &cobra.Command{
		Use:   "init-db",
		Short: "Create schema + apply migrations",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ctx.cfg.EnsureDirs(); err != nil {
				return err
			}
			conn, err := db.Open(ctx.cfg.DBPath)
			if err != nil {
				return err
			}
			defer conn.Close()
			fmt.Println("db initialized at", ctx.cfg.DBPath)
			return nil
		},
	}
}

func notImplemented(use, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("%s: not implemented yet", use)
		},
	}
}

func newIngestCmd(ctx *appCtx) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ingest [paths...]",
		Short: "Scan paths, probe with ffprobe, persist to DB",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			workers, _ := cmd.Flags().GetInt("workers")
			if err := ctx.cfg.EnsureDirs(); err != nil {
				return err
			}
			conn, err := db.Open(ctx.cfg.DBPath)
			if err != nil {
				return err
			}
			defer conn.Close()
			return ingest.Scan(cmd.Context(), ctx.cfg, conn, args, workers, nil)
		},
	}
	cmd.Flags().IntP("workers", "w", 4, "parallel probe workers")
	return cmd
}

func newExtractAudioCmd(ctx *appCtx) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "extract-audio",
		Short: "Decode each video to 16 kHz mono WAV",
		RunE: func(cmd *cobra.Command, args []string) error {
			workers, _ := cmd.Flags().GetInt("workers")
			force, _ := cmd.Flags().GetBool("force")
			if err := ctx.cfg.EnsureDirs(); err != nil {
				return err
			}
			conn, err := db.Open(ctx.cfg.DBPath)
			if err != nil {
				return err
			}
			defer conn.Close()
			return audio.ExtractAll(cmd.Context(), ctx.cfg, conn, workers, force)
		},
	}
	cmd.Flags().IntP("workers", "w", 4, "parallel extract workers")
	cmd.Flags().Bool("force", false, "re-extract even if audio_done=1")
	return cmd
}

func newTranscribeCmd(ctx *appCtx) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "transcribe",
		Short: "ASR via whisper.cpp",
		RunE: func(cmd *cobra.Command, args []string) error {
			videoID, _ := cmd.Flags().GetInt64("video-id")
			force, _ := cmd.Flags().GetBool("force")
			if err := ctx.cfg.EnsureDirs(); err != nil {
				return err
			}
			conn, err := db.Open(ctx.cfg.DBPath)
			if err != nil {
				return err
			}
			defer conn.Close()
			if videoID > 0 {
				return asr.Transcribe(cmd.Context(), ctx.cfg, conn, videoID, force)
			}
			return asr.TranscribeAll(cmd.Context(), ctx.cfg, conn, force)
		},
	}
	cmd.Flags().Int64("video-id", 0, "transcribe a single video id (0 = all pending)")
	cmd.Flags().Bool("force", false, "re-transcribe even if transcribe_done=1")
	return cmd
}

func newDetectEventsCmd(ctx *appCtx) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "detect-events",
		Short: "PANNs audio-event detection",
		RunE: func(cmd *cobra.Command, args []string) error {
			videoID, _ := cmd.Flags().GetInt64("video-id")
			force, _ := cmd.Flags().GetBool("force")
			if err := ctx.cfg.EnsureDirs(); err != nil {
				return err
			}
			conn, err := db.Open(ctx.cfg.DBPath)
			if err != nil {
				return err
			}
			defer conn.Close()
			if videoID > 0 {
				return events.Detect(cmd.Context(), ctx.cfg, conn, videoID, force)
			}
			return events.DetectAll(cmd.Context(), ctx.cfg, conn, force)
		},
	}
	cmd.Flags().Int64("video-id", 0, "detect events for a single video id (0 = all pending)")
	cmd.Flags().Bool("force", false, "re-detect even if events_done=1")
	return cmd
}

func newDetectVisionCmd(ctx *appCtx) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "detect-vision",
		Short: "Florence-2 frame sampling + OCR",
		RunE: func(cmd *cobra.Command, args []string) error {
			videoID, _ := cmd.Flags().GetInt64("video-id")
			force, _ := cmd.Flags().GetBool("force")
			if err := ctx.cfg.EnsureDirs(); err != nil {
				return err
			}
			conn, err := db.Open(ctx.cfg.DBPath)
			if err != nil {
				return err
			}
			defer conn.Close()
			if videoID > 0 {
				return vision.Detect(cmd.Context(), ctx.cfg, conn, videoID, force)
			}
			return vision.DetectAll(cmd.Context(), ctx.cfg, conn, force)
		},
	}
	cmd.Flags().Int64("video-id", 0, "detect vision for a single video id (0 = all pending)")
	cmd.Flags().Bool("force", false, "re-run even if vision_done=1")
	return cmd
}

func newScoreCmd(ctx *appCtx) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "score",
		Short: "Density-based interestingness scoring",
		RunE: func(cmd *cobra.Command, args []string) error {
			videoID, _ := cmd.Flags().GetInt64("video-id")
			sliding, _ := cmd.Flags().GetBool("sliding")
			if err := ctx.cfg.EnsureDirs(); err != nil {
				return err
			}
			conn, err := db.Open(ctx.cfg.DBPath)
			if err != nil {
				return err
			}
			defer conn.Close()

			scoreFn := scoring.Score
			if sliding {
				scoreFn = scoring.SlidingScore
			}

			if videoID > 0 {
				return scoreFn(cmd.Context(), ctx.cfg, conn, videoID)
			}

			rows, err := conn.QueryContext(cmd.Context(),
				`SELECT id FROM video WHERE transcribe_done=1 AND events_done=1 AND score_done=0`)
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
				if err := scoreFn(cmd.Context(), ctx.cfg, conn, id); err != nil {
					log.Printf("score video %d: %v", id, err)
				}
			}
			fmt.Printf("scored %d videos\n", len(ids))
			return nil
		},
	}
	cmd.Flags().Int64("video-id", 0, "score a single video id (0 = all pending)")
	cmd.Flags().Bool("sliding", false, "use fixed-window fallback scorer")
	return cmd
}

func newRankCmd(ctx *appCtx) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rank",
		Short: "LLM re-ranking via claude CLI",
		RunE: func(cmd *cobra.Command, args []string) error {
			videoID, _ := cmd.Flags().GetInt64("video-id")
			force, _ := cmd.Flags().GetBool("force")
			if err := ctx.cfg.EnsureDirs(); err != nil {
				return err
			}
			conn, err := db.Open(ctx.cfg.DBPath)
			if err != nil {
				return err
			}
			defer conn.Close()
			if videoID > 0 {
				return rank.Rank(cmd.Context(), ctx.cfg, conn, videoID)
			}
			return rank.RankAll(cmd.Context(), ctx.cfg, conn, force)
		},
	}
	cmd.Flags().Int64("video-id", 0, "rank a single video id (0 = all pending)")
	cmd.Flags().Bool("force", false, "re-rank even if rank_done=1")
	return cmd
}

func newCreateClipCmd(ctx *appCtx) *cobra.Command {
	return &cobra.Command{
		Use:   "create-clip <id>",
		Short: "Cut a clip mp4 with ffmpeg",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clipID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid clip id: %w", err)
			}
			if err := ctx.cfg.EnsureDirs(); err != nil {
				return err
			}
			conn, err := db.Open(ctx.cfg.DBPath)
			if err != nil {
				return err
			}
			defer conn.Close()
			return clipper.Create(cmd.Context(), ctx.cfg, conn, clipID)
		},
	}
}

func newReprocessCmd(ctx *appCtx) *cobra.Command {
	return &cobra.Command{
		Use:   "reprocess-video <id> <stage>",
		Short: "Re-run pipeline for a single video from a stage onward",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			videoID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid video id: %w", err)
			}
			stage := args[1]
			if err := ctx.cfg.EnsureDirs(); err != nil {
				return err
			}
			conn, err := db.Open(ctx.cfg.DBPath)
			if err != nil {
				return err
			}
			defer conn.Close()
			return reprocessVideo(cmd.Context(), ctx.cfg, conn, videoID, stage)
		},
	}
}

func newLibraryCmd(ctx *appCtx) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "library",
		Short: "Manage scheduled libraries",
	}

	add := &cobra.Command{
		Use:   "add <name> <path>",
		Short: "Register a library",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			interval, _ := cmd.Flags().GetInt("interval")
			if err := ctx.cfg.EnsureDirs(); err != nil {
				return err
			}
			conn, err := db.Open(ctx.cfg.DBPath)
			if err != nil {
				return err
			}
			defer conn.Close()
			id, err := library.Add(cmd.Context(), conn, args[0], args[1], interval)
			if err != nil {
				return err
			}
			fmt.Printf("library %d added\n", id)
			return nil
		},
	}
	add.Flags().Int("interval", 60, "rescan interval in minutes")

	list := &cobra.Command{
		Use:   "list",
		Short: "List libraries",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ctx.cfg.EnsureDirs(); err != nil {
				return err
			}
			conn, err := db.Open(ctx.cfg.DBPath)
			if err != nil {
				return err
			}
			defer conn.Close()
			libs, err := library.List(cmd.Context(), conn)
			if err != nil {
				return err
			}
			for _, l := range libs {
				active := "active"
				if !l.Active {
					active = "inactive"
				}
				fmt.Printf("%d\t%s\t%s\tinterval=%dm\tlast=%s\tnext=%s\t%s\n",
					l.ID, l.Name, l.Path, l.ScanIntervalMinutes,
					l.LastScanAt.String, l.NextScanAt.String, active)
			}
			return nil
		},
	}

	sync := &cobra.Command{
		Use:   "sync <id>",
		Short: "Rescan a library",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid library id: %w", err)
			}
			workers, _ := cmd.Flags().GetInt("workers")
			if err := ctx.cfg.EnsureDirs(); err != nil {
				return err
			}
			conn, err := db.Open(ctx.cfg.DBPath)
			if err != nil {
				return err
			}
			defer conn.Close()
			ingestFn := func(c context.Context, roots []string, libID *int64) error {
				return ingest.Scan(c, ctx.cfg, conn, roots, workers, libID)
			}
			return library.Sync(cmd.Context(), ctx.cfg, conn, id, ingestFn)
		},
	}
	sync.Flags().IntP("workers", "w", 4, "parallel probe workers")

	del := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a library",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid library id: %w", err)
			}
			if err := ctx.cfg.EnsureDirs(); err != nil {
				return err
			}
			conn, err := db.Open(ctx.cfg.DBPath)
			if err != nil {
				return err
			}
			defer conn.Close()
			return library.Delete(cmd.Context(), conn, id)
		},
	}

	cmd.AddCommand(add, list, sync, del)
	return cmd
}

func newPipelineCmd(ctx *appCtx) *cobra.Command {
	cmd := notImplemented("pipeline <path>", "Run the full pipeline end-to-end on one file")
	cmd.Flags().Bool("single-file", false, "treat path as a single file, stop before clip cutting")
	return cmd
}

func buildDeps(ctx *appCtx) (queue.Deps, error) {
	if err := ctx.cfg.EnsureDirs(); err != nil {
		return queue.Deps{}, err
	}
	conn, err := db.Open(ctx.cfg.DBPath)
	if err != nil {
		return queue.Deps{}, err
	}
	return queue.Deps{
		DB:     conn,
		Config: ctx.cfg,

		Ingest: func(c context.Context, paths []string, workers int) error {
			return ingest.Scan(c, ctx.cfg, conn, paths, workers, nil)
		},
		ExtractAudio: func(c context.Context, videoID int64, force bool) error {
			return audio.Extract(c, ctx.cfg, conn, videoID, force)
		},
		Transcribe: func(c context.Context, videoID int64, force bool) error {
			return asr.Transcribe(c, ctx.cfg, conn, videoID, force)
		},
		DetectEvents: func(c context.Context, videoID int64, force bool) error {
			return events.Detect(c, ctx.cfg, conn, videoID, force)
		},
		DetectVision: func(c context.Context, videoID int64, force bool) error {
			return vision.Detect(c, ctx.cfg, conn, videoID, force)
		},
		Score: func(c context.Context, videoID int64, force bool) error {
			return scoring.Score(c, ctx.cfg, conn, videoID)
		},
		Rank: func(c context.Context, videoID int64, force bool) error {
			return rank.Rank(c, ctx.cfg, conn, videoID)
		},
		CreateClip: func(c context.Context, clipID int64) error {
			return clipper.Create(c, ctx.cfg, conn, clipID)
		},
		Reprocess: func(c context.Context, videoID int64, fromStage string) error {
			return reprocessVideo(c, ctx.cfg, conn, videoID, fromStage)
		},
		LibrarySync: func(c context.Context, libraryID int64) error {
			ingestFn := func(ic context.Context, roots []string, libID *int64) error {
				return ingest.Scan(ic, ctx.cfg, conn, roots, 4, libID)
			}
			return library.Sync(c, ctx.cfg, conn, libraryID, ingestFn)
		},
	}, nil
}

// stageOrder defines the end-to-end pipeline. `reprocess` starts at a named
// stage, resets downstream flags, and re-runs each stage in order. The `clip`
// pseudo-stage cuts any newly created candidate_clip rows; it has no
// `*_done` flag on video — completeness is tracked per-clip via clip_path.
var stageOrder = []string{"audio", "transcribe", "events", "vision", "score", "clip", "rank"}

var stageToFlag = map[string]string{
	"audio":      "audio_done",
	"transcribe": "transcribe_done",
	"events":     "events_done",
	"vision":     "vision_done",
	"score":      "score_done",
	"rank":       "rank_done",
}

// reprocessVideo resets stage flags from `fromStage` onward and runs each
// stage inline. Logs each stage boundary so `docker compose logs bken` shows
// progress; errors are wrapped with the failing stage name.
func reprocessVideo(ctx context.Context, cfg *config.Config, conn *sql.DB, videoID int64, fromStage string) error {
	if fromStage == "" || fromStage == "full" {
		fromStage = "audio"
	}
	start := -1
	for i, s := range stageOrder {
		if s == fromStage {
			start = i
			break
		}
	}
	if start < 0 {
		return fmt.Errorf("unknown stage %q", fromStage)
	}

	sets := make([]string, 0, len(stageOrder)-start)
	for _, s := range stageOrder[start:] {
		if col, ok := stageToFlag[s]; ok {
			sets = append(sets, col+"=0")
		}
	}
	if len(sets) > 0 {
		q := "UPDATE video SET " + strings.Join(sets, ", ") + ", updated_at=datetime('now') WHERE id = ?"
		if _, err := conn.ExecContext(ctx, q, videoID); err != nil {
			return fmt.Errorf("reset flags: %w", err)
		}
	}

	for i, s := range stageOrder[start:] {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Update progress based on stage index.
		progress := int(float64(i) / float64(len(stageOrder[start:])) * 100)
		if tid, _ := queue.TaskID(ctx); tid != "" {
			_ = queue.UpdateProgress(conn, tid, progress)
		}

		log.Printf("reprocess video=%d stage=%s: start", videoID, s)
		if err := runStage(ctx, cfg, conn, videoID, s); err != nil {
			log.Printf("reprocess video=%d stage=%s: FAIL %v", videoID, s, err)
			return fmt.Errorf("%s: %w", s, err)
		}
		log.Printf("reprocess video=%d stage=%s: done", videoID, s)
	}
	return nil
}

func runStage(ctx context.Context, cfg *config.Config, conn *sql.DB, videoID int64, stage string) error {
	switch stage {
	case "audio":
		return audio.Extract(ctx, cfg, conn, videoID, true)
	case "transcribe":
		return asr.Transcribe(ctx, cfg, conn, videoID, true)
	case "events":
		return events.Detect(ctx, cfg, conn, videoID, true)
	case "vision":
		return vision.Detect(ctx, cfg, conn, videoID, true)
	case "score":
		return scoring.Score(ctx, cfg, conn, videoID)
	case "clip":
		return clipVideoCandidates(ctx, cfg, conn, videoID)
	case "rank":
		return rank.Rank(ctx, cfg, conn, videoID)
	}
	return fmt.Errorf("unknown stage %q", stage)
}

// clipVideoCandidates cuts every active candidate_clip for a video that
// doesn't yet have a clip_path on disk. Matches the Python `cmd_clip` step
// that runs between score and rank.
func clipVideoCandidates(ctx context.Context, cfg *config.Config, conn *sql.DB, videoID int64) error {
	rows, err := conn.QueryContext(ctx,
		`SELECT id FROM candidate_clip WHERE video_id=? AND active=1 AND (clip_path IS NULL OR clip_path='') ORDER BY start_s`,
		videoID)
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
		if err := clipper.Create(ctx, cfg, conn, id); err != nil {
			return fmt.Errorf("clip %d: %w", id, err)
		}
	}
	log.Printf("clip video=%d: cut %d clip(s)", videoID, len(ids))
	return nil
}

func newServeCmd(ctx *appCtx) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start Echo web server + asynq worker",
		RunE: func(cmd *cobra.Command, args []string) error {
			concurrency, _ := cmd.Flags().GetInt("concurrency")
			deps, err := buildDeps(ctx)
			if err != nil {
				return err
			}
			defer deps.DB.Close()

			if err := queue.CleanOrphans(deps.DB); err != nil {
				log.Printf("clean orphan jobs: %v", err)
			}

			rootCtx, stopSignals := signal.NotifyContext(cmd.Context(),
				syscall.SIGINT, syscall.SIGTERM)
			defer stopSignals()

			client := queue.NewClient(ctx.cfg.RedisAddr)
			defer client.Close()
			inspector := queue.NewInspector(ctx.cfg.RedisAddr)
			defer inspector.Close()

			mux := queue.NewMux(deps)
			asynqSrv := queue.NewServer(ctx.cfg.RedisAddr, concurrency)

			// Start asynq worker in the background. `Start` returns after
			// the server is ready; errors during `Run` would have blocked.
			if err := asynqSrv.Start(mux); err != nil {
				return fmt.Errorf("start asynq server: %w", err)
			}

			stopScheduler := web.StartScheduler(rootCtx, deps.DB, client)

			httpSrv := web.New(ctx.cfg, deps.DB, client, inspector)

			httpErr := make(chan error, 1)
			go func() {
				log.Printf("HTTP server listening on %s", ctx.cfg.HTTPListen)
				err := httpSrv.Start(ctx.cfg.HTTPListen)
				if err != nil && !errors.Is(err, http.ErrServerClosed) {
					httpErr <- err
					return
				}
				httpErr <- nil
			}()

			select {
			case <-rootCtx.Done():
				log.Println("shutdown signal received")
			case err := <-httpErr:
				if err != nil {
					log.Printf("http server error: %v", err)
				}
			}

			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := httpSrv.Shutdown(shutdownCtx); err != nil {
				log.Printf("http shutdown: %v", err)
			}
			stopScheduler()
			asynqSrv.Shutdown()
			return nil
		},
	}
	cmd.Flags().Int("concurrency", 4, "asynq worker concurrency")
	return cmd
}

func newWorkerCmd(ctx *appCtx) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worker",
		Short: "Start asynq worker only (no HTTP)",
		RunE: func(cmd *cobra.Command, args []string) error {
			concurrency, _ := cmd.Flags().GetInt("concurrency")
			deps, err := buildDeps(ctx)
			if err != nil {
				return err
			}
			defer deps.DB.Close()

			srv := queue.NewServer(ctx.cfg.RedisAddr, concurrency)
			mux := queue.NewMux(deps)
			return srv.Run(mux)
		},
	}
	cmd.Flags().Int("concurrency", 4, "asynq worker concurrency")
	return cmd
}
