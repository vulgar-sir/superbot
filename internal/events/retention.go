package events

import "time"

// RetentionPolicy decides which rows of the events table may be pruned. It
// holds no SQL: the pruning job applies it.
type RetentionPolicy struct {
	// DeltaMaxAge is how long streaming delta events are kept. Zero or
	// negative disables pruning.
	DeltaMaxAge time.Duration
	// KeepLastPerConversation is the number of newest events (by seq) of each
	// conversation that are never pruned, whatever their type or age.
	KeepLastPerConversation int
}

// DefaultRetention prunes streaming deltas older than 7 days while always
// keeping the newest 2000 events of every conversation.
func DefaultRetention() RetentionPolicy {
	return RetentionPolicy{
		DeltaMaxAge:             7 * 24 * time.Hour,
		KeepLastPerConversation: 2000,
	}
}

// PrunableTypes returns the event types that may be pruned: streaming deltas,
// whose content is already persisted in the final message. A new slice is
// returned on every call.
func PrunableTypes() []string {
	return []string{TypeTextDelta, TypeReasoningDelta}
}

// IsPrunableType reports whether events of this type may ever be pruned.
func IsPrunableType(typ string) bool {
	return typ == TypeTextDelta || typ == TypeReasoningDelta
}

// Cutoff returns the instant before which prunable events are old enough to
// delete. ok is false when the policy disables pruning.
func (p RetentionPolicy) Cutoff(now time.Time) (cutoff time.Time, ok bool) {
	if p.DeltaMaxAge <= 0 {
		return time.Time{}, false
	}
	return now.Add(-p.DeltaMaxAge), true
}

// ShouldPrune decides for a single event. latestSeq is the highest seq of the
// event's conversation.
func (p RetentionPolicy) ShouldPrune(typ string, seq, latestSeq int64, createdAt, now time.Time) bool {
	cutoff, ok := p.Cutoff(now)
	if !ok || !IsPrunableType(typ) {
		return false
	}
	if p.KeepLastPerConversation > 0 && seq > latestSeq-int64(p.KeepLastPerConversation) {
		return false
	}
	return createdAt.Before(cutoff)
}
