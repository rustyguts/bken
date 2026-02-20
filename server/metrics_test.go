package main

import (
	"bytes"
	"context"
	"log"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRunMetricsLogsWhenActive(t *testing.T) {
	room := NewRoom()
	c := newTestClient("alice")
	room.AddClient(c)

	// Inject some traffic.
	room.totalDatagrams.Store(10)
	room.totalBytes.Store(5000)

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		RunMetrics(ctx, room, 50*time.Millisecond)
		close(done)
	}()

	// Wait for at least one tick.
	time.Sleep(120 * time.Millisecond)
	cancel()
	<-done // wait for goroutine to exit before reading buf

	output := buf.String()
	if !strings.Contains(output, "[metrics]") {
		t.Errorf("expected metrics log output, got: %q", output)
	}
	if !strings.Contains(output, "clients=1") {
		t.Errorf("expected clients=1 in output, got: %q", output)
	}
}

func TestRunMetricsSilentWhenEmpty(t *testing.T) {
	room := NewRoom()

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		RunMetrics(ctx, room, 50*time.Millisecond)
		close(done)
	}()

	time.Sleep(120 * time.Millisecond)
	cancel()
	<-done

	if strings.Contains(buf.String(), "[metrics]") {
		t.Errorf("expected no output for empty room, got: %q", buf.String())
	}
}

func TestRunMetricsStopsOnCancel(t *testing.T) {
	room := NewRoom()
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		RunMetrics(ctx, room, 50*time.Millisecond)
		close(done)
	}()

	cancel()
	select {
	case <-done:
		// OK
	case <-time.After(2 * time.Second):
		t.Fatal("RunMetrics did not exit after cancel")
	}
}
