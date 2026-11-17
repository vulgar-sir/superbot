package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"superbot/internal/store"
)

func TestUpgradeBuiltinRewritesOnlyUntouchedCopies(t *testing.T) {
	builtin := Builtin{
		Name:        "demo",
		Description: "演示",
		Content:     "# 新版\n- 用 send_file 交付",
		Previous:    []BuiltinVersion{{Content: "# 旧版\n- 只给路径"}},
	}
	dir := t.TempDir()
	untouched := filepath.Join(dir, "untouched.md")
	edited := filepath.Join(dir, "edited.md")
	old := renderBuiltin(Builtin{Name: builtin.Name, Description: builtin.Description, Content: builtin.Previous[0].Content})
	if err := os.WriteFile(untouched, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(edited, []byte(old+"\n- 我自己加的规则\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	service := &Service{}
	service.upgradeBuiltin(t.Context(), [16]byte{}, store.Skill{Name: "demo", Description: "演示", FilesPath: untouched}, builtin)
	service.upgradeBuiltin(t.Context(), [16]byte{}, store.Skill{Name: "demo", Description: "演示", FilesPath: edited}, builtin)

	if raw, _ := os.ReadFile(untouched); string(raw) != renderBuiltin(builtin) {
		t.Fatalf("untouched copy not upgraded:\n%s", raw)
	}
	if raw, _ := os.ReadFile(edited); !strings.Contains(string(raw), "我自己加的规则") || strings.Contains(string(raw), "新版") {
		t.Fatalf("edited copy was overwritten:\n%s", raw)
	}
}

func TestShippedBuiltinsMentionSendFile(t *testing.T) {
	for _, builtin := range Builtins {
		if builtin.Name == "library" || builtin.Name == "box-desktop" {
			if !strings.Contains(builtin.Content, "send_file") {
				t.Errorf("%s does not tell bots to hand files over with send_file", builtin.Name)
			}
			if len(builtin.Previous) == 0 {
				t.Errorf("%s has no previous version, so existing copies would never be upgraded", builtin.Name)
			}
		}
	}
}
