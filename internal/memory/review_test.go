package memory

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

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
		{"person and replacements", `{"memories":[{"content":"王磊搬到了滨江区","scope":"user","user":"u2","replaces":["M3"]}]}`,
			[]ExtractedMemory{{Content: "王磊搬到了滨江区", Scope: "user", User: "U2", Replaces: []string{"M3"}}}},
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
				if !reflect.DeepEqual(got[i], tc.want[i]) {
					t.Fatalf("item %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestParseReviewForget(t *testing.T) {
	reply := ParseReview(`{"memories":[],"forget":[{"id":" M2 ","reason":"已离职"},{"id":"","reason":"空"}]}`)
	if len(reply.Memories) != 0 || len(reply.Forget) != 1 || reply.Forget[0].ID != "M2" {
		t.Fatalf("reply = %+v", reply)
	}
	only := ParseReview(`{"forget":[{"id":"M1"}]}`)
	if len(only.Forget) != 1 {
		t.Fatalf("a reply with only forget operations must be read, got %+v", only)
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

func TestRenderTranscript(t *testing.T) {
	alice, bob := uuid.New(), uuid.New()
	rows := []store.ListMessagesForMemoryReviewRow{
		{Ordinal: 10, SenderType: "bot", Content: "Hello"},
		{Ordinal: 11, SenderType: "bot", Content: "   "},
		{Ordinal: 12, SenderType: "user", Content: "  以后都用中文回答我  "},
		{Ordinal: 13, SenderType: "bot", Content: "好的，记下了", BotName: "小助手"},
	}
	direct := store.ListConversationsDueForMemoryReviewRow{Kind: "direct"}
	got := renderTranscript(rows, newReviewPeople(direct, rows))
	want := "#10 [Bot] Hello\n#12 [用户] 以后都用中文回答我\n#13 [Bot:小助手] 好的，记下了"
	if got != want {
		t.Fatalf("transcript =\n%s\nwant\n%s", got, want)
	}

	team := store.ListConversationsDueForMemoryReviewRow{Kind: "team"}
	teamRows := []store.ListMessagesForMemoryReviewRow{
		{Ordinal: 1, SenderType: "user", Content: "我对花生过敏", SenderUserID: &alice, SenderUserName: "Alice"},
		{Ordinal: 2, SenderType: "user", Content: "我住在杭州", SenderUserID: &bob},
		{Ordinal: 3, SenderType: "user", Content: "提醒一下", SenderUserID: &alice, SenderUserName: "Alice"},
		{Ordinal: 4, SenderType: "user", Content: "例行任务"},
	}
	people := newReviewPeople(team, teamRows)
	got = renderTranscript(teamRows, people)
	want = "#1 [用户 U1·Alice] 我对花生过敏\n#2 [用户 U2] 我住在杭州\n#3 [用户 U1·Alice] 提醒一下\n#4 [用户] 例行任务"
	if got != want {
		t.Fatalf("team transcript =\n%s\nwant\n%s", got, want)
	}

	huge := renderTranscript([]store.ListMessagesForMemoryReviewRow{
		{Ordinal: 1, SenderType: "user", Content: strings.Repeat("字", 5000)},
	}, newReviewPeople(direct, nil))
	if runes := len([]rune(huge)); runes > reviewMessageRunes+40 {
		t.Fatalf("a single message must be truncated, got %d runes", runes)
	}
}

func TestSplitReviewWindowsCoversEverything(t *testing.T) {
	var rows []store.ListMessagesForMemoryReviewRow
	for ordinal := int64(1); ordinal <= 100; ordinal++ {
		rows = append(rows, store.ListMessagesForMemoryReviewRow{
			Ordinal: ordinal, SenderType: "user", Content: strings.Repeat("字", 100),
		})
	}
	windows := splitReviewWindows(rows, 1200, reviewMinNewMessages)
	if len(windows) < 10 {
		t.Fatalf("100 messages of ~130 runes in 1200-rune windows need >= 10 windows, got %d", len(windows))
	}
	covered := map[int64]bool{}
	previousLast := int64(0)
	for i, window := range windows {
		if window.rows[0].Ordinal > previousLast+1 {
			t.Fatalf("window %d starts at #%d, after #%d: messages were skipped", i, window.rows[0].Ordinal, previousLast)
		}
		size := 0
		for j, row := range window.rows {
			covered[row.Ordinal] = true
			size += reviewLineRunes(row)
			if j > 0 && row.Ordinal != window.rows[j-1].Ordinal+1 {
				t.Fatalf("window %d is not contiguous", i)
			}
		}
		if i < len(windows)-1 && size > 1200 {
			t.Fatalf("window %d holds %d runes, cap is 1200", i, size)
		}
		if len(window.rows) < reviewMinNewMessages {
			t.Fatalf("window %d has only %d messages", i, len(window.rows))
		}
		previousLast = window.last()
	}
	if len(covered) != 100 || previousLast != 100 {
		t.Fatalf("windows cover %d messages ending at #%d, want all 100", len(covered), previousLast)
	}

	// A short tail borrows the end of the window before it for context.
	tail := splitReviewWindows(rows[:10], 8*(100+reviewLineOverhead), reviewMinNewMessages)
	if len(tail) != 2 || len(tail[1].rows) != reviewMinNewMessages || tail[1].last() != 10 {
		t.Fatalf("tail window = %d windows, last has %d rows", len(tail), len(tail[len(tail)-1].rows))
	}
}

func TestReviewScope(t *testing.T) {
	bot, team, user, alice, bob := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	direct := store.ListConversationsDueForMemoryReviewRow{Kind: "direct", BotID: bot, DirectUserID: &user}
	legacyDirect := store.ListConversationsDueForMemoryReviewRow{Kind: "direct", BotID: bot}
	teamConv := store.ListConversationsDueForMemoryReviewRow{Kind: "team", BotID: bot, TeamID: &team}
	twoPeople := newReviewPeople(teamConv, []store.ListMessagesForMemoryReviewRow{
		{SenderType: "user", SenderUserID: &alice}, {SenderType: "user", SenderUserID: &bob},
	})
	onePerson := newReviewPeople(teamConv, []store.ListMessagesForMemoryReviewRow{{SenderType: "user", SenderUserID: &alice}})
	oneKnownOneUnknown := newReviewPeople(teamConv, []store.ListMessagesForMemoryReviewRow{
		{SenderType: "user", SenderUserID: &alice}, {SenderType: "user"},
	})
	directPeople := newReviewPeople(direct, nil)
	cases := []struct {
		name   string
		item   ExtractedMemory
		conv   store.ListConversationsDueForMemoryReviewRow
		people reviewPeople
		want   Scope
		ok     bool
	}{
		{"direct bot", ExtractedMemory{Scope: "bot"}, direct, directPeople, Scope{Type: "bot", ID: bot}, true},
		{"direct user", ExtractedMemory{Scope: "user"}, direct, directPeople, Scope{Type: "user", ID: user}, true},
		{"legacy direct user", ExtractedMemory{Scope: "user"}, legacyDirect, newReviewPeople(legacyDirect, nil), Scope{Type: "bot", ID: bot}, true},
		{"direct team", ExtractedMemory{Scope: "team"}, direct, directPeople, Scope{Type: "bot", ID: bot}, true},
		{"direct nonsense", ExtractedMemory{Scope: "nonsense"}, direct, directPeople, Scope{Type: "bot", ID: bot}, true},
		{"team bot", ExtractedMemory{Scope: "bot"}, teamConv, twoPeople, Scope{Type: "team", ID: team}, true},
		{"team team", ExtractedMemory{Scope: "team"}, teamConv, twoPeople, Scope{Type: "team", ID: team}, true},
		{"team user named", ExtractedMemory{Scope: "user", User: "U2"}, teamConv, twoPeople, Scope{Type: "user", ID: bob}, true},
		{"team user unnamed, two people", ExtractedMemory{Scope: "user"}, teamConv, twoPeople, Scope{}, false},
		{"team user unknown label", ExtractedMemory{Scope: "user", User: "U7"}, teamConv, twoPeople, Scope{}, false},
		{"team user unnamed, one person", ExtractedMemory{Scope: "user"}, teamConv, onePerson, Scope{Type: "user", ID: alice}, true},
		{"team user unnamed, unknown sender too", ExtractedMemory{Scope: "user"}, teamConv, oneKnownOneUnknown, Scope{}, false},
	}
	for _, tc := range cases {
		got, ok := reviewScope(tc.item, tc.conv, tc.people)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s: reviewScope = %+v, %v; want %+v, %v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
	scopes := twoPeople.memoryScopes(teamConv)
	if len(scopes) != 3 || scopes[0] != (Scope{Type: "team", ID: team}) || scopes[2] != (Scope{Type: "user", ID: bob}) {
		t.Fatalf("team review must see the team and each speaker's own memories: %+v", scopes)
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
	advanced  map[uuid.UUID][]int64
	failures  map[uuid.UUID]int
}

func newFakeReviewStore() *fakeReviewStore {
	return &fakeReviewStore{
		messages:  map[uuid.UUID][]store.ListMessagesForMemoryReviewRow{},
		unclaimed: map[uuid.UUID]bool{},
		advanced:  map[uuid.UUID][]int64{},
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

// ListMessagesForMemoryReview behaves like the query: oldest first, after
// the cursor, at most MaxRows.
func (f *fakeReviewStore) ListMessagesForMemoryReview(_ context.Context, arg store.ListMessagesForMemoryReviewParams) ([]store.ListMessagesForMemoryReviewRow, error) {
	var out []store.ListMessagesForMemoryReviewRow
	for _, row := range f.messages[arg.ConversationID] {
		if row.Ordinal > arg.AfterOrdinal && len(out) < int(arg.MaxRows) {
			out = append(out, row)
		}
	}
	return out, nil
}

func (f *fakeReviewStore) AdvanceMemoryReviewCursor(_ context.Context, arg store.AdvanceMemoryReviewCursorParams) error {
	f.advanced[arg.ConversationID] = append(f.advanced[arg.ConversationID], arg.LastOrdinal)
	return nil
}

func (f *fakeReviewStore) RecordMemoryReviewFailure(_ context.Context, conversationID uuid.UUID) error {
	f.failures[conversationID]++
	return nil
}

func (f *fakeReviewStore) cursor(id uuid.UUID) (int64, bool) {
	moves := f.advanced[id]
	if len(moves) == 0 {
		return 0, false
	}
	return moves[len(moves)-1], true
}

type supersedeCall struct {
	id, by uuid.UUID
	scope  Scope
}

// fakeMemories records what the reviewer does with memories.
type fakeMemories struct {
	saved      []SaveInput
	existing   []store.Memory
	scopesSeen [][]Scope
	superseded []supersedeCall
	forgotten  []uuid.UUID
}

func (f *fakeMemories) SaveWith(_ context.Context, in SaveInput) (SaveResult, error) {
	if _, err := validateContent(in.Content); err != nil {
		return SaveResult{}, err
	}
	f.saved = append(f.saved, in)
	return SaveResult{Memory: store.Memory{ID: uuid.New(), ScopeType: in.ScopeType, ScopeID: in.ScopeID, Content: in.Content}}, nil
}

func (f *fakeMemories) Supersede(_ context.Context, _ uuid.UUID, memoryID uuid.UUID, scope Scope, by uuid.UUID, _ string) (store.Memory, error) {
	f.superseded = append(f.superseded, supersedeCall{id: memoryID, by: by, scope: scope})
	return store.Memory{ID: memoryID}, nil
}

func (f *fakeMemories) Forget(_ context.Context, _ uuid.UUID, scopes []Scope, memoryID uuid.UUID, _ string) (store.Memory, error) {
	f.forgotten = append(f.forgotten, memoryID)
	return store.Memory{ID: memoryID}, nil
}

func (f *fakeMemories) ExistingForReview(_ context.Context, _ uuid.UUID, scopes []Scope, _ []string, _ int) []store.Memory {
	f.scopesSeen = append(f.scopesSeen, scopes)
	return f.existing
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

// chat returns count alternating user/bot messages from ordinal from,
// oldest first.
func chat(from int64, count int) []store.ListMessagesForMemoryReviewRow {
	rows := make([]store.ListMessagesForMemoryReviewRow, 0, count)
	for i := 0; i < count; i++ {
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

	outdated, gone, pinned := uuid.New(), uuid.New(), uuid.New()
	memories := &fakeMemories{existing: []store.Memory{
		{ID: outdated, ScopeType: "user", ScopeID: user, Content: "用户常驻北京", UpdatedAt: time.Now()},
		{ID: gone, ScopeType: "bot", ScopeID: bot, Content: "项目代号是 Apollo"},
		{ID: pinned, ScopeType: "user", ScopeID: user, Content: "用户生日 3 月 1 日", Pinned: true},
	}}
	extractor := &mockExtractor{provider: llm.NewScriptedMock("mock",
		llm.MockStep{Text: "```json\n" + `{"memories":[
			{"content":"用户常驻上海，时区 UTC+8","scope":"user","reason":"自述","replaces":["M1","M3","M9"]},
			{"content":"回答要先给结论","scope":"bot","reason":"明确要求"},
			{"content":"数据库密码：pg-Secret-42","scope":"bot","reason":"不该记"}],
			"forget":[{"id":"M2","reason":"项目已结束"},{"id":"M3","reason":"固定的不能删"}]}` + "\n```", Chunks: 4},
		llm.MockStep{Text: `{"memories":[]}`},
		llm.MockStep{Err: errors.New("provider down")},
	)}
	reviewer := newReviewer(db, memories, extractor, nil)

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

	if len(memories.saved) != 2 {
		t.Fatalf("saved %d memories, want 2 (the secret must be dropped): %+v", len(memories.saved), memories.saved)
	}
	first, second := memories.saved[0], memories.saved[1]
	if first.ScopeType != "user" || first.ScopeID != user || second.ScopeType != "bot" || second.ScopeID != bot {
		t.Fatalf("scopes wrong: %+v / %+v", first, second)
	}
	for _, in := range memories.saved {
		if in.Source != SourceReview || in.OwnerID != owner || in.SourceConversationID == nil ||
			*in.SourceConversationID != withFacts || in.SourceOrdinal != 17 {
			t.Fatalf("provenance wrong: %+v", in)
		}
	}
	// The outdated memory is superseded; the pinned one and an unknown label
	// are left alone. Only the unpinned memory asked for is forgotten.
	if len(memories.superseded) != 1 || memories.superseded[0].id != outdated ||
		memories.superseded[0].scope != (Scope{Type: "user", ID: user}) {
		t.Fatalf("superseded = %+v", memories.superseded)
	}
	if len(memories.forgotten) != 1 || memories.forgotten[0] != gone {
		t.Fatalf("forgotten = %+v", memories.forgotten)
	}
	if seen := memories.scopesSeen[0]; len(seen) != 2 || seen[0] != (Scope{Type: "bot", ID: bot}) || seen[1] != (Scope{Type: "user", ID: user}) {
		t.Fatalf("a direct review must see the bot's and the user's memories, saw %+v", seen)
	}

	// The cursor moves to the newest message read, also when nothing was found.
	if got, _ := db.cursor(withFacts); got != 17 {
		t.Fatalf("cursor for the fact conversation = %d, want 17", got)
	}
	if got, _ := db.cursor(empty); got != 24 {
		t.Fatalf("cursor for the empty conversation = %d, want 24", got)
	}
	// A failed call keeps the cursor and records the failure for backoff.
	if _, moved := db.cursor(failing); moved || db.failures[failing] != 1 {
		t.Fatalf("failing conversation: advanced=%v failures=%d", moved, db.failures[failing])
	}
	// A conversation another worker claimed is skipped without a model call.
	if _, moved := db.cursor(busy); moved {
		t.Fatal("unclaimed conversation must not be touched")
	}
	// Too few usable messages: skip the model but move on.
	if got, _ := db.cursor(thin); got != 52 {
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
	if !strings.Contains(request.User, "M1 [用户个人] 用户常驻北京") || !strings.Contains(request.User, "［固定］") {
		t.Fatalf("existing memories missing from the prompt:\n%s", request.User)
	}
	if !strings.Contains(request.System, `{"memories":[`) || !strings.Contains(request.System, "凭据") ||
		!strings.Contains(request.System, "replaces") || !strings.Contains(request.System, "forget") {
		t.Fatal("system prompt must demand the JSON shape, the update operations and forbid secrets")
	}
}

// A backlog longer than one window is read completely, window by window,
// with the cursor following each answered window.
func TestReviewReadsWholeBacklogInWindows(t *testing.T) {
	t.Setenv(ReviewEnv, "")
	bot, owner, user := uuid.New(), uuid.New(), uuid.New()
	conv := uuid.New()
	db := newFakeReviewStore()
	var rows []store.ListMessagesForMemoryReviewRow
	for ordinal := int64(1); ordinal <= 60; ordinal++ {
		rows = append(rows, store.ListMessagesForMemoryReviewRow{
			Ordinal: ordinal, SenderType: "user", Content: fmt.Sprintf("第 %d 条 %s", ordinal, strings.Repeat("长", 600)),
		})
	}
	db.messages[conv] = rows
	db.due = []store.ListConversationsDueForMemoryReviewRow{
		{ConversationID: conv, Kind: "direct", BotID: bot, OwnerID: owner, CreatedBy: user, DirectUserID: &user, MaxOrdinal: 61, NewCount: 61},
	}
	steps := make([]llm.MockStep, 0, 8)
	for i := 0; i < 8; i++ {
		steps = append(steps, llm.MockStep{Text: `{"memories":[]}`})
	}
	extractor := &mockExtractor{provider: llm.NewScriptedMock("mock", steps...)}
	reviewer := newReviewer(db, &fakeMemories{}, extractor, nil)

	// 60 messages of ~630 runes in 12000-rune windows: 4 windows per pass.
	if _, err := reviewer.ReviewDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls := extractor.provider.Calls(); calls != reviewMaxWindows {
		t.Fatalf("first pass made %d calls, want %d", calls, reviewMaxWindows)
	}
	firstPass := db.advanced[conv]
	if len(firstPass) != reviewMaxWindows {
		t.Fatalf("the cursor must follow every window, moves = %v", firstPass)
	}
	for i := 1; i < len(firstPass); i++ {
		if firstPass[i] <= firstPass[i-1] {
			t.Fatalf("cursor moved backwards: %v", firstPass)
		}
	}
	if !strings.Contains(extractor.requests[0].User, "#1 [用户] 第 1 条") {
		t.Fatal("the oldest unreviewed message must be in the first window")
	}

	// Next pass starts where the first stopped and finishes the backlog.
	db.due[0].LastOrdinal = firstPass[len(firstPass)-1]
	if _, err := reviewer.ReviewDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := db.cursor(conv)
	if got != 61 {
		t.Fatalf("after the backlog the cursor must reach the newest message (61), got %d", got)
	}
	seen := map[int]bool{}
	for _, request := range extractor.requests {
		for ordinal := 1; ordinal <= 60; ordinal++ {
			if strings.Contains(request.User, fmt.Sprintf("#%d [用户] 第 %d 条", ordinal, ordinal)) {
				seen[ordinal] = true
			}
		}
	}
	if len(seen) != 60 {
		t.Fatalf("only %d of 60 messages were ever shown to the model", len(seen))
	}
}

func TestReviewWindowFailureKeepsEarlierProgress(t *testing.T) {
	t.Setenv(ReviewEnv, "")
	bot, owner, user := uuid.New(), uuid.New(), uuid.New()
	conv := uuid.New()
	db := newFakeReviewStore()
	var rows []store.ListMessagesForMemoryReviewRow
	for ordinal := int64(1); ordinal <= 40; ordinal++ {
		rows = append(rows, store.ListMessagesForMemoryReviewRow{
			Ordinal: ordinal, SenderType: "user", Content: strings.Repeat("长", 600),
		})
	}
	db.messages[conv] = rows
	db.due = []store.ListConversationsDueForMemoryReviewRow{
		{ConversationID: conv, Kind: "direct", BotID: bot, OwnerID: owner, CreatedBy: user, DirectUserID: &user, MaxOrdinal: 40, NewCount: 40},
	}
	extractor := &mockExtractor{provider: llm.NewScriptedMock("mock",
		llm.MockStep{Text: `{"memories":[]}`},
		llm.MockStep{Err: errors.New("provider down")},
	)}
	reviewer := newReviewer(db, &fakeMemories{}, extractor, nil)
	reviewed, _ := reviewer.ReviewDue(context.Background())
	if reviewed != 1 || db.failures[conv] != 1 {
		t.Fatalf("reviewed=%d failures=%d", reviewed, db.failures[conv])
	}
	got, _ := db.cursor(conv)
	if got <= 0 || got >= 40 {
		t.Fatalf("the cursor must stay after the answered window, got %d", got)
	}
}

func TestReviewTeamAttributesFactsToTheirPerson(t *testing.T) {
	t.Setenv(ReviewEnv, "")
	bot, owner, team, alice, bob := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	conv := uuid.New()
	db := newFakeReviewStore()
	db.messages[conv] = []store.ListMessagesForMemoryReviewRow{
		{Ordinal: 1, SenderType: "user", Content: "我对花生过敏", SenderUserID: &alice, SenderUserName: "Alice"},
		{Ordinal: 2, SenderType: "bot", Content: "记下了"},
		{Ordinal: 3, SenderType: "user", Content: "我住在杭州", SenderUserID: &bob, SenderUserName: "Bob"},
		{Ordinal: 4, SenderType: "user", Content: "周报每周五发", SenderUserID: &bob, SenderUserName: "Bob"},
	}
	db.due = []store.ListConversationsDueForMemoryReviewRow{
		{ConversationID: conv, Kind: "team", BotID: bot, OwnerID: owner, CreatedBy: owner, TeamID: &team, MaxOrdinal: 4, NewCount: 4},
	}
	extractor := &mockExtractor{provider: llm.NewScriptedMock("mock", llm.MockStep{Text: `{"memories":[
		{"content":"Alice 对花生过敏","scope":"user","user":"U1"},
		{"content":"Bob 住在杭州","scope":"user","user":"U2"},
		{"content":"有人喜欢咖啡","scope":"user"},
		{"content":"周报每周五发","scope":"team"}]}`})}
	memories := &fakeMemories{}
	reviewer := newReviewer(db, memories, extractor, nil)
	if _, err := reviewer.ReviewDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(memories.saved) != 3 {
		t.Fatalf("an unattributed personal fact must not be stored, saved %+v", memories.saved)
	}
	want := []Scope{{Type: "user", ID: alice}, {Type: "user", ID: bob}, {Type: "team", ID: team}}
	for i, in := range memories.saved {
		if (Scope{Type: in.ScopeType, ID: in.ScopeID}) != want[i] {
			t.Fatalf("memory %d %q went to %s/%s, want %+v", i, in.Content, in.ScopeType, in.ScopeID, want[i])
		}
	}
	if !strings.Contains(extractor.requests[0].User, "[用户 U1·Alice] 我对花生过敏") {
		t.Fatalf("the transcript must label who spoke:\n%s", extractor.requests[0].User)
	}
}

func TestReviewDueDisabled(t *testing.T) {
	t.Setenv(ReviewEnv, "off")
	db := newFakeReviewStore()
	db.due = []store.ListConversationsDueForMemoryReviewRow{{ConversationID: uuid.New(), Kind: "direct"}}
	extractor := &mockExtractor{provider: llm.NewMock("mock")}
	reviewer := newReviewer(db, &fakeMemories{}, extractor, nil)
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
