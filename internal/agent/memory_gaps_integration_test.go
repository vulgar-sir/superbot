//go:build integration

package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"superbot/internal/memory"
	"superbot/internal/store"
	"superbot/internal/tools"
)

// flakyEmbedder is an OpenAI-compatible embedding endpoint that can be taken
// down; its vectors are the local hash of the input.
type flakyEmbedder struct {
	down  atomic.Bool
	calls atomic.Int32
}

func (f *flakyEmbedder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.calls.Add(1)
	if f.down.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	var payload struct {
		Input string `json:"input"`
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": []map[string]any{{"embedding": memory.Embed(payload.Input)}},
	})
}

// perConversationExtractor answers its reply only for the target
// conversation and nothing worth keeping for any other.
type perConversationExtractor struct {
	target  uuid.UUID
	reply   string
	request *memory.ExtractRequest
}

func (p *perConversationExtractor) Extract(_ context.Context, req memory.ExtractRequest) (string, error) {
	if req.ConversationID != p.target {
		return `{"memories":[]}`, nil
	}
	p.request = &req
	return p.reply, nil
}

func memoryRow(t *testing.T, f *fixture, id uuid.UUID) store.Memory {
	t.Helper()
	row, err := f.db.GetMemory(context.Background(), store.GetMemoryParams{ID: id, OwnerID: f.account})
	if err != nil {
		t.Fatalf("get memory: %v", err)
	}
	return row
}

// A save during an embedding outage is a save: it is stored, reported as
// pending, found by keyword meanwhile, and indexed by the background pass.
func TestIntegrationMemorySaveDuringOutageIsReembedded(t *testing.T) {
	f := newFixture(t, 128_000, 8_000)
	ctx := context.Background()
	bot := f.bot("Outage", true)
	embedder := &flakyEmbedder{}
	server := httptest.NewServer(embedder)
	defer server.Close()
	svc := memory.NewService(f.db, "key", server.URL, "test-embedding-model")
	svc.SetMinSimilarity(0.12) // the fake embeds with the local hash

	embedder.down.Store(true)
	saved, err := svc.SaveWith(ctx, memory.SaveInput{
		OwnerID: f.account, ScopeType: "bot", ScopeID: bot.ID,
		Content: "部署包保存在 /srv/releases 目录", Source: memory.SourceTool,
	})
	if err != nil {
		t.Fatalf("a save during an outage must not fail: %v", err)
	}
	if !saved.Pending || saved.Duplicate {
		t.Fatalf("result = %+v, want a new pending memory", saved)
	}
	if row := memoryRow(t, f, saved.Memory.ID); row.Embedding != nil {
		t.Fatal("no vector can exist yet")
	}
	// Back up (one failure does not open the breaker): until the background
	// pass runs, the memory has no vector and is found by keyword.
	embedder.down.Store(false)
	if hits := svc.Recall(ctx, f.account, bot.ID, nil, nil, "/srv/releases", 6); len(hits) != 1 {
		t.Fatalf("keyword recall must find the pending memory, got %d", len(hits))
	}

	report, err := svc.ReembedPending(ctx, 100, time.Minute)
	if err != nil {
		t.Fatalf("re-embed: %v", err)
	}
	if report.Embedded < 1 {
		t.Fatalf("re-embed report = %+v", report)
	}
	row := memoryRow(t, f, saved.Memory.ID)
	if row.Embedding == nil || row.EmbeddingModel != svc.ModelName() {
		t.Fatalf("the background pass must index the memory, model=%q", row.EmbeddingModel)
	}
	if hits := svc.Recall(ctx, f.account, bot.ID, nil, nil, "部署包放在哪个目录", 6); len(hits) != 1 || hits[0].ID != saved.Memory.ID {
		t.Fatalf("vector recall after re-embed got %d hits", len(hits))
	}

	// An edit during an outage drops the stale vector instead of keeping it.
	embedder.down.Store(true)
	updated, pending, err := svc.UpdateInScope(ctx, f.account, saved.Memory.ID, memory.Scope{Type: "bot", ID: bot.ID}, "部署包改放在 /data/releases 目录")
	if err != nil || !pending || updated.Content != "部署包改放在 /data/releases 目录" {
		t.Fatalf("update = %+v, pending=%v, err=%v", updated, pending, err)
	}
	if row := memoryRow(t, f, saved.Memory.ID); row.Embedding != nil {
		t.Fatal("the vector of the old text must go with it")
	}
	embedder.down.Store(false)
	if _, err := svc.ReembedPending(ctx, 100, time.Minute); err != nil {
		t.Fatalf("re-embed after edit: %v", err)
	}
	if row := memoryRow(t, f, saved.Memory.ID); row.Embedding == nil {
		t.Fatal("the edited memory must be indexed again")
	}
}

