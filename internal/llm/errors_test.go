package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"syscall"
	"testing"
	"time"
)

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestIsRetryable(t *testing.T) {
	api := func(status int) error {
		return fmt.Errorf("wrapped: %w", &APIError{Provider: "p", Status: status, Message: "m"})
	}
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"408", api(408), true},
		{"409", api(409), true},
		{"425", api(425), true},
		{"429", api(429), true},
		{"500", api(500), true},
		{"502", api(502), true},
		{"503", api(503), true},
		{"529", api(529), true},
		{"400", api(400), false},
		{"401", api(401), false},
		{"403", api(403), false},
		{"404", api(404), false},
		{"422", api(422), false},
		{"stalled", fmt.Errorf("p stream: %w", ErrStreamStalled), true},
		{"unexpected EOF", fmt.Errorf("read: %w", io.ErrUnexpectedEOF), true},
		{"net timeout", &net.OpError{Op: "read", Err: timeoutErr{}}, true},
		{"conn reset", &net.OpError{Op: "read", Err: syscall.ECONNRESET}, true},
		{"conn refused", &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}, true},
		{"windows reset text", errors.New("wsarecv: An existing connection was forcibly closed by the remote host."), true},
		{"context canceled", fmt.Errorf("req: %w", context.Canceled), false},
		{"context deadline", fmt.Errorf("req: %w", context.DeadlineExceeded), false},
		{"plain", errors.New("boom"), false},
		{"decode", fmt.Errorf("decode: %w", errors.New("invalid character")), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRetryable(tt.err); got != tt.want {
				t.Fatalf("IsRetryable(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestIsContextOverflow(t *testing.T) {
	tests := []struct {
		name   string
		status int
		msg    string
		want   bool
	}{
		{"openai code", 400, `{"error":{"code":"context_length_exceeded","message":"..."}}`, true},
		{"openai text", 400, "This model's maximum context length is 65536 tokens.", true},
		{"mixed case", 400, "Maximum Context window reached", true},
		{"context length", 400, "input exceeds context length", true},
		{"too many tokens", 400, "Too many tokens in request", true},
		{"anthropic", 400, "prompt is too long: 250000 tokens > 200000 maximum", true},
		{"exceeds maximum", 400, "Input exceeds the maximum allowed size", true},
		{"reduce", 400, "Please reduce the length of the messages.", true},
		{"413", 413, "prompt is too long", true},
		{"413 unrelated", 413, "request body too large", false},
		{"400 unrelated", 400, "invalid tool schema", false},
		{"wrong status", 500, "context_length_exceeded", false},
		{"429", 429, "too many tokens per minute", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := fmt.Errorf("outer: %w", &APIError{Provider: "p", Status: tt.status, Message: tt.msg})
			if got := IsContextOverflow(err); got != tt.want {
				t.Fatalf("IsContextOverflow(%d, %q) = %v, want %v", tt.status, tt.msg, got, tt.want)
			}
		})
	}
	if IsContextOverflow(errors.New("context_length_exceeded")) {
		t.Fatal("non-APIError must not count as overflow")
	}
	if IsContextOverflow(nil) {
		t.Fatal("nil must not count as overflow")
	}
}

func TestIsAuthError(t *testing.T) {
	for status, want := range map[int]bool{401: true, 403: true, 400: false, 429: false, 500: false} {
		err := fmt.Errorf("x: %w", &APIError{Status: status})
		if got := IsAuthError(err); got != want {
			t.Errorf("IsAuthError(%d) = %v, want %v", status, got, want)
		}
	}
	if IsAuthError(errors.New("401")) {
		t.Error("non-APIError must not count as auth error")
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	tests := []struct {
		in   string
		want time.Duration
	}{
		{"", 0},
		{"2", 2 * time.Second},
		{"0.5", 500 * time.Millisecond},
		{" 7 ", 7 * time.Second},
		{"120", 30 * time.Second},
		{"-3", 0},
		{"garbage", 0},
		{now.Add(10 * time.Second).Format(http.TimeFormat), 10 * time.Second},
		{now.Add(10 * time.Minute).Format(http.TimeFormat), 30 * time.Second},
		{now.Add(-time.Minute).Format(http.TimeFormat), 0},
	}
	for _, tt := range tests {
		if got := parseRetryAfter(tt.in, now); got != tt.want {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestRetryDelayBackoff(t *testing.T) {
	plain := errors.New("x")
	for attempt, ceil := range map[int]time.Duration{1: 500 * time.Millisecond, 2: time.Second, 3: 2 * time.Second, 10: 8 * time.Second} {
		for range 50 {
			d := retryDelay(attempt, plain)
			if d < ceil/2 || d > ceil {
				t.Fatalf("attempt %d: delay %v outside [%v, %v]", attempt, d, ceil/2, ceil)
			}
		}
	}
	if d := retryDelay(1, &APIError{Status: 429, RetryAfter: 3 * time.Second}); d != 3*time.Second {
		t.Fatalf("Retry-After not honored: %v", d)
	}
}
