package chat

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestWithSenderStampsTheAuthenticatedUser(t *testing.T) {
	user := uuid.New()
	forged := uuid.New()
	raw, err := withSender(json.RawMessage(`{"source":"task_dispatch","sender_user_id":"`+forged.String()+`"}`), user)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["source"] != "task_dispatch" || fields[SenderUserIDKey] != user.String() {
		t.Fatalf("metadata = %s", raw)
	}
	if got := SenderUserID(raw); got == nil || *got != user {
		t.Fatalf("SenderUserID = %v", got)
	}

	empty, err := withSender(nil, user)
	if err != nil || SenderUserID(empty) == nil {
		t.Fatalf("empty metadata must still carry the sender: %s, %v", empty, err)
	}
	if _, err := withSender(json.RawMessage(`[1,2]`), user); err == nil {
		t.Fatal("metadata that is not an object must be refused")
	}
	if got := SenderUserID(json.RawMessage(`{"sender_user_id":"not-a-uuid"}`)); got != nil {
		t.Fatal("a malformed sender is no sender")
	}
	if got := SenderUserID(nil); got != nil {
		t.Fatal("no metadata, no sender")
	}
}
