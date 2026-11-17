package memory

import (
	"errors"
	"sync"
	"time"
)

// ErrEmbeddingUnavailable is returned without a network call while the
// embedding service is considered down.
var ErrEmbeddingUnavailable = errors.New("embedding service is temporarily unavailable")

const (
	breakerFailures = 2
	breakerBase     = 30 * time.Second
	breakerMax      = 10 * time.Minute
)

// breaker is a small circuit breaker for the embedding service. After
// breakerFailures consecutive outages it opens for breakerBase; while open,
// callers fail fast instead of waiting for a timeout. When the period ends one
// call is let through; if it fails too the breaker reopens for twice as long
// (up to breakerMax), a success closes it.
type breaker struct {
	mu        sync.Mutex
	failures  int
	backoff   time.Duration
	openUntil time.Time
	now       func() time.Time
}

func newBreaker() *breaker { return &breaker{now: time.Now} }

// Allow reports whether a call may go out now.
func (b *breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !b.now().Before(b.openUntil)
}

// Open reports whether the breaker currently blocks calls.
func (b *breaker) Open() bool { return !b.Allow() }

func (b *breaker) Success() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures = 0
	b.backoff = 0
	b.openUntil = time.Time{}
}

func (b *breaker) Failure() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures++
	if b.failures < breakerFailures {
		return
	}
	if b.backoff == 0 {
		b.backoff = breakerBase
	} else {
		b.backoff = min(b.backoff*2, breakerMax)
	}
	b.openUntil = b.now().Add(b.backoff)
}

// RetryAt is when the next call will be allowed (zero when closed).
func (b *breaker) RetryAt() time.Time {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.openUntil
}
