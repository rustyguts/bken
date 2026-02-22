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
		Kind:         "attachment",
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

func TestInsertAndGetMessages(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "bken.db")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	ctx := context.Background()
	id, err := st.InsertMessage(ctx, "srv1", "ch1", "u1", "Alice", "hello", 1000, "", "", 0)
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive message id, got %d", id)
	}

	rows, err := st.GetMessages(ctx, "srv1", "ch1", 50)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 message, got %d", len(rows))
	}
	if rows[0].Username != "Alice" || rows[0].Message != "hello" {
		t.Fatalf("unexpected message: %+v", rows[0])
	}
}

func TestAddAndRemoveReaction(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "bken.db")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	ctx := context.Background()

	// Insert a message to react to.
	msgID, err := st.InsertMessage(ctx, "srv1", "ch1", "u1", "Alice", "hi", 1000, "", "", 0)
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}

	// Add a reaction.
	if err := st.AddReaction(ctx, msgID, "u1", "👍"); err != nil {
		t.Fatalf("add reaction: %v", err)
	}

	// Add same reaction again (idempotent).
	if err := st.AddReaction(ctx, msgID, "u1", "👍"); err != nil {
		t.Fatalf("add duplicate reaction: %v", err)
	}

	// Add different user reaction.
	if err := st.AddReaction(ctx, msgID, "u2", "👍"); err != nil {
		t.Fatalf("add reaction u2: %v", err)
	}

	// Fetch reactions.
	rxMap, err := st.GetReactionsForMessages(ctx, []int64{msgID})
	if err != nil {
		t.Fatalf("get reactions: %v", err)
	}
	rxs := rxMap[msgID]
	if len(rxs) != 2 {
		t.Fatalf("expected 2 reaction rows, got %d", len(rxs))
	}

	// Remove one reaction.
	if err := st.RemoveReaction(ctx, msgID, "u1", "👍"); err != nil {
		t.Fatalf("remove reaction: %v", err)
	}

	rxMap, err = st.GetReactionsForMessages(ctx, []int64{msgID})
	if err != nil {
		t.Fatalf("get reactions after remove: %v", err)
	}
	rxs = rxMap[msgID]
	if len(rxs) != 1 {
		t.Fatalf("expected 1 reaction row, got %d", len(rxs))
	}
	if rxs[0].UserID != "u2" {
		t.Fatalf("expected u2, got %s", rxs[0].UserID)
	}
}

func TestGetReactionsForMessagesEmpty(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "bken.db")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	rxMap, err := st.GetReactionsForMessages(context.Background(), nil)
	if err != nil {
		t.Fatalf("get reactions empty: %v", err)
	}
	if rxMap != nil {
		t.Fatalf("expected nil map, got %v", rxMap)
	}
}
