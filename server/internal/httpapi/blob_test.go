package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"bken/server/internal/blob"
	"bken/server/internal/core"
	"bken/server/internal/store"
)

func TestBlobUploadAndDownload(t *testing.T) {
	t.Parallel()

	temp := t.TempDir()
	dbPath := filepath.Join(temp, "bken.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})

	blobsDir := filepath.Join(temp, "blobs")
	blobStore, err := blob.NewStore(blobsDir, st)
	if err != nil {
		t.Fatalf("create blob store: %v", err)
	}

	api := New(core.NewChannelState(""), st, blobStore)
	ts := httptest.NewServer(api.Echo())
	t.Cleanup(ts.Close)

	wantBytes := []byte("blob-bytes-for-recording")

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	filePart, err := writer.CreateFormFile("file", "recording.ogg")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := filePart.Write(wantBytes); err != nil {
		t.Fatalf("write multipart bytes: %v", err)
	}
	if err := writer.WriteField("kind", "recording"); err != nil {
		t.Fatalf("write kind field: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/blobs", &body)
	if err != nil {
		t.Fatalf("new upload request: %v", err)
	}
	req.Header.Set(echoHeaderContentType, writer.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected %d from upload, got %d: %s", http.StatusCreated, resp.StatusCode, string(raw))
	}

	var uploaded blobUploadResponse
	if err := json.NewDecoder(resp.Body).Decode(&uploaded); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if uploaded.ID == "" {
		t.Fatal("expected uploaded id")
	}
	if uploaded.Kind != "recording" {
		t.Fatalf("expected kind=recording, got %q", uploaded.Kind)
	}
	if uploaded.SizeBytes != int64(len(wantBytes)) {
		t.Fatalf("expected size=%d, got %d", len(wantBytes), uploaded.SizeBytes)
	}

	meta, err := st.BlobByID(context.Background(), uploaded.ID)
	if err != nil {
		t.Fatalf("lookup uploaded metadata: %v", err)
	}
	if meta.DiskName != uploaded.ID {
		t.Fatalf("expected uuid disk filename %q, got %q", uploaded.ID, meta.DiskName)
	}
	if meta.OriginalName != "recording.ogg" {
		t.Fatalf("unexpected original name %q", meta.OriginalName)
	}

	downloadResp, err := http.Get(ts.URL + "/api/blobs/" + uploaded.ID)
	if err != nil {
		t.Fatalf("download request: %v", err)
	}
	defer downloadResp.Body.Close()
	if downloadResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(downloadResp.Body)
		t.Fatalf("expected %d from download, got %d: %s", http.StatusOK, downloadResp.StatusCode, string(raw))
	}
	gotBytes, err := io.ReadAll(downloadResp.Body)
	if err != nil {
		t.Fatalf("read downloaded body: %v", err)
	}
	if !bytes.Equal(gotBytes, wantBytes) {
		t.Fatalf("downloaded bytes mismatch: got=%q want=%q", string(gotBytes), string(wantBytes))
	}
}

const echoHeaderContentType = "Content-Type"
