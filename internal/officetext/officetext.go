// Package officetext pulls the plain text out of Office Open XML files
// (.docx, .pptx, .xlsx) so a bot can read a Word template or a spreadsheet
// that a user uploaded, and the Library can show and search it. It reads the
// XML parts directly; layout, images and formatting are dropped.
package officetext

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// maxPartBytes bounds how much of one XML part is read, so a crafted archive
// cannot make extraction allocate without limit.
const maxPartBytes = 32 << 20

// ErrUnsupported means the file is not a format this package reads.
var ErrUnsupported = errors.New("officetext: unsupported file type")

// Supported reports whether name has an extension Extract understands.
func Supported(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".docx", ".pptx", ".xlsx":
		return true
	}
	return false
}

// Extract returns the text of an Office file given its name (for the
// extension) and its bytes. The result is capped at limit runes when limit is
// positive; truncated reports whether anything was cut.
func Extract(name string, data []byte, limit int) (text string, truncated bool, err error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", false, fmt.Errorf("officetext: open archive: %w", err)
	}
	var out string
	switch strings.ToLower(filepath.Ext(name)) {
	case ".docx":
		out, err = docx(reader)
	case ".pptx":
		out, err = pptx(reader)
	case ".xlsx":
		out, err = xlsx(reader)
	default:
		return "", false, ErrUnsupported
	}
	if err != nil {
		return "", false, err
	}
	out = strings.TrimSpace(collapseBlankLines(out))
	if limit > 0 {
		runes := []rune(out)
		if len(runes) > limit {
			return string(runes[:limit]), true, nil
		}
	}
	return out, false, nil
}

func readPart(reader *zip.Reader, name string) ([]byte, error) {
	for _, file := range reader.File {
		if file.Name != name {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(io.LimitReader(rc, maxPartBytes))
	}
	return nil, fmt.Errorf("officetext: part %s not found", name)
}

// docx walks word/document.xml: paragraphs become lines, table cells are
// separated by " | " and rows by line breaks.
func docx(reader *zip.Reader) (string, error) {
	raw, err := readPart(reader, "word/document.xml")
	if err != nil {
		return "", err
	}
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	var sb strings.Builder
	var paragraph strings.Builder
	inText := false
	cellDepth := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("officetext: parse document: %w", err)
		}
		switch t := token.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				inText = true
			case "tab":
				paragraph.WriteByte('\t')
			case "br", "cr":
				paragraph.WriteByte('\n')
			case "tc":
				cellDepth++
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				line := strings.TrimRight(paragraph.String(), " \t")
				paragraph.Reset()
				if cellDepth > 0 {
					if line != "" {
						sb.WriteString(line)
						sb.WriteByte(' ')
					}
					continue
				}
				sb.WriteString(line)
				sb.WriteByte('\n')
			case "tc":
				cellDepth--
				sb.WriteString("| ")
			case "tr":
				sb.WriteByte('\n')
			case "tbl":
				sb.WriteByte('\n')
			}
		case xml.CharData:
			if inText {
				paragraph.Write(t)
			}
		}
	}
	return sb.String(), nil
}

var slideNumber = regexp.MustCompile(`^ppt/slides/slide(\d+)\.xml$`)