// Rows from an older embedding space move to the current one.
func TestIntegrationMemoryReembedMovesOldSpace(t *testing.T) {
	f := newFixture(t, 128_000, 8_000)
	ctx := context.Background()
	bot := f.bot("Spaces", true)
	if _, err := f.db.Pool.Exec(ctx, `
		INSERT INTO memories (owner_id, scope_type, scope_id, content, embedding, embedding_model, metadata)
		VALUES ($1, 'bot', $2, '旧空间里的记忆', $3::vector, 'remote:intfloat/multilingual-e5-large', '{}'::jsonb)`,
		f.account, bot.ID, memory.VectorText("旧空间里的记忆")); err != nil {
		t.Fatalf("insert: %v", err)
	}
	embedder := &flakyEmbedder{}
	server := httptest.NewServer(embedder)
	defer server.Close()
	svc := memory.NewService(f.db, "key", server.URL, "intfloat/multilingual-e5-large")
	// The newest active rows go first, so one small pass reaches this one
	// without walking the rest of a shared test database.
	if _, err := svc.ReembedPending(ctx, 50, time.Minute); err != nil {
		t.Fatalf("re-embed: %v", err)
	}
	var model string
	if err := f.db.Pool.QueryRow(ctx, `SELECT embedding_model FROM memories WHERE scope_id = $1`, bot.ID).Scan(&model); err != nil {
		t.Fatal(err)
	}
	if model != "remote:intfloat/multilingual-e5-large@e5-prefix-v1" {
		t.Fatalf("embedding_model = %q", model)
	}
}

type supersedeAllJudge struct{ calls int }

func (j *supersedeAllJudge) JudgeConflict(_ context.Context, _ string, related []store.Memory) (memory.ConflictDecision, error) {
	j.calls++
	var decision memory.ConflictDecision
	for _, item := range related {
		decision.Supersedes = append(decision.Supersedes, item.ID)
	}
	return decision, nil
}

// An updated fact retires the old one (archived, pointing at the new one);
// a pinned old fact stays and is reported; without a judge both are kept.
func TestIntegrationMemoryConflictsSupersede(t *testing.T) {
	f := newFixture(t, 128_000, 8_000)
	ctx := context.Background()
	bot := f.bot("Conflicts", true)
	svc := f.engine.memory
	scope := memory.Scope{Type: "bot", ID: bot.ID}
	save := func(content string, judge memory.ConflictJudge) memory.SaveResult {
		t.Helper()
		result, err := svc.SaveWith(ctx, memory.SaveInput{
			OwnerID: f.account, ScopeType: scope.Type, ScopeID: scope.ID,
			Content: content, Source: memory.SourceTool, Judge: judge,
		})
		if err != nil {
			t.Fatalf("save %q: %v", content, err)
		}
		return result
	}

	old := save("测试服务器的 SSH 端口是 2222", nil)
	kept := save("测试服务器的 SSH 端口是 2200", nil)
	if len(kept.Related) == 0 || kept.Related[0].ID != old.Memory.ID || len(kept.Superseded) != 0 {
		t.Fatalf("without a judge a similar memory is reported, not retired: %+v", kept)
	}

	judge := &supersedeAllJudge{}
	newer := save("测试服务器的 SSH 端口改成了 2022", judge)
	if judge.calls != 1 || len(newer.Superseded) < 1 {
		t.Fatalf("the judge must be asked and its verdict applied: calls=%d result=%+v", judge.calls, newer)
	}
	retired := memoryRow(t, f, old.Memory.ID)
	var meta map[string]string
	_ = json.Unmarshal(retired.Metadata, &meta)
	if retired.ArchivedAt == nil || meta["superseded_by"] != newer.Memory.ID.String() || meta["superseded_reason"] != memory.SourceTool {
		t.Fatalf("old memory archived=%v metadata=%s", retired.ArchivedAt != nil, retired.Metadata)
	}
	for _, hit := range svc.Recall(ctx, f.account, bot.ID, nil, nil, "测试服务器 SSH 端口", 6) {
		if hit.ID == old.Memory.ID {
			t.Fatal("a superseded memory must not be recalled")
		}
	}
	restored, err := svc.Restore(ctx, f.account, old.Memory.ID)
	if err != nil || restored.ArchivedAt != nil || strings.Contains(string(restored.Metadata), "superseded_by") {
		t.Fatalf("restore must clear the replacement: %s, %v", restored.Metadata, err)
	}

	if _, err := svc.SetPinned(ctx, f.account, old.Memory.ID, true); err != nil {
		t.Fatal(err)
	}
	pinnedCase := save("测试服务器的 SSH 端口最终定为 2223", judge)
	found := false
	for _, item := range pinnedCase.PinnedConflicts {
		found = found || item.ID == old.Memory.ID
	}
	if !found || memoryRow(t, f, old.Memory.ID).ArchivedAt != nil {
		t.Fatalf("a pinned memory is reported, never retired: %+v", pinnedCase.PinnedConflicts)
	}

	// The user can retire a pinned memory explicitly.
	archived, err := svc.ArchiveInScope(ctx, f.account, old.Memory.ID, scope, pinnedCase.Memory.ID)
	if err != nil || archived.ArchivedAt == nil {
		t.Fatalf("archive by the user: %v", err)
	}
}

