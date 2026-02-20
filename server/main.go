package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"time"
)

func main() {
	addr    := flag.String("addr",     ":4433", "WebTransport listen address")
	apiAddr := flag.String("api-addr", ":8080", "REST API listen address (empty to disable)")
	flag.Parse()

	tlsConfig, fingerprint := generateTLSConfig()
	log.Printf("[server] TLS certificate fingerprint: %s", fingerprint)

	room := NewRoom()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Graceful shutdown on interrupt.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		log.Println("[server] shutting down...")
		cancel()
	}()

	// Start metrics logging.
	go RunMetrics(ctx, room, 5*time.Second)

	// Start REST API server if an address is configured.
	if *apiAddr != "" {
		api := NewAPIServer(room)
		go api.Run(ctx, *apiAddr)
		log.Printf("[api] listening on %s", *apiAddr)
	}

	srv := NewServer(*addr, tlsConfig, room)
	if err := srv.Run(ctx); err != nil {
		log.Fatalf("[server] %v", err)
	}
}
