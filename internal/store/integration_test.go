//go:build integration

package store

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestDocumentEventsPostgres(t *testing.T) {
	databaseURL := os.Getenv("SUPERBOT_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("SUPERBOT_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	var accountID uuid.UUID
	if err := db.Pool.QueryRow(ctx, `SELECT id FROM accounts ORDER BY created_at LIMIT 1`).Scan(&accountID); err != nil {
		t.Fatalf("load account: %v", err)
	}
	title := "integration-" + uuid.NewString()
	doc, err := db.CreateDocument(ctx, CreateDocumentParams{
		OwnerID: accountID, Title: title, Content: "v1", Kind: "document",
	})
	if err != nil {
		t.Fatalf("create document: %v", err)
	}
	defer func() {
		_, _ = db.DeleteDocument(context.Background(), DeleteDocumentParams{ID: doc.ID, OwnerID: accountID})
	}()
	if doc.Revision != 1 {
		t.Fatalf("initial revision = %d, want 1", doc.Revision)
	}
	updated, err := db.UpdateDocument(ctx, UpdateDocumentParams{
		ID: doc.ID, OwnerID: accountID, Title: title, Content: "v2", Kind: "document",
	})
	if err != nil {
		t.Fatalf("update document: %v", err)
	}
	if updated.Revision != 2 {
		t.Fatalf("updated revision = %d, want 2", updated.Revision)
	}
	payload, _ := json.Marshal(map[string]any{"type": "update", "content": "v2"})
	event, err := db.CreateDocumentEvent(ctx, CreateDocumentEventParams{DocumentID: doc.ID, Payload: payload})
	if err != nil {
		t.Fatalf("create event: %v", err)
	}
	latest, err := db.GetLatestDocumentEventID(ctx, doc.ID)
	if err != nil || latest < event.ID {
		t.Fatalf("latest event id = %d err=%v, want >= %d", latest, err, event.ID)
	}
	events, err := db.ListDocumentEventsAfter(ctx, ListDocumentEventsAfterParams{DocumentID: doc.ID, ID: event.ID - 1})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 || events[0].ID != event.ID {
		t.Fatalf("events = %#v, want one event %d", events, event.ID)
	}
}
