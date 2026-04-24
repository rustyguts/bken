package web

import (
	"context"
	"database/sql"
	"log"
	"time"

	"github.com/hibiken/asynq"

	"github.com/rustyguts/bken/internal/library"
	"github.com/rustyguts/bken/internal/queue"
)

// StartScheduler wakes every 60s, asks the library package which entries
// are past their next_scan_at, and enqueues a library_sync task for each.
// Returns a stop func the caller invokes at shutdown. Port of
// app/server/plugins/scheduler.ts, but driven by `library.DueForScan`
// instead of per-library cron handles (simpler + survives restart).
func StartScheduler(ctx context.Context, conn *sql.DB, client *asynq.Client) (stop func()) {
	sctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	go func() {
		defer close(done)
		// First tick runs immediately so a freshly-added library doesn't
		// wait a full minute before its initial sync fires.
		tick := time.NewTimer(0)
		defer tick.Stop()
		for {
			select {
			case <-sctx.Done():
				return
			case <-tick.C:
				runSchedulerPass(sctx, conn, client)
				tick.Reset(60 * time.Second)
			}
		}
	}()

	return func() {
		cancel()
		<-done
	}
}

func runSchedulerPass(ctx context.Context, conn *sql.DB, client *asynq.Client) {
	libs, err := library.DueForScan(ctx, conn)
	if err != nil {
		log.Printf("scheduler: DueForScan: %v", err)
		return
	}
	for _, l := range libs {
		// Skip if a pending/running sync already exists for this library —
		// no point stacking duplicates.
		var existing int64
		err := conn.QueryRowContext(ctx,
			`SELECT id FROM job
			  WHERE library_id = ? AND type = 'library_sync'
			    AND status IN ('pending', 'running')
			  LIMIT 1`, l.ID).Scan(&existing)
		if err == nil {
			continue
		}
		if err != sql.ErrNoRows {
			log.Printf("scheduler: check pending for lib %d: %v", l.ID, err)
			continue
		}
		if _, err := queue.Enqueue(ctx, client, conn, queue.TypeLibrarySync,
			queue.LibrarySyncPayload{LibraryID: l.ID}); err != nil {
			log.Printf("scheduler: enqueue sync for lib %d: %v", l.ID, err)
		}
	}
}
