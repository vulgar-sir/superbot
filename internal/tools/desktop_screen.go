package tools

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// desktopScreenScript captures the screen, lists windows and runs OCR inside
// the computer; see desktop_screen.py.
//
//go:embed desktop_screen.py
var desktopScreenScript string

// screenOptions are passed to desktop_screen.py as JSON.
type screenOptions struct {
	Capture bool    `json:"capture"`
	OCR     bool    `json:"ocr"`
	Windows bool    `json:"windows"`
	Region  []int   `json:"region,omitempty"`
	Scale   float64 `json:"scale,omitempty"`
	Save    string  `json:"save,omitempty"`
	JPEG    string  `json:"jpeg,omitempty"`
}

type screenWord struct {
	Text  string `json:"t"`
	Conf  int    `json:"c"`
	X     int    `json:"x"`
	Y     int    `json:"y"`
	W     int    `json:"w"`
	H     int    `json:"h"`
	Block int    `json:"b"`
	Par   int    `json:"p"`
	Line  int    `json:"l"`
}

type screenWindow struct {
	ID      string `json:"id"`
	Desktop int    `json:"desktop"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	W       int    `json:"w"`
	H       int    `json:"h"`
	Title   string `json:"title"`
}

type screenCapture struct {
	Width   int            `json:"width"`
	Height  int            `json:"height"`
	Mouse   []int          `json:"mouse"`
	Active  string         `json:"active"`
	Windows []screenWindow `json:"windows"`
	Words   []screenWord   `json:"words"`
	Errors  []string       `json:"errors"`
}

func parseScreenCapture(stdout string) (screenCapture, error) {
	var capture screenCapture
	trimmed := strings.TrimSpace(stdout)
	// Anything a profile script printed comes before the JSON line.
	if i := strings.LastIndex(trimmed, "\n{"); i >= 0 {
		trimmed = trimmed[i+1:]
	}
	if err := json.Unmarshal([]byte(trimmed), &capture); err != nil {
		return screenCapture{}, fmt.Errorf("unreadable screen data: %w", err)
	}
	return capture, nil
}

// minWordConf drops OCR guesses; real UI text, Chinese included, scores far
// higher than the noise tesseract reads into icons and borders.
const minWordConf = 35

// textSegment is a run of words that belong together: words on one OCR line
// separated by no more than a normal space. A wide gap starts a new segment,
// which keeps separate buttons and menu items apart.
type textSegment struct {
	Text                   string
	Left, Top, Right, Bott int
	words                  []screenWord
}

func (s textSegment) center() (int, int) { return (s.Left + s.Right) / 2, (s.Top + s.Bott) / 2 }

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r) || (r >= 0x3000 && r <= 0x303f) || (r >= 0xff00 && r <= 0xffef)
}

// joinWords glues OCR words back into text: tesseract reports each Chinese
// character as its own word, which must not be spaced apart.
func joinWords(words []screenWord) string {
	var b strings.Builder
	for i, w := range words {
		if i > 0 {
			prev, _ := utf8.DecodeLastRuneInString(words[i-1].Text)
			next, _ := utf8.DecodeRuneInString(w.Text)
			if !(isCJK(prev) && isCJK(next)) {
				b.WriteByte(' ')
			}
		}
		b.WriteString(w.Text)
	}
	return b.String()
}

// isNoise reports text that is almost certainly a misread icon or border.
func isNoise(text string) bool {
	meaningful := 0
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			meaningful++
		}
	}
	return meaningful == 0
}

func segmentWords(words []screenWord) []textSegment {
	type lineKey struct{ b, p, l int }
	lines := map[lineKey][]screenWord{}
	var order []lineKey
	for _, w := range words {
		if w.Conf < minWordConf || strings.TrimSpace(w.Text) == "" {
			continue
		}
		key := lineKey{w.Block, w.Par, w.Line}
		if _, ok := lines[key]; !ok {
			order = append(order, key)
		}
		lines[key] = append(lines[key], w)
	}
	var segments []textSegment
	for _, key := range order {
		line := lines[key]
		sort.SliceStable(line, func(i, j int) bool { return line[i].X < line[j].X })
		height := 0
		for _, w := range line {
			height += w.H
		}
		height /= len(line)
		maxGap := max(12, height)
		start := 0
		for i := 1; i <= len(line); i++ {
			if i < len(line) && line[i].X-(line[i-1].X+line[i-1].W) <= maxGap {
				continue
			}
			group := line[start:i]
			start = i
			seg := textSegment{Text: joinWords(group), Left: group[0].X, Top: group[0].Y,
				Right: group[0].X + group[0].W, Bott: group[0].Y + group[0].H, words: group}
			for _, w := range group[1:] {
				seg.Left = min(seg.Left, w.X)
				seg.Top = min(seg.Top, w.Y)
				seg.Right = max(seg.Right, w.X+w.W)
				seg.Bott = max(seg.Bott, w.Y+w.H)
			}
			if isNoise(seg.Text) {
				continue
			}
			segments = append(segments, seg)
		}
	}
	// Reading order: rows top to bottom (centers within a few pixels share a
	// row), left to right within a row.
	sort.SliceStable(segments, func(i, j int) bool {
		_, yi := segments[i].center()
		_, yj := segments[j].center()
		if abs(yi-yj) > 6 {
			return yi < yj
		}
		return segments[i].Left < segments[j].Left
	})
	return segments
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// normalizeForMatch folds case and drops spaces, so "保 存" matches "保存"
// and "save" matches "Save".
func normalizeForMatch(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(text) {
		if !unicode.IsSpace(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// findText returns, for every place the query occurs, the box of just the
// words that make up the match, so the center is clickable even when the
// match is one item of a longer segment.
func findText(segments []textSegment, query string) []textSegment {
	needle := normalizeForMatch(query)
	if needle == "" {
		return nil
	}
	var found []textSegment
	for _, seg := range segments {
		// Map every normalized rune back to the word it came from.
		var runes []rune
		var owner []int
		for i, w := range seg.words {
			for _, r := range normalizeForMatch(w.Text) {
				runes = append(runes, r)
				owner = append(owner, i)
			}
		}
		hay := string(runes)
		offset := 0
		for {
			idx := strings.Index(hay[offset:], needle)
			if idx < 0 {
				break
			}
			byteStart := offset + idx
			startRune := utf8.RuneCountInString(hay[:byteStart])
			endRune := startRune + utf8.RuneCountInString(needle) - 1
			first, last := owner[startRune], owner[endRune]
			match := textSegment{Text: joinWords(seg.words[first : last+1]), words: seg.words[first : last+1]}
			match.Left, match.Top = seg.words[first].X, seg.words[first].Y
			match.Right, match.Bott = seg.words[first].X+seg.words[first].W, seg.words[first].Y+seg.words[first].H
			for _, w := range seg.words[first+1 : last+1] {
				match.Left = min(match.Left, w.X)
				match.Top = min(match.Top, w.Y)
				match.Right = max(match.Right, w.X+w.W)
				match.Bott = max(match.Bott, w.Y+w.H)
			}
			found = append(found, match)
			offset = byteStart + len(needle)
		}
	}
	return found
}

// maxScreenText bounds the OCR text one tool result carries.
const maxScreenText = 9000

// formatScreenText lists text segments as "(x,y) text", the click point
// first, joining segments of one row with " · ".
func formatScreenText(segments []textSegment, limit int) string {
	var b strings.Builder
	rowY := -1000
	shown := 0
	for _, seg := range segments {
		x, y := seg.center()
		item := fmt.Sprintf("(%d,%d) %s", x, y, seg.Text)
		if b.Len()+len(item)+4 > limit {
			break
		}
		if abs(y-rowY) <= 6 && b.Len() > 0 {
			b.WriteString(" · ")
		} else {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			rowY = y
		}
		b.WriteString(item)
		shown++
	}
	if shown < len(segments) {
		fmt.Fprintf(&b, "\n… %d more text items not shown; read a part of the screen with desktop_read_screen region, or search with find.", len(segments)-shown)
	}
	return b.String()
}

// formatWindows lists the normal windows (not the panel or the desktop
// background), the active one marked with *.
func formatWindows(capture screenCapture) string {
	var b strings.Builder
	count := 0
	for _, w := range capture.Windows {
		if w.Desktop < 0 {
			continue // sticky: panel, desktop background
		}
		marker := " "
		if strings.EqualFold(w.ID, capture.Active) {
			marker = "*"
		}
		title := w.Title
		if title == "" {
			title = "(untitled)"
		}
		fmt.Fprintf(&b, "%s %q at (%d,%d) size %dx%d\n", marker, title, w.X, w.Y, w.W, w.H)
		count++
	}
	if count == 0 {
		return "(no open windows)\n"
	}
	return b.String()
}

// formatScreen renders a capture for the model: screen size, mouse, the
// windows and the text with click coordinates.
func formatScreen(capture screenCapture, query string, withWindows bool) string {
	var b strings.Builder
	if capture.Width > 0 {
		fmt.Fprintf(&b, "Screen %dx%d", capture.Width, capture.Height)
		if len(capture.Mouse) == 2 {
			fmt.Fprintf(&b, ", mouse at (%d,%d)", capture.Mouse[0], capture.Mouse[1])
		}
		b.WriteString(". Coordinates below are screen pixels; click a text item at its (x,y).\n")
	}
	if withWindows {
		b.WriteString("\nWindows (* = active):\n")
		b.WriteString(formatWindows(capture))
	}
	segments := segmentWords(capture.Words)
	if strings.TrimSpace(query) != "" {
		matches := findText(segments, query)
		if len(matches) == 0 {
			fmt.Fprintf(&b, "\nText %q was not found on the screen.", query)
			if len(segments) > 0 {
				b.WriteString(" Text that is visible:\n")
				b.WriteString(formatScreenText(segments, 2500))
			}
			return strings.TrimSpace(b.String())
		}
		fmt.Fprintf(&b, "\nFound %q %d time(s):\n", query, len(matches))
		b.WriteString(formatScreenText(matches, maxScreenText))
		return strings.TrimSpace(b.String())
	}
	b.WriteString("\nText on screen:\n")
	if len(segments) == 0 {
		b.WriteString("(no readable text)")
	} else {
		b.WriteString(formatScreenText(segments, maxScreenText))
	}
	if len(capture.Errors) > 0 {
		b.WriteString("\n\n[notes] " + strings.Join(capture.Errors, "; "))
	}
	return strings.TrimSpace(b.String())
}
