package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"bken/server/internal/blob"
	"bken/server/internal/core"
	"bken/server/internal/httpapi"
	"bken/server/internal/store"
)

// Version is injected at build time with -ldflags.
var Version = "0.1.0-dev"

func main() {
	addr := flag.String("addr", ":8080", "Echo listen address")
	dbPath := flag.String("db", "bken.db", "SQLite database path")
	blobsDir := flag.String("blobs-dir", "", "Blob directory path (defaults to <db-dir>/blobs)")
	flag.Parse()

	sqliteStore, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("[server] open sqlite store: %v", err)
	}
	defer func() {
		if closeErr := sqliteStore.Close(); closeErr != nil {
			log.Printf("[server] close sqlite store: %v", closeErr)
		}
	}()

	blobRoot := strings.TrimSpace(*blobsDir)
	if blobRoot == "" {
		blobRoot = filepath.Join(filepath.Dir(*dbPath), "blobs")
	}
	blobStore, err := blob.NewStore(blobRoot, sqliteStore)
	if err != nil {
		log.Fatalf("[server] initialize blob store: %v", err)
	}

	channelState := core.NewChannelState()
	server := httpapi.New(channelState, blobStore)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		cancel()
	}()

	log.Printf("[server] listening on %s", *addr)
	if err := server.Run(ctx, *addr); err != nil {
		log.Fatalf("[server] error: %v", err)
	}
}