// pptx reads every slide in order and prints its text runs, one paragraph
// per line, under a "Slide N" heading.
func pptx(reader *zip.Reader) (string, error) {
	type slide struct {
		n    int
		name string
	}
	var slides []slide
	for _, file := range reader.File {
		if match := slideNumber.FindStringSubmatch(file.Name); match != nil {
			n, _ := strconv.Atoi(match[1])
			slides = append(slides, slide{n: n, name: file.Name})
		}
	}
	if len(slides) == 0 {
		return "", errors.New("officetext: no slides found")
	}
	sort.Slice(slides, func(a, b int) bool { return slides[a].n < slides[b].n })
	var sb strings.Builder
	for _, item := range slides {
		raw, err := readPart(reader, item.name)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&sb, "## Slide %d\n", item.n)
		decoder := xml.NewDecoder(bytes.NewReader(raw))
		var line strings.Builder
		inText := false
		for {
			token, err := decoder.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", fmt.Errorf("officetext: parse slide: %w", err)
			}
			switch t := token.(type) {
			case xml.StartElement:
				if t.Name.Local == "t" {
					inText = true
				}
			case xml.EndElement:
				switch t.Name.Local {
				case "t":
					inText = false
				case "p":
					if text := strings.TrimSpace(line.String()); text != "" {
						sb.WriteString(text)
						sb.WriteByte('\n')
					}
					line.Reset()
				}
			case xml.CharData:
				if inText {
					line.Write(t)
				}
			}
		}
		sb.WriteByte('\n')
	}
	return sb.String(), nil
}

var sheetNumber = regexp.MustCompile(`^xl/worksheets/sheet(\d+)\.xml$`)

// xlsx prints each worksheet as tab-separated rows, resolving shared strings.
func xlsx(reader *zip.Reader) (string, error) {
	var shared []string
	if raw, err := readPart(reader, "xl/sharedStrings.xml"); err == nil {
		shared = sharedStrings(raw)
	}
	type sheet struct {
		n    int
		name string
	}
	var sheets []sheet
	for _, file := range reader.File {
		if match := sheetNumber.FindStringSubmatch(file.Name); match != nil {
			n, _ := strconv.Atoi(match[1])
			sheets = append(sheets, sheet{n: n, name: file.Name})
		}
	}
	if len(sheets) == 0 {
		return "", errors.New("officetext: no worksheets found")
	}
	sort.Slice(sheets, func(a, b int) bool { return sheets[a].n < sheets[b].n })
	var sb strings.Builder
	for _, item := range sheets {
		raw, err := readPart(reader, item.name)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&sb, "## Sheet %d\n", item.n)
		decoder := xml.NewDecoder(bytes.NewReader(raw))
		var row []string
		cellType := ""
		inValue, inInline := false, false
		var value strings.Builder
		for {
			token, err := decoder.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", fmt.Errorf("officetext: parse sheet: %w", err)
			}
			switch t := token.(type) {
			case xml.StartElement:
				switch t.Name.Local {
				case "c":
					cellType = ""
					for _, attr := range t.Attr {
						if attr.Name.Local == "t" {
							cellType = attr.Value
						}
					}
					value.Reset()
				case "v":
					inValue = true
				case "t":
					inInline = cellType == "inlineStr"
				}
			case xml.EndElement:
				switch t.Name.Local {
				case "v":
					inValue = false
				case "t":
					inInline = false
				case "c":
					text := value.String()
					if cellType == "s" {
						if index, err := strconv.Atoi(strings.TrimSpace(text)); err == nil && index >= 0 && index < len(shared) {
							text = shared[index]
						}
					}
					row = append(row, strings.ReplaceAll(text, "\n", " "))
				case "row":
					if strings.TrimSpace(strings.Join(row, "")) != "" {
						sb.WriteString(strings.Join(row, "\t"))
						sb.WriteByte('\n')
					}
					row = row[:0]
				}
			case xml.CharData:
				if inValue || inInline {
					value.Write(t)
				}
			}
		}
		sb.WriteByte('\n')
	}
	return sb.String(), nil
}

func sharedStrings(raw []byte) []string {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	var out []string
	var current strings.Builder
	inItem, inText := false, false
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := token.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "si":
				inItem = true
				current.Reset()
			case "t":
				inText = inItem
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "si":
				inItem = false
				out = append(out, current.String())
			case "t":
				inText = false
			}
		case xml.CharData:
			if inText {
				current.Write(t)
			}
		}
	}
	return out
}

var blankLines = regexp.MustCompile(`\n{3,}`)

func collapseBlankLines(text string) string {
	return blankLines.ReplaceAllString(text, "\n\n")
}
