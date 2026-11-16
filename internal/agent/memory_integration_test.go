//go:build integration

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"superbot/internal/memory"
	"superbot/internal/store"
)

type fixedExtractor struct {
	reply string
	calls int
	last  memory.ExtractRequest
}

func (f *fixedExtractor) Extract(_ context.Context, req memory.ExtractRequest) (string, error) {
	f.calls++
	f.last = req
	return f.reply, nil
}

func TestIntegrationMemoryLifecycle(t *testing.T) {
	f := newFixture(t, 128_000, 8_000)
	ctx := context.Background()
	bot := f.bot("Rememberer", true)
	svc := f.engine.memory
	scope := memory.Scope{Type: "bot", ID: bot.ID}
	save := func(content string) memory.SaveResult {
		t.Helper()
		result, err := svc.SaveWith(ctx, memory.SaveInput{
			OwnerID: f.account, ScopeType: scope.Type, ScopeID: scope.ID, Content: content, Source: memory.SourceTool,
		})
		if err != nil {
			t.Fatalf("save %q: %v", content, err)
		}
		return result
	}

	first := save("部署包保存在 /srv/releases 目录")
	if first.Duplicate {
		t.Fatal("first save reported as duplicate")
	}
	again := save("  部署包保存在 /srv/releases 目录。 ")
	if !again.Duplicate || again.Memory.ID != first.Memory.ID {
		t.Fatalf("normalized-equal content must refresh the existing row: %+v", again)
	}
	if _, err := svc.SaveWith(ctx, memory.SaveInput{
		OwnerID: f.account, ScopeType: scope.Type, ScopeID: scope.ID,
		Content: "生产库密码是 password=Zx9!kLm2Qw8#", Source: memory.SourceTool,
	}); !errors.Is(err, memory.ErrSecretContent) {
		t.Fatalf("a credential must be refused, got %v", err)
	}

	recalled := svc.Recall(ctx, f.account, bot.ID, nil, nil, "部署包放在哪个目录", 6)
	if len(recalled) != 1 || recalled[0].ID != first.Memory.ID {
		t.Fatalf("related question should recall the memory, got %d", len(recalled))
	}
	if unrelated := svc.Recall(ctx, f.account, bot.ID, nil, nil, "今天天气怎么样适合跑步吗", 6); len(unrelated) != 0 {
		t.Fatalf("an unrelated question must recall nothing, got %q", unrelated[0].Content)
	}

	pinned, err := svc.SetPinned(ctx, f.account, first.Memory.ID, true)
	if err != nil || !pinned.Pinned {
		t.Fatalf("pin: %v", err)
	}
	if _, err := svc.Forget(ctx, f.account, []memory.Scope{scope}, first.Memory.ID, ""); !errors.Is(err, memory.ErrPinned) {
		t.Fatalf("a pinned memory must not be forgotten, got %v", err)
	}
	if _, err := svc.SetPinned(ctx, f.account, first.Memory.ID, false); err != nil {
		t.Fatalf("unpin: %v", err)
	}
	if _, err := svc.Forget(ctx, f.account, []memory.Scope{scope}, first.Memory.ID, ""); err != nil {
		t.Fatalf("forget: %v", err)
	}
	if gone := svc.Recall(ctx, f.account, bot.ID, nil, nil, "部署包放在哪个目录", 6); len(gone) != 0 {
		t.Fatal("an archived memory must not be recalled")
	}
	restored, err := svc.Restore(ctx, f.account, first.Memory.ID)
	if err != nil || restored.ArchivedAt != nil {
		t.Fatalf("restore: %v", err)
	}
	if back := svc.Recall(ctx, f.account, bot.ID, nil, nil, "部署包放在哪个目录", 6); len(back) != 1 {
		t.Fatal("a restored memory should be recalled again")
	}
}

