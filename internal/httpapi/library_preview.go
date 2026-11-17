package httpapi

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"superbot/internal/store"
)

const maxPreviewCells = 5000

var previewTextExtensions = map[string]bool{
	".txt": true, ".csv": true, ".tsv": true, ".json": true, ".log": true, ".md": true,
	".py": true, ".go": true, ".js": true, ".ts": true, ".tsx": true, ".jsx": true,
	".sh": true, ".css": true, ".yaml": true, ".yml": true, ".sql": true, ".rs": true,
	".java": true, ".c": true, ".cpp": true, ".html": true, ".htm": true,
}

// handleLibraryPreview renders Word/Excel/text files as HTML so the Library can
// show them inline without a third-party viewer. Every piece of source text is
// HTML-escaped; the response never includes script.
func (s *Server) handleLibraryPreview(w http.ResponseWriter, r *http.Request) {
	itemID, ok := parseUUIDParam(w, r, "itemID")
	if !ok {
		return
	}
	item, err := s.db.GetLibraryItem(r.Context(), store.GetLibraryItemParams{
		ID: itemID, OwnerID: accountFrom(r),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "library item not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not load library item")
		return
	}
	if strings.TrimSpace(item.FilePath) == "" {
		writeError(w, http.StatusNotFound, "not_found", "library item has no file")
		return
	}
	path, found := s.libraryFilePath(item)
	if !found {
		writeError(w, http.StatusNotFound, "not_found", "library file not found")
		return
	}

	extension := strings.ToLower(filepath.Ext(path))
	var body string
	switch extension {
	case ".docx":
		body, err = docxToHTML(path)
	case ".xlsx":
		body, err = xlsxToHTML(path)
	case ".pptx":
		body, err = pptxToHTML(path)
	default:
		if !previewTextExtensions[extension] {
			writeError(w, http.StatusUnsupportedMediaType, "unsupported_preview", "no inline preview for this file type")
			return
		}
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			err = readErr
			break
		}
		if len(raw) > 1<<20 {
			raw = raw[:1<<20]
		}
		body = `<pre class="text">` + html.EscapeString(string(raw)) + `</pre>`
	}
	if err != nil {
		s.logger.Error("library preview failed", "item_id", item.ID, "error", err)
		writeError(w, http.StatusUnprocessableEntity, "preview_failed", "could not render preview")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	_, _ = io.WriteString(w, previewDocument(item.Title, body))
}

func previewDocument(title, body string) string {
	return `<!doctype html><html lang="zh"><head><meta charset="utf-8">` +
		`<meta name="viewport" content="width=device-width,initial-scale=1">` +
		`<style>
body{margin:0;padding:24px 28px;font:14px/1.65 -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;color:#1c1c1c;background:#fff}
h1{font-size:15px;font-weight:600;margin:0 0 16px;color:#6f737a}
p{margin:0 0 10px;white-space:pre-wrap}
table{border-collapse:collapse;margin:0 0 20px;font-size:13px}
td,th{border:1px solid #e4e4e4;padding:4px 8px;vertical-align:top;white-space:pre-wrap}
th{background:#f5f5f5;text-align:left}
.sheet{font-size:13px;font-weight:600;margin:18px 0 8px;color:#1c1c1c}
pre.text{white-space:pre-wrap;word-break:break-word;margin:0;font:12.5px/1.6 ui-monospace,SFMono-Regular,Menlo,monospace}
</style></head><body><h1>` + html.EscapeString(title) + `</h1>` + body + `</body></html>`
}

