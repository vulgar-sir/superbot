package httpapi

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	writer := zip.NewWriter(file)
	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create entry: %v", err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("write entry: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}
}

func TestDocxToHTML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.docx")
	document := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p><w:r><w:t>第一段 &lt;script&gt;alert(1)&lt;/script&gt;</w:t></w:r></w:p>
    <w:p><w:r><w:t>第二段</w:t></w:r></w:p>
    <w:tbl>
      <w:tr><w:tc><w:p><w:r><w:t>姓名</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>分数</w:t></w:r></w:p></w:tc></w:tr>
      <w:tr><w:tc><w:p><w:r><w:t>张三</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>90</w:t></w:r></w:p></w:tc></w:tr>
    </w:tbl>
  </w:body>
</w:document>`
	writeZip(t, path, map[string]string{"word/document.xml": document})
	output, err := docxToHTML(path)
	if err != nil {
		t.Fatalf("docxToHTML: %v", err)
	}
	for _, want := range []string{"第一段", "第二段", "<table>", "张三", "90"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q: %s", want, output)
		}
	}
	if strings.Contains(output, "<script>") {
		t.Fatalf("script tag escaped incorrectly: %s", output)
	}
	if !strings.Contains(output, "&lt;script&gt;") {
		t.Fatalf("expected escaped script text: %s", output)
	}
}

func TestXlsxToHTML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.xlsx")
	shared := `<?xml version="1.0" encoding="UTF-8"?>
<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="2" uniqueCount="2">
  <si><t>城市</t></si>
  <si><t>人口</t></si>
</sst>`
	sheet := `<?xml version="1.0" encoding="UTF-8"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <sheetData>
    <row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c></row>
    <row r="2"><c r="A2" t="inlineStr"><is><t>北京</t></is></c><c r="B2"><v>2189</v></c></row>
  </sheetData>
</worksheet>`
	writeZip(t, path, map[string]string{
		"xl/sharedStrings.xml":     shared,
		"xl/worksheets/sheet1.xml": sheet,
	})
	output, err := xlsxToHTML(path)
	if err != nil {
		t.Fatalf("xlsxToHTML: %v", err)
	}
	for _, want := range []string{"城市", "人口", "北京", "2189", "<table>"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q: %s", want, output)
		}
	}
}

func TestPptxToHTML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.pptx")
	slide := func(title, body string) string {
		return `<?xml version="1.0" encoding="UTF-8"?>
<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">
  <p:cSld><p:spTree>
    <p:sp><p:txBody>
      <a:p><a:r><a:t>` + title + `</a:t></a:r></a:p>
      <a:p><a:r><a:t>` + body + `</a:t></a:r></a:p>
    </p:txBody></p:sp>
  </p:spTree></p:cSld>
</p:sld>`
	}
	writeZip(t, path, map[string]string{
		"ppt/slides/slide1.xml": slide("季度总结", "收入增长 &lt;18%&gt;"),
		"ppt/slides/slide2.xml": slide("下一步", "扩展市场"),
	})
	output, err := pptxToHTML(path)
	if err != nil {
		t.Fatalf("pptxToHTML: %v", err)
	}
	for _, want := range []string{"第 1 页", "季度总结", "扩展市场", "&lt;18%&gt;"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q: %s", want, output)
		}
	}
	if strings.Contains(output, "<18%>") {
		t.Fatalf("text was not escaped: %s", output)
	}
}
