package main

import (
	"context"
	"crypto/tls"
	"os"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestProbeRemoteWebTransport is retained as a manual connectivity probe,
// but now targets the websocket signaling endpoint used by the app.
// Set BKEN_PROBE_ADDR (host:port) to run it.
func TestProbeRemoteWebTransport(t *testing.T) {
	target := os.Getenv("BKEN_PROBE_ADDR")
	if target == "" {
		t.Skip("set BKEN_PROBE_ADDR=host:port to run connectivity probe")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	d := websocket.Dialer{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
	}

	conn, _, err := d.DialContext(ctx, "wss://"+target+"/ws", nil)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	_ = conn.Close()
}