func TestIntegrationMemoryTidyArchivesDuplicates(t *testing.T) {
	f := newFixture(t, 128_000, 8_000)
	ctx := context.Background()
	bot := f.bot("Tidy", true)
	// Insert through the store so the duplicates really exist; SaveWith would
	// have merged them on the way in.
	insert := func(content string) {
		t.Helper()
		if _, err := f.db.Pool.Exec(ctx, `
			INSERT INTO memories (owner_id, scope_type, scope_id, content, embedding, embedding_model, metadata)
			VALUES ($1, 'bot', $2, $3, $4::vector, $5, '{}'::jsonb)`,
			f.account, bot.ID, content, memory.VectorText(content), f.engine.memory.ModelName()); err != nil {
			t.Fatalf("insert memory: %v", err)
		}
	}
	for i := 0; i < 20; i++ {
		insert(fmt.Sprintf("第 %d 条互不相同的事实：项目代号 %c%d", i, 'A'+rune(i), i*7))
	}
	insert("用户偏好深色主题")
	insert("用户偏好深色主题")
	insert("用户偏好深色主题")

	report, err := f.engine.memory.Tidy(ctx, f.account, "bot", bot.ID)
	if err != nil {
		t.Fatalf("tidy: %v", err)
	}
	if len(report.Archived) != 2 {
		t.Fatalf("want the two extra copies archived, got %d: %+v", len(report.Archived), report.Archived)
	}
	rows, err := f.db.ListMemoriesIncludingArchived(ctx, store.ListMemoriesIncludingArchivedParams{
		OwnerID: f.account, ScopeType: "bot", ScopeID: bot.ID, Limit: 200,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	active := 0
	for _, row := range rows {
		if row.ArchivedAt == nil && strings.Contains(row.Content, "深色主题") {
			active++
		}
	}
	if active != 1 {
		t.Fatalf("exactly one copy should stay active, got %d", active)
	}
}

func TestIntegrationMemoryReviewExtractsAndAdvances(t *testing.T) {
	t.Setenv(memory.ReviewIdleSecondsEnv, "1")
	f := newFixture(t, 128_000, 8_000)
	ctx := context.Background()
	bot := f.bot("Reviewer", true)
	if _, err := f.db.Pool.Exec(ctx, `UPDATE bots SET memory_enabled = true WHERE id = $1`, bot.ID); err != nil {
		t.Fatalf("enable memory: %v", err)
	}
	conv := f.direct(bot)
	for i := 0; i < 6; i++ {
		params := store.CreateMessageParams{
			ConversationID: conv.ID, SenderType: "user", Metadata: json.RawMessage(`{}`),
			Content: fmt.Sprintf("第 %d 句：以后周报都用表格格式发给我", i),
		}
		if i%2 == 1 {
			params.SenderType = "bot"
			params.SenderBotID = &bot.ID
			params.Content = "好的，记下了"
		}
		if _, err := f.db.CreateMessage(ctx, params); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	// Make the conversation look idle without sleeping through the window.
	if _, err := f.db.Pool.Exec(ctx, `UPDATE messages SET created_at = now() - interval '5 minutes' WHERE conversation_id = $1`, conv.ID); err != nil {
		t.Fatalf("age messages: %v", err)
	}

	extractor := &fixedExtractor{reply: "```json\n" + `{"memories":[{"content":"用户希望周报用表格格式","scope":"bot","reason":"明确偏好"},{"content":"sk-abcdefghijklmnopqrstuvwxyz0123456789","scope":"bot","reason":"不该保存"}]}` + "\n```"}
	reviewer := memory.NewReviewer(f.db, f.engine.memory, extractor, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if !reviewer.Enabled() {
		t.Skip("memory review is disabled by environment")
	}
	reviewed, err := reviewer.ReviewDue(ctx)
	if err != nil {
		t.Fatalf("review: %v", err)
	}
	if reviewed < 1 || extractor.calls < 1 {
		t.Fatalf("the idle conversation was not reviewed (reviewed=%d calls=%d)", reviewed, extractor.calls)
	}
	if !strings.Contains(extractor.last.User, "表格格式") {
		t.Fatal("the review prompt did not include the new messages")
	}
	rows, err := f.db.ListMemoriesIncludingArchived(ctx, store.ListMemoriesIncludingArchivedParams{
		OwnerID: f.account, ScopeType: "bot", ScopeID: bot.ID, Limit: 200,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || rows[0].Source != memory.SourceReview || !strings.Contains(rows[0].Content, "表格格式") {
		t.Fatalf("want exactly the one durable fact saved with source=review, got %+v", rows)
	}

	// Nothing new was said, so a second pass must not call the model again.
	before := extractor.calls
	if _, err := reviewer.ReviewDue(ctx); err != nil {
		t.Fatalf("second review: %v", err)
	}
	if extractor.calls != before {
		t.Fatal("the same messages were reviewed twice")
	}
}

func TestIntegrationMemoryConcurrentSavesStoreOneRow(t *testing.T) {
	f := newFixture(t, 128_000, 8_000)
	ctx := context.Background()
	bot := f.bot("Concurrent", true)
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := f.engine.memory.SaveWith(ctx, memory.SaveInput{
				OwnerID: f.account, ScopeType: "bot", ScopeID: bot.ID,
				Content: strings.Repeat(" ", i%3) + "部署端口固定为 32608", Source: memory.SourceTool,
			})
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("save: %v", err)
		}
	}
	rows, err := f.db.ListMemoriesIncludingArchived(ctx, store.ListMemoriesIncludingArchivedParams{
		OwnerID: f.account, ScopeType: "bot", ScopeID: bot.ID, Limit: 50,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("10 concurrent saves of one fact stored %d rows, want 1", len(rows))
	}
}

func TestIntegrationMemoryReviewRespectsConversationBudget(t *testing.T) {
	f := newFixture(t, 128_000, 8_000)
	ctx := context.Background()
	if _, err := f.db.Pool.Exec(ctx, `UPDATE models SET price_in_micros_per_mtok = 1000000, price_out_micros_per_mtok = 1000000 WHERE provider_id = $1`, f.provider.ID); err != nil {
		t.Fatalf("price model: %v", err)
	}
	bot := f.bot("Budgeted", true)
	if _, err := f.db.Pool.Exec(ctx, `UPDATE bots SET memory_enabled = true WHERE id = $1`, bot.ID); err != nil {
		t.Fatalf("enable memory: %v", err)
	}
	conv := f.direct(bot)
	// The conversation has already spent its whole budget.
	if _, err := f.db.Pool.Exec(ctx, `UPDATE conversations SET budget_micros = 10, spent_micros = 10 WHERE id = $1`, conv.ID); err != nil {
		t.Fatalf("exhaust budget: %v", err)
	}
	extractor := &memoryExtractor{engine: f.engine}
	_, err := extractor.Extract(ctx, memory.ExtractRequest{
		BotID: bot.ID, OwnerID: f.account, UserID: f.user, ConversationID: conv.ID,
		System: "提取记忆", User: "以后周报都用表格格式",
	})
	if err == nil || !strings.Contains(err.Error(), "预算") {
		t.Fatalf("a review must not spend past the conversation budget, got %v", err)
	}
	var spent, reserved int64
	if err := f.db.Pool.QueryRow(ctx, `SELECT spent_micros, reserved_micros FROM conversations WHERE id = $1`, conv.ID).Scan(&spent, &reserved); err != nil {
		t.Fatalf("read budget: %v", err)
	}
	if spent != 10 || reserved != 0 {
		t.Fatalf("budget changed by a refused review: spent=%d reserved=%d", spent, reserved)
	}

	// With room left, the cost lands on the conversation.
	if _, err := f.db.Pool.Exec(ctx, `UPDATE conversations SET budget_micros = 0, spent_micros = 0 WHERE id = $1`, conv.ID); err != nil {
		t.Fatalf("reset budget: %v", err)
	}
	if _, err := extractor.Extract(ctx, memory.ExtractRequest{
		BotID: bot.ID, OwnerID: f.account, UserID: f.user, ConversationID: conv.ID,
		System: "提取记忆", User: strings.Repeat("以后周报都用表格格式。", 50),
	}); err != nil {
		t.Fatalf("extract: %v", err)
	}
	if err := f.db.Pool.QueryRow(ctx, `SELECT spent_micros, reserved_micros FROM conversations WHERE id = $1`, conv.ID).Scan(&spent, &reserved); err != nil {
		t.Fatalf("read budget: %v", err)
	}
	if spent <= 0 || reserved != 0 {
		t.Fatalf("review cost must be settled on the conversation: spent=%d reserved=%d", spent, reserved)
	}
}
