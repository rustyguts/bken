package main

import (
	"context"
	"flag"
	"log/slog"
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
	serverName := flag.String("name", "bken server", "Server display name")
	debug := flag.Bool("debug", false, "Enable debug logging (auto-enabled for dev builds)")
	flag.Parse()

	// Auto-enable debug logging for dev builds; override with -debug flag.
	level := slog.LevelInfo
	if *debug || strings.Contains(Version, "dev") {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	slog.Info("starting server", "version", Version, "addr", *addr, "db", *dbPath)

	sqliteStore, err := store.Open(*dbPath)
	if err != nil {
		slog.Error("open sqlite store", "err", err)
		os.Exit(1)
	}
	defer func() {
		if closeErr := sqliteStore.Close(); closeErr != nil {
			slog.Error("close sqlite store", "err", closeErr)
		}
	}()

	blobRoot := strings.TrimSpace(*blobsDir)
	if blobRoot == "" {
		blobRoot = filepath.Join(filepath.Dir(*dbPath), "blobs")
	}
	slog.Debug("blob store", "dir", blobRoot)

	blobStore, err := blob.NewStore(blobRoot, sqliteStore)
	if err != nil {
		slog.Error("initialize blob store", "err", err)
		os.Exit(1)
	}

	channelState := core.NewChannelState(*serverName)
	slog.Debug("channel state initialized", "server_name", *serverName)

	server := httpapi.New(channelState, sqliteStore, blobStore)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		slog.Info("received interrupt, shutting down")
		cancel()
	}()

	slog.Info("listening", "addr", *addr)
	if err := server.Run(ctx, *addr); err != nil {
		slog.Error("server error", "err", err)
		os.Exit(1)
	}
	slog.Info("server stopped")
}
