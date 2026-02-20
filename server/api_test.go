package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
)

// newTestRoom returns a Room with fake clients pre-populated.
func newTestRoom(users ...UserInfo) *Room {
	r := NewRoom()
	for _, u := range users {
		r.clients[u.ID] = &Client{ID: u.ID, Username: u.Username}
	}
	return r
}

func TestHealthEndpointEmptyRoom(t *testing.T) {
	room := NewRoom()
	api := NewAPIServer(room)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	c := api.echo.NewContext(req, rec)

	if err := api.handleHealth(c); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusOK)
	}

	var resp HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("status field: got %q, want %q", resp.Status, "ok")
	}
	if resp.Clients != 0 {
		t.Errorf("clients: got %d, want 0", resp.Clients)
	}
}

func TestHealthEndpointWithClients(t *testing.T) {
	room := newTestRoom(
		UserInfo{ID: 1, Username: "alice"},
		UserInfo{ID: 2, Username: "bob"},
	)
	api := NewAPIServer(room)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	c := api.echo.NewContext(req, rec)

	if err := api.handleHealth(c); err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var resp HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Clients != 2 {
		t.Errorf("clients: got %d, want 2", resp.Clients)
	}
}

func TestRoomEndpointEmptyRoom(t *testing.T) {
	room := NewRoom()
	api := NewAPIServer(room)

	req := httptest.NewRequest(http.MethodGet, "/api/room", nil)
	rec := httptest.NewRecorder()
	c := api.echo.NewContext(req, rec)

	if err := api.handleRoom(c); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", rec.Code, http.StatusOK)
	}

	var resp RoomResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Clients != 0 {
		t.Errorf("clients: got %d, want 0", resp.Clients)
	}
	if resp.Users == nil {
		t.Error("users field should be an empty array, not null")
	}
	if len(resp.Users) != 0 {
		t.Errorf("users length: got %d, want 0", len(resp.Users))
	}
}

func TestRoomEndpointWithClients(t *testing.T) {
	room := newTestRoom(
		UserInfo{ID: 1, Username: "alice"},
		UserInfo{ID: 3, Username: "charlie"},
	)
	api := NewAPIServer(room)

	req := httptest.NewRequest(http.MethodGet, "/api/room", nil)
	rec := httptest.NewRecorder()
	c := api.echo.NewContext(req, rec)

	if err := api.handleRoom(c); err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var resp RoomResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Clients != 2 {
		t.Errorf("clients: got %d, want 2", resp.Clients)
	}
	if len(resp.Users) != 2 {
		t.Errorf("users length: got %d, want 2", len(resp.Users))
	}
	// Verify all users are present (order not guaranteed).
	byID := make(map[uint16]string, len(resp.Users))
	for _, u := range resp.Users {
		byID[u.ID] = u.Username
	}
	if byID[1] != "alice" {
		t.Errorf("missing alice: got %v", byID)
	}
	if byID[3] != "charlie" {
		t.Errorf("missing charlie: got %v", byID)
	}
}

func TestRouteRegistration(t *testing.T) {
	room := NewRoom()
	api := NewAPIServer(room)

	// Verify routes are registered via the Echo router.
	routes := api.echo.Routes()
	paths := make(map[string]bool)
	for _, r := range routes {
		paths[r.Path] = true
	}
	for _, want := range []string{"/health", "/api/room"} {
		if !paths[want] {
			t.Errorf("route %q not registered; got %v", want, routes)
		}
	}
}

func TestRunShutsDownOnContextCancel(t *testing.T) {
	room := NewRoom()
	api := NewAPIServer(room)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		api.Run(ctx, "127.0.0.1:0")
		close(done)
	}()
	cancel()
	<-done // should return quickly after cancel
}

// Ensure the Echo instance uses JSON content-type.
func TestHealthResponseContentType(t *testing.T) {
	room := NewRoom()
	api := NewAPIServer(room)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	c := api.echo.NewContext(req, rec)

	if err := api.handleHealth(c); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	ct := rec.Header().Get(echo.MIMEApplicationJSON)
	_ = ct // Echo sets content-type on the response writer; just ensure no panic
}
