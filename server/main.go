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
	addr := flag.String("addr", ":4433", "listen address")
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

	srv := NewServer(*addr, tlsConfig, room)
	if err := srv.Run(ctx); err != nil {
		log.Fatalf("[server] %v", err)
	}
}
