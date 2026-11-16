package caldav

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"
)

type Config struct {
	URL      string `json:"url"`
	Username string `json:"username"`
}

type Event struct {
	Href        string    `json:"href"`
	UID         string    `json:"uid"`
	Title       string    `json:"title"`
	StartsAt    time.Time `json:"starts_at"`
	EndsAt      time.Time `json:"ends_at"`
	Description string    `json:"description"`
	Location    string    `json:"location"`
}

type Service struct {
	client *http.Client
}

func NewService() *Service {
	return &Service{client: &http.Client{Timeout: 20 * time.Second}}
}

func (s *Service) List(ctx context.Context, cfg Config, password string) ([]Event, error) {
	if strings.TrimSpace(cfg.URL) == "" {
		return nil, fmt.Errorf("CalDAV url is required")
	}
	body := `<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:"><d:prop><d:resourcetype/><d:getetag/></d:prop></d:propfind>`
	req, err := http.NewRequestWithContext(ctx, "PROPFIND", cfg.URL, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(cfg.Username, password)
	req.Header.Set("Depth", "1")
	req.Header.Set("Content-Type", "application/xml")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMultiStatus {
		return nil, fmt.Errorf("CalDAV PROPFIND returned %d", resp.StatusCode)
	}
	var multi struct {
		Responses []struct {
			Href string `xml:"href"`
		} `xml:"response"`
	}
	if err := xml.NewDecoder(resp.Body).Decode(&multi); err != nil {
		return nil, fmt.Errorf("decode CalDAV response: %w", err)
	}
	events := make([]Event, 0)
	for _, item := range multi.Responses {
		href := strings.TrimSuffix(item.Href, "/")
		if !strings.HasSuffix(strings.ToLower(href), ".ics") {
			continue
		}
		event, err := s.getEvent(ctx, cfg, password, href)
		if err != nil || event == nil {
			continue
		}
		events = append(events, *event)
	}
	return events, nil
}

func (s *Service) getEvent(ctx context.Context, cfg Config, password, href string) (*Event, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resolveHref(cfg.URL, href), nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(cfg.Username, password)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("CalDAV GET returned %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	event := parseICS(string(data))
	event.Href = href
	return &event, nil
}

type NewEvent struct {
	UID         string
	Title       string
	Description string
	Location    string
	StartsAt    time.Time
	EndsAt      time.Time
}

func (s *Service) Create(ctx context.Context, cfg Config, password string, event NewEvent) (string, error) {
	if event.UID == "" {
		return "", fmt.Errorf("event UID is required")
	}
	ics := formatICS(event)
	target := strings.TrimRight(cfg.URL, "/") + "/" + event.UID + ".ics"
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, target, strings.NewReader(ics))
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(cfg.Username, password)
	req.Header.Set("Content-Type", "text/calendar; charset=utf-8")
	req.Header.Set("If-None-Match", "*")
	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		return "", fmt.Errorf("CalDAV PUT returned %d", resp.StatusCode)
	}
	return target, nil
}

func resolveHref(base, href string) string {
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href
	}
	parsed, err := http.NewRequest(http.MethodGet, base, nil)
	if err != nil {
		return base
	}
	u := parsed.URL
	u.Path = href
	u.RawQuery = ""
	return u.String()
}

func parseICS(data string) Event {
	event := Event{}
	lines := strings.Split(strings.ReplaceAll(data, "\r\n", "\n"), "\n")
	for _, line := range lines {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.ToUpper(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		switch {
		case key == "UID":
			event.UID = value
		case key == "SUMMARY":
			event.Title = value
		case key == "DESCRIPTION":
			event.Description = value
		case key == "LOCATION":
			event.Location = value
		case strings.HasPrefix(key, "DTSTART"):
			event.StartsAt = parseICSDate(value)
		case strings.HasPrefix(key, "DTEND"):
			event.EndsAt = parseICSDate(value)
		}
	}
	return event
}

func parseICSDate(value string) time.Time {
	for _, layout := range []string{"20060102T150405Z", "20060102T150405", "20060102T1504Z", "20060102T1504"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func formatICS(event NewEvent) string {
	return fmt.Sprintf(
		"BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//superbot//caldav//EN\r\nBEGIN:VEVENT\r\nUID:%s\r\nDTSTAMP:%s\r\nDTSTART:%s\r\nDTEND:%s\r\nSUMMARY:%s\r\nDESCRIPTION:%s\r\nLOCATION:%s\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n",
		event.UID, time.Now().UTC().Format("20060102T150405Z"),
		event.StartsAt.UTC().Format("20060102T150405Z"), event.EndsAt.UTC().Format("20060102T150405Z"),
		escape(event.Title), escape(event.Description), escape(event.Location),
	)
}

func escape(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, ";", "\\;")
	value = strings.ReplaceAll(value, ",", "\\,")
	value = strings.ReplaceAll(value, "\n", "\\n")
	return value
}

func joinURL(base, name string) string {
	u, err := http.NewRequest(http.MethodGet, base, nil)
	if err != nil {
		return base
	}
	u.URL.Path = path.Join(u.URL.Path, name)
	return u.URL.String()
}
