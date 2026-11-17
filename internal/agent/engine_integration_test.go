//go:build integration

package agent

// These tests drive the real engine against a real Postgres. The model is the
// built-in "mock" provider, steered with inline directives in the user
// message ([[tool:…]], [[slow:…]], [[fail:…]]), so no network or API key is
// involved. Point SUPERBOT_TEST_DATABASE_URL at a throwaway database.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"superbot/internal/chat"
	"superbot/internal/config"
	"superbot/internal/events"
	"superbot/internal/goals"
	"superbot/internal/llm"
	"superbot/internal/secrets"
	"superbot/internal/store"
)

type fixture struct {
	t        *testing.T
	db       *store.DB
	bus      *events.Bus
	engine   *Engine
	chat     *chat.Service
	account  uuid.UUID
	user     uuid.UUID
	provider store.Provider
	dataDir  string
}

func newFixture(t *testing.T, contextWindow, maxOutput int32) *fixture {
	t.Helper()
	databaseURL := os.Getenv("SUPERBOT_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("SUPERBOT_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := store.Migrate(ctx, databaseURL); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db, err := store.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(db.Close)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if os.Getenv("SUPERBOT_TEST_VERBOSE") != "" {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	box, err := secrets.New([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("secret box: %v", err)
	}
	bus := events.NewBus(db.Pool, logger)
	dataDir := t.TempDir()
	cfg := config.Config{DataDir: dataDir, LLMTimeout: 30 * time.Second}

	tag := uuid.NewString()[:8]
	user, err := db.CreateUser(ctx, store.CreateUserParams{
		Email: "it-" + tag + "@example.test", PasswordHash: "x", DisplayName: "IT " + tag,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := db.CreatePersonalAccount(ctx, store.CreatePersonalAccountParams{
		ID: user.ID, Name: "it-" + tag, CreatedByUserID: user.ID,
	}); err != nil {
		t.Fatalf("create account: %v", err)
	}
	if err := db.AddAccountMember(ctx, store.AddAccountMemberParams{AccountID: user.ID, UserID: user.ID, Role: "owner"}); err != nil {
		t.Fatalf("add member: %v", err)
	}
	if err := db.SetUserAutoReview(ctx, store.SetUserAutoReviewParams{ID: user.ID, AutoReview: true}); err != nil {
		t.Fatalf("set auto review: %v", err)
	}
	provider, err := db.CreateProvider(ctx, store.CreateProviderParams{
		OwnerID: user.ID, Type: "llm", Kind: "mock", Name: "mock-" + tag,
		Config: json.RawMessage(`{}`), Enabled: true,
	})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	if _, err := db.CreateModel(ctx, store.CreateModelParams{
		ProviderID: provider.ID, Name: "mock-model", ContextWindow: contextWindow, MaxOutput: maxOutput,
		SupportsTools: true,
	}); err != nil {
		t.Fatalf("create model: %v", err)
	}
	// Leftover cycles from an aborted earlier run would be claimed first.
	if _, err := db.Pool.Exec(ctx, `UPDATE cycles SET status = 'canceled', lease_id = NULL, lease_until = NULL WHERE status IN ('queued', 'running')`); err != nil {
		t.Fatalf("clear stale cycles: %v", err)
	}
	return &fixture{
		t: t, db: db, bus: bus, account: user.ID, user: user.ID, provider: provider, dataDir: dataDir,
		engine: NewEngine(db, llm.NewFactory(box), bus, cfg, nil, box, logger),
		chat:   chat.NewService(db, bus, logger),
	}
}

func (f *fixture) bot(name string, withProvider bool) store.Bot {
	f.t.Helper()
	params := store.CreateBotParams{
		OwnerID: f.account, Name: name, Slug: strings.ToLower(name) + "-" + uuid.NewString()[:6],
		Model: "mock-model", Params: json.RawMessage(`{}`), Capabilities: json.RawMessage(`{}`),
		ToolPolicy: json.RawMessage(`{}`), ComputerProfile: "none", Kind: "personal",
		AvatarShape: "circle", AvatarColor: "blue",
	}
	if withProvider {
		params.ProviderID = &f.provider.ID
	}
	bot, err := f.db.CreateBot(context.Background(), params)
	if err != nil {
		f.t.Fatalf("create bot %s: %v", name, err)
	}
	return bot
}

func (f *fixture) direct(bot store.Bot) store.Conversation {
	f.t.Helper()
	conv, err := f.chat.CreateDirectConversation(context.Background(), f.account, f.user, bot.ID, "")
	if err != nil {
		f.t.Fatalf("create direct conversation: %v", err)
	}
	return conv
}

func (f *fixture) send(conv store.Conversation, content string) chat.SendResult {
	f.t.Helper()
	result, err := f.chat.SendUserMessage(context.Background(), f.account, f.user, conv.ID, content, "", nil, nil, nil)
	if err != nil {
		f.t.Fatalf("send %q: %v", content, err)
	}
	return result
}

// step claims and runs one cycle. It reports false when nothing is runnable.
func (f *fixture) step(ctx context.Context) bool {
	f.t.Helper()
	cycle, err := f.db.ClaimCycle(context.Background())
	if errors.Is(err, pgx.ErrNoRows) {
		return false
	}
	if err != nil {
		f.t.Fatalf("claim cycle: %v", err)
	}
	_ = f.engine.ProcessCycle(ctx, cycle, *cycle.LeaseID)
	return true
}

func (f *fixture) drain(limit int) int {
	ran := 0
	for ran < limit && f.step(context.Background()) {
		ran++
	}
	return ran
}

func (f *fixture) cycle(id uuid.UUID) store.Cycle {
	f.t.Helper()
	cycle, err := f.db.GetCycle(context.Background(), id)
	if err != nil {
		f.t.Fatalf("get cycle: %v", err)
	}
	return cycle
}

func (f *fixture) messages(conv store.Conversation) []store.Message {
	f.t.Helper()
	rows, err := f.db.ListMessages(context.Background(), conv.ID)
	if err != nil {
		f.t.Fatalf("list messages: %v", err)
	}
	return rows
}

func botReplies(messages []store.Message) []store.Message {
	var out []store.Message
	for _, msg := range messages {
		if msg.SenderType == "bot" {
			out = append(out, msg)
		}
	}
	return out
}

func (f *fixture) toolRuns(cycleID uuid.UUID, tool string) int {
	f.t.Helper()
	var count int
	err := f.db.Pool.QueryRow(context.Background(), `
		SELECT count(*) FROM tool_invocations ti
		JOIN runs r ON r.id = ti.run_id
		WHERE r.cycle_id = $1 AND ti.name = $2`, cycleID, tool).Scan(&count)
	if err != nil {
		f.t.Fatalf("count tool invocations: %v", err)
	}
	return count
}

func TestIntegrationDirectTurnWithTool(t *testing.T) {
	f := newFixture(t, 128_000, 8_000)
	conv := f.direct(f.bot("Alpha", true))
	sent := f.send(conv, `请回查 [[tool:search_chat_history {"query":"不存在的词"}]]`)
	if sent.Cycle == nil {
		t.Fatal("first message must create a cycle")
	}
	if ran := f.drain(3); ran != 1 {
		t.Fatalf("ran %d cycles, want 1", ran)
	}
	if status := f.cycle(sent.Cycle.ID).Status; status != "done" {
		t.Fatalf("cycle status = %s", status)
	}
	replies := botReplies(f.messages(conv))
	if len(replies) != 1 || !strings.Contains(replies[0].Content, "Mock 工具结果") {
		t.Fatalf("unexpected replies: %+v", replies)
	}
	if got := f.toolRuns(sent.Cycle.ID, "search_chat_history"); got != 1 {
		t.Fatalf("tool ran %d times, want 1", got)
	}
	if used := messageToolsUsed(replies[0]); len(used) != 1 || used[0] != "search_chat_history" {
		t.Fatalf("tools_used metadata = %v", used)
	}
}

func TestIntegrationInterjectionReachesRunningTurn(t *testing.T) {
	f := newFixture(t, 128_000, 8_000)
	conv := f.direct(f.bot("Beta", true))
	sent := f.send(conv, `[[slow:1200]] [[tool:search_chat_history {"query":"zzz"}]]`)

	done := make(chan struct{})
	go func() {
		defer close(done)
		f.step(context.Background())
	}()
	time.Sleep(300 * time.Millisecond)
	interjection := f.send(conv, "改成只查上周的记录")
	if !interjection.Interjection || interjection.Cycle != nil {
		t.Fatalf("message during a running cycle must be an interjection: %+v", interjection)
	}
	<-done

	replies := botReplies(f.messages(conv))
	if len(replies) != 1 {
		t.Fatalf("want exactly one reply, got %d", len(replies))
	}
	if !strings.Contains(replies[0].Content, "改成只查上周的记录") {
		t.Fatalf("the running turn never saw the interjection: %q", replies[0].Content)
	}
	rows, err := f.db.ListEventsAfter(context.Background(), store.ListEventsAfterParams{ConversationID: conv.ID, Seq: 0, Limit: 500})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	delivered := false
	for _, row := range rows {
		if row.Type == events.TypeStatus && strings.Contains(string(row.Payload), interjection.Message.ID.String()) {
			delivered = true
		}
	}
	if !delivered {
		t.Fatal("no status event reported the interjection as delivered")
	}
	if f.step(context.Background()) {
		t.Fatal("a delivered interjection must not also start a follow-up cycle")
	}
	if status := f.cycle(sent.Cycle.ID).Status; status != "done" {
		t.Fatalf("cycle status = %s", status)
	}
}

func TestIntegrationLateInterjectionGetsFollowUp(t *testing.T) {
	f := newFixture(t, 128_000, 8_000)
	conv := f.direct(f.bot("Gamma", true))
	f.send(conv, "[[slow:1200]] 第一问")

	done := make(chan struct{})
	go func() {
		defer close(done)
		f.step(context.Background())
	}()
	time.Sleep(300 * time.Millisecond)
	late := f.send(conv, "第二问")
	<-done

	active, err := f.db.GetActiveCycle(context.Background(), conv.ID)
	if err != nil {
		t.Fatalf("a message that arrived during the final model call must get a follow-up cycle: %v", err)
	}
	if active.TriggerMessageID != late.Message.ID {
		t.Fatal("follow-up cycle is not triggered by the late message")
	}
	f.drain(2)
	replies := botReplies(f.messages(conv))
	if len(replies) != 2 || !strings.Contains(replies[1].Content, "第二问") {
		t.Fatalf("late message was not answered: %+v", replies)
	}
}

func TestIntegrationResumeDoesNotReplayTools(t *testing.T) {
	f := newFixture(t, 128_000, 8_000)
	conv := f.direct(f.bot("Delta", true))
	sent := f.send(conv, `[[slow:1500]] [[tool:search_chat_history {"query":"zzz"}]]`)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.step(ctx)
	}()
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := f.db.GetProgressCheckpointForCycle(context.Background(), sent.Cycle.ID); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no progress checkpoint was written after the tool round")
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Simulate the worker being shut down during the second model call.
	cancel()
	<-done

	interrupted := f.cycle(sent.Cycle.ID)
	if interrupted.Status != "running" {
		t.Fatalf("interrupted cycle status = %s, want running (handed back)", interrupted.Status)
	}
	if interrupted.Attempt != 0 {
		t.Fatalf("a graceful shutdown must not consume an attempt, attempt = %d", interrupted.Attempt)
	}
	if !f.step(context.Background()) {
		t.Fatal("the released cycle was not reclaimable immediately")
	}
	if status := f.cycle(sent.Cycle.ID).Status; status != "done" {
		t.Fatalf("resumed cycle status = %s", status)
	}
	if got := f.toolRuns(sent.Cycle.ID, "search_chat_history"); got != 1 {
		t.Fatalf("tool ran %d times across the restart, want 1", got)
	}
	if _, err := f.db.GetProgressCheckpointForCycle(context.Background(), sent.Cycle.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("progress checkpoint should be closed after the turn finishes")
	}
	if replies := botReplies(f.messages(conv)); len(replies) != 1 {
		t.Fatalf("want one final reply, got %d", len(replies))
	}
}

func TestIntegrationPermanentFailureIsNotRetried(t *testing.T) {
	f := newFixture(t, 128_000, 8_000)

	unconfigured := f.direct(f.bot("NoProvider", false))
	first := f.send(unconfigured, "你好")
	f.drain(1)
	failed := f.cycle(first.Cycle.ID)
	if failed.Status != "failed" || failed.Attempt != 1 {
		t.Fatalf("missing provider: status=%s attempt=%d, want failed after 1 attempt", failed.Status, failed.Attempt)
	}

	rejected := f.direct(f.bot("BadKey", true))
	second := f.send(rejected, "[[fail:401]] 你好")
	f.drain(1)
	failed = f.cycle(second.Cycle.ID)
	if failed.Status != "failed" || failed.Attempt != 1 {
		t.Fatalf("auth failure: status=%s attempt=%d, want failed after 1 attempt", failed.Status, failed.Attempt)
	}
}

func TestIntegrationTransientFailureIsRequeued(t *testing.T) {
	f := newFixture(t, 128_000, 8_000)
	conv := f.direct(f.bot("Flaky", true))
	sent := f.send(conv, "[[fail:500]] 你好")
	f.drain(1)
	cycle := f.cycle(sent.Cycle.ID)
	if cycle.Status != "queued" {
		t.Fatalf("a 5xx must be retried later, status = %s (%s)", cycle.Status, cycle.Error)
	}
	if !cycle.NextAttemptAt.After(time.Now()) {
		t.Fatal("retry should be delayed by backoff")
	}
}

func TestIntegrationAutoCheckpoint(t *testing.T) {
	f := newFixture(t, 8_000, 1_000)
	bot := f.bot("Epsilon", true)
	conv := f.direct(bot)
	ctx := context.Background()
	for i := 0; i < 30; i++ {
		params := store.CreateMessageParams{
			ConversationID: conv.ID, SenderType: "user", Metadata: json.RawMessage(`{}`),
			Content: fmt.Sprintf("第 %d 条：%s", i, strings.Repeat("历史内容", 100)),
		}
		if i%2 == 1 {
			params.SenderType = "bot"
			params.SenderBotID = &bot.ID
		}
		if _, err := f.db.CreateMessage(ctx, params); err != nil {
			t.Fatalf("seed history: %v", err)
		}
	}
	sent := f.send(conv, "继续")
	f.drain(1)
	if status := f.cycle(sent.Cycle.ID).Status; status != "done" {
		t.Fatalf("cycle status = %s (%s)", status, f.cycle(sent.Cycle.ID).Error)
	}
	summary, err := f.db.GetLatestConversationSummary(ctx, conv.ID)
	if err != nil {
		t.Fatalf("no checkpoint summary was created: %v", err)
	}
	// The mock model cannot write the required headings, so the mechanical
	// fallback must have been used rather than a malformed note trusted.
	if summary.Source != "fallback" {
		t.Fatalf("summary source = %s, want fallback", summary.Source)
	}
	if !strings.Contains(summary.Content, verbatimHeading) || !strings.Contains(summary.Content, "第 0 条") {
		t.Fatalf("checkpoint lost the user's words: %s", truncateRunes(summary.Content, 300))
	}
	marker := false
	for _, msg := range f.messages(conv) {
		if messageKind(msg) == messageKindCheckpoint {
			marker = true
		}
	}
	if !marker {
		t.Fatal("no checkpoint marker message for the UI")
	}

	// A second turn must build on the checkpoint instead of re-summarizing.
	f.send(conv, "再继续")
	f.drain(1)
	count, _ := f.db.CountConversationSummaries(ctx, conv.ID)
	if count != 1 {
		t.Fatalf("checkpoint count = %d, want 1 (history after the cut is small)", count)
	}
}

func TestIntegrationApprovalMidPipelineContinues(t *testing.T) {
	f := newFixture(t, 128_000, 8_000)
	ctx := context.Background()
	first, second := f.bot("StageOne", true), f.bot("StageTwo", true)
	team, err := f.db.CreateTeam(ctx, store.CreateTeamParams{
		OwnerID: f.account, Name: "pipe-" + uuid.NewString()[:6], Mode: "pipeline", Config: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	for index, bot := range []store.Bot{first, second} {
		if err := f.db.AddTeamBot(ctx, store.AddTeamBotParams{TeamID: team.ID, BotID: bot.ID, Position: int32(index), Role: "member"}); err != nil {
			t.Fatalf("add team bot: %v", err)
		}
	}
	conv, err := f.db.CreateConversation(ctx, store.CreateConversationParams{
		TeamID: &team.ID, Kind: "team", Title: "pipeline", CreatedBy: f.account,
	})
	if err != nil {
		t.Fatalf("create team conversation: %v", err)
	}
	// Writing in the bot's own workspace no longer asks by default; the
	// user's own rule makes it, and keeps the decision with the user.
	if _, err := f.db.CreatePermissionRule(ctx, store.CreatePermissionRuleParams{
		OwnerID: f.account, Kind: "always_ask", Pattern: "workspace_write",
	}); err != nil {
		t.Fatalf("create permission rule: %v", err)
	}
	f.send(conv, `[[tool:workspace_write {"path":"note.txt","content":"hi"}]]`)

	approve := func(stage string) {
		t.Helper()
		pending, err := f.db.ListPendingApprovalsForConversation(ctx, conv.ID)
		if err != nil || len(pending) != 1 {
			t.Fatalf("%s: want one pending approval, got %d (%v)", stage, len(pending), err)
		}
		if _, err := f.db.ResolveToolApproval(ctx, store.ResolveToolApprovalParams{
			ID: pending[0].ID, OwnerID: f.account, Status: "allowed",
		}); err != nil {
			t.Fatalf("%s: resolve approval: %v", stage, err)
		}
		checkpoint, err := f.db.GetRunCheckpointForApproval(ctx, &pending[0].ID)
		if err != nil {
			t.Fatalf("%s: load checkpoint: %v", stage, err)
		}
		started, err := f.chat.ResumeAfterApproval(ctx, checkpoint, "工具审批已通过", nil)
		if err != nil || !started {
			t.Fatalf("%s: resume not started (%v)", stage, err)
		}
	}

	f.drain(1)
	if replies := botReplies(f.messages(conv)); len(replies) != 0 {
		t.Fatalf("stage one must be paused for approval, but %d replies exist", len(replies))
	}
	approve("stage one")
	// One resume cycle must finish stage one AND run stage two: before the
	// fix the remaining stages were dropped after an approval pause.
	f.drain(1)

	speakers := map[uuid.UUID]int{}
	for _, msg := range botReplies(f.messages(conv)) {
		speakers[*msg.SenderBotID]++
	}
	if speakers[first.ID] != 1 || speakers[second.ID] != 1 {
		t.Fatalf("both pipeline stages must finish after the approval, got %v", speakers)
	}
	path := fmt.Sprintf("%s/workspaces/%s/note.txt", f.dataDir, first.ID)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("approved tool did not run: %v", err)
	}
	if _, err := f.db.GetActiveCycle(ctx, conv.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("pipeline should be finished")
	}
}

func TestIntegrationAgentReplyWakesSender(t *testing.T) {
	f := newFixture(t, 128_000, 8_000)
	ctx := context.Background()
	asker, helper := f.bot("Asker", true), f.bot("Helper", true)
	conv := f.direct(asker)
	f.send(conv, fmt.Sprintf(`[[tool:send_to_agent {"bot_name":"%s","message":"帮我核对版本号"}]]`, helper.Name))

	f.drain(1) // asker sends the private message
	f.drain(1) // helper answers in the agent thread

	var card *store.Message
	for _, msg := range f.messages(conv) {
		if text := messageInjectText(msg); text != "" {
			card = &msg
			if !strings.Contains(text, "帮我核对版本号") {
				t.Fatalf("the reply handed back is not the helper's answer: %q", text)
			}
		}
	}
	if card == nil {
		t.Fatal("the helper's reply was not delivered back to the asker's conversation")
	}
	if messageForBot(*card) != asker.ID.String() {
		t.Fatal("delivered reply is not addressed to the asking bot")
	}
	active, err := f.db.GetActiveCycle(ctx, conv.ID)
	if err != nil {
		t.Fatalf("the asker was not woken by the reply: %v", err)
	}
	if active.TriggerMessageID != card.ID {
		t.Fatal("wake-up cycle is not triggered by the delivered reply")
	}

	// Woken by the reply, the asker answers with it and the exchange ends.
	f.drain(10)
	if _, err := f.db.GetActiveCycle(ctx, conv.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("the exchange should be finished")
	}
	replies := botReplies(f.messages(conv))
	if len(replies) != 2 || !strings.Contains(replies[1].Content, "帮我核对版本号") {
		t.Fatalf("the asker should have continued with the helper's reply in hand: %+v", replies)
	}
}

func TestIntegrationAutoWakeLimit(t *testing.T) {
	f := newFixture(t, 128_000, 8_000)
	ctx := context.Background()
	conv := f.direct(f.bot("Sleepy", true))
	f.send(conv, "你好")
	f.drain(1)
	// Results keep arriving with no human in between; past the limit they
	// must stop restarting the conversation.
	woken := 0
	for i := 0; i < maxAutoWakes+4; i++ {
		meta, _ := json.Marshal(map[string]any{"inject": "true", "inject_text": fmt.Sprintf("结果 %d", i)})
		msg, err := f.db.CreateMessage(ctx, store.CreateMessageParams{
			ConversationID: conv.ID, SenderType: "system", Content: "卡片", Metadata: meta,
		})
		if err != nil {
			t.Fatalf("create result: %v", err)
		}
		f.engine.wakeForResult(ctx, conv.ID, msg)
		woken += f.drain(1)
	}
	if woken != maxAutoWakes {
		t.Fatalf("conversation was restarted %d times, want exactly %d", woken, maxAutoWakes)
	}
	f.send(conv, "我回来了")
	f.drain(1)
	meta, _ := json.Marshal(map[string]any{"inject": "true", "inject_text": "新结果"})
	msg, _ := f.db.CreateMessage(ctx, store.CreateMessageParams{
		ConversationID: conv.ID, SenderType: "system", Content: "卡片", Metadata: meta,
	})
	f.engine.wakeForResult(ctx, conv.ID, msg)
	if f.drain(1) != 1 {
		t.Fatal("a human message must re-arm automatic wake-ups")
	}
}

func TestIntegrationGroupGoalDriverStopsByItself(t *testing.T) {
	f := newFixture(t, 128_000, 8_000)
	ctx := context.Background()
	lead, worker := f.bot("Lead", true), f.bot("Worker", true)
	config, _ := json.Marshal(map[string]any{"primary_bot_id": lead.ID.String()})
	team, err := f.db.CreateTeam(ctx, store.CreateTeamParams{
		OwnerID: f.account, Name: "goal-" + uuid.NewString()[:6], Mode: "group", Config: config,
	})
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	for index, bot := range []store.Bot{worker, lead} {
		if err := f.db.AddTeamBot(ctx, store.AddTeamBotParams{TeamID: team.ID, BotID: bot.ID, Position: int32(index), Role: "member"}); err != nil {
			t.Fatalf("add team bot: %v", err)
		}
	}
	conv, err := f.db.CreateConversation(ctx, store.CreateConversationParams{
		TeamID: &team.ID, Kind: "team", Title: "goal", CreatedBy: f.account,
	})
	if err != nil {
		t.Fatalf("create team conversation: %v", err)
	}

	f.send(conv, `[[tool:group_goal {"action":"draft","objective":"发布落地页","acceptance":"页面可访问","stages":[{"title":"文案","acceptance":"定稿"}]}]]`)
	f.drain(1)
	goal, _, err := f.engine.goals.Load(ctx, conv.ID)
	if err != nil || goal == nil || goal.Status != goals.StatusDrafting {
		t.Fatalf("the lead should have drafted the goal through its tool: %+v (%v)", goal, err)
	}
	replies := botReplies(f.messages(conv))
	if len(replies) != 1 || *replies[0].SenderBotID != lead.ID {
		t.Fatal("an unaddressed group message must be answered by the configured lead")
	}

	roster, err := goals.LoadRoster(ctx, f.db, conv)
	if err != nil || roster.LeadID != lead.ID {
		t.Fatalf("roster lead = %v (%v)", roster.LeadID, err)
	}
	if _, _, err := f.engine.goals.Apply(ctx, conv.ID, f.account, goals.Action{Action: "start"}, goals.Actor{IsUser: true}, roster.Members); err != nil {
		t.Fatalf("user start: %v", err)
	}
	if !f.engine.driveGoal(ctx, conv) {
		t.Fatal("an active goal with nothing running must wake the lead")
	}
	// Every wake ends with the goal unchanged (the mock lead makes no
	// progress), so the driver must give up instead of looping.
	f.drain(20)
	if _, err := f.db.GetActiveCycle(ctx, conv.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("goal driver kept waking the lead")
	}
	goal, _, _ = f.engine.goals.Load(ctx, conv.ID)
	if goal.Nudges != goals.MaxNudges {
		t.Fatalf("nudges = %d, want %d", goal.Nudges, goals.MaxNudges)
	}
	nudges := 0
	for _, msg := range f.messages(conv) {
		if messageKind(msg) == messageKindGoalNudge {
			nudges++
			if messageForBot(msg) != lead.ID.String() {
				t.Fatal("goal check must be addressed to the lead")
			}
		}
	}
	if nudges != goals.MaxNudges {
		t.Fatalf("goal checks sent = %d, want %d", nudges, goals.MaxNudges)
	}

	// A human speaking re-arms the driver.
	f.send(conv, "进展如何")
	f.drain(20)
	goal, _, _ = f.engine.goals.Load(ctx, conv.ID)
	if goal.Nudges != goals.MaxNudges {
		t.Fatalf("after the user spoke the driver should have run its budget again, nudges = %d", goal.Nudges)
	}
}

func TestIntegrationReclaimedPipelineResumesAtUnfinishedStage(t *testing.T) {
	f := newFixture(t, 128_000, 8_000)
	ctx := context.Background()
	first, second := f.bot("ResumeOne", true), f.bot("ResumeTwo", true)
	team, err := f.db.CreateTeam(ctx, store.CreateTeamParams{
		OwnerID: f.account, Name: "resume-" + uuid.NewString()[:6], Mode: "pipeline", Config: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	for index, bot := range []store.Bot{first, second} {
		if err := f.db.AddTeamBot(ctx, store.AddTeamBotParams{TeamID: team.ID, BotID: bot.ID, Position: int32(index), Role: "member"}); err != nil {
			t.Fatalf("add team bot: %v", err)
		}
	}
	conv, err := f.db.CreateConversation(ctx, store.CreateConversationParams{
		TeamID: &team.ID, Kind: "team", Title: "resume", CreatedBy: f.account,
	})
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	sent := f.send(conv, "[[slow:1500]] 两个阶段依次处理")

	// Stop the worker while stage two is thinking.
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.step(runCtx)
	}()
	deadline := time.Now().Add(15 * time.Second)
	for len(botReplies(f.messages(conv))) < 1 {
		if time.Now().After(deadline) {
			t.Fatal("stage one never finished")
		}
		time.Sleep(50 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	cancel()
	<-done
	if got := len(botReplies(f.messages(conv))); got != 1 {
		t.Fatalf("only stage one should have answered before the stop, got %d replies", got)
	}

	if !f.step(ctx) {
		t.Fatal("the handed-back cycle was not reclaimable")
	}
	if cycle := f.cycle(sent.Cycle.ID); cycle.Status != "done" {
		t.Fatalf("cycle status = %s (%s)", cycle.Status, cycle.Error)
	}
	speakers := map[uuid.UUID]int{}
	for _, msg := range botReplies(f.messages(conv)) {
		speakers[*msg.SenderBotID]++
	}
	if speakers[first.ID] != 1 || speakers[second.ID] != 1 {
		t.Fatalf("stage one must not run again after the restart: %v", speakers)
	}
}

func TestIntegrationTeamRoutinePostsIntoTheGroupItself(t *testing.T) {
	f := newFixture(t, 128_000, 8_000)
	ctx := context.Background()
	lead := f.bot("RoutineLead", true)
	config, _ := json.Marshal(map[string]any{"primary_bot_id": lead.ID.String()})
	team, err := f.db.CreateTeam(ctx, store.CreateTeamParams{
		OwnerID: f.account, Name: "routine-" + uuid.NewString()[:6], Mode: "group", Config: config,
	})
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	if err := f.db.AddTeamBot(ctx, store.AddTeamBotParams{TeamID: team.ID, BotID: lead.ID, Role: "member"}); err != nil {
		t.Fatalf("add team bot: %v", err)
	}
	room, err := f.engine.chatSvc.CreateTeamConversation(ctx, f.account, team.ID, "")
	if err != nil {
		t.Fatalf("create room: %v", err)
	}
	past := time.Now().Add(-time.Minute)
	routine, err := f.db.CreateRoutine(ctx, store.CreateRoutineParams{
		TeamID: team.ID, Name: "每日站会", Cron: "0 9 * * *", Prompt: "汇报昨天的进展", Enabled: true, NextRunAt: &past,
	})
	if err != nil {
		t.Fatalf("create routine: %v", err)
	}
	f.engine.routineSvc.ReconcileDue(ctx)
	f.drain(2)

	var count int
	if err := f.db.Pool.QueryRow(ctx, `SELECT count(*) FROM conversations WHERE team_id = $1`, team.ID).Scan(&count); err != nil {
		t.Fatalf("count conversations: %v", err)
	}
	if count != 1 {
		t.Fatalf("a routine must not open a new conversation per run, team has %d", count)
	}
	var trigger *store.Message
	for _, msg := range f.messages(room) {
		if messageKind(msg) == "routine" {
			trigger = &msg
		}
	}
	if trigger == nil || !strings.Contains(trigger.Content, "汇报昨天的进展") {
		t.Fatalf("the routine was not posted into the group's room: %+v", f.messages(room))
	}
	replies := botReplies(f.messages(room))
	if len(replies) != 1 || *replies[0].SenderBotID != lead.ID {
		t.Fatalf("the group lead should answer the routine in the room, got %d replies", len(replies))
	}
	updated, err := f.db.GetRoutineForOwner(ctx, store.GetRoutineForOwnerParams{ID: routine.ID, OwnerID: f.account})
	if err == nil && updated.NextRunAt != nil && !updated.NextRunAt.After(time.Now()) {
		t.Fatal("the next run must be scheduled in the future")
	}
}
