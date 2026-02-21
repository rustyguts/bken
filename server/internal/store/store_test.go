package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestCreateBlobAndLookup(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "bken.db")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})

	in := BlobMetadata{
		ID:           "35e748f1-45ef-4f12-b5e3-f17fe80326b0",
		Kind:         "recording",
		OriginalName: "voice.ogg",
		ContentType:  "audio/ogg",
		DiskName:     "35e748f1-45ef-4f12-b5e3-f17fe80326b0",
		SizeBytes:    42,
		CreatedAt:    time.UnixMilli(1_700_000_000_000).UTC(),
	}
	if err := st.CreateBlob(context.Background(), in); err != nil {
		t.Fatalf("create blob metadata: %v", err)
	}

	got, err := st.BlobByID(context.Background(), in.ID)
	if err != nil {
		t.Fatalf("lookup blob metadata: %v", err)
	}
	if got.ID != in.ID || got.Kind != in.Kind {
		t.Fatalf("unexpected blob metadata identity: %#v", got)
	}
	if got.OriginalName != in.OriginalName || got.ContentType != in.ContentType {
		t.Fatalf("unexpected blob metadata content fields: %#v", got)
	}
	if got.DiskName != in.DiskName || got.SizeBytes != in.SizeBytes {
		t.Fatalf("unexpected blob metadata disk fields: %#v", got)
	}
	if !got.CreatedAt.Equal(in.CreatedAt) {
		t.Fatalf("expected created_at=%s got=%s", in.CreatedAt, got.CreatedAt)
	}
}