// In a group, personal facts go to the person who said them; the review
// reads the speakers from the messages the chat service stamped.
func TestIntegrationMemoryTeamReviewAttributesPeople(t *testing.T) {
	t.Setenv(memory.ReviewIdleSecondsEnv, "1")
	f := newFixture(t, 128_000, 8_000)
	ctx := context.Background()
	bot := f.bot("TeamReviewer", true)
	if _, err := f.db.Pool.Exec(ctx, `UPDATE bots SET memory_enabled = true WHERE id = $1`, bot.ID); err != nil {
		t.Fatal(err)
	}
	other, err := f.db.CreateUser(ctx, store.CreateUserParams{
		Email: "it-mate-" + uuid.NewString()[:8] + "@example.test", PasswordHash: "x", DisplayName: "Mate",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.db.AddAccountMember(ctx, store.AddAccountMemberParams{AccountID: f.account, UserID: other.ID, Role: "member"}); err != nil {
		t.Fatal(err)
	}
	team, err := f.db.CreateTeam(ctx, store.CreateTeamParams{
		OwnerID: f.account, Name: "mem-" + uuid.NewString()[:6], Mode: "group", Config: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.db.AddTeamBot(ctx, store.AddTeamBotParams{TeamID: team.ID, BotID: bot.ID, Role: "member"}); err != nil {
		t.Fatal(err)
	}
	conv, err := f.db.CreateConversation(ctx, store.CreateConversationParams{
		TeamID: &team.ID, Kind: "team", Title: "memory", CreatedBy: f.account,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []struct {
		user    uuid.UUID
		content string
	}{
		{f.user, "我对花生过敏，订餐别点含花生的"},
		{other.ID, "我住在杭州滨江区"},
		{f.user, "周报以后每周五下午发"},
		{other.ID, "好的，那就这么定"},
	} {
		if _, err := f.chat.SendUserMessage(ctx, f.account, line.user, conv.ID, line.content, "", nil, nil, nil); err != nil {
			t.Fatalf("send: %v", err)
		}
	}
	if _, err := f.db.Pool.Exec(ctx, `UPDATE cycles SET status = 'canceled' WHERE conversation_id = $1`, conv.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool.Exec(ctx, `UPDATE messages SET created_at = now() - interval '5 minutes' WHERE conversation_id = $1`, conv.ID); err != nil {
		t.Fatal(err)
	}

	extractor := &perConversationExtractor{target: conv.ID, reply: `{"memories":[
		{"content":"用户对花生过敏","scope":"user","user":"U1"},
		{"content":"用户住在杭州滨江区","scope":"user","user":"U2"},
		{"content":"周报每周五下午发","scope":"team"}]}`}
	reviewer := memory.NewReviewer(f.db, f.engine.memory, extractor, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if !reviewer.Enabled() {
		t.Skip("memory review is disabled by environment")
	}
	// Other idle conversations of a shared test database may be due first.
	for i := 0; i < 20 && extractor.request == nil; i++ {
		if _, err := reviewer.ReviewDue(ctx); err != nil {
			t.Fatalf("review: %v", err)
		}
	}
	if extractor.request == nil {
		t.Fatal("the group conversation was never reviewed")
	}
	if prompt := extractor.request.User; !strings.Contains(prompt, "[用户 U1") || !strings.Contains(prompt, "[用户 U2·Mate]") {
		t.Fatalf("the transcript must label the speakers:\n%s", prompt)
	}
	var cursor int64
	if err := f.db.Pool.QueryRow(ctx, `SELECT last_ordinal FROM memory_review_cursors WHERE conversation_id = $1`, conv.ID).Scan(&cursor); err != nil {
		t.Fatalf("read cursor: %v", err)
	}
	var newest int64
	if err := f.db.Pool.QueryRow(ctx, `SELECT max(ordinal) FROM messages WHERE conversation_id = $1 AND sender_type IN ('user', 'bot')`, conv.ID).Scan(&newest); err != nil {
		t.Fatal(err)
	}
	if cursor != newest {
		t.Fatalf("cursor = %d, want the newest message %d", cursor, newest)
	}
	list := func(scopeType string, id uuid.UUID) []store.Memory {
		rows, err := f.db.ListMemories(ctx, store.ListMemoriesParams{OwnerID: f.account, ScopeType: scopeType, ScopeID: id, Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	if rows := list("user", f.user); len(rows) != 1 || !strings.Contains(rows[0].Content, "花生") {
		t.Fatalf("first speaker's memories = %+v", rows)
	}
	if rows := list("user", other.ID); len(rows) != 1 || !strings.Contains(rows[0].Content, "滨江") {
		t.Fatalf("second speaker's memories = %+v", rows)
	}
	if rows := list("team", team.ID); len(rows) != 1 || !strings.Contains(rows[0].Content, "周报") {
		t.Fatalf("team memories = %+v", rows)
	}

	// The speaker's own memories are recalled for them in the group, and
	// not for the other person.
	messages := f.messages(conv)
	var fromFirst, fromOther store.Message
	for _, msg := range messages {
		if strings.Contains(msg.Content, "花生") {
			fromFirst = msg
		}
		if strings.Contains(msg.Content, "滨江") {
			fromOther = msg
		}
	}
	if got := recallUserID(conv, fromFirst); got == nil || *got != f.user {
		t.Fatalf("recall user of the first speaker = %v", got)
	}
	if got := recallUserID(conv, fromOther); got == nil || *got != other.ID {
		t.Fatalf("recall user of the second speaker = %v", got)
	}
	hits := f.engine.memory.Recall(ctx, f.account, bot.ID, conv.TeamID, recallUserID(conv, fromOther), "花生过敏", 6)
	for _, hit := range hits {
		if hit.ScopeType == "user" && hit.ScopeID == f.user {
			t.Fatal("one person's memories must never be recalled for another")
		}
	}
}

// Recall follows the bot's memory switch: off means no memories in the prompt.
func TestIntegrationMemoryRecallFollowsMemorySwitch(t *testing.T) {
	f := newFixture(t, 128_000, 8_000)
	ctx := context.Background()
	bot := f.bot("Switch", true)
	conv := f.direct(bot)
	if _, err := f.engine.memory.SaveWith(ctx, memory.SaveInput{
		OwnerID: f.account, ScopeType: "user", ScopeID: f.user, Content: "部署端口是 32608", Source: memory.SourceManual,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.CreateMessage(ctx, store.CreateMessageParams{
		ConversationID: conv.ID, SenderType: "user", Metadata: json.RawMessage(`{}`), Content: "部署端口是多少",
	}); err != nil {
		t.Fatal(err)
	}
	prompt := func(enabled bool) string {
		t.Helper()
		bot.MemoryEnabled = enabled
		messages, _, _, err := f.engine.buildMessages(ctx, bot, conv, nil, tools.NewRegistry(), "", "", "", false, nil)
		if err != nil {
			t.Fatalf("build messages: %v", err)
		}
		var all strings.Builder
		for _, msg := range messages {
			for _, part := range msg.Parts {
				all.WriteString(part.Text)
			}
		}
		return all.String()
	}
	if on := prompt(true); !strings.Contains(on, "相关记忆") || !strings.Contains(on, "32608") {
		t.Fatal("with memory on, the related memory must be recalled")
	}
	if off := prompt(false); strings.Contains(off, "相关记忆") || strings.Contains(off, "32608") {
		t.Fatal("with memory off, nothing may be recalled")
	}
}
