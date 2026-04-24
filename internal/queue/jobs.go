package queue

import (
	"context"
	"database/sql"

	"github.com/hibiken/asynq"
)

// TaskID returns the asynq task id bound to a worker handler's context.
// Wrapper around asynq.GetTaskID so callers can import one queue package.
func TaskID(ctx context.Context) (string, bool) {
	return asynq.GetTaskID(ctx)
}

// CleanOrphans marks `pending`/`running` job rows older than 30s as failed.
// Run on `bken serve` startup so jobs left dangling by an earlier crash,
// restart, or queue flush don't stay stuck forever. The grace window keeps
// just-enqueued tasks intact when the worker is still coming up.
func CleanOrphans(database *sql.DB) error {
	_, err := database.Exec(
		`UPDATE job SET status='failed',
		                error_message=COALESCE(NULLIF(error_message, ''), 'orphaned by restart'),
		                updated_at=datetime('now')
		 WHERE status IN ('pending','running')
		   AND updated_at < datetime('now', '-30 seconds')`,
	)
	return err
}

// Job mirrors a row from the `job` table.
type Job struct {
	ID           int64
	VideoID      sql.NullInt64
	LibraryID    sql.NullInt64
	Type         string
	Status       string
	TargetID     sql.NullInt64
	ErrorMessage sql.NullString
	AsynqID      sql.NullString
	Progress     int
	CreatedAt    string
	UpdatedAt    string
}

// MarkRunning flips status to 'running' for the job matched by asynq_id.
func MarkRunning(database *sql.DB, asynqID string) error {
	_, err := database.Exec(
		`UPDATE job SET status='running', updated_at=datetime('now') WHERE asynq_id=?`,
		asynqID,
	)
	return err
}

// UpdateProgress updates the progress of a job.
func UpdateProgress(database *sql.DB, asynqID string, progress int) error {
	_, err := database.Exec(
		`UPDATE job SET progress=?, updated_at=datetime('now') WHERE asynq_id=?`,
		progress, asynqID,
	)
	return err
}

// MarkDone flips status to 'done' + clears error_message + sets progress to 100.
func MarkDone(database *sql.DB, asynqID string) error {
	_, err := database.Exec(
		`UPDATE job SET status='done', error_message=NULL, progress=100, updated_at=datetime('now') WHERE asynq_id=?`,
		asynqID,
	)
	return err
}

// MarkFailed flips status to 'failed' + records error text.
func MarkFailed(database *sql.DB, asynqID, errMsg string) error {
	_, err := database.Exec(
		`UPDATE job SET status='failed', error_message=?, updated_at=datetime('now') WHERE asynq_id=?`,
		errMsg, asynqID,
	)
	return err
}

// ListJobs returns the most recent jobs (by updated_at desc).
func ListJobs(database *sql.DB, limit int) ([]Job, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := database.Query(
		`SELECT id, video_id, library_id, type, status, target_id, error_message, asynq_id, progress, created_at, updated_at
		 FROM job ORDER BY updated_at DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Job
	for rows.Next() {
		var j Job
		if err := rows.Scan(&j.ID, &j.VideoID, &j.LibraryID, &j.Type, &j.Status,
			&j.TargetID, &j.ErrorMessage, &j.AsynqID, &j.Progress, &j.CreatedAt, &j.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// GetJobByAsynqID fetches the job row that holds a given asynq task id.
func GetJobByAsynqID(database *sql.DB, asynqID string) (*Job, error) {
	row := database.QueryRow(
		`SELECT id, video_id, library_id, type, status, target_id, error_message, asynq_id, progress, created_at, updated_at
		 FROM job WHERE asynq_id=?`,
		asynqID,
	)
	var j Job
	if err := row.Scan(&j.ID, &j.VideoID, &j.LibraryID, &j.Type, &j.Status,
		&j.TargetID, &j.ErrorMessage, &j.AsynqID, &j.Progress, &j.CreatedAt, &j.UpdatedAt); err != nil {
		return nil, err
	}
	return &j, nil
}
