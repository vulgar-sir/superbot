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

// Creating a bot queues the creation of its computer and deleting it queues
// the removal, whichever code path creates or deletes the bot.
func TestIntegrationBotComputerLifecycleTriggers(t *testing.T) {
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
	user, err := db.CreateUser(ctx, CreateUserParams{Email: "lifecycle-" + tag + "@example.test", PasswordHash: "x", DisplayName: "lifecycle " + tag})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := db.CreatePersonalAccount(ctx, CreatePersonalAccountParams{ID: user.ID, Name: "lifecycle-" + tag, CreatedByUserID: user.ID}); err != nil {
		t.Fatalf("create account: %v", err)
	}
	accountID := user.ID
	var defaultProfile string
	if err := db.Pool.QueryRow(ctx, `SELECT column_default FROM information_schema.columns
		WHERE table_name = 'bots' AND column_name = 'computer_profile'`).Scan(&defaultProfile); err != nil {
		t.Fatalf("read default profile: %v", err)
	}
	if !strings.Contains(defaultProfile, "browser") {
		t.Fatalf("bots must default to a browser computer, default is %s", defaultProfile)
	}

	short := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	bot, err := db.CreateBot(ctx, CreateBotParams{
		OwnerID: accountID, Name: "lifecycle-" + short, Slug: "lifecycle-" + short,
		Params: json.RawMessage(`{}`), Capabilities: json.RawMessage(`{}`), ToolPolicy: json.RawMessage(`{}`),
		ComputerProfile: "browser", Kind: "personal", AvatarShape: "circle", AvatarColor: "blue",
	})
	if err != nil {
		t.Fatalf("create bot: %v", err)
	}
	actions, err := db.ListWorkspaceActionsForBot(ctx, ListWorkspaceActionsForBotParams{OwnerID: accountID, BotID: bot.ID})
	if err != nil {
		t.Fatalf("list actions: %v", err)
	}
	if len(actions) != 1 || actions[0].Kind != "provision" || actions[0].Status != "pending" {
		t.Fatalf("a new bot must queue one pending provision action, got %#v", actions)
	}

	if n, err := db.DeleteBot(ctx, DeleteBotParams{ID: bot.ID, OwnerID: accountID}); err != nil || n != 1 {
		t.Fatalf("delete bot: n=%d err=%v", n, err)
	}
	defer func() { _ = db.DeleteComputerRemoval(context.Background(), bot.ID) }()
	due, err := db.ListDueComputerRemovals(ctx, 1000)
	if err != nil {
		t.Fatalf("list removals: %v", err)
	}
	if !containsRemoval(due, bot.ID, accountID) {
		t.Fatalf("deleting a bot must queue the removal of its computer, got %#v", due)
	}
	existing, err := db.ExistingBotIDs(ctx, []uuid.UUID{bot.ID})
	if err != nil || len(existing) != 0 {
		t.Fatalf("a deleted bot must not be reported as existing: %v %v", existing, err)
	}

	// A failed removal backs off instead of being retried every tick.
	if err := db.DeferComputerRemoval(ctx, DeferComputerRemovalParams{BotID: bot.ID, LastError: "docker unavailable"}); err != nil {
		t.Fatalf("defer removal: %v", err)
	}
	due, _ = db.ListDueComputerRemovals(ctx, 1000)
	if containsRemoval(due, bot.ID, accountID) {
		t.Fatal("a deferred removal must wait before it is due again")
	}
	if err := db.DeleteComputerRemoval(ctx, bot.ID); err != nil {
		t.Fatalf("delete removal: %v", err)
	}
}

func containsRemoval(rows []ComputerRemoval, botID, ownerID uuid.UUID) bool {
	for _, row := range rows {
		if row.BotID == botID && row.OwnerID == ownerID {
			return true
		}
	}
	return false
}
