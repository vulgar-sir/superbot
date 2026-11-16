package httpapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCollabHubBroadcastAndPresence(t *testing.T) {
	hub := newCollabHub()
	docID := uuid.New()
	first := hub.join(docID, "client-a")
	second := hub.join(docID, "client-b")
	if hub.count(docID) != 2 {
		t.Fatalf("count = %d, want 2", hub.count(docID))
	}
	hub.broadcast(docID, map[string]any{"type": "update", "event_id": 7, "content": "x"})
	for _, sub := range []*collabSubscriber{first, second} {
		select {
		case data := <-sub.events:
			var payload struct {
				Type    string `json:"type"`
				EventID int64  `json:"event_id"`
			}
			if err := json.Unmarshal(data, &payload); err != nil {
				t.Fatalf("decode event: %v", err)
			}
			if payload.Type != "update" || payload.EventID != 7 {
				t.Fatalf("unexpected event: %#v", payload)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for broadcast")
		}
	}
	hub.leave(docID, first)
	if hub.count(docID) != 1 {
		t.Fatalf("count after leave = %d, want 1", hub.count(docID))
	}
	hub.leave(docID, second)
	if hub.count(docID) != 0 {
		t.Fatalf("count after all leave = %d, want 0", hub.count(docID))
	}
}

func TestCollabHubRoomsAreIsolated(t *testing.T) {
	hub := newCollabHub()
	firstDoc := uuid.New()
	secondDoc := uuid.New()
	sub := hub.join(firstDoc, "client")
	hub.broadcast(secondDoc, map[string]any{"type": "update"})
	select {
	case data := <-sub.events:
		t.Fatalf("received cross-room event: %s", data)
	case <-time.After(50 * time.Millisecond):
	}
	hub.leave(firstDoc, sub)
}
