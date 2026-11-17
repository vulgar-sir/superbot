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

// Deleting a bot or a group deletes the memories scoped to it; nothing else
// is touched.
func TestIntegrationScopedMemoriesGoWithTheirScope(t *testing.T) {
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
	user, err := db.CreateUser(ctx, CreateUserParams{Email: "memclean-" + tag + "@example.test", PasswordHash: "x", DisplayName: "memclean " + tag})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := db.CreatePersonalAccount(ctx, CreatePersonalAccountParams{ID: user.ID, Name: "memclean-" + tag, CreatedByUserID: user.ID}); err != nil {
		t.Fatalf("create account: %v", err)
	}
	newBot := func(name string) Bot {
		short := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
		bot, err := db.CreateBot(ctx, CreateBotParams{
			OwnerID: user.ID, Name: name + "-" + short, Slug: name + "-" + short,
			Params: json.RawMessage(`{}`), Capabilities: json.RawMessage(`{}`), ToolPolicy: json.RawMessage(`{}`),
			ComputerProfile: "browser", Kind: "personal", AvatarShape: "circle", AvatarColor: "blue",
		})
		if err != nil {
			t.Fatalf("create bot: %v", err)
		}
		return bot
	}
	gone, kept := newBot("gone"), newBot("kept")
	team, err := db.CreateTeam(ctx, CreateTeamParams{OwnerID: user.ID, Name: "memclean-" + tag, Mode: "group", Config: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	remember := func(scopeType string, scopeID uuid.UUID) uuid.UUID {
		var id uuid.UUID
		if err := db.Pool.QueryRow(ctx, `INSERT INTO memories (owner_id, scope_type, scope_id, content)
			VALUES ($1, $2, $3, $4) RETURNING id`, user.ID, scopeType, scopeID, scopeType+" fact "+tag).Scan(&id); err != nil {
			t.Fatalf("insert %s memory: %v", scopeType, err)
		}
		return id
	}
	goneMemory := remember("bot", gone.ID)
	keptMemory := remember("bot", kept.ID)
	teamMemory := remember("team", team.ID)
	userMemory := remember("user", user.ID)
	exists := func(id uuid.UUID) bool {
		var n int
		if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM memories WHERE id = $1`, id).Scan(&n); err != nil {
			t.Fatalf("count memory: %v", err)
		}
		return n == 1
	}

	if _, err := db.DeleteBot(ctx, DeleteBotParams{ID: gone.ID, OwnerID: user.ID}); err != nil {
		t.Fatalf("delete bot: %v", err)
	}
	if exists(goneMemory) {
		t.Fatal("a deleted bot's memory must be deleted with it")
	}
	if !exists(keptMemory) || !exists(teamMemory) || !exists(userMemory) {
		t.Fatal("deleting one bot must not touch other scopes' memories")
	}
	if _, err := db.DeleteTeam(ctx, DeleteTeamParams{ID: team.ID, OwnerID: user.ID}); err != nil {
		t.Fatalf("delete team: %v", err)
	}
	if exists(teamMemory) {
		t.Fatal("a deleted group's memory must be deleted with it")
	}
	if !exists(keptMemory) || !exists(userMemory) {
		t.Fatal("deleting a group must not touch bot or personal memories")
	}
}
