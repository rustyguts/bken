package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"

	"bken/server/internal/core"
	"bken/server/internal/httpapi"
)

// Version is injected at build time with -ldflags.
var Version = "0.1.0-dev"

func main() {
	addr := flag.String("addr", ":8080", "Echo listen address")
	flag.Parse()

	room := core.NewRoom()
	server := httpapi.New(room)

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
