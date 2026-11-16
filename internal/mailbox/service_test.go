package mailbox

import "testing"

func TestGroupThreadsFollowsReferences(t *testing.T) {
	messages := []Message{
		{MessageID: "<a>", Subject: "Root"},
		{MessageID: "<b>", Subject: "Re: Root", InReplyTo: []string{"<a>"}},
		{MessageID: "<c>", Subject: "Re: Root", InReplyTo: []string{"<b>"}},
		{MessageID: "<d>", Subject: "Other", InReplyTo: []string{"<missing>"}},
		{Subject: "Re: No IDs"},
	}
	assignThreads(messages)
	threads := GroupThreads(messages)
	if len(threads) != 3 {
		t.Fatalf("threads = %d, want 3: %#v", len(threads), threads)
	}
	if len(threads[0].Messages) != 3 || threads[0].ID != "<a>" {
		t.Fatalf("first thread = %#v", threads[0])
	}
	if threads[1].ID != "<missing>" {
		t.Fatalf("second thread = %#v", threads[1])
	}
	if threads[2].ID != "subject:no ids" {
		t.Fatalf("third thread = %#v", threads[2])
	}
}

func TestParseMessageBodyAndAttachment(t *testing.T) {
	raw := "From: a@example.com\r\nTo: b@example.com\r\nSubject: Files\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=xyz\r\n\r\n" +
		"--xyz\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nhello body\r\n" +
		"--xyz\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=file.pdf\r\n\r\nPDFDATA\r\n" +
		"--xyz--\r\n"
	body, attachments := parseMessage([]byte(raw))
	if body != "hello body" {
		t.Fatalf("body = %q", body)
	}
	if len(attachments) != 1 || attachments[0].Filename != "file.pdf" || attachments[0].Size != 7 {
		t.Fatalf("attachments = %#v", attachments)
	}
}
