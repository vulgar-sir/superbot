package officetext

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func archive(t *testing.T, parts map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for name, body := range parts {
		w, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractDocxKeepsParagraphsAndTables(t *testing.T) {
	data := archive(t, map[string]string{
		"word/document.xml": `<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
<w:p><w:r><w:t>本科毕业论文开题报告</w:t></w:r></w:p>
<w:p><w:r><w:t xml:space="preserve">一、选题</w:t></w:r><w:r><w:t>依据</w:t></w:r></w:p>
<w:tbl><w:tr><w:tc><w:p><w:r><w:t>姓名</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>学号</w:t></w:r></w:p></w:tc></w:tr></w:tbl>
<w:p><w:r><w:t>二、研究内容</w:t></w:r></w:p>
</w:body></w:document>`,
	})
	text, truncated, err := Extract("模板.docx", data, 0)
	if err != nil {
		t.Fatal(err)
	}
	if truncated {
		t.Fatal("unexpected truncation")
	}
	for _, want := range []string{"本科毕业论文开题报告\n", "一、选题依据\n", "姓名 | 学号 |", "二、研究内容"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
}

func TestExtractPptxOrdersSlides(t *testing.T) {
	slide := func(text string) string {
		return `<p:sld xmlns:p="p" xmlns:a="a"><p:cSld><p:spTree><p:sp><p:txBody><a:p><a:r><a:t>` + text + `</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`
	}
	data := archive(t, map[string]string{
		"ppt/slides/slide10.xml": slide("第十页"),
		"ppt/slides/slide2.xml":  slide("第二页"),
	})
	text, _, err := Extract("deck.pptx", data, 0)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(text, "第二页") > strings.Index(text, "第十页") {
		t.Fatalf("slides out of order:\n%s", text)
	}
}

func TestExtractXlsxResolvesSharedStrings(t *testing.T) {
	data := archive(t, map[string]string{
		"xl/sharedStrings.xml": `<sst><si><t>变量</t></si><si><r><t>复购</t></r><r><t>意愿</t></r></si></sst>`,
		"xl/worksheets/sheet1.xml": `<worksheet><sheetData>
<row><c t="s"><v>0</v></c><c><v>3.5</v></c></row>
<row><c t="s"><v>1</v></c><c t="inlineStr"><is><t>内联</t></is></c></row>
</sheetData></worksheet>`,
	})
	text, _, err := Extract("data.xlsx", data, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"变量\t3.5", "复购意愿\t内联"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
}

func TestExtractLimitAndErrors(t *testing.T) {
	data := archive(t, map[string]string{
		"word/document.xml": `<w:document xmlns:w="w"><w:body><w:p><w:r><w:t>一二三四五六</w:t></w:r></w:p></w:body></w:document>`,
	})
	text, truncated, err := Extract("a.docx", data, 3)
	if err != nil || !truncated || text != "一二三" {
		t.Fatalf("got %q truncated=%v err=%v", text, truncated, err)
	}
	if _, _, err := Extract("a.docx", []byte("not a zip"), 0); err == nil {
		t.Fatal("expected an error for a non-zip file")
	}
	if _, _, err := Extract("a.pdf", data, 0); err != ErrUnsupported {
		t.Fatalf("expected ErrUnsupported, got %v", err)
	}
	if !Supported("X.DOCX") || Supported("x.doc") {
		t.Fatal("Supported gave the wrong answer")
	}
}
