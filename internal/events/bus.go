package events

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const notifyChannel = "superbot_events"

// Bus fans out Postgres LISTEN/NOTIFY signals to SSE subscribers in this API
// process. Notifications only carry a conversation id; subscribers re-read
// events from the database, so a missed notification degrades to a poll.
type Bus struct {
	pool   *pgxpool.Pool
	logger *slog.Logger

	mu   sync.RWMutex
	subs map[uuid.UUID]map[*subscription]struct{}
}

type subscription struct {
	ch     chan struct{}
	closed bool
}

func NewBus(pool *pgxpool.Pool, logger *slog.Logger) *Bus {
	return &Bus{
		pool:   pool,
		logger: logger,
		subs:   make(map[uuid.UUID]map[*subscription]struct{}),
	}
}

// Subscribe returns a signal channel and a cancel function. The channel is
// closed exactly once by cancel.
func (b *Bus) Subscribe(conversationID uuid.UUID) (<-chan struct{}, func()) {
	sub := &subscription{ch: make(chan struct{}, 1)}
	b.mu.Lock()
	if b.subs[conversationID] == nil {
		b.subs[conversationID] = make(map[*subscription]struct{})
	}
	b.subs[conversationID][sub] = struct{}{}
	b.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			if set, ok := b.subs[conversationID]; ok {
				delete(set, sub)
				if len(set) == 0 {
					delete(b.subs, conversationID)
				}
			}
			if !sub.closed {
				sub.closed = true
				close(sub.ch)
			}
		})
	}
	return sub.ch, cancel
}

func (b *Bus) Publish(conversationID uuid.UUID) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for sub := range b.subs[conversationID] {
		if sub.closed {
			continue
		}
		select {
		case sub.ch <- struct{}{}:
		default:
		}
	}
}

// Notify sends a Postgres notification. It should be called after the
// transaction that wrote the events has committed.
func (b *Bus) Notify(ctx context.Context, conversationID uuid.UUID) error {
	_, err := b.pool.Exec(ctx, `SELECT pg_notify($1, $2)`, notifyChannel, conversationID.String())
	return err
}

// Run listens for notifications until the context is canceled, reconnecting
// with a short backoff if the dedicated connection drops.
func (b *Bus) Run(ctx context.Context) {
	for ctx.Err() == nil {
		err := b.listenOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		b.logger.Warn("event listener disconnected", "error", err)
		select {
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
			return
		}
	}
}

func (b *Bus) listenOnce(ctx context.Context) error {
	conn, err := b.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "LISTEN "+notifyChannel); err != nil {
		return err
	}
	for {
		notification, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		payload := strings.TrimSpace(notification.Payload)
		id, err := uuid.Parse(payload)
		if err != nil {
			b.logger.Warn("invalid notification payload", "payload", payload)
			continue
		}
		b.Publish(id)
	}
}
