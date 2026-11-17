package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"superbot/internal/llm"
	"superbot/internal/tools"
)

func screenshotResult() tools.Result {
	return tools.Result{Content: "Text on screen: (10,10) OK", Images: []llm.Image{{MIMEType: "image/jpeg", Data: []byte{1, 2, 3}}}}
}

func TestToolImagesOnlyForVisionModels(t *testing.T) {
	images := collectToolImages(nil, "desktop_screenshot", screenshotResult())
	images = collectToolImages(images, "shell", tools.Result{Content: "no picture"})
	if len(images) != 1 {
		t.Fatalf("collected %d images", len(images))
	}
	base := []llm.Message{llm.TextMessage(llm.RoleUser, "hi")}
	if got := appendToolImages(base, images, false); len(got) != 1 {
		t.Fatal("a text-only model must not receive images")
	}
	got := appendToolImages(base, images, true)
	if len(got) != 2 || got[1].Role != llm.RoleUser || len(got[1].Parts) != 2 || got[1].Parts[1].Type != llm.PartImage {
		t.Fatalf("vision messages = %+v", got)
	}
	if !strings.Contains(got[1].Parts[0].Text, "desktop_screenshot") {
		t.Fatalf("image message should name the tool: %q", got[1].Parts[0].Text)
	}
}

func TestToolImagesKeepOnlyTheLatestScreenshot(t *testing.T) {
	images := collectToolImages(nil, "desktop_screenshot", screenshotResult())
	messages := appendToolImages([]llm.Message{llm.TextMessage(llm.RoleUser, "hi")}, images, true)
	messages = append(messages, llm.AssistantMessage("", "", nil))
	messages = appendToolImages(messages, images, true)
	count := 0
	for _, message := range messages {
		for _, part := range message.Parts {
			if part.Type == llm.PartImage {
				count++
			}
		}
	}
	if count != 1 {
		t.Fatalf("%d images in context; older screenshots must be dropped", count)
	}
	if messages[1].Text() != toolImagesDropped {
		t.Fatalf("older screenshot message = %q", messages[1].Text())
	}
}

func TestComputerSectionByProfile(t *testing.T) {
	desktop := computerSection("desktop", []string{"desktop_screenshot", "local_exec"})
	for _, want := range []string{"desktop_read_screen", "find", "desktop_launch", "userapt install", "background=true", "shell_jobs", "local_exec"} {
		if !strings.Contains(desktop, want) {
			t.Errorf("desktop section lacks %q", want)
		}
	}
	browser := computerSection("browser", []string{"computer_enable"})
	if !strings.Contains(browser, "profile=desktop") || !strings.Contains(browser, "userapt") {
		t.Errorf("browser section should offer the desktop upgrade and the installer:\n%s", browser)
	}
	if strings.Contains(browser, "local_exec") {
		t.Error("without a paired PC the prompt must not promise local_exec")
	}
	none := computerSection("none", nil)
	if !strings.Contains(none, "computer_enable") || !strings.Contains(none, "profile=desktop") || !strings.Contains(none, "不能联网") {
		t.Errorf("no-computer section:\n%s", none)
	}
}

func TestComputerEnableOffersDesktop(t *testing.T) {
	tool := &computerEnableTool{}
	var schema struct {
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(tool.Parameters(), &schema); err != nil {
		t.Fatal(err)
	}
	if got := schema.Properties["profile"].Enum; len(got) != 2 || got[0] != "browser" || got[1] != "desktop" {
		t.Fatalf("profile enum = %v", got)
	}
	if !strings.Contains(tool.Description(), "desktop") {
		t.Fatal("description must explain the desktop profile")
	}
}
