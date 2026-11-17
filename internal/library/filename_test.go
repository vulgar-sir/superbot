package library

import (
	"encoding/json"
	"testing"

	"superbot/internal/store"
)

func TestFileName(t *testing.T) {
	cases := []struct {
		name string
		item store.LibraryItem
		want string
	}{
		{
			name: "uploaded file keeps its original name",
			item: store.LibraryItem{Title: "模板", FilePath: "/data/library/x.docx", Metadata: json.RawMessage(`{"original_name":"1.开题报告模板.docx"}`)},
			want: "1.开题报告模板.docx",
		},
		{
			name: "workspace file titled with its name",
			item: store.LibraryItem{Title: "报告.docx", FilePath: "/data/workspaces/b/报告.docx"},
			want: "报告.docx",
		},
		{
			name: "title without the extension gets it",
			item: store.LibraryItem{Title: "海报", FilePath: "/data/workspaces/b/abc.PNG"},
			want: "海报.PNG",
		},
		{
			name: "document without a file is markdown",
			item: store.LibraryItem{Title: "答辩要点", MimeType: "text/markdown"},
			want: "答辩要点.md",
		},
		{
			name: "calendar event",
			item: store.LibraryItem{Title: "组会", MimeType: "text/calendar"},
			want: "组会.ics",
		},
		{
			name: "untitled",
			item: store.LibraryItem{MimeType: "text/plain; charset=utf-8"},
			want: "file.txt",
		},
	}
	for _, tc := range cases {
		if got := FileName(tc.item); got != tc.want {
			t.Errorf("%s: FileName = %q, want %q", tc.name, got, tc.want)
		}
	}
}
