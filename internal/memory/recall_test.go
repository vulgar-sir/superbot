package memory

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"superbot/internal/store"
)

func hashSimilarity(a, b string) float64 {
	x, y := Embed(a), Embed(b)
	var dot float64
	for i := range x {
		dot += float64(x[i]) * float64(y[i])
	}
	return dot
}

// The local hash embedder is lexical, so "related" here means the query
// shares a real term with the memory. Pairs that are related only in meaning
// are out of its reach whatever the threshold.
func TestLocalHashThreshold(t *testing.T) {
	related := [][2]string{
		{"部署端口是 32608，使用 nginx 反向代理", "部署端口是多少"},
		{"用户的公司叫星河科技，主营跨境电商", "给星河科技写一段公司简介"},
		{"The user prefers dark mode and compact layout", "switch the UI to dark mode please"},
		{"The staging server runs Ubuntu 22.04 on AWS", "which OS does the staging server use?"},
		{"User's daughter is named Lily and her birthday is March 3", "what should I buy for Lily's birthday"},
		{"User prefers answers in Chinese", "Please answer in Chinese"},
	}
	unrelated := [][2]string{
		{"用户喜欢喝无糖的冰美式咖啡", "帮我写一个快速排序算法"},
		{"部署端口是 32608，使用 nginx 反向代理", "今天天气怎么样，适合出去跑步吗"},
		{"项目使用 PostgreSQL 和 pgvector 存储向量", "推荐几部好看的科幻电影"},
		{"The user prefers dark mode and compact layout", "how do I cook a medium rare steak"},
		{"The staging server runs Ubuntu 22.04 on AWS", "translate this poem into French"},
		{"User's daughter is named Lily and her birthday is March 3", "explain the difference between TCP and UDP"},
		{"周报每周五下午发给张经理", "解释一下量子纠缠是什么"},
		{"用户的公司叫星河科技，主营跨境电商", "帮我看看这段代码为什么报空指针"},
		{"User prefers answers in Chinese", "what is the capital of Australia"},
		{"用户喜欢喝无糖的冰美式咖啡", "用户想知道明天的会议几点开始"},
		{"好的", "谢谢"},
	}
	threshold := defaultMinSimilarity(localModel)
	for _, pair := range related {
		if score := hashSimilarity(pair[0], pair[1]); score < threshold {
			t.Errorf("related pair below threshold %.2f: %.3f %q | %q", threshold, score, pair[0], pair[1])
		}
	}
	for _, pair := range unrelated {
		if score := hashSimilarity(pair[0], pair[1]); score >= threshold {
			t.Errorf("unrelated pair above threshold %.2f: %.3f %q | %q", threshold, score, pair[0], pair[1])
		}
	}
}

func TestLocalHashDuplicateThresholds(t *testing.T) {
	if score := hashSimilarity("用户喜欢喝无糖的冰美式咖啡", "用户喜欢喝无糖的冰美式咖啡。"); score < tidySimilarity {
		t.Errorf("punctuation-only variant scored %.3f, want >= %.2f", score, tidySimilarity)
	}
	if score := hashSimilarity("The user prefers dark mode and compact layout", "The user prefers light mode and compact layout"); score >= duplicateSimilarity {
		t.Errorf("contradicting facts scored %.3f, must stay below %.2f", score, duplicateSimilarity)
	}
	if score := hashSimilarity("部署端口是 32608", "部署端口是 32609"); score >= duplicateSimilarity {
		t.Errorf("different values scored %.3f, must stay below %.2f", score, duplicateSimilarity)
	}
}

func TestMinSimilarityDefaultsAndOverrides(t *testing.T) {
	t.Setenv(MinSimilarityEnv, "")
	local := NewService(nil, "", "", "")
	if got := local.MinSimilarity(); got != localMinSimilarity {
		t.Fatalf("local default = %v, want %v", got, localMinSimilarity)
	}
	remote := NewService(nil, "key", "", "")
	if got := remote.MinSimilarity(); got != remoteMinSimilarity {
		t.Fatalf("remote default = %v, want %v", got, remoteMinSimilarity)
	}
	remote.SetMinSimilarity(0.7)
	if got := remote.MinSimilarity(); got != 0.7 {
		t.Fatalf("after SetMinSimilarity = %v", got)
	}
	remote.SetMinSimilarity(-1)
	if got := remote.MinSimilarity(); got != remoteMinSimilarity {
		t.Fatalf("negative must restore the default, got %v", got)
	}

	t.Setenv(MinSimilarityEnv, "0.4")
	if got := NewService(nil, "", "", "").MinSimilarity(); got != 0.4 {
		t.Fatalf("env override = %v, want 0.4", got)
	}
	for _, bad := range []string{"abc", "-0.1", "1.5", "NaN"} {
		t.Setenv(MinSimilarityEnv, bad)
		if got := NewService(nil, "", "", "").MinSimilarity(); got != localMinSimilarity {
			t.Fatalf("invalid env %q must be ignored, got %v", bad, got)
		}
	}
}

