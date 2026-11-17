//go:build integration

package store

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Bots whose provider was deleted or whose model left the provider's list
// move to a usable model; a provider without a model list is left alone.
func TestIntegrationRepointStaleBots(t *testing.T) {
	databaseURL := os.Getenv("SUPERBOT_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("SUPERBOT_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := Migrate(ctx, databaseURL); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()

	tag := uuid.NewString()[:8]
	user, err := db.CreateUser(ctx, CreateUserParams{Email: "repoint-" + tag + "@example.test", PasswordHash: "x", DisplayName: "repoint " + tag})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := db.CreatePersonalAccount(ctx, CreatePersonalAccountParams{ID: user.ID, Name: "repoint-" + tag, CreatedByUserID: user.ID}); err != nil {
		t.Fatalf("create account: %v", err)
	}
	provider := func(name string, models ...string) Provider {
		p, err := db.CreateProvider(ctx, CreateProviderParams{
			OwnerID: user.ID, Type: "llm", Kind: "openai_compat", Name: name, BaseUrl: "https://example.test",
			Config: json.RawMessage(`{}`), Enabled: true,
		})
		if err != nil {
			t.Fatalf("create provider: %v", err)
		}
		for _, m := range models {
			if _, err := db.CreateModel(ctx, CreateModelParams{ProviderID: p.ID, Name: m, SupportsTools: true}); err != nil {
				t.Fatalf("create model: %v", err)
			}
		}
		return p
	}
	bot := func(name string, providerID *uuid.UUID, model string) Bot {
		short := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
		b, err := db.CreateBot(ctx, CreateBotParams{
			OwnerID: user.ID, Name: name + "-" + short, Slug: name + "-" + short, ProviderID: providerID, Model: model,
			Params: json.RawMessage(`{}`), Capabilities: json.RawMessage(`{}`), ToolPolicy: json.RawMessage(`{}`),
			ComputerProfile: "browser", Kind: "personal", AvatarShape: "circle", AvatarColor: "blue",
		})
		if err != nil {
			t.Fatalf("create bot: %v", err)
		}
		return b
	}
	reload := func(b Bot) Bot {
		fresh, err := db.GetBot(ctx, GetBotParams{ID: b.ID, OwnerID: user.ID})
		if err != nil {
			t.Fatalf("get bot: %v", err)
		}
		return fresh
	}

	old := provider("old", "old-chat")
	listed := provider("listed", "listed-a", "listed-b")
	freeform := provider("freeform")
	orphan := bot("orphan", &old.ID, "old-chat")
	retired := bot("retired", &listed.ID, "listed-gone")
	fine := bot("fine", &listed.ID, "listed-b")
	custom := bot("custom", &freeform.ID, "anything-the-user-typed")

	if _, err := db.DeleteProvider(ctx, DeleteProviderParams{ID: old.ID, OwnerID: user.ID}); err != nil {
		t.Fatalf("delete provider: %v", err)
	}
	newest := provider("newest", "newest-chat")
	moved, err := db.RepointStaleBots(ctx, user.ID)
	if err != nil {
		t.Fatalf("repoint: %v", err)
	}
	if len(moved) != 2 {
		t.Fatalf("want the orphan and the retired-model bot moved, got %+v", moved)
	}
	if b := reload(orphan); b.ProviderID == nil || *b.ProviderID != newest.ID || b.Model != "newest-chat" {
		t.Fatalf("a bot whose provider was deleted goes to the newest provider: %+v / %s", b.ProviderID, b.Model)
	}
	if b := reload(retired); b.ProviderID == nil || *b.ProviderID != listed.ID || b.Model != "listed-a" {
		t.Fatalf("a bot whose model was retired stays with its provider: %+v / %s", b.ProviderID, b.Model)
	}
	if b := reload(fine); b.Model != "listed-b" {
		t.Fatalf("a working bot must not move: %s", b.Model)
	}
	if b := reload(custom); b.ProviderID == nil || *b.ProviderID != freeform.ID || b.Model != "anything-the-user-typed" {
		t.Fatalf("a provider without a model list is taken at its word: %+v / %s", b.ProviderID, b.Model)
	}
	if again, err := db.RepointStaleBots(ctx, user.ID); err != nil || len(again) != 0 {
		t.Fatalf("a second pass must find nothing to move: %+v, %v", again, err)
	}
}
