package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ErrStreamStalled is reported (wrapped) when a streaming response delivers no
// bytes for longer than the provider's idle timeout.
var ErrStreamStalled = errors.New("llm stream stalled")

// APIError is a non-2xx response (or an in-stream error event) from a provider.
type APIError struct {
	Provider   string
	Status     int
	Message    string
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s returned HTTP %d: %s", e.Provider, e.Status, e.Message)
}

// IsRetryable reports whether repeating the same request may succeed.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	// Checked first: a cancelled request also surfaces as a net.Error timeout.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		switch apiErr.Status {
		case http.StatusRequestTimeout, http.StatusConflict, http.StatusTooEarly, http.StatusTooManyRequests:
			return true
		}
		return apiErr.Status >= 500
	}
	if errors.Is(err, ErrStreamStalled) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	for _, errno := range []syscall.Errno{syscall.ECONNRESET, syscall.ECONNREFUSED, syscall.ECONNABORTED, syscall.EPIPE} {
		if errors.Is(err, errno) {
			return true
		}
	}
	// Windows reports WSA* codes that do not match the portable errnos above.
	msg := strings.ToLower(err.Error())
	for _, s := range []string{"connection reset", "connection refused", "forcibly closed", "actively refused", "broken pipe", "server closed idle connection"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

var contextOverflowPhrases = []string{
	"context length",
	"maximum context",
	"context_length_exceeded",
	"too many tokens",
	"prompt is too long",
	"exceeds the maximum",
	"reduce the length",
}

// IsContextOverflow reports whether the provider rejected the request because
// the prompt does not fit the model's context window.
func IsContextOverflow(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	if apiErr.Status != http.StatusBadRequest && apiErr.Status != http.StatusRequestEntityTooLarge {
		return false
	}
	msg := strings.ToLower(apiErr.Message)
	for _, phrase := range contextOverflowPhrases {
		if strings.Contains(msg, phrase) {
			return true
		}
	}
	return false
}

// IsAuthError reports whether the provider rejected the credentials.
func IsAuthError(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.Status == http.StatusUnauthorized || apiErr.Status == http.StatusForbidden
}

const maxRetryAfter = 30 * time.Second

// parseRetryAfter accepts both forms of the Retry-After header: delay seconds
// or an HTTP date.
func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	var d time.Duration
	if secs, err := strconv.ParseFloat(value, 64); err == nil {
		d = time.Duration(secs * float64(time.Second))
	} else if at, err := http.ParseTime(value); err == nil {
		d = at.Sub(now)
	}
	if d < 0 {
		return 0
	}
	return min(d, maxRetryAfter)
}
