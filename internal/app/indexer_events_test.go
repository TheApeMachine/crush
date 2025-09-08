package app

import (
	"context"
	"testing"
	"time"
)

// TestIndexerEvents_PublishSubscribe verifies that published indexer events are delivered to subscribers.
func TestIndexerEvents_PublishSubscribe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := SubscribeIndexerEvents(ctx)

	IndexerWarn("Failed storing relationships", "file.go")

	select {
	case ev := <-ch:
		if ev.Payload.Type != IndexerEventWarn {
			t.Fatalf("expected type %q, got %q", IndexerEventWarn, ev.Payload.Type)
		}
		if ev.Payload.Message != "Failed storing relationships" {
			t.Fatalf("unexpected message: %q", ev.Payload.Message)
		}
		if ev.Payload.Path != "file.go" {
			t.Fatalf("unexpected path: %q", ev.Payload.Path)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event received from indexer broker")
	}
}
