//go:build integration

package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSSEDocumentCollaboration(t *testing.T) {
	baseURL := strings.TrimRight(os.Getenv("SUPERBOT_TEST_BASE_URL"), "/")
	email := os.Getenv("SUPERBOT_TEST_EMAIL")
	password := os.Getenv("SUPERBOT_TEST_PASSWORD")
	if baseURL == "" || email == "" || password == "" {
		t.Skip("SUPERBOT_TEST_BASE_URL/EMAIL/PASSWORD are required")
	}
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 30 * time.Second}
	login, _ := json.Marshal(map[string]string{"email": email, "password": password})
	resp, err := client.Post(baseURL+"/api/auth/login", "application/json", bytes.NewReader(login))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d", resp.StatusCode)
	}
	create, _ := json.Marshal(map[string]string{"title": "sse-integration", "content": "base", "kind": "document"})
	resp, err = client.Post(baseURL+"/api/documents", "application/json", bytes.NewReader(create))
	if err != nil {
		t.Fatalf("create document: %v", err)
	}
	var created struct {
		Document struct {
			ID string `json:"id"`
		} `json:"document"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated || created.Document.ID == "" {
		t.Fatalf("create status = %d", resp.StatusCode)
	}
	docID := created.Document.ID
	defer func() {
		req, _ := http.NewRequest(http.MethodDelete, baseURL+"/api/documents/"+docID, strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		if resp, err := client.Do(req); err == nil {
			resp.Body.Close()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/documents/"+docID+"/events?client_id=integration-test", nil)
	stream, err := client.Do(req)
	if err != nil {
		t.Fatalf("open SSE: %v", err)
	}
	defer stream.Body.Close()
	if stream.StatusCode != http.StatusOK {
		t.Fatalf("SSE status = %d", stream.StatusCode)
	}
	events := make(chan string, 8)
	go func() {
		scanner := bufio.NewScanner(stream.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				events <- strings.TrimPrefix(line, "data: ")
			}
		}
		close(events)
	}()

	time.Sleep(200 * time.Millisecond)
	update, _ := json.Marshal(map[string]any{
		"title": "sse-integration", "content": "remote-sse-ok", "kind": "document",
		"client_id": "integration-other", "base_revision": 1,
	})
	resp, err = client.Post(baseURL+"/api/documents/"+docID+"/collab", "application/json", bytes.NewReader(update))
	if err != nil {
		t.Fatalf("collab update: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("collab status = %d", resp.StatusCode)
	}
	deadline := time.After(5 * time.Second)
	for {
		select {
		case data, ok := <-events:
			if !ok {
				t.Fatal("SSE stream closed before update")
			}
			if strings.Contains(data, "remote-sse-ok") {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for SSE update")
		}
	}
}
