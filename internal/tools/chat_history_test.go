package tools

import (
	"strings"
	"testing"

	"superbot/internal/store"
)

func TestPickAround(t *testing.T) {
	var rows []store.Message
	for _, ordinal := range []int64{90, 10, 50, 30, 70, 20, 60, 40, 80} {
		rows = append(rows, store.Message{Ordinal: ordinal})
	}
	picked := pickAround(rows, 50, 2)
	got := make([]int64, 0, len(picked))
	for _, row := range picked {
		got = append(got, row.Ordinal)
	}
	want := []int64{30, 40, 50, 60, 70}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	// A target that belongs to another conversation still yields neighbours.
	if picked := pickAround(rows, 55, 1); len(picked) != 2 || picked[0].Ordinal != 50 || picked[1].Ordinal != 60 {
		t.Fatalf("missing-target window wrong: %v", picked)
	}
	if picked := pickAround(nil, 5, 3); len(picked) != 0 {
		t.Fatal("empty input must yield nothing")
	}
}

func TestSnippetAroundCentresOnMatch(t *testing.T) {
	content := strings.Repeat("前", 2000) + "部署端口是 32608" + strings.Repeat("后", 2000)
	snippet := snippetAround(content, "32608", 200)
	if !strings.Contains(snippet, "32608") {
		t.Fatalf("snippet lost the match: %s", snippet)
	}
	if n := len([]rune(snippet)); n > 202 {
		t.Fatalf("snippet too long: %d", n)
	}
	if !strings.HasPrefix(snippet, "…") || !strings.HasSuffix(snippet, "…") {
		t.Fatal("cut snippet should be marked on both sides")
	}
	if short := snippetAround("短消息", "消息", 200); short != "短消息" {
		t.Fatalf("short content must be returned whole, got %q", short)
	}
}

func TestEscapeLike(t *testing.T) {
	if got := escapeLike(`100%_done\x`); got != `100\%\_done\\x` {
		t.Fatalf("got %q", got)
	}
}
