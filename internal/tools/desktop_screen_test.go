package tools

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func word(text string, x, y, w int, line int) screenWord {
	return screenWord{Text: text, Conf: 90, X: x, Y: y, W: w, H: 14, Block: 1, Par: 1, Line: line}
}

func TestSegmentWordsSplitsMenuItemsAndJoinsChinese(t *testing.T) {
	words := []screenWord{
		// A menu bar: wide gaps between items.
		word("File", 10, 100, 28, 1), word("Edit", 70, 100, 28, 1), word("Search", 130, 100, 50, 1),
		// A sentence: normal word spacing stays one segment.
		word("Hello", 10, 200, 40, 2), word("desktop", 56, 200, 60, 2),
		// Chinese: one OCR word per character.
		word("保", 300, 300, 14, 3), word("存", 315, 300, 14, 3), word("文", 330, 300, 14, 3), word("件", 345, 300, 14, 3),
		// Noise and low confidence are dropped.
		{Text: "|", Conf: 95, X: 500, Y: 400, W: 3, H: 14, Block: 2, Par: 1, Line: 1},
		{Text: "ghost", Conf: 10, X: 600, Y: 400, W: 30, H: 14, Block: 2, Par: 1, Line: 1},
	}
	segments := segmentWords(words)
	var texts []string
	for _, s := range segments {
		texts = append(texts, s.Text)
	}
	want := []string{"File", "Edit", "Search", "Hello desktop", "保存文件"}
	if !slices.Equal(texts, want) {
		t.Fatalf("segments = %q, want %q", texts, want)
	}
	if x, y := segments[1].center(); x != 84 || y != 107 {
		t.Fatalf("Edit center = %d,%d", x, y)
	}
}

func TestFindTextPointsAtTheMatchedWords(t *testing.T) {
	segments := segmentWords([]screenWord{
		word("另", 100, 50, 14, 1), word("存", 115, 50, 14, 1), word("为", 130, 50, 14, 1),
		word("保", 145, 50, 14, 1), word("存", 160, 50, 14, 1),
		word("Save", 400, 50, 30, 2), word("As", 435, 50, 16, 2),
	})
	matches := findText(segments, "保存")
	if len(matches) != 1 || matches[0].Text != "保存" {
		t.Fatalf("matches = %+v", matches)
	}
	if x, _ := matches[0].center(); x != 159 {
		t.Fatalf("click x = %d, want the middle of 保存 (159), not of the whole line", x)
	}
	if got := findText(segments, "save as"); len(got) != 1 || got[0].Text != "Save As" {
		t.Fatalf("case and space insensitive match = %+v", got)
	}
	if got := findText(segments, "missing"); len(got) != 0 {
		t.Fatalf("unexpected match %+v", got)
	}
}

func TestFormatScreen(t *testing.T) {
	capture := screenCapture{
		Width: 1440, Height: 900, Mouse: []int{10, 20}, Active: "0x02a00003",
		Windows: []screenWindow{
			{ID: "0x00a00003", Desktop: -1, Title: "xfce4-panel", W: 1440, H: 27},
			{ID: "0x02a00003", Desktop: 0, X: 100, Y: 80, W: 400, H: 300, Title: "galculator"},
			{ID: "0x02200003", Desktop: 0, X: 10, Y: 37, W: 1050, H: 853, Title: "about:blank - Chromium"},
		},
		Words: []screenWord{word("OK", 200, 300, 20, 1), word("Cancel", 260, 300, 50, 1)},
	}
	out := formatScreen(capture, "", true)
	for _, want := range []string{
		"Screen 1440x900, mouse at (10,20)",
		`* "galculator" at (100,80) size 400x300`,
		`  "about:blank - Chromium"`,
		"(210,307) OK · (285,307) Cancel",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "xfce4-panel") {
		t.Error("the panel is not a window the bot works with")
	}
	found := formatScreen(capture, "cancel", false)
	if !strings.Contains(found, `Found "cancel" 1 time(s):`) || !strings.Contains(found, "(285,307) Cancel") || strings.Contains(found, "OK") {
		t.Errorf("find output:\n%s", found)
	}
	missing := formatScreen(capture, "保存", false)
	if !strings.Contains(missing, "was not found") || !strings.Contains(missing, "(210,307) OK") {
		t.Errorf("not-found output should list the visible text:\n%s", missing)
	}
}

func TestFormatScreenTextIsBounded(t *testing.T) {
	var words []screenWord
	for i := 0; i < 2000; i++ {
		words = append(words, word("Item"+strings.Repeat("x", 10), 10, 10+i*20, 100, i))
	}
	out := formatScreenText(segmentWords(words), maxScreenText)
	if len(out) > maxScreenText+200 || !strings.Contains(out, "more text items not shown") {
		t.Fatalf("output %d bytes; tail %q", len(out), out[len(out)-120:])
	}
}

func TestParseScreenCaptureSkipsProfileNoise(t *testing.T) {
	capture, err := parseScreenCapture("some login banner\n{\"width\": 1440, \"height\": 900, \"words\": [{\"t\": \"OK\", \"c\": 91, \"x\": 1, \"y\": 2, \"w\": 3, \"h\": 4, \"b\": 1, \"p\": 1, \"l\": 1}]}\n")
	if err != nil || capture.Width != 1440 || len(capture.Words) != 1 || capture.Words[0].Text != "OK" {
		t.Fatalf("capture = %+v, %v", capture, err)
	}
	if _, err := parseScreenCapture("Traceback ..."); err == nil {
		t.Fatal("garbage must be an error")
	}
}

