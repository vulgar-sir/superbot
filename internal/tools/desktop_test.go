package tools

import (
	"testing"

	"github.com/google/uuid"
)

func TestDesktopToolsRegistered(t *testing.T) {
	items := NewDesktopTools(nil, uuid.New(), uuid.New())
	want := map[string]bool{
		"desktop_screenshot":    false,
		"desktop_click":         false,
		"desktop_type":          false,
		"desktop_key":           false,
		"desktop_scroll":        false,
		"desktop_list_windows":  false,
		"desktop_window_action": false,
	}
	for _, item := range items {
		if _, ok := want[item.Name()]; ok {
			want[item.Name()] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Fatalf("desktop tool %s is not registered", name)
		}
	}
}
