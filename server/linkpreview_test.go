package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- extractFirstURL ---

func TestExtractFirstURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"https url", "check out https://example.com/page", "https://example.com/page"},
		{"http url", "visit http://example.com", "http://example.com"},
		{"no url", "just a plain message", ""},
		{"url only", "https://example.com", "https://example.com"},
		{"multiple urls picks first", "see https://a.com and https://b.com", "https://a.com"},
		{"url with path and query", "link: https://example.com/path?q=1&b=2", "https://example.com/path?q=1&b=2"},
		{"url with fragment", "https://example.com/page#section", "https://example.com/page#section"},
		{"no scheme", "check example.com", ""},
		{"ftp not matched", "ftp://files.example.com", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFirstURL(tt.input)
			if got != tt.want {
				t.Errorf("extractFirstURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// --- parseOGTags ---

func TestParseOGTags(t *testing.T) {
	html := `<!DOCTYPE html>
<html>
<head>
	<title>Fallback Title</title>
	<meta property="og:title" content="OG Title">
	<meta property="og:description" content="OG Description">
	<meta property="og:image" content="https://example.com/img.jpg">
	<meta property="og:site_name" content="Example Site">
</head>
<body></body>
</html>`
	lp, err := parseOGTags("https://example.com", strings.NewReader(html))
	if err != nil {
		t.Fatalf("parseOGTags error: %v", err)
	}
	if lp.Title != "OG Title" {
		t.Errorf("Title: got %q, want %q", lp.Title, "OG Title")
	}
	if lp.Desc != "OG Description" {
		t.Errorf("Desc: got %q, want %q", lp.Desc, "OG Description")
	}
	if lp.Image != "https://example.com/img.jpg" {
		t.Errorf("Image: got %q, want %q", lp.Image, "https://example.com/img.jpg")
	}
	if lp.SiteName != "Example Site" {
		t.Errorf("SiteName: got %q, want %q", lp.SiteName, "Example Site")
	}
	if lp.URL != "https://example.com" {
		t.Errorf("URL: got %q, want %q", lp.URL, "https://example.com")
	}
}

func TestParseOGTagsFallbackTitle(t *testing.T) {
	html := `<html><head><title>Page Title</title></head><body></body></html>`
	lp, err := parseOGTags("https://example.com", strings.NewReader(html))
	if err != nil {
		t.Fatalf("parseOGTags error: %v", err)
	}
	if lp.Title != "Page Title" {
		t.Errorf("Title: got %q, want %q", lp.Title, "Page Title")
	}
}

func TestParseOGTagsFallbackMetaDescription(t *testing.T) {
	html := `<html><head><meta name="description" content="Meta Desc"></head><body></body></html>`
	lp, err := parseOGTags("https://example.com", strings.NewReader(html))
	if err != nil {
		t.Fatalf("parseOGTags error: %v", err)
	}
	if lp.Desc != "Meta Desc" {
		t.Errorf("Desc: got %q, want %q", lp.Desc, "Meta Desc")
	}
}

func TestParseOGTagsOGOverridesFallback(t *testing.T) {
	html := `<html><head>
		<title>Fallback</title>
		<meta name="description" content="Fallback desc">
		<meta property="og:title" content="OG Title">
		<meta property="og:description" content="OG Desc">
	</head><body></body></html>`
	lp, _ := parseOGTags("https://example.com", strings.NewReader(html))
	if lp.Title != "OG Title" {
		t.Errorf("Title should prefer OG: got %q", lp.Title)
	}
	if lp.Desc != "OG Desc" {
		t.Errorf("Desc should prefer OG: got %q", lp.Desc)
	}
}

func TestParseOGTagsEmptyHTML(t *testing.T) {
	lp, err := parseOGTags("https://example.com", strings.NewReader(""))
	if err != nil {
		t.Fatalf("parseOGTags error: %v", err)
	}
	if lp.Title != "" || lp.Desc != "" || lp.Image != "" {
		t.Errorf("empty HTML should produce empty preview, got %+v", lp)
	}
}

func TestParseOGTagsStopsAtBody(t *testing.T) {
	html := `<html><head><title>Head Title</title></head><body><title>Body Title</title></body></html>`
	lp, _ := parseOGTags("https://example.com", strings.NewReader(html))
	if lp.Title != "Head Title" {
		t.Errorf("Title: got %q, want %q (should stop at <body>)", lp.Title, "Head Title")
	}
}

// --- fetchLinkPreview with httptest ---

func TestFetchLinkPreview(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><head>
			<meta property="og:title" content="Test Page">
			<meta property="og:description" content="A test description">
			<meta property="og:image" content="https://example.com/preview.jpg">
			<meta property="og:site_name" content="Test Site">
		</head><body></body></html>`)
	}))
	defer srv.Close()

	lp, err := fetchLinkPreview(srv.URL)
	if err != nil {
		t.Fatalf("fetchLinkPreview error: %v", err)
	}
	if lp.Title != "Test Page" {
		t.Errorf("Title: got %q, want %q", lp.Title, "Test Page")
	}
	if lp.Desc != "A test description" {
		t.Errorf("Desc: got %q, want %q", lp.Desc, "A test description")
	}
	if lp.Image != "https://example.com/preview.jpg" {
		t.Errorf("Image: got %q, want %q", lp.Image, "https://example.com/preview.jpg")
	}
	if lp.SiteName != "Test Site" {
		t.Errorf("SiteName: got %q, want %q", lp.SiteName, "Test Site")
	}
}

func TestFetchLinkPreviewNonHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"key": "value"}`)
	}))
	defer srv.Close()

	lp, err := fetchLinkPreview(srv.URL)
	if err != nil {
		t.Fatalf("fetchLinkPreview error: %v", err)
	}
	if lp.Title != "" || lp.Desc != "" || lp.Image != "" {
		t.Errorf("non-HTML should have empty metadata, got %+v", lp)
	}
}

func TestFetchLinkPreviewServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	lp, err := fetchLinkPreview(srv.URL)
	if err != nil {
		t.Fatalf("fetchLinkPreview should not error on 500, got: %v", err)
	}
	if lp.Title != "" {
		t.Errorf("500 response should have empty title, got %q", lp.Title)
	}
}

// --- processControl: chat assigns MsgID ---

func TestProcessControlChatAssignsMsgID(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	room.AddClient(sender)

	processControl(ControlMsg{Type: "chat", Message: "hello"}, sender, room)

	got := decodeControl(t, senderBuf)
	if got.MsgID == 0 {
		t.Error("chat broadcast should have a non-zero MsgID")
	}
}

func TestProcessControlChatMsgIDIncrementing(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	room.AddClient(sender)

	processControl(ControlMsg{Type: "chat", Message: "first"}, sender, room)
	first := decodeControl(t, senderBuf)

	processControl(ControlMsg{Type: "chat", Message: "second"}, sender, room)
	second := decodeControl(t, senderBuf)

	if second.MsgID <= first.MsgID {
		t.Errorf("MsgID should be monotonically increasing: first=%d, second=%d", first.MsgID, second.MsgID)
	}
}

// --- processControl: chat with URL sends async link_preview ---

// safeBuffer wraps bytes.Buffer with a mutex so the async link_preview
// goroutine can write safely alongside the test reader.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (sb *safeBuffer) Write(p []byte) (n int, err error) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.buf.Write(p)
}

func (sb *safeBuffer) readAll() []byte {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return append([]byte{}, sb.buf.Bytes()...)
}

func TestProcessControlChatWithURLSendsLinkPreview(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head>
			<meta property="og:title" content="Preview Title">
			<meta property="og:description" content="Preview Desc">
		</head><body></body></html>`)
	}))
	defer srv.Close()

	room := NewRoom()
	sb := &safeBuffer{}
	sender := &Client{
		Username: "alice",
		session:  &mockSender{},
		ctrl:     sb,
	}
	room.AddClient(sender)

	msg := fmt.Sprintf("check this out %s", srv.URL)
	processControl(ControlMsg{Type: "chat", Message: msg}, sender, room)

	// Wait for the async link_preview goroutine (up to 5 seconds).
	var msgs []ControlMsg
	deadline := time.After(5 * time.Second)
	for {
		data := sb.readAll()
		msgs = msgs[:0]
		for _, line := range bytes.Split(data, []byte("\n")) {
			line = bytes.TrimSpace(line)
			if len(line) == 0 {
				continue
			}
			var m ControlMsg
			if err := json.Unmarshal(line, &m); err == nil {
				msgs = append(msgs, m)
			}
		}
		if len(msgs) >= 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for link_preview; got %d messages: %+v", len(msgs), msgs)
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}

	chat := msgs[0]
	preview := msgs[1]

	if chat.Type != "chat" {
		t.Errorf("first message: got type %q, want %q", chat.Type, "chat")
	}
	if chat.MsgID == 0 {
		t.Error("chat should have MsgID")
	}

	if preview.Type != "link_preview" {
		t.Errorf("second message: got type %q, want %q", preview.Type, "link_preview")
	}
	if preview.MsgID != chat.MsgID {
		t.Errorf("link_preview MsgID=%d should match chat MsgID=%d", preview.MsgID, chat.MsgID)
	}
	if preview.LinkTitle != "Preview Title" {
		t.Errorf("LinkTitle: got %q, want %q", preview.LinkTitle, "Preview Title")
	}
	if preview.LinkDesc != "Preview Desc" {
		t.Errorf("LinkDesc: got %q, want %q", preview.LinkDesc, "Preview Desc")
	}
}

func TestProcessControlChatNoURLNoLinkPreview(t *testing.T) {
	room := NewRoom()
	sender, senderBuf := newCtrlClient("alice")
	room.AddClient(sender)

	processControl(ControlMsg{Type: "chat", Message: "just a message"}, sender, room)

	got := decodeControl(t, senderBuf)
	if got.Type != "chat" {
		t.Errorf("expected chat, got %q", got.Type)
	}
	// Give a moment for any unexpected goroutine.
	time.Sleep(100 * time.Millisecond)
	if senderBuf.Len() != 0 {
		t.Errorf("no URL in message should produce no link_preview, got %d extra bytes", senderBuf.Len())
	}
}

func TestProcessControlChatURLNoMetadataSkipsLinkPreview(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head></head><body>No metadata here</body></html>`)
	}))
	defer srv.Close()

	room := NewRoom()
	sb := &safeBuffer{}
	sender := &Client{
		Username: "alice",
		session:  &mockSender{},
		ctrl:     sb,
	}
	room.AddClient(sender)

	processControl(ControlMsg{Type: "chat", Message: srv.URL}, sender, room)

	// Wait briefly for the async goroutine to complete.
	time.Sleep(500 * time.Millisecond)

	data := sb.readAll()
	var msgs []ControlMsg
	for _, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var m ControlMsg
		if err := json.Unmarshal(line, &m); err == nil {
			msgs = append(msgs, m)
		}
	}

	if len(msgs) != 1 {
		t.Errorf("expected only 1 message (chat), got %d: %+v", len(msgs), msgs)
	}
	if msgs[0].Type != "chat" {
		t.Errorf("expected chat, got %q", msgs[0].Type)
	}
}
