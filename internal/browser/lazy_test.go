package browser

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestLazyToolsDoNotStartTheComputerUntilUsed(t *testing.T) {
	opened := 0
	lazy := LazyTools(func(context.Context) (*Session, error) {
		opened++
		return nil, errors.New("computer unavailable")
	})
	eager := Tools(nil)
	if len(lazy) != len(eager) {
		t.Fatalf("want %d tools, got %d", len(eager), len(lazy))
	}
	for index, tool := range lazy {
		if tool.Name() != eager[index].Name() || tool.Description() != eager[index].Description() ||
			string(tool.Parameters()) != string(eager[index].Parameters()) {
			t.Fatalf("lazy tool %d does not describe itself like %s", index, eager[index].Name())
		}
	}
	if opened != 0 {
		t.Fatalf("offering the tools must not start the computer (opened %d times)", opened)
	}
	_, err := lazy[0].Execute(context.Background(), json.RawMessage(`{"url":"https://example.com"}`))
	if opened != 1 {
		t.Fatalf("the first call must start the computer once, opened %d times", opened)
	}
	if err == nil || !strings.Contains(err.Error(), "computer unavailable") {
		t.Fatalf("a failed start must surface to the bot, got %v", err)
	}
}