func TestRankRecall(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	scored := []scoredMemory{
		{memory: store.Memory{ID: a, ScopeType: "bot"}, similarity: 0.86},
		{memory: store.Memory{ID: b, ScopeType: "user"}, similarity: 0.90},
		{memory: store.Memory{ID: a, ScopeType: "bot"}, similarity: 0.86},
		{memory: store.Memory{ID: c, ScopeType: "team"}, similarity: 0.88},
	}
	got := rankRecall(scored, 2)
	if len(got) != 2 || got[0].ID != b || got[1].ID != c {
		t.Fatalf("rankRecall order wrong: %+v", got)
	}
	if got := rankRecall(scored, 10); len(got) != 3 {
		t.Fatalf("duplicates must collapse, got %d rows", len(got))
	}
	if got := rankRecall(nil, 6); len(got) != 0 {
		t.Fatal("no hits must stay empty")
	}
}

func TestRecallScopes(t *testing.T) {
	bot, user, team := uuid.New(), uuid.New(), uuid.New()
	if got := recallScopes(bot, nil, nil); len(got) != 1 || got[0] != (Scope{Type: "bot", ID: bot}) {
		t.Fatalf("bot only: %+v", got)
	}
	got := recallScopes(bot, &team, &user)
	want := []Scope{{Type: "bot", ID: bot}, {Type: "user", ID: user}, {Type: "team", ID: team}}
	if len(got) != len(want) {
		t.Fatalf("scopes = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("scopes = %+v, want %+v", got, want)
		}
	}
}

func TestKeywordQuery(t *testing.T) {
	if _, _, ok := keywordQuery(" a "); ok {
		t.Fatal("one-character query must not trigger the keyword fallback")
	}
	text, pattern, ok := keywordQuery(`  50%_off\deal  `)
	if !ok || text != `50%_off\deal` || pattern != `50\%\_off\\deal` {
		t.Fatalf("keywordQuery = %q, %q, %v", text, pattern, ok)
	}
	long := make([]rune, keywordQueryRunes+50)
	for i := range long {
		long[i] = '记'
	}
	text, _, ok = keywordQuery(string(long))
	if !ok || len([]rune(text)) != keywordQueryRunes {
		t.Fatalf("long query must be capped, got %d runes", len([]rune(text)))
	}
}

func TestNormalizeContent(t *testing.T) {
	cases := map[string]string{
		"  Hello   World.  ":       "hello world",
		"用户喜欢喝咖啡。":                 "用户喜欢喝咖啡",
		"用户喜欢喝咖啡！！":                "用户喜欢喝咖啡",
		"Deploy\tPort\n is 32608;": "deploy port is 32608",
		"Prefers DARK mode . ":     "prefers dark mode",
		"版本是 v1.2":                 "版本是 v1.2",
		"ÀB":                       "Àb",
		"...":                      "",
		"":                         "",
	}
	for input, want := range cases {
		if got := NormalizeContent(input); got != want {
			t.Errorf("NormalizeContent(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestFindNormalizedDuplicate(t *testing.T) {
	newer := store.Memory{ID: uuid.New(), Content: "User prefers dark mode.", UpdatedAt: time.Now()}
	older := store.Memory{ID: uuid.New(), Content: "user  prefers DARK mode", UpdatedAt: time.Now().Add(-time.Hour)}
	other := store.Memory{ID: uuid.New(), Content: "User prefers light mode"}
	rows := []store.Memory{other, newer, older}
	got, ok := findNormalizedDuplicate("  USER prefers dark mode!", rows)
	if !ok || got.ID != newer.ID {
		t.Fatalf("expected the first (newest) normalized match, got %+v ok=%v", got, ok)
	}
	if _, ok := findNormalizedDuplicate("User prefers dark mode on mobile", rows); ok {
		t.Fatal("different text must not match")
	}
	if _, ok := findNormalizedDuplicate("。。。", rows); ok {
		t.Fatal("text that normalizes to nothing must not match")
	}
}

func TestE5ModelsGetTheirOwnThreshold(t *testing.T) {
	t.Setenv(MinSimilarityEnv, "")
	if got := NewService(nil, "key", "", "intfloat/multilingual-e5-large").MinSimilarity(); got != e5MinSimilarity {
		t.Fatalf("e5 default = %v, want %v", got, e5MinSimilarity)
	}
	if got := NewService(nil, "key", "", "text-embedding-3-small").MinSimilarity(); got != remoteMinSimilarity {
		t.Fatalf("other remote models keep %v, got %v", remoteMinSimilarity, got)
	}
}

func TestRankRecallDropsTrailingHits(t *testing.T) {
	scored := []scoredMemory{
		{memory: store.Memory{ID: uuid.New(), Content: "weak"}, similarity: 0.80},
		{memory: store.Memory{ID: uuid.New(), Content: "best"}, similarity: 0.93},
		{memory: store.Memory{ID: uuid.New(), Content: "close"}, similarity: 0.90},
	}
	got := rankRecall(scored, 6)
	if len(got) != 2 || got[0].Content != "best" || got[1].Content != "close" {
		t.Fatalf("want best and close only, got %+v", got)
	}
}
