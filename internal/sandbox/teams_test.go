package sandbox

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestTeamBindsOnlyMountsOwnTeams(t *testing.T) {
	dataDir := t.TempDir()
	a := uuid.MustParse("bbbbbbbb-0000-0000-0000-000000000000")
	b := uuid.MustParse("aaaaaaaa-0000-0000-0000-000000000000")
	var created []string
	binds, label, err := teamBinds(dataDir, []uuid.UUID{a, b}, func(dir string) error {
		created = append(created, dir)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(binds) != 2 || len(created) != 2 {
		t.Fatalf("want one mount per team, got %v", binds)
	}
	if !strings.HasPrefix(label, b.String()) {
		t.Fatalf("label must be order-independent (sorted), got %q", label)
	}
	for _, bind := range binds {
		parts := strings.SplitN(bind, ":/teams/", 2)
		if len(parts) != 2 || !strings.HasSuffix(parts[1], "/shared") {
			t.Fatalf("each team must be mounted at /teams/<id>/shared, got %q", bind)
		}
		if parts[0] == filepath.ToSlash(filepath.Join(dataDir, "teams")) {
			t.Fatal("the whole teams directory must never be mounted")
		}
	}
	_, reversed, _ := teamBinds(dataDir, []uuid.UUID{b, a}, func(string) error { return nil })
	if reversed != label {
		t.Fatal("the same membership must give the same label regardless of order")
	}
	none, empty, err := teamBinds(dataDir, nil, func(string) error { return nil })
	if err != nil || len(none) != 0 || empty != "" {
		t.Fatalf("a bot without teams gets no team mounts, got %v %q", none, empty)
	}
}
