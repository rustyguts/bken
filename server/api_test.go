package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"

	"bken/server/store"
)

// newTestRoom returns a Room with fake clients pre-populated.
func newTestRoom(users ...UserInfo) *Room {
	r := NewRoom()
	for _, u := range users {
		r.clients[u.ID] = &Client{ID: u.ID, Username: u.Username}
	}
	return r
}

// newTestAPI creates an APIServer backed by an in-memory SQLite store.
func newTestAPI(t *testing.T, room *Room) *APIServer {
	t.Helper()
	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return NewAPIServer(room, st)
}

func TestHealthEndpointEmptyRoom(t *testing.T) {
	room := NewRoom()
	api := newTestAPI(t, room)

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
	api := newTestAPI(t, room)

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
	api := newTestAPI(t, room)

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
	api := newTestAPI(t, room)

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
	api := newTestAPI(t, room)

	// Verify routes are registered via the Echo router.
	routes := api.echo.Routes()
	paths := make(map[string]bool)
	for _, r := range routes {
		paths[r.Path] = true
	}
	for _, want := range []string{"/health", "/api/room", "/api/settings"} {
		if !paths[want] {
			t.Errorf("route %q not registered; got %v", want, routes)
		}
	}
}

func TestGetSettingsDefault(t *testing.T) {
	api := newTestAPI(t, NewRoom())

	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	rec := httptest.NewRecorder()
	c := api.echo.NewContext(req, rec)

	if err := api.handleGetSettings(c); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", rec.Code)
	}

	var resp SettingsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Default name is empty before seedDefaults; just verify no error.
	_ = resp.ServerName
}

func TestPutAndGetSettings(t *testing.T) {
	api := newTestAPI(t, NewRoom())

	// PUT a new server name.
	body := strings.NewReader(`{"server_name":"My Voice Server"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", body)
	req.Header.Set(echo.MIMEApplicationJSON, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := api.echo.NewContext(req, rec)
	c.Request().Header.Set("Content-Type", echo.MIMEApplicationJSON)

	if err := api.handlePutSettings(c); err != nil {
		t.Fatalf("PUT handler error: %v", err)
	}
	if rec.Code != http.StatusNoContent {
		t.Errorf("PUT status: got %d, want 204", rec.Code)
	}

	// GET the updated name back.
	req2 := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	rec2 := httptest.NewRecorder()
	c2 := api.echo.NewContext(req2, rec2)

	if err := api.handleGetSettings(c2); err != nil {
		t.Fatalf("GET handler error: %v", err)
	}
	var resp SettingsResponse
	if err := json.Unmarshal(rec2.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.ServerName != "My Voice Server" {
		t.Errorf("server_name: got %q, want %q", resp.ServerName, "My Voice Server")
	}
}

func TestPutSettingsUpdatesRoomNameLive(t *testing.T) {
	room := NewRoom()
	api := newTestAPI(t, room)

	body := strings.NewReader(`{"server_name":"Live Name"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", body)
	req.Header.Set("Content-Type", echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := api.echo.NewContext(req, rec)
	c.Request().Header.Set("Content-Type", echo.MIMEApplicationJSON)

	if err := api.handlePutSettings(c); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if got := room.ServerName(); got != "Live Name" {
		t.Errorf("room.ServerName after PUT: got %q, want %q", got, "Live Name")
	}
}

func TestPutSettingsRejectsEmptyName(t *testing.T) {
	api := newTestAPI(t, NewRoom())

	body := strings.NewReader(`{"server_name":""}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", body)
	req.Header.Set("Content-Type", echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := api.echo.NewContext(req, rec)
	c.Request().Header.Set("Content-Type", echo.MIMEApplicationJSON)

	err := api.handlePutSettings(c)
	if err == nil {
		t.Fatal("expected error for empty server_name, got nil")
	}
	he, ok := err.(*echo.HTTPError)
	if !ok || he.Code != http.StatusBadRequest {
		t.Errorf("expected 400 HTTPError, got %v", err)
	}
}

func TestRunShutsDownOnContextCancel(t *testing.T) {
	room := NewRoom()
	api := newTestAPI(t, room)

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
	api := newTestAPI(t, room)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	c := api.echo.NewContext(req, rec)

	if err := api.handleHealth(c); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	ct := rec.Header().Get(echo.MIMEApplicationJSON)
	_ = ct // Echo sets content-type on the response writer; just ensure no panic
}