func docxToHTML(path string) (string, error) {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return "", err
	}
	defer archive.Close()
	var document *zip.File
	for _, file := range archive.File {
		if file.Name == "word/document.xml" {
			document = file
			break
		}
	}
	if document == nil {
		return "", errors.New("word/document.xml not found")
	}
	reader, err := document.Open()
	if err != nil {
		return "", err
	}
	defer reader.Close()

	var out strings.Builder
	decoder := xml.NewDecoder(reader)
	inParagraph := false
	inText := false
	var paragraph strings.Builder
	var table [][]string
	var row []string
	var cell strings.Builder
	inCell := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		switch element := token.(type) {
		case xml.StartElement:
			switch element.Name.Local {
			case "p":
				inParagraph = true
				paragraph.Reset()
			case "t":
				inText = true
			case "tbl":
				table = nil
			case "tr":
				row = nil
			case "tc":
				inCell = true
				cell.Reset()
			case "tab":
				if inParagraph {
					paragraph.WriteString("\t")
				}
			case "br", "cr":
				if inParagraph {
					paragraph.WriteString("\n")
				}
			}
		case xml.EndElement:
			switch element.Name.Local {
			case "t":
				inText = false
			case "p":
				if inParagraph {
					if inCell {
						if cell.Len() > 0 {
							cell.WriteString("\n")
						}
						cell.WriteString(paragraph.String())
					} else {
						text := strings.TrimSpace(paragraph.String())
						if text != "" {
							out.WriteString("<p>" + html.EscapeString(text) + "</p>\n")
						}
					}
					inParagraph = false
				}
			case "tc":
				if inCell {
					row = append(row, strings.TrimRight(cell.String(), "\n"))
					inCell = false
				}
			case "tr":
				if len(row) > 0 {
					table = append(table, row)
					row = nil
				}
			case "tbl":
				out.WriteString(renderTable(table))
				table = nil
			}
		case xml.CharData:
			if inText && inParagraph {
				paragraph.WriteString(string(element))
			}
		}
	}
	if out.Len() == 0 {
		return "<p>（空文档）</p>", nil
	}
	return out.String(), nil
}

func renderTable(rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString("<table>")
	for index, row := range rows {
		out.WriteString("<tr>")
		for _, value := range row {
			tag := "td"
			if index == 0 {
				tag = "th"
			}
			out.WriteString("<" + tag + ">" + html.EscapeString(value) + "</" + tag + ">")
		}
		out.WriteString("</tr>")
	}
	out.WriteString("</table>\n")
	return out.String()
}

func xlsxToHTML(path string) (string, error) {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return "", err
	}
	defer archive.Close()
	files := map[string]*zip.File{}
	for _, file := range archive.File {
		files[file.Name] = file
	}

	shared := make([]string, 0)
	if entry, ok := files["xl/sharedStrings.xml"]; ok {
		reader, err := entry.Open()
		if err != nil {
			return "", err
		}
		decoder := xml.NewDecoder(reader)
		var current strings.Builder
		inText := false
		for {
			token, err := decoder.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				reader.Close()
				return "", err
			}
			switch element := token.(type) {
			case xml.StartElement:
				if element.Name.Local == "si" {
					current.Reset()
				}
				if element.Name.Local == "t" {
					inText = true
				}
			case xml.EndElement:
				if element.Name.Local == "t" {
					inText = false
				}
				if element.Name.Local == "si" {
					shared = append(shared, current.String())
				}
			case xml.CharData:
				if inText {
					current.WriteString(string(element))
				}
			}
		}
		reader.Close()
	}

	sheetNames := map[string]string{}
	if entry, ok := files["xl/workbook.xml"]; ok {
		reader, err := entry.Open()
		if err == nil {
			decoder := xml.NewDecoder(reader)
			for {
				token, err := decoder.Token()
				if err != nil {
					break
				}
				if element, ok := token.(xml.StartElement); ok && element.Name.Local == "sheet" {
					name := ""
					id := ""
					for _, attribute := range element.Attr {
						switch attribute.Name.Local {
						case "name":
							name = attribute.Value
						case "sheetId":
							id = attribute.Value
						}
					}
					if id != "" {
						sheetNames[id] = name
					}
				}
			}
			reader.Close()
		}
	}

	sheetPaths := make([]string, 0)
	for name := range files {
		if strings.HasPrefix(name, "xl/worksheets/sheet") && strings.HasSuffix(name, ".xml") {
			sheetPaths = append(sheetPaths, name)
		}
	}
	if len(sheetPaths) == 0 {
		return "", errors.New("no worksheets found")
	}
	sortStringsByTrailingNumber(sheetPaths)

	var out strings.Builder
	cells := 0
	for index, sheetPath := range sheetPaths {
		rows, err := xlsxSheetRows(files[sheetPath], shared, &cells)
		if err != nil {
			return "", err
		}
		label := fmt.Sprintf("工作表 %d", index+1)
		if name := sheetNames[strconv.Itoa(index+1)]; name != "" {
			label = name
		}
		out.WriteString(`<div class="sheet">` + html.EscapeString(label) + `</div>`)
		out.WriteString(renderTable(rows))
		if cells >= maxPreviewCells {
			out.WriteString(`<p>（表格过大，仅显示前 5000 个单元格）</p>`)
			break
		}
	}
	if out.Len() == 0 {
		return "<p>（空工作簿）</p>", nil
	}
	return out.String(), nil
}

