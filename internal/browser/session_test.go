package browser

import "testing"

func TestRewriteWebSocketHost(t *testing.T) {
	body := []byte(`{"Browser":"Chrome/156","webSocketDebuggerUrl":"ws://127.0.0.1:9222/devtools/browser/abc-123"}`)
	got, err := rewriteWebSocketHost(body, "superbot-bot-0123456789ab:9223")
	if err != nil {
		t.Fatal(err)
	}
	if want := "ws://superbot-bot-0123456789ab:9223/devtools/browser/abc-123"; got != want {
		t.Fatalf("url = %q, want %q", got, want)
	}
	if _, err := rewriteWebSocketHost([]byte(`{}`), "x:1"); err == nil {
		t.Fatal("a reply without an address must be an error")
	}
	if _, err := rewriteWebSocketHost([]byte(`forbidden`), "x:1"); err == nil {
		t.Fatal("a non-JSON reply must be an error")
	}
}