func TestClickCommand(t *testing.T) {
	cmd, summary, err := clickCommand(10, 20, "right", 2, []string{"ctrl"})
	if err != nil {
		t.Fatal(err)
	}
	// No --sync: xdotool waits ~15s for a motion event when the pointer is
	// already there.
	want := []string{"xdotool", "mousemove", "10", "20", "sleep", "0.05", "keydown", "ctrl", "click", "--repeat", "2", "--delay", "80", "3", "keyup", "ctrl"}
	if !slices.Equal(cmd, want) {
		t.Fatalf("cmd = %q", cmd)
	}
	if summary != "ctrl+right double-click at 10,20" {
		t.Fatalf("summary = %q", summary)
	}
	if cmd, _, _ := clickCommand(1, 2, "", 0, nil); cmd[len(cmd)-1] != "1" || cmd[len(cmd)-4] != "1" {
		t.Fatalf("default is one left click: %q", cmd)
	}
	for _, bad := range []struct {
		button string
		clicks int
		mods   []string
	}{{"side", 1, nil}, {"left", 4, nil}, {"left", 1, []string{"hyper"}}} {
		if _, _, err := clickCommand(1, 1, bad.button, bad.clicks, bad.mods); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	if _, _, err := clickCommand(-1, 5, "", 1, nil); err == nil {
		t.Error("accepted negative coordinates")
	}
}

func TestDragCommandMovesInSteps(t *testing.T) {
	cmd, err := dragCommand(0, 0, 100, 50, "")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(cmd, " ")
	if !strings.Contains(joined, "mousedown 1") || !strings.HasSuffix(joined, "mouseup 1") || strings.Contains(joined, "--sync") ||
		!strings.Contains(joined, "mousemove 20 10") || !strings.Contains(joined, "mousemove 100 50") {
		t.Fatalf("drag = %s", joined)
	}
}

func TestKeyCommand(t *testing.T) {
	cmd, err := keyCommand("ctrl+a  Delete", 2)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"xdotool", "key", "--clearmodifiers", "--delay", "40", "ctrl+a", "Delete", "ctrl+a", "Delete"}; !slices.Equal(cmd, want) {
		t.Fatalf("cmd = %q", cmd)
	}
	for _, bad := range []string{"", "ctrl+a; rm", "$(id)", strings.Repeat("a ", 21)} {
		if _, err := keyCommand(bad, 1); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	if _, err := keyCommand("Return", 51); err == nil {
		t.Error("accepted repeat 51")
	}
}

func TestNewWindows(t *testing.T) {
	before := screenCapture{Windows: []screenWindow{{ID: "0x01", Desktop: 0}, {ID: "0x02", Desktop: -1}}}
	after := screenCapture{Windows: []screenWindow{{ID: "0x01", Desktop: 0}, {ID: "0x02", Desktop: -1},
		{ID: "0x03", Desktop: -1, Title: "panel"}, {ID: "0x04", Desktop: 0, Title: "app"}}}
	opened := newWindows(before, after)
	if len(opened) != 1 || opened[0].ID != "0x04" {
		t.Fatalf("opened = %+v", opened)
	}
}

func TestLaunchScriptDetachesFromTheExec(t *testing.T) {
	for _, want := range []string{"env -u SUPERBOT_EXEC_MARK setsid", "</dev/null &", `sh -lc "$1"`} {
		if !strings.Contains(launchScript, want) {
			t.Errorf("launch script lacks %q", want)
		}
	}
}

func TestShellDescriptionFollowsTheComputer(t *testing.T) {
	none := NewShellTool(nil, uuid.New(), uuid.New(), "none").Description()
	desktop := NewShellTool(nil, uuid.New(), uuid.New(), "desktop").Description()
	if !strings.Contains(none, "no network") || strings.Contains(desktop, "no network") {
		t.Fatalf("network wording wrong:\nnone: %s\ndesktop: %s", none, desktop)
	}
	for _, want := range []string{"background=true", "userapt install", "desktop_launch", "/workspace/.home"} {
		if !strings.Contains(desktop, want) {
			t.Errorf("shell description lacks %q", want)
		}
	}
}

func TestClickSequenceChainsClicksInOneCommand(t *testing.T) {
	cmd, summary, err := clickSequence([]clickPoint{{X: 10, Y: 20}, {X: 30, Y: 40}, {X: 50, Y: 60}}, "", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(cmd, " ")
	if cmd[0] != "xdotool" || strings.Count(joined, "xdotool") != 1 {
		t.Fatalf("one xdotool invocation expected: %q", joined)
	}
	for _, want := range []string{"mousemove 10 20", "mousemove 30 40", "mousemove 50 60"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %q", want, joined)
		}
	}
	if strings.Count(joined, "click --repeat 1") != 3 || strings.Count(summary, "left click at") != 3 {
		t.Fatalf("three clicks expected: %q / %q", joined, summary)
	}
	if strings.Index(joined, "mousemove 30 40") < strings.Index(joined, "mousemove 10 20") {
		t.Fatalf("clicks out of order: %q", joined)
	}
	tooMany := make([]clickPoint, maxClickSequence+2)
	for i := range tooMany {
		tooMany[i] = clickPoint{X: i, Y: i}
	}
	if _, _, err := clickSequence(tooMany, "", 0, nil); err == nil {
		t.Fatal("an overlong sequence must be refused")
	}
	if _, _, err := clickSequence([]clickPoint{{X: 1, Y: 1}, {X: -5, Y: 1}}, "", 0, nil); err == nil {
		t.Fatal("a bad point anywhere in the sequence must be refused")
	}
}
