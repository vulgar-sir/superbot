package llm

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

const (
	defaultIdleTimeout    = 120 * time.Second
	responseHeaderTimeout = 60 * time.Second
	maxRequestAttempts    = 3
	retryBaseDelay        = 500 * time.Millisecond
	retryMaxDelay         = 8 * time.Second
)

// transport is the HTTP machinery shared by the streaming providers: request
// retries, typed errors, the idle watchdog and channel delivery.
type transport struct {
	provider string
	client   *http.Client
	idle     time.Duration
	// sleep waits between request attempts; tests replace it.
	sleep func(ctx context.Context, d time.Duration) error
}

func newTransport(provider string, idleTimeout time.Duration) *transport {
	if idleTimeout <= 0 {
		idleTimeout = defaultIdleTimeout
	}
	rt := http.DefaultTransport.(*http.Transport).Clone()
	rt.ResponseHeaderTimeout = responseHeaderTimeout
	return &transport{
		provider: provider,
		client:   &http.Client{Transport: rt},
		idle:     idleTimeout,
		sleep:    sleepContext,
	}
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// open sends the request, retrying retryable failures. It only covers the
// phase before a response body exists, so no event can have been delivered.
func (t *transport) open(ctx context.Context, newReq func(context.Context) (*http.Request, error)) (*http.Response, error) {
	for attempt := 1; ; attempt++ {
		req, err := newReq(ctx)
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		resp, err := t.client.Do(req)
		if err == nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, nil
		}
		if err != nil {
			err = fmt.Errorf("%s request: %w", t.provider, err)
		} else {
			apiErr := t.apiError(resp)
			dumpRejected(req, apiErr)
			err = apiErr
		}
		if ctx.Err() != nil || attempt >= maxRequestAttempts || !IsRetryable(err) {
			return nil, err
		}
		if sleepErr := t.sleep(ctx, retryDelay(attempt, err)); sleepErr != nil {
			return nil, fmt.Errorf("%s request: %w", t.provider, sleepErr)
		}
	}
}

func (t *transport) apiError(resp *http.Response) *APIError {
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return &APIError{
		Provider:   t.provider,
		Status:     resp.StatusCode,
		Message:    strings.TrimSpace(string(body)),
		RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()),
	}
}

func retryDelay(attempt int, err error) time.Duration {
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.RetryAfter > 0 {
		return apiErr.RetryAfter
	}
	d := min(retryBaseDelay<<(attempt-1), retryMaxDelay)
	return d/2 + rand.N(d/2+1)
}

// parseFunc decodes a response body into events. A returned error becomes the
// stream's terminal EventError.
type parseFunc func(ctx context.Context, body io.Reader, emit func(Event)) error

func (t *transport) stream(ctx context.Context, newReq func(context.Context) (*http.Request, error), parse parseFunc) (Stream, error) {
	streamCtx, cancel := context.WithCancel(ctx)
	// The request gets its own context so the idle watchdog can abort the body
	// while streamCtx stays alive to deliver the stall error.
	reqCtx, abort := context.WithCancel(streamCtx)
	resp, err := t.open(reqCtx, newReq)
	if err != nil {
		abort()
		cancel()
		return nil, err
	}
	body := newIdleBody(resp.Body, t.idle, abort)

	ch := make(chan Event, 32)
	emit := func(ev Event) {
		select {
		case ch <- ev:
		case <-streamCtx.Done():
		}
	}
	go func() {
		defer close(ch)
		defer cancel()
		defer body.Close()
		if err := parse(streamCtx, body, emit); err != nil && streamCtx.Err() == nil {
			emit(Event{Type: EventError, Err: fmt.Errorf("%s stream: %w", t.provider, err)})
		}
	}()
	return NewChanStream(ch, func() {
		cancel()
		_ = body.Close()
	}), nil
}

// idleBody aborts the response when a single Read blocks longer than idle.
// The timer only runs while a Read is in flight, so a slow consumer is never
// mistaken for a stalled provider.
type idleBody struct {
	body    io.ReadCloser
	idle    time.Duration
	timer   *time.Timer
	stalled atomic.Bool
}

func newIdleBody(body io.ReadCloser, idle time.Duration, abort func()) *idleBody {
	b := &idleBody{body: body, idle: idle}
	b.timer = time.AfterFunc(idle, func() {
		b.stalled.Store(true)
		abort()
		_ = body.Close()
	})
	b.timer.Stop()
	return b
}

func (b *idleBody) Read(p []byte) (int, error) {
	b.timer.Reset(b.idle)
	n, err := b.body.Read(p)
	b.timer.Stop()
	if err != nil && b.stalled.Load() {
		return n, fmt.Errorf("%w: no data for %s", ErrStreamStalled, b.idle)
	}
	return n, err
}

func (b *idleBody) Close() error {
	b.timer.Stop()
	return b.body.Close()
}

// scanSSE calls fn with the payload of every `data:` line until fn reports
// done, fn fails, or the body ends.
func scanSSE(body io.Reader, fn func(data string) (done bool, err error)) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		done, err := fn(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		if err != nil || done {
			return err
		}
	}
	return scanner.Err()
}

// finalToolCalls orders accumulated tool calls by stream index and fills in
// empty arguments.
func finalToolCalls(byIndex map[int]*ToolCall) []ToolCall {
	if len(byIndex) == 0 {
		return nil
	}
	indexes := make([]int, 0, len(byIndex))
	for idx := range byIndex {
		indexes = append(indexes, idx)
	}
	sort.Ints(indexes)
	calls := make([]ToolCall, 0, len(indexes))
	for _, idx := range indexes {
		call := byIndex[idx]
		if len(call.Arguments) == 0 {
			call.Arguments = []byte("{}")
		}
		calls = append(calls, *call)
	}
	return calls
}
