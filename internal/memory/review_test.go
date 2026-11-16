package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"superbot/internal/llm"
	"superbot/internal/store"
)

func TestParseExtraction(t *testing.T) {
	long := strings.Repeat("长", reviewItemRunes+1)
	cases := []struct {
		name  string
		reply string
		want  []ExtractedMemory
	}{
		{"plain", `{"memories":[{"content":"用户常驻上海","scope":"user","reason":"自我介绍"}]}`,
			[]ExtractedMemory{{Content: "用户常驻上海", Scope: "user", Reason: "自我介绍"}}},
		{"fenced with prose", "好的，结果如下：\n```json\n{\"memories\":[{\"content\":\"周报每周五发\",\"scope\":\"team\"}]}\n```\n以上。",
			[]ExtractedMemory{{Content: "周报每周五发", Scope: "team"}}},
		{"braces in prose before json", `说明 {不是 JSON} 然后 {"memories":[{"content":"喜欢简洁回答","scope":"BOT"}]}`,
			[]ExtractedMemory{{Content: "喜欢简洁回答", Scope: "bot"}}},
		{"nested wrapper", `{"result":{"memories":[{"content":"用 Go 写后端","scope":"bot"}]}}`,
			[]ExtractedMemory{{Content: "用 Go 写后端", Scope: "bot"}}},
		{"invalid scope defaults to bot", `{"memories":[{"content":"偏好深色模式","scope":"global"}]}`,
			[]ExtractedMemory{{Content: "偏好深色模式", Scope: "bot"}}},
		{"drops secrets, long, empty and repeats", `{"memories":[
			{"content":"API key 是 sk-proj-AbCdEf0123456789AbCdEf0123456789","scope":"bot"},
			{"content":"` + long + `","scope":"bot"},
			{"content":"   ","scope":"bot"},
			{"content":"用户是素食者","scope":"user"},
			{"content":"用户是素食者。","scope":"user"}]}`,
			[]ExtractedMemory{{Content: "用户是素食者", Scope: "user"}}},
		{"empty list", `{"memories":[]}`, nil},
		{"not json", "没有什么值得记住的。", nil},
		{"truncated json", `{"memories":[{"content":"半截`, nil},
		{"wrong type", `{"memories":"none"}`, nil},
		{"empty reply", "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseExtraction(tc.reply)
			if len(got) != len(tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("item %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestParseExtractionCapsAtFive(t *testing.T) {
	var items []string
	for i := 0; i < 9; i++ {
		items = append(items, fmt.Sprintf(`{"content":"事实 %d","scope":"bot"}`, i))
	}
	got := ParseExtraction(`{"memories":[` + strings.Join(items, ",") + `]}`)
	if len(got) != reviewMaxItems || got[4].Content != "事实 4" {
		t.Fatalf("got %d items: %+v", len(got), got)
	}
}

func TestBuildReviewTranscript(t *testing.T) {
	rows := []store.ListMessagesForMemoryReviewRow{
		{Ordinal: 13, SenderType: "bot", Content: "好的，记下了", BotName: "小助手"},
		{Ordinal: 12, SenderType: "user", Content: "  以后都用中文回答我  "},
		{Ordinal: 11, SenderType: "bot", Content: "   "},
		{Ordinal: 10, SenderType: "bot", Content: "Hello"},
	}
	got := BuildReviewTranscript(rows, 1000)
	want := "#10 [Bot] Hello\n#12 [用户] 以后都用中文回答我\n#13 [Bot:小助手] 好的，记下了"
	if got != want {
		t.Fatalf("transcript =\n%s\nwant\n%s", got, want)
	}

	// When the cap is hit the newest messages win and order stays oldest first.
	var many []store.ListMessagesForMemoryReviewRow
	for ordinal := int64(100); ordinal >= 1; ordinal-- {
		many = append(many, store.ListMessagesForMemoryReviewRow{
			Ordinal: ordinal, SenderType: "user", Content: strings.Repeat("字", 100),
		})
	}
	capped := BuildReviewTranscript(many, 1200)
	if runes := len([]rune(capped)); runes > 1200 {
		t.Fatalf("transcript has %d runes, cap is 1200", runes)
	}
	lines := strings.Split(capped, "\n")
	if !strings.HasPrefix(lines[len(lines)-1], "#100 ") || len(lines) < 5 || len(lines) > 11 {
		t.Fatalf("expected the newest messages to be kept, got %d lines ending %q", len(lines), lines[len(lines)-1][:8])
	}
	if !strings.HasPrefix(lines[0], fmt.Sprintf("#%d ", 100-len(lines)+1)) {
		t.Fatalf("lines are not contiguous oldest-first: first=%q", lines[0][:8])
	}

	huge := BuildReviewTranscript([]store.ListMessagesForMemoryReviewRow{
		{Ordinal: 1, SenderType: "user", Content: strings.Repeat("字", 5000)},
	}, reviewTranscriptRunes)
	if runes := len([]rune(huge)); runes > reviewMessageRunes+40 {
		t.Fatalf("a single message must be truncated, got %d runes", runes)
	}
}

func TestReviewScope(t *testing.T) {
	bot, team, user := uuid.New(), uuid.New(), uuid.New()
	direct := store.ListConversationsDueForMemoryReviewRow{Kind: "direct", BotID: bot, DirectUserID: &user}
	teamConv := store.ListConversationsDueForMemoryReviewRow{Kind: "team", BotID: bot, TeamID: &team}
	cases := []struct {
		scope string
		conv  store.ListConversationsDueForMemoryReviewRow
		want  Scope
	}{
		{"bot", direct, Scope{Type: "bot", ID: bot}},
		{"user", direct, Scope{Type: "user", ID: user}},
		{"team", direct, Scope{Type: "bot", ID: bot}},
		{"nonsense", direct, Scope{Type: "bot", ID: bot}},
		{"bot", teamConv, Scope{Type: "team", ID: team}},
		{"team", teamConv, Scope{Type: "team", ID: team}},
		{"user", teamConv, Scope{Type: "team", ID: team}},
	}
	for _, tc := range cases {
		if got := reviewScope(tc.scope, tc.conv); got != tc.want {
			t.Errorf("reviewScope(%q, %s) = %+v, want %+v", tc.scope, tc.conv.Kind, got, tc.want)
		}
	}
}

func TestReviewEnvParsing(t *testing.T) {
	for _, off := range []string{"off", "OFF", " false ", "0", "disabled"} {
		if reviewEnabled(off) {
			t.Errorf("%q must disable review", off)
		}
	}
	for _, on := range []string{"", "on", "1", "true"} {
		if !reviewEnabled(on) {
			t.Errorf("%q must leave review enabled", on)
		}
	}
	if got := reviewIdle("90"); got.Seconds() != 90 {
		t.Errorf("reviewIdle(90) = %v", got)
	}
	for _, bad := range []string{"", "abc", "0", "-5"} {
		if got := reviewIdle(bad); got != defaultReviewIdle {
			t.Errorf("reviewIdle(%q) = %v, want the default", bad, got)
		}
	}
}

type fakeReviewStore struct {
	due       []store.ListConversationsDueForMemoryReviewRow
	listArgs  store.ListConversationsDueForMemoryReviewParams
	messages  map[uuid.UUID][]store.ListMessagesForMemoryReviewRow
	unclaimed map[uuid.UUID]bool
	advanced  map[uuid.UUID]int64
	failures  map[uuid.UUID]int
}

func newFakeReviewStore() *fakeReviewStore {
	return &fakeReviewStore{
		messages:  map[uuid.UUID][]store.ListMessagesForMemoryReviewRow{},
		unclaimed: map[uuid.UUID]bool{},
		advanced:  map[uuid.UUID]int64{},
		failures:  map[uuid.UUID]int{},
	}
}

func (f *fakeReviewStore) ListConversationsDueForMemoryReview(_ context.Context, arg store.ListConversationsDueForMemoryReviewParams) ([]store.ListConversationsDueForMemoryReviewRow, error) {
	f.listArgs = arg
	return f.due, nil
}

func (f *fakeReviewStore) ClaimMemoryReview(_ context.Context, arg store.ClaimMemoryReviewParams) (int64, error) {
	if f.unclaimed[arg.ConversationID] {
		return 0, nil
	}
	return 1, nil
}

func (f *fakeReviewStore) ListMessagesForMemoryReview(_ context.Context, arg store.ListMessagesForMemoryReviewParams) ([]store.ListMessagesForMemoryReviewRow, error) {
	return f.messages[arg.ConversationID], nil
}

func (f *fakeReviewStore) AdvanceMemoryReviewCursor(_ context.Context, arg store.AdvanceMemoryReviewCursorParams) error {
	f.advanced[arg.ConversationID] = arg.LastOrdinal
	return nil
}

func (f *fakeReviewStore) RecordMemoryReviewFailure(_ context.Context, conversationID uuid.UUID) error {
	f.failures[conversationID]++
	return nil
}

type fakeSaver struct {
	saved []SaveInput
}

func (f *fakeSaver) SaveWith(_ context.Context, in SaveInput) (SaveResult, error) {
	if _, err := validateContent(in.Content); err != nil {
		return SaveResult{}, err
	}
	f.saved = append(f.saved, in)
	return SaveResult{}, nil
}

// mockExtractor drives the reviewer through a scripted llm provider, the same
// way the engine's implementation streams one request.
type mockExtractor struct {
	provider *llm.MockProvider
	requests []ExtractRequest
}

func (m *mockExtractor) Extract(ctx context.Context, req ExtractRequest) (string, error) {
	m.requests = append(m.requests, req)
	stream, err := m.provider.Stream(ctx, llm.Request{
		Model: "mock", JSONMode: true,
		Messages: []llm.Message{llm.TextMessage(llm.RoleSystem, req.System), llm.TextMessage(llm.RoleUser, req.User)},
	})
	if err != nil {
		return "", err
	}
	defer stream.Close()
	var text strings.Builder
	var streamErr error
	for event := range stream.Events() {
		switch event.Type {
		case llm.EventTextDelta:
			text.WriteString(event.Text)
		case llm.EventError:
			streamErr = event.Err
		}
	}
	return text.String(), streamErr
}

func chat(from int64, count int) []store.ListMessagesForMemoryReviewRow {
	rows := make([]store.ListMessagesForMemoryReviewRow, 0, count)
	for i := count - 1; i >= 0; i-- {
		sender := "user"
		if i%2 == 1 {
			sender = "bot"
		}
		rows = append(rows, store.ListMessagesForMemoryReviewRow{
			Ordinal: from + int64(i), SenderType: sender, Content: fmt.Sprintf("消息 %d", from+int64(i)),
		})
	}
	return rows
}

func TestReviewDue(t *testing.T) {
	t.Setenv(ReviewEnv, "")
	t.Setenv(ReviewIdleSecondsEnv, "120")
	bot, owner, user, team := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	withFacts, empty, failing, busy, thin := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()

	db := newFakeReviewStore()
	db.due = []store.ListConversationsDueForMemoryReviewRow{
		{ConversationID: withFacts, Kind: "direct", BotID: bot, OwnerID: owner, CreatedBy: user, DirectUserID: &user, LastOrdinal: 10, MaxOrdinal: 16, NewCount: 6},
		{ConversationID: empty, Kind: "team", BotID: bot, OwnerID: owner, CreatedBy: user, TeamID: &team, MaxOrdinal: 24, NewCount: 4},
		{ConversationID: failing, Kind: "direct", BotID: bot, OwnerID: owner, CreatedBy: user, LastOrdinal: 30, MaxOrdinal: 36, NewCount: 6},
		{ConversationID: busy, Kind: "direct", BotID: bot, OwnerID: owner, CreatedBy: user, MaxOrdinal: 44, NewCount: 4},
		{ConversationID: thin, Kind: "direct", BotID: bot, OwnerID: owner, CreatedBy: user, MaxOrdinal: 52, NewCount: 4},
	}
	db.messages[withFacts] = chat(11, 7)
	db.messages[empty] = chat(21, 4)
	db.messages[failing] = chat(31, 6)
	db.messages[busy] = chat(41, 4)
	db.messages[thin] = chat(51, 2)
	db.unclaimed[busy] = true

	extractor := &mockExtractor{provider: llm.NewScriptedMock("mock",
		llm.MockStep{Text: "```json\n" + `{"memories":[
			{"content":"用户常驻上海，时区 UTC+8","scope":"user","reason":"自述"},
			{"content":"回答要先给结论","scope":"bot","reason":"明确要求"},
			{"content":"数据库密码：pg-Secret-42","scope":"bot","reason":"不该记"}]}` + "\n```", Chunks: 4},
		llm.MockStep{Text: `{"memories":[]}`},
		llm.MockStep{Err: errors.New("provider down")},
	)}
	saver := &fakeSaver{}
	reviewer := newReviewer(db, saver, extractor, nil)

	reviewed, err := reviewer.ReviewDue(context.Background())
	if err != nil {
		t.Fatalf("ReviewDue: %v", err)
	}
	if reviewed != 3 {
		t.Fatalf("reviewed = %d, want 3 (facts, empty, thin)", reviewed)
	}
	if db.listArgs.IdleSeconds != 120 || db.listArgs.MinNewMessages != reviewMinNewMessages || db.listArgs.MaxConversations != defaultReviewBatch {
		t.Fatalf("list params = %+v", db.listArgs)
	}

	if len(saver.saved) != 2 {
		t.Fatalf("saved %d memories, want 2 (the secret must be dropped): %+v", len(saver.saved), saver.saved)
	}
	first, second := saver.saved[0], saver.saved[1]
	if first.ScopeType != "user" || first.ScopeID != user || second.ScopeType != "bot" || second.ScopeID != bot {
		t.Fatalf("scopes wrong: %+v / %+v", first, second)
	}
	for _, in := range saver.saved {
		if in.Source != SourceReview || in.OwnerID != owner || in.SourceConversationID == nil ||
			*in.SourceConversationID != withFacts || in.SourceOrdinal != 17 {
			t.Fatalf("provenance wrong: %+v", in)
		}
	}

	// The cursor moves to the newest message read, also when nothing was found.
	if got := db.advanced[withFacts]; got != 17 {
		t.Fatalf("cursor for the fact conversation = %d, want 17", got)
	}
	if got := db.advanced[empty]; got != 24 {
		t.Fatalf("cursor for the empty conversation = %d, want 24", got)
	}
	// A failed call keeps the cursor and records the failure for backoff.
	if _, moved := db.advanced[failing]; moved || db.failures[failing] != 1 {
		t.Fatalf("failing conversation: advanced=%v failures=%d", moved, db.failures[failing])
	}
	// A conversation another worker claimed is skipped without a model call.
	if _, moved := db.advanced[busy]; moved {
		t.Fatal("unclaimed conversation must not be touched")
	}
	// Too few usable messages: skip the model but move on.
	if got := db.advanced[thin]; got != 52 {
		t.Fatalf("thin conversation cursor = %d, want 52", got)
	}
	if calls := extractor.provider.Calls(); calls != 3 {
		t.Fatalf("model calls = %d, want 3", calls)
	}

	request := extractor.requests[0]
	if request.BotID != bot || request.OwnerID != owner || request.UserID != user || request.ConversationID != withFacts {
		t.Fatalf("extract request ids wrong: %+v", request)
	}
	if !strings.Contains(request.User, "#11 [用户] 消息 11") || !strings.Contains(request.User, "#17 [用户] 消息 17") {
		t.Fatalf("transcript missing from the prompt:\n%s", request.User)
	}
	if !strings.Contains(request.System, `{"memories":[`) || !strings.Contains(request.System, "凭据") {
		t.Fatal("system prompt must demand the JSON shape and forbid secrets")
	}
}

func TestReviewDueDisabled(t *testing.T) {
	t.Setenv(ReviewEnv, "off")
	db := newFakeReviewStore()
	db.due = []store.ListConversationsDueForMemoryReviewRow{{ConversationID: uuid.New(), Kind: "direct"}}
	extractor := &mockExtractor{provider: llm.NewMock("mock")}
	reviewer := newReviewer(db, &fakeSaver{}, extractor, nil)
	if reviewer.Enabled() {
		t.Fatal("reviewer must report disabled")
	}
	if reviewed, err := reviewer.ReviewDue(context.Background()); err != nil || reviewed != 0 {
		t.Fatalf("ReviewDue = %d, %v", reviewed, err)
	}
	if extractor.provider.Calls() != 0 {
		t.Fatal("a disabled reviewer must not call the model")
	}
}