func xlsxSheetRows(entry *zip.File, shared []string, cells *int) ([][]string, error) {
	reader, err := entry.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	decoder := xml.NewDecoder(reader)
	var rows [][]string
	var row []string
	var value strings.Builder
	cellType := ""
	inValue := false
	column := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch element := token.(type) {
		case xml.StartElement:
			switch element.Name.Local {
			case "row":
				row = nil
				column = 0
			case "c":
				cellType = ""
				value.Reset()
				for _, attribute := range element.Attr {
					if attribute.Name.Local == "t" {
						cellType = attribute.Value
					}
					if attribute.Name.Local == "r" {
						column = columnFromCellRef(attribute.Value)
					}
				}
			case "v", "t":
				inValue = true
			}
		case xml.EndElement:
			switch element.Name.Local {
			case "v", "t":
				inValue = false
			case "c":
				text := value.String()
				if cellType == "s" {
					if index, err := strconv.Atoi(strings.TrimSpace(text)); err == nil && index >= 0 && index < len(shared) {
						text = shared[index]
					}
				}
				if *cells < maxPreviewCells {
					for len(row) < column {
						row = append(row, "")
					}
					row = append(row, text)
					*cells = *cells + 1
				}
			case "row":
				if len(row) > 0 {
					rows = append(rows, row)
				}
			}
		case xml.CharData:
			if inValue {
				value.WriteString(string(element))
			}
		}
	}
	if len(rows) == 0 {
		return [][]string{{"（空工作表）"}}, nil
	}
	return rows, nil
}

func columnFromCellRef(ref string) int {
	column := 0
	for _, r := range ref {
		if r < 'A' || r > 'Z' {
			break
		}
		column = column*26 + int(r-'A'+1)
	}
	if column == 0 {
		return 0
	}
	return column - 1
}

func sortStringsByTrailingNumber(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && trailingNumber(values[j]) < trailingNumber(values[j-1]); j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

func trailingNumber(value string) int {
	digits := ""
	for i := len(value) - 1; i >= 0; i-- {
		if value[i] < '0' || value[i] > '9' {
			break
		}
		digits = string(value[i]) + digits
	}
	number, _ := strconv.Atoi(digits)
	return number
}

// pptxToHTML renders slide text (titles and bullets) as sections. Shapes,
// images and layout are intentionally not reproduced.
func pptxToHTML(path string) (string, error) {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return "", err
	}
	defer archive.Close()
	slides := make([]*zip.File, 0)
	for _, file := range archive.File {
		if strings.HasPrefix(file.Name, "ppt/slides/slide") && strings.HasSuffix(file.Name, ".xml") {
			slides = append(slides, file)
		}
	}
	if len(slides) == 0 {
		return "", errors.New("no slides found")
	}
	names := make([]string, 0, len(slides))
	byName := map[string]*zip.File{}
	for _, file := range slides {
		names = append(names, file.Name)
		byName[file.Name] = file
	}
	sortStringsByTrailingNumber(names)
	var out strings.Builder
	for index, name := range names {
		paragraphs, err := pptxSlideParagraphs(byName[name])
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&out, `<div class="sheet">第 %d 页</div>`, index+1)
		if len(paragraphs) == 0 {
			out.WriteString("<p>（空白页）</p>")
			continue
		}
		for i, text := range paragraphs {
			if i == 0 {
				out.WriteString(`<p><strong>` + html.EscapeString(text) + `</strong></p>`)
			} else {
				out.WriteString("<p>• " + html.EscapeString(text) + "</p>")
			}
		}
	}
	return out.String(), nil
}

func pptxSlideParagraphs(entry *zip.File) ([]string, error) {
	reader, err := entry.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	decoder := xml.NewDecoder(reader)
	paragraphs := make([]string, 0, 8)
	var current strings.Builder
	inText := false
	inParagraph := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch element := token.(type) {
		case xml.StartElement:
			switch element.Name.Local {
			case "p":
				inParagraph = true
				current.Reset()
			case "t":
				inText = true
			}
		case xml.EndElement:
			switch element.Name.Local {
			case "t":
				inText = false
			case "p":
				if inParagraph {
					text := strings.TrimSpace(current.String())
					if text != "" {
						paragraphs = append(paragraphs, text)
					}
					inParagraph = false
				}
			}
		case xml.CharData:
			if inText && inParagraph {
				current.WriteString(string(element))
			}
		}
	}
	return paragraphs, nil
}
