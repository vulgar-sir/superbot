package memory

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBreakerOpensAndBacksOff(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	b := newBreaker()
	b.now = func() time.Time { return now }
	if !b.Allow() {
		t.Fatal("a new breaker is closed")
	}
	b.Failure()
	if !b.Allow() {
		t.Fatal("one failure must not open the breaker")
	}
	b.Failure()
	if b.Allow() || !b.RetryAt().Equal(now.Add(breakerBase)) {
		t.Fatalf("two failures open it for %v, retry at %v", breakerBase, b.RetryAt())
	}
	now = now.Add(breakerBase)
	if !b.Allow() {
		t.Fatal("after the open period one call is let through")
	}
	b.Failure()
	if b.Allow() || !b.RetryAt().Equal(now.Add(2*breakerBase)) {
		t.Fatalf("a failed probe doubles the wait, retry at %v", b.RetryAt())
	}
	for i := 0; i < 10; i++ {
		now = b.RetryAt()
		b.Failure()
	}
	if got := b.RetryAt().Sub(now); got != breakerMax {
		t.Fatalf("backoff must cap at %v, got %v", breakerMax, got)
	}
	b.Success()
	if !b.Allow() || !b.RetryAt().IsZero() {
		t.Fatal("a success closes the breaker")
	}
	b.Failure()
	if !b.Allow() {
		t.Fatal("after a success the count starts over")
	}
}

func TestIsOutage(t *testing.T) {
	cases := map[error]bool{
		errors.New("dial tcp: connection refused"):              true,
		context.DeadlineExceeded:                                true,
		&apiStatusError{status: http.StatusBadGateway}:          true,
		&apiStatusError{status: http.StatusTooManyRequests}:     true,
		&apiStatusError{status: http.StatusBadRequest}:          false,
		&apiStatusError{status: http.StatusUnprocessableEntity}: false,
	}
	for err, want := range cases {
		if got := isOutage(err); got != want {
			t.Errorf("isOutage(%v) = %v, want %v", err, got, want)
		}
	}
}

// A hung embedding service costs a recall at most the recall timeout, and
// once the breaker is open nothing at all.
func TestHungEmbeddingServiceDoesNotSlowRecall(t *testing.T) {
	release := make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer close(release)

	t.Setenv(RecallTimeoutEnv, "50")
	service := NewService(nil, "key", server.URL, "intfloat/multilingual-e5-large")
	if service.recallTimeout != 50*time.Millisecond {
		t.Fatalf("recall timeout = %v", service.recallTimeout)
	}
	for i := 0; i < breakerFailures; i++ {
		start := time.Now()
		if _, err := service.embedVector(context.Background(), "部署端口是多少", kindQuery, service.recallTimeout); err == nil {
			t.Fatal("a hung service must time out")
		}
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Fatalf("a recall waited %v for a hung service", elapsed)
		}
	}
	before := calls.Load()
	start := time.Now()
	_, err := service.embedVector(context.Background(), "部署端口是多少", kindQuery, service.recallTimeout)
	if !errors.Is(err, ErrEmbeddingUnavailable) || time.Since(start) > 10*time.Millisecond {
		t.Fatalf("with the breaker open a call must fail at once, got %v after %v", err, time.Since(start))
	}
	if calls.Load() != before {
		t.Fatal("an open breaker must not reach the service")
	}
	if service.EmbeddingAvailable() {
		t.Fatal("EmbeddingAvailable must report the outage")
	}
}

func TestCallerCancellationDoesNotTripBreaker(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer close(release)
	service := NewService(nil, "key", server.URL, "small")
	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		_, _ = service.embedVector(ctx, "x", kindQuery, time.Minute)
		cancel()
	}
	if !service.breaker.Allow() {
		t.Fatal("callers giving up say nothing about the service")
	}
}

func TestRejectedInputDoesNotTripBreaker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"input too long"}}`))
	}))
	defer server.Close()
	service := NewService(nil, "key", server.URL, "small")
	for i := 0; i < 3; i++ {
		if _, err := service.EmbedText(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "400") {
			t.Fatalf("expected the 400 to surface, got %v", err)
		}
	}
	if !service.breaker.Allow() {
		t.Fatal("a refused input is not an outage")
	}
}

// fakeEmbeddingServer answers like the OpenAI API and records every input.
type fakeEmbeddingServer struct {
	mu     sync.Mutex
	inputs []string
}

func (f *fakeEmbeddingServer) handler(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Input json.RawMessage `json:"input"`
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)
	var inputs []string
	var one string
	if err := json.Unmarshal(payload.Input, &one); err == nil {
		inputs = []string{one}
	} else {
		_ = json.Unmarshal(payload.Input, &inputs)
	}
	f.mu.Lock()
	f.inputs = append(f.inputs, inputs...)
	f.mu.Unlock()
	data := make([]map[string]any, len(inputs))
	for i, input := range inputs {
		data[i] = map[string]any{"index": i, "embedding": Embed(input)}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}

func (f *fakeEmbeddingServer) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.inputs...)
}

func TestE5PrefixesReachTheService(t *testing.T) {
	fake := &fakeEmbeddingServer{}
	server := httptest.NewServer(http.HandlerFunc(fake.handler))
	defer server.Close()
	service := NewService(nil, "key", server.URL, "intfloat/multilingual-e5-large")
	if _, err := service.EmbedText(context.Background(), "用户对花生过敏"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.embedVector(context.Background(), "我能吃宫保鸡丁吗", kindQuery, time.Second); err != nil {
		t.Fatal(err)
	}
	seen := fake.seen()
	if len(seen) != 2 || seen[0] != "passage: 用户对花生过敏" || seen[1] != "query: 我能吃宫保鸡丁吗" {
		t.Fatalf("inputs = %q", seen)
	}
}

func TestWarmCalibrationBatchesReferencePassages(t *testing.T) {
	fake := &fakeEmbeddingServer{}
	server := httptest.NewServer(http.HandlerFunc(fake.handler))
	defer server.Close()
	t.Setenv(MinSimilarityEnv, "")
	service := NewService(nil, "key", server.URL, "intfloat/multilingual-e5-large")
	if err := service.WarmCalibration(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := len(service.calibrationVectors()); got != len(calibrationPassages) {
		t.Fatalf("calibration vectors = %d, want %d", got, len(calibrationPassages))
	}
	seen := fake.seen()
	if len(seen) != len(calibrationPassages) || seen[0] != "passage: "+calibrationPassages[0] {
		t.Fatalf("calibration inputs = %d, first %q", len(seen), seen[0])
	}
	// Loaded once: a second warm-up is free.
	if err := service.WarmCalibration(context.Background()); err != nil || len(fake.seen()) != len(seen) {
		t.Fatal("calibration must be loaded once")
	}
	query, _ := service.embedVector(context.Background(), "部署端口", kindQuery, time.Second)
	if got := service.recallThreshold(query); got < service.space.RecallFloor || got == service.space.RecallMin {
		t.Fatalf("a calibrated threshold was expected, got %v", got)
	}
}
