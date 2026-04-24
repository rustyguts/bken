package queue

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/hibiken/asynq"

	"github.com/rustyguts/bken/internal/config"
)

// StageHandler is the common signature for per-video pipeline stages.
type StageHandler func(ctx context.Context, videoID int64, force bool) error

// IngestHandler processes an IngestPayload (filesystem scan).
type IngestHandler func(ctx context.Context, paths []string, workers int) error

// CreateClipHandler cuts a single candidate clip by id.
type CreateClipHandler func(ctx context.Context, clipID int64) error

// ReprocessHandler re-runs stages for a video.
type ReprocessHandler func(ctx context.Context, videoID int64, fromStage string) error

// LibrarySyncHandler rescans a registered library.
type LibrarySyncHandler func(ctx context.Context, libraryID int64) error

// Deps is the concrete set of stage handlers the worker knows about. A real
// handler lives behind each field; stubs returning "not implemented" are fine
// until the matching task lands.
type Deps struct {
	DB     *sql.DB
	Config *config.Config

	Ingest        IngestHandler
	ExtractAudio  StageHandler
	Transcribe    StageHandler
	DetectEvents  StageHandler
	DetectVision  StageHandler
	Score         StageHandler
	Rank          StageHandler
	CreateClip    CreateClipHandler
	Reprocess     ReprocessHandler
	LibrarySync   LibrarySyncHandler
}

// NewServer builds the asynq server bound to redis and a concurrency level.
func NewServer(redisAddr string, concurrency int) *asynq.Server {
	if concurrency <= 0 {
		concurrency = 4
	}
	return asynq.NewServer(
		dragonflyOpt{Addr: redisAddr},
		asynq.Config{Concurrency: concurrency},
	)
}

// NewMux registers one handler per task type. Each wrapper marks the `job`
// row running/done/failed based on the result.
func NewMux(deps Deps) *asynq.ServeMux {
	mux := asynq.NewServeMux()

	stage := func(typ string, h StageHandler) {
		if h == nil {
			h = func(ctx context.Context, id int64, force bool) error {
				return fmt.Errorf("%s handler not wired", typ)
			}
		}
		mux.HandleFunc(typ, func(ctx context.Context, t *asynq.Task) error {
			var p VideoStagePayload
			if err := json.Unmarshal(t.Payload(), &p); err != nil {
				return markAndReturn(deps.DB, ctx, fmt.Errorf("unmarshal %s: %w: %w", typ, err, asynq.SkipRetry))
			}
			return runJob(ctx, deps.DB, func(ctx context.Context) error {
				return h(ctx, p.VideoID, p.Force)
			})
		})
	}

	stage(TypeExtractAudio, deps.ExtractAudio)
	stage(TypeTranscribe, deps.Transcribe)
	stage(TypeDetectEvents, deps.DetectEvents)
	stage(TypeDetectVision, deps.DetectVision)
	stage(TypeScore, deps.Score)
	stage(TypeRank, deps.Rank)

	mux.HandleFunc(TypeIngest, func(ctx context.Context, t *asynq.Task) error {
		var p IngestPayload
		if err := json.Unmarshal(t.Payload(), &p); err != nil {
			return markAndReturn(deps.DB, ctx, fmt.Errorf("unmarshal ingest: %w: %w", err, asynq.SkipRetry))
		}
		return runJob(ctx, deps.DB, func(ctx context.Context) error {
			if deps.Ingest == nil {
				return fmt.Errorf("ingest handler not wired")
			}
			return deps.Ingest(ctx, p.Paths, p.Workers)
		})
	})

	mux.HandleFunc(TypeCreateClip, func(ctx context.Context, t *asynq.Task) error {
		var p CreateClipPayload
		if err := json.Unmarshal(t.Payload(), &p); err != nil {
			return markAndReturn(deps.DB, ctx, fmt.Errorf("unmarshal create_clip: %w: %w", err, asynq.SkipRetry))
		}
		return runJob(ctx, deps.DB, func(ctx context.Context) error {
			if deps.CreateClip == nil {
				return fmt.Errorf("create_clip handler not wired")
			}
			return deps.CreateClip(ctx, p.ClipID)
		})
	})

	mux.HandleFunc(TypeReprocess, func(ctx context.Context, t *asynq.Task) error {
		var p ReprocessPayload
		if err := json.Unmarshal(t.Payload(), &p); err != nil {
			return markAndReturn(deps.DB, ctx, fmt.Errorf("unmarshal reprocess: %w: %w", err, asynq.SkipRetry))
		}
		return runJob(ctx, deps.DB, func(ctx context.Context) error {
			if deps.Reprocess == nil {
				return fmt.Errorf("reprocess handler not wired")
			}
			return deps.Reprocess(ctx, p.VideoID, p.FromStage)
		})
	})

	mux.HandleFunc(TypeLibrarySync, func(ctx context.Context, t *asynq.Task) error {
		var p LibrarySyncPayload
		if err := json.Unmarshal(t.Payload(), &p); err != nil {
			return markAndReturn(deps.DB, ctx, fmt.Errorf("unmarshal library_sync: %w: %w", err, asynq.SkipRetry))
		}
		return runJob(ctx, deps.DB, func(ctx context.Context) error {
			if deps.LibrarySync == nil {
				return fmt.Errorf("library_sync handler not wired")
			}
			return deps.LibrarySync(ctx, p.LibraryID)
		})
	})

	return mux
}

// runJob marks the job running, invokes fn, then marks done/failed based on result.
func runJob(ctx context.Context, database *sql.DB, fn func(context.Context) error) error {
	taskID, _ := asynq.GetTaskID(ctx)
	if taskID != "" {
		_ = MarkRunning(database, taskID)
	}
	err := fn(ctx)
	if err != nil {
		if taskID != "" {
			_ = MarkFailed(database, taskID, err.Error())
		}
		return err
	}
	if taskID != "" {
		_ = MarkDone(database, taskID)
	}
	return nil
}

// markAndReturn is for unmarshal-failure paths where we haven't entered runJob.
func markAndReturn(database *sql.DB, ctx context.Context, err error) error {
	if taskID, ok := asynq.GetTaskID(ctx); ok {
		_ = MarkFailed(database, taskID, err.Error())
	}
	return err
}
