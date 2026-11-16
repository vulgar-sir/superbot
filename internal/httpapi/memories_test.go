package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"superbot/internal/memory"
	"superbot/internal/store"
)

func TestMemoryDTOShape(t *testing.T) {
	archived := time.Date(2026, 11, 16, 8, 0, 0, 0, time.UTC)
	raw, err := json.Marshal(toMemoryDTO(store.Memory{
		ID: uuid.New(), Content: "用户常驻上海", ScopeType: "user", Source: "review",
		Pinned: true, ArchivedAt: &archived, CreatedAt: archived, UpdatedAt: archived,
	}))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"id", "content", "scope_type", "source", "pinned", "archived_at", "created_at", "updated_at"} {
		if _, ok := fields[key]; !ok {
			t.Errorf("memory JSON is missing %q: %s", key, raw)
		}
	}
	if len(fields) != 8 {
		t.Errorf("unexpected extra fields: %s", raw)
	}
	if fields["source"] != "review" || fields["pinned"] != true {
		t.Errorf("values wrong: %s", raw)
	}

	raw, _ = json.Marshal(toMemoryDTO(store.Memory{Source: "manual"}))
	_ = json.Unmarshal(raw, &fields)
	if value, ok := fields["archived_at"]; !ok || value != nil {
		t.Errorf("an active memory must serialize archived_at as null: %s", raw)
	}
}

func TestWriteMemoryError(t *testing.T) {
	cases := []struct {
		err  error
		code string
	}{
		{memory.ErrSecretContent, "secret_rejected"},
		{errors.New("content is required"), "invalid_request"},
	}
	for _, tc := range cases {
		recorder := httptest.NewRecorder()
		writeMemoryError(recorder, tc.err)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("status = %d", recorder.Code)
		}
		var body errorBody
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Error.Code != tc.code || body.Error.Message != tc.err.Error() {
			t.Fatalf("body = %+v, want code %q", body, tc.code)
		}
	}
}
