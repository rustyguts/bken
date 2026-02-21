package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"bken/server/internal/core"
)

func TestHealthAndState(t *testing.T) {
	channelState := core.NewChannelState()
	session, _, err := channelState.Add("alice", 8)
	if err != nil {
		t.Fatalf("add user: %v", err)
	}
	if _, _, err := channelState.ConnectServer(session.UserID, "srv-1"); err != nil {
		t.Fatalf("connect server: %v", err)
	}
	if _, _, err := channelState.JoinVoice(session.UserID, "srv-1", "chan-a"); err != nil {
		t.Fatalf("join voice: %v", err)
	}

	api := New(channelState)
	ts := httptest.NewServer(api.Echo())
	defer ts.Close()

	healthResp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer healthResp.Body.Close()
	if healthResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from /health, got %d", healthResp.StatusCode)
	}
	var health healthResponse
	if err := json.NewDecoder(healthResp.Body).Decode(&health); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	if health.Status != "ok" || health.Clients != 1 {
		t.Fatalf("unexpected health payload: %#v", health)
	}

	stateResp, err := http.Get(ts.URL + "/api/state")
	if err != nil {
		t.Fatalf("GET /api/state: %v", err)
	}
	defer stateResp.Body.Close()
	if stateResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from /api/state, got %d", stateResp.StatusCode)
	}
	var state stateResponse
	if err := json.NewDecoder(stateResp.Body).Decode(&state); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if state.Clients != 1 || len(state.Users) != 1 {
		t.Fatalf("unexpected state payload: %#v", state)
	}
	if state.Users[0].Username != "alice" {
		t.Fatalf("expected alice in state, got %#v", state.Users[0])
	}
	if state.Users[0].Voice == nil {
		t.Fatalf("expected voice presence in state, got %#v", state.Users[0])
	}
}
