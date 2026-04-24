// Package queue wires asynq tasks to the `job` table. Every pipeline stage
// has its own task type; payloads are JSON-serialized into `asynq.Task`.
// Enqueueing always writes a row to `job` first so the UI can show state
// even before the worker picks the task up.
package queue

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
)

// Task type constants. One per pipeline stage + orchestration helpers.
const (
	TypeIngest        = "ingest"
	TypeExtractAudio  = "extract_audio"
	TypeTranscribe    = "transcribe"
	TypeDetectEvents  = "detect_events"
	TypeDetectVision  = "detect_vision"
	TypeScore         = "score"
	TypeRank          = "rank"
	TypeCreateClip    = "create_clip"
	TypeReprocess     = "reprocess"
	TypeLibrarySync   = "library_sync"
)

// IngestPayload triggers ffprobe-driven ingest of one or more filesystem paths.
type IngestPayload struct {
	Paths   []string `json:"paths"`
	Workers int      `json:"workers"`
}

// VideoStagePayload is the common shape for per-video stage tasks
// (extract-audio, transcribe, detect-events, detect-vision, score, rank).
type VideoStagePayload struct {
	VideoID int64 `json:"video_id"`
	Force   bool  `json:"force"`
}

// CreateClipPayload cuts a single candidate clip.
type CreateClipPayload struct {
	ClipID int64 `json:"clip_id"`
}

// ReprocessPayload re-runs the pipeline for one video starting at a named stage.
type ReprocessPayload struct {
	VideoID   int64  `json:"video_id"`
	FromStage string `json:"from_stage"`
}

// LibrarySyncPayload rescans a library directory.
type LibrarySyncPayload struct {
	LibraryID int64 `json:"library_id"`
}

// NewTask json-marshals payload and wraps it in an asynq.Task. Applies a
// generous default timeout + retention so long ML stages (transcribe,
// encode) don't get recycled mid-run and finished jobs stay inspectable
// for a day.
func NewTask(typ string, payload any) (*asynq.Task, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal %s payload: %w", typ, err)
	}
	return asynq.NewTask(typ, b,
		asynq.Timeout(4*time.Hour),
		asynq.MaxRetry(0),
		asynq.Retention(24*time.Hour),
	), nil
}

// Enqueue inserts a job row, enqueues the task, and backfills asynq_id.
// Returns the `job.id` rowid so callers can track it.
func Enqueue(ctx context.Context, client *asynq.Client, database *sql.DB, typ string, payload any, opts ...asynq.Option) (int64, error) {
	task, err := NewTask(typ, payload)
	if err != nil {
		return 0, err
	}

	var (
		videoID  sql.NullInt64
		libID    sql.NullInt64
		targetID sql.NullInt64
	)
	switch p := payload.(type) {
	case VideoStagePayload:
		videoID = sql.NullInt64{Int64: p.VideoID, Valid: p.VideoID != 0}
	case *VideoStagePayload:
		videoID = sql.NullInt64{Int64: p.VideoID, Valid: p.VideoID != 0}
	case CreateClipPayload:
		targetID = sql.NullInt64{Int64: p.ClipID, Valid: p.ClipID != 0}
	case *CreateClipPayload:
		targetID = sql.NullInt64{Int64: p.ClipID, Valid: p.ClipID != 0}
	case ReprocessPayload:
		videoID = sql.NullInt64{Int64: p.VideoID, Valid: p.VideoID != 0}
	case *ReprocessPayload:
		videoID = sql.NullInt64{Int64: p.VideoID, Valid: p.VideoID != 0}
	case LibrarySyncPayload:
		libID = sql.NullInt64{Int64: p.LibraryID, Valid: p.LibraryID != 0}
	case *LibrarySyncPayload:
		libID = sql.NullInt64{Int64: p.LibraryID, Valid: p.LibraryID != 0}
	}

	res, err := database.ExecContext(ctx,
		`INSERT INTO job(video_id, library_id, type, status, target_id, created_at, updated_at)
		 VALUES (?, ?, ?, 'pending', ?, datetime('now'), datetime('now'))`,
		videoID, libID, typ, targetID,
	)
	if err != nil {
		return 0, fmt.Errorf("insert job: %w", err)
	}
	jobID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	info, err := client.EnqueueContext(ctx, task, opts...)
	if err != nil {
		// Couldn't enqueue: mark the job failed so it doesn't sit pending forever.
		_, _ = database.ExecContext(ctx,
			`UPDATE job SET status='failed', error_message=?, updated_at=datetime('now') WHERE id=?`,
			err.Error(), jobID,
		)
		return jobID, fmt.Errorf("enqueue %s: %w", typ, err)
	}

	if _, err := database.ExecContext(ctx,
		`UPDATE job SET asynq_id=?, updated_at=datetime('now') WHERE id=?`,
		info.ID, jobID,
	); err != nil {
		return jobID, fmt.Errorf("record asynq_id: %w", err)
	}
	return jobID, nil
}
