package events

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"superbot/internal/store"
)

const (
	TypeMessageCreated = "message.created"
	TypeCycleUpdated   = "cycle.updated"
	TypeRouteDecision  = "route_decision"
	TypeTextDelta      = "text_delta"
	TypeReasoningDelta = "reasoning_delta"
	TypeToolCall       = "tool_call"
	TypeToolResult     = "tool_result"
	TypeToolApproval   = "tool_approval"
	TypeStatus         = "status"
	TypeUsage          = "usage"
	TypeResync         = "resync"
	TypeError          = "error"
	TypeGoalUpdated    = "goal.updated"
)

// Envelope is what the SSE endpoint sends to the browser. Payload is the
// event-specific JSON object.
type Envelope struct {
	Seq       int64           `json:"seq"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

// Append allocates the next per-conversation sequence number and inserts an
// event. Both steps must run in the same transaction as any message write so
// that commit order and seq order agree.
func Append(
	ctx context.Context,
	q *store.Queries,
	conversationID uuid.UUID,
	cycleID *uuid.UUID,
	runID *uuid.UUID,
	typ string,
	payload any,
) (store.Event, error) {
	seq, err := q.AllocateConversationEventSeq(ctx, conversationID)
	if err != nil {
		return store.Event{}, fmt.Errorf("allocate event seq: %w", err)
	}
	raw := json.RawMessage(`{}`)
	if payload != nil {
		body, err := json.Marshal(payload)
		if err != nil {
			return store.Event{}, fmt.Errorf("encode event payload: %w", err)
		}
		raw = body
	}
	event, err := q.InsertEvent(ctx, store.InsertEventParams{
		ConversationID: conversationID,
		Seq:            seq,
		CycleID:        cycleID,
		RunID:          runID,
		Type:           typ,
		Payload:        raw,
	})
	if err != nil {
		return store.Event{}, fmt.Errorf("insert event: %w", err)
	}
	return event, nil
}
