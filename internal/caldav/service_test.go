package caldav

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestListAndCreate(t *testing.T) {
	var putBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "user" || pass != "pass" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == "PROPFIND" && r.URL.Path == "/dav/cal/":
			w.WriteHeader(http.StatusMultiStatus)
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<d:multistatus xmlns:d="DAV:">
  <d:response><d:href>/dav/cal/</d:href><d:propstat><d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>
  <d:response><d:href>/dav/cal/one.ics</d:href><d:propstat><d:prop><d:getetag>"1"</d:getetag></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>
</d:multistatus>`)
		case r.Method == http.MethodGet && r.URL.Path == "/dav/cal/one.ics":
			_, _ = io.WriteString(w, "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:one\r\nSUMMARY:Standup\r\nDTSTART:20261112T100000Z\r\nDTEND:20261112T101500Z\r\nLOCATION:Room 1\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
		case r.Method == http.MethodPut && r.URL.Path == "/dav/cal/new.ics":
			data, _ := io.ReadAll(r.Body)
			putBody = string(data)
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	service := NewService()
	events, err := service.List(context.Background(), Config{URL: server.URL + "/dav/cal/", Username: "user"}, "pass")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(events) != 1 || events[0].Title != "Standup" || events[0].Location != "Room 1" {
		t.Fatalf("unexpected events: %#v", events)
	}
	target, err := service.Create(context.Background(), Config{URL: server.URL + "/dav/cal/", Username: "user"}, "pass", NewEvent{
		UID: "new", Title: "Review", StartsAt: time.Date(2026, 11, 12, 11, 0, 0, 0, time.UTC), EndsAt: time.Date(2026, 11, 12, 11, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.HasSuffix(target, "/new.ics") || !strings.Contains(putBody, "SUMMARY:Review") {
		t.Fatalf("unexpected create target/body: %s %q", target, putBody)
	}
}

func TestListHandlesTrailingSlashHref(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "PROPFIND":
			w.WriteHeader(http.StatusMultiStatus)
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<d:multistatus xmlns:d="DAV:">
  <d:response><d:href>/dav/cal/one.ics/</d:href><d:propstat><d:prop><d:getetag>"1"</d:getetag></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>
</d:multistatus>`)
		case r.Method == http.MethodGet && r.URL.Path == "/dav/cal/one.ics":
			_, _ = io.WriteString(w, "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:one\r\nSUMMARY:Trailing\r\nDTSTART:20261112T100000Z\r\nDTEND:20261112T101500Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	service := NewService()
	events, err := service.List(context.Background(), Config{URL: server.URL + "/dav/cal/", Username: "user"}, "pass")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(events) != 1 || events[0].Title != "Trailing" {
		t.Fatalf("unexpected events: %#v", events)
	}
}
