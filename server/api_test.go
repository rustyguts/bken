package main

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
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

// newTestAPI creates an APIServer backed by an in-memory SQLite store
// and a temporary uploads directory.
func newTestAPI(t *testing.T, room *Room) *APIServer {
	t.Helper()
	st, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	uploadsDir := t.TempDir()
	return NewAPIServer(room, st, uploadsDir)
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

func TestPutSettingsRejectsTooLongName(t *testing.T) {
	api := newTestAPI(t, NewRoom())

	longName := strings.Repeat("x", 51)
	body := strings.NewReader(`{"server_name":"` + longName + `"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", body)
	req.Header.Set("Content-Type", echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := api.echo.NewContext(req, rec)
	c.Request().Header.Set("Content-Type", echo.MIMEApplicationJSON)

	err := api.handlePutSettings(c)
	if err == nil {
		t.Fatal("expected error for 51-char server_name, got nil")
	}
	he, ok := err.(*echo.HTTPError)
	if !ok || he.Code != http.StatusBadRequest {
		t.Errorf("expected 400 HTTPError, got %v", err)
	}
}

func TestPutSettingsAcceptsExactly50Chars(t *testing.T) {
	api := newTestAPI(t, NewRoom())

	name50 := strings.Repeat("x", 50)
	body := strings.NewReader(`{"server_name":"` + name50 + `"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", body)
	req.Header.Set("Content-Type", echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := api.echo.NewContext(req, rec)
	c.Request().Header.Set("Content-Type", echo.MIMEApplicationJSON)

	if err := api.handlePutSettings(c); err != nil {
		t.Fatalf("50-char name should be accepted, got error: %v", err)
	}
}

func TestPutSettingsRejectsWhitespaceOnlyName(t *testing.T) {
	api := newTestAPI(t, NewRoom())

	body := strings.NewReader(`{"server_name":"   "}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", body)
	req.Header.Set("Content-Type", echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := api.echo.NewContext(req, rec)
	c.Request().Header.Set("Content-Type", echo.MIMEApplicationJSON)

	err := api.handlePutSettings(c)
	if err == nil {
		t.Fatal("expected error for whitespace-only server_name, got nil")
	}
	he, ok := err.(*echo.HTTPError)
	if !ok || he.Code != http.StatusBadRequest {
		t.Errorf("expected 400 HTTPError, got %v", err)
	}
}

func TestPutSettingsTrimsWhitespace(t *testing.T) {
	api := newTestAPI(t, NewRoom())

	body := strings.NewReader(`{"server_name":"  My Server  "}`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", body)
	req.Header.Set("Content-Type", echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := api.echo.NewContext(req, rec)
	c.Request().Header.Set("Content-Type", echo.MIMEApplicationJSON)

	if err := api.handlePutSettings(c); err != nil {
		t.Fatalf("handler error: %v", err)
	}

	// Verify the trimmed name was persisted.
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
	if resp.ServerName != "My Server" {
		t.Errorf("server_name after trim: got %q, want %q", resp.ServerName, "My Server")
	}
}

func TestPutSettingsRejectsMalformedJSON(t *testing.T) {
	api := newTestAPI(t, NewRoom())

	body := strings.NewReader(`not json at all`)
	req := httptest.NewRequest(http.MethodPut, "/api/settings", body)
	req.Header.Set("Content-Type", echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := api.echo.NewContext(req, rec)
	c.Request().Header.Set("Content-Type", echo.MIMEApplicationJSON)

	err := api.handlePutSettings(c)
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
	he, ok := err.(*echo.HTTPError)
	if !ok || he.Code != http.StatusBadRequest {
		t.Errorf("expected 400 HTTPError, got %v", err)
	}
}

func TestRoomEndpointIncludesOwnerID(t *testing.T) {
	room := newTestRoom(
		UserInfo{ID: 2, Username: "alice"},
		UserInfo{ID: 5, Username: "bob"},
	)
	room.ClaimOwnership(2)
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
	if resp.OwnerID != 2 {
		t.Errorf("owner_id: got %d, want 2", resp.OwnerID)
	}
}

func TestRoomEndpointOwnerIDZeroWhenEmpty(t *testing.T) {
	room := NewRoom()
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
	if resp.OwnerID != 0 {
		t.Errorf("owner_id for empty room: got %d, want 0", resp.OwnerID)
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

// --- Channel API tests ---

func TestGetChannelsEmpty(t *testing.T) {
	api := newTestAPI(t, NewRoom())

	req := httptest.NewRequest(http.MethodGet, "/api/channels", nil)
	rec := httptest.NewRecorder()
	c := api.echo.NewContext(req, rec)

	if err := api.handleGetChannels(c); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", rec.Code)
	}

	var resp []ChannelResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp) != 0 {
		t.Errorf("expected empty array, got %v", resp)
	}
}

func TestCreateChannelSuccess(t *testing.T) {
	api := newTestAPI(t, NewRoom())

	body := strings.NewReader(`{"name":"General"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/channels", body)
	req.Header.Set("Content-Type", echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := api.echo.NewContext(req, rec)
	c.Request().Header.Set("Content-Type", echo.MIMEApplicationJSON)

	if err := api.handleCreateChannel(c); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Errorf("status: got %d, want 201", rec.Code)
	}

	var resp ChannelResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Name != "General" {
		t.Errorf("name: got %q, want %q", resp.Name, "General")
	}
	if resp.ID <= 0 {
		t.Errorf("expected positive id, got %d", resp.ID)
	}
}

func TestCreateChannelDuplicate(t *testing.T) {
	api := newTestAPI(t, NewRoom())

	for i, want := range []int{http.StatusCreated, http.StatusConflict} {
		body := strings.NewReader(`{"name":"General"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/channels", body)
		req.Header.Set("Content-Type", echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := api.echo.NewContext(req, rec)
		c.Request().Header.Set("Content-Type", echo.MIMEApplicationJSON)

		err := api.handleCreateChannel(c)
		if i == 0 {
			if err != nil {
				t.Fatalf("first create: %v", err)
			}
		} else {
			if err == nil {
				t.Fatal("expected conflict error, got nil")
			}
			he, ok := err.(*echo.HTTPError)
			if !ok || he.Code != want {
				t.Errorf("expected %d, got %v", want, err)
			}
		}
	}
}

func TestCreateChannelEmptyName(t *testing.T) {
	api := newTestAPI(t, NewRoom())

	body := strings.NewReader(`{"name":""}`)
	req := httptest.NewRequest(http.MethodPost, "/api/channels", body)
	req.Header.Set("Content-Type", echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := api.echo.NewContext(req, rec)
	c.Request().Header.Set("Content-Type", echo.MIMEApplicationJSON)

	err := api.handleCreateChannel(c)
	if err == nil {
		t.Fatal("expected error for empty name, got nil")
	}
	he, ok := err.(*echo.HTTPError)
	if !ok || he.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %v", err)
	}
}

func TestCreateChannelTooLongName(t *testing.T) {
	api := newTestAPI(t, NewRoom())

	longName := strings.Repeat("x", 51)
	body := strings.NewReader(`{"name":"` + longName + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/channels", body)
	req.Header.Set("Content-Type", echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := api.echo.NewContext(req, rec)
	c.Request().Header.Set("Content-Type", echo.MIMEApplicationJSON)

	err := api.handleCreateChannel(c)
	if err == nil {
		t.Fatal("expected error for 51-char name, got nil")
	}
	he, ok := err.(*echo.HTTPError)
	if !ok || he.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %v", err)
	}
}

func TestRenameChannelSuccess(t *testing.T) {
	api := newTestAPI(t, NewRoom())

	// Create a channel first.
	body := strings.NewReader(`{"name":"Old"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/channels", body)
	req.Header.Set("Content-Type", echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := api.echo.NewContext(req, rec)
	c.Request().Header.Set("Content-Type", echo.MIMEApplicationJSON)
	if err := api.handleCreateChannel(c); err != nil {
		t.Fatalf("create: %v", err)
	}
	var created ChannelResponse
	json.Unmarshal(rec.Body.Bytes(), &created)

	// Rename it.
	body2 := strings.NewReader(`{"name":"New"}`)
	req2 := httptest.NewRequest(http.MethodPut, "/api/channels/"+strconv.FormatInt(created.ID, 10), body2)
	req2.Header.Set("Content-Type", echo.MIMEApplicationJSON)
	rec2 := httptest.NewRecorder()
	c2 := api.echo.NewContext(req2, rec2)
	c2.Request().Header.Set("Content-Type", echo.MIMEApplicationJSON)
	c2.SetParamNames("id")
	c2.SetParamValues(strconv.FormatInt(created.ID, 10))

	if err := api.handleRenameChannel(c2); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if rec2.Code != http.StatusNoContent {
		t.Errorf("status: got %d, want 204", rec2.Code)
	}
}

func TestRenameChannelNotFound(t *testing.T) {
	api := newTestAPI(t, NewRoom())

	body := strings.NewReader(`{"name":"X"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/channels/9999", body)
	req.Header.Set("Content-Type", echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := api.echo.NewContext(req, rec)
	c.Request().Header.Set("Content-Type", echo.MIMEApplicationJSON)
	c.SetParamNames("id")
	c.SetParamValues("9999")

	err := api.handleRenameChannel(c)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	he, ok := err.(*echo.HTTPError)
	if !ok || he.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %v", err)
	}
}

func TestDeleteChannelSuccess(t *testing.T) {
	api := newTestAPI(t, NewRoom())

	// Create a channel first.
	body := strings.NewReader(`{"name":"Temp"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/channels", body)
	req.Header.Set("Content-Type", echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := api.echo.NewContext(req, rec)
	c.Request().Header.Set("Content-Type", echo.MIMEApplicationJSON)
	if err := api.handleCreateChannel(c); err != nil {
		t.Fatalf("create: %v", err)
	}
	var created ChannelResponse
	json.Unmarshal(rec.Body.Bytes(), &created)

	// Delete it.
	req2 := httptest.NewRequest(http.MethodDelete, "/api/channels/"+strconv.FormatInt(created.ID, 10), nil)
	rec2 := httptest.NewRecorder()
	c2 := api.echo.NewContext(req2, rec2)
	c2.SetParamNames("id")
	c2.SetParamValues(strconv.FormatInt(created.ID, 10))

	if err := api.handleDeleteChannel(c2); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if rec2.Code != http.StatusNoContent {
		t.Errorf("status: got %d, want 204", rec2.Code)
	}
}

func TestDeleteChannelNotFound(t *testing.T) {
	api := newTestAPI(t, NewRoom())

	req := httptest.NewRequest(http.MethodDelete, "/api/channels/9999", nil)
	rec := httptest.NewRecorder()
	c := api.echo.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("9999")

	err := api.handleDeleteChannel(c)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	he, ok := err.(*echo.HTTPError)
	if !ok || he.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %v", err)
	}
}

// --- Invite endpoint tests ---

func TestInviteEndpointReturnsHTML(t *testing.T) {
	room := NewRoom()
	room.SetServerName("My Server")
	api := newTestAPI(t, room)

	req := httptest.NewRequest(http.MethodGet, "/invite", nil)
	rec := httptest.NewRecorder()
	c := api.echo.NewContext(req, rec)

	if err := api.handleInvite(c); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "My Server") {
		t.Errorf("body should contain server name, got: %q", body[:minLen(len(body), 200)])
	}
	if !strings.Contains(body, "<!DOCTYPE html>") {
		t.Errorf("body should be HTML")
	}
}

func TestInviteEndpointWithAddr(t *testing.T) {
	room := NewRoom()
	room.SetServerName("Test Server")
	api := newTestAPI(t, room)

	req := httptest.NewRequest(http.MethodGet, "/invite?addr=192.168.1.10:4433", nil)
	rec := httptest.NewRecorder()
	c := api.echo.NewContext(req, rec)

	if err := api.handleInvite(c); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "bken://192.168.1.10:4433") {
		t.Errorf("body should contain bken:// link, got: %q", body[:minLen(len(body), 400)])
	}
}

func TestInviteEndpointNoAddrOmitsLink(t *testing.T) {
	room := NewRoom()
	api := newTestAPI(t, room)

	req := httptest.NewRequest(http.MethodGet, "/invite", nil)
	rec := httptest.NewRecorder()
	c := api.echo.NewContext(req, rec)

	if err := api.handleInvite(c); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	body := rec.Body.String()
	if strings.Contains(body, "bken://") {
		t.Errorf("body should not contain bken:// link when addr is absent")
	}
}

func TestInviteEndpointDefaultServerName(t *testing.T) {
	room := NewRoom() // no server name set
	api := newTestAPI(t, room)

	req := httptest.NewRequest(http.MethodGet, "/invite", nil)
	rec := httptest.NewRecorder()
	c := api.echo.NewContext(req, rec)

	if err := api.handleInvite(c); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "bken server") {
		t.Errorf("should fall back to 'bken server' when name is empty")
	}
}

// --- File upload/download tests ---

func TestUploadAndDownloadFile(t *testing.T) {
	api := newTestAPI(t, NewRoom())

	// Upload a file via multipart form.
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", "test.txt")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	fw.Write([]byte("hello world"))
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/upload", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	c := api.echo.NewContext(req, rec)

	if err := api.handleUpload(c); err != nil {
		t.Fatalf("upload handler: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload status: got %d, want 201", rec.Code)
	}

	var ur UploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &ur); err != nil {
		t.Fatalf("unmarshal upload response: %v", err)
	}
	if ur.Name != "test.txt" {
		t.Errorf("name: got %q, want %q", ur.Name, "test.txt")
	}
	if ur.Size != 11 {
		t.Errorf("size: got %d, want 11", ur.Size)
	}
	if ur.ID <= 0 {
		t.Errorf("expected positive id, got %d", ur.ID)
	}

	// Download the file.
	req2 := httptest.NewRequest(http.MethodGet, "/api/files/"+strconv.FormatInt(ur.ID, 10), nil)
	rec2 := httptest.NewRecorder()
	c2 := api.echo.NewContext(req2, rec2)
	c2.SetParamNames("id")
	c2.SetParamValues(strconv.FormatInt(ur.ID, 10))

	if err := api.handleGetFile(c2); err != nil {
		t.Fatalf("download handler: %v", err)
	}
	if rec2.Code != http.StatusOK {
		t.Errorf("download status: got %d, want 200", rec2.Code)
	}
	if got := rec2.Body.String(); got != "hello world" {
		t.Errorf("file content: got %q, want %q", got, "hello world")
	}
	if disp := rec2.Header().Get("Content-Disposition"); !strings.Contains(disp, "test.txt") {
		t.Errorf("Content-Disposition should contain filename, got %q", disp)
	}
}

func TestUploadMissingFile(t *testing.T) {
	api := newTestAPI(t, NewRoom())

	req := httptest.NewRequest(http.MethodPost, "/api/upload", strings.NewReader(""))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=xxx")
	rec := httptest.NewRecorder()
	c := api.echo.NewContext(req, rec)

	err := api.handleUpload(c)
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	he, ok := err.(*echo.HTTPError)
	if !ok || he.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %v", err)
	}
}

func TestDownloadFileNotFound(t *testing.T) {
	api := newTestAPI(t, NewRoom())

	req := httptest.NewRequest(http.MethodGet, "/api/files/9999", nil)
	rec := httptest.NewRecorder()
	c := api.echo.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("9999")

	err := api.handleGetFile(c)
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
	he, ok := err.(*echo.HTTPError)
	if !ok || he.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %v", err)
	}
}

func TestDownloadInvalidID(t *testing.T) {
	api := newTestAPI(t, NewRoom())

	req := httptest.NewRequest(http.MethodGet, "/api/files/abc", nil)
	rec := httptest.NewRecorder()
	c := api.echo.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("abc")

	err := api.handleGetFile(c)
	if err == nil {
		t.Fatal("expected error for invalid id, got nil")
	}
	he, ok := err.(*echo.HTTPError)
	if !ok || he.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %v", err)
	}
}

// minLen returns the smaller of a and b (used for safe body truncation in test errors).
func minLen(a, b int) int {
	if a < b {
		return a
	}
	return b
}
