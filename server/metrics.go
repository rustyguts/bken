package main

import (
	"context"
	"log"
	"time"
)

// RunMetrics logs room stats every interval until ctx is canceled.
func RunMetrics(ctx context.Context, room *Room, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			datagrams, bytes, clients := room.Stats()
			if clients > 0 || datagrams > 0 {
				log.Printf("[metrics] clients=%d datagrams=%d bytes=%d (%.1f KB/s)",
					clients, datagrams, bytes,
					float64(bytes)/interval.Seconds()/1024)
			}
		}
	}
}
