package imagegen

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var ErrNotConfigured = errors.New("image generation is not configured")

type Service struct {
	apiKey  string
	baseURL string
	model   string
	client  *http.Client
}

type Image struct {
	URL      string `json:"url"`
	FileName string `json:"file_name"`
}

type Request struct {
	Prompt        string
	Size          string
	Quality       string
	Background    string
	ReferenceURLs []string
}

func New(apiKey, baseURL, model string) *Service {
	return &Service{
		apiKey: apiKey, baseURL: strings.TrimRight(baseURL, "/"), model: model,
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

func (s *Service) Configured() bool { return s.apiKey != "" }

func (s *Service) Generate(ctx context.Context, request Request) ([]Image, error) {
	if !s.Configured() {
		return nil, ErrNotConfigured
	}
	if strings.TrimSpace(request.Prompt) == "" {
		return nil, errors.New("prompt is required")
	}
	payload := map[string]any{
		"model":  s.model,
		"prompt": request.Prompt,
	}
	if request.Size != "" {
		payload["size"] = request.Size
	}
	if request.Quality != "" {
		payload["quality"] = request.Quality
	}
	if request.Background != "" {
		payload["background"] = request.Background
	}
	if len(request.ReferenceURLs) > 0 {
		payload["image"] = request.ReferenceURLs
	}
	body, _ := json.Marshal(payload)
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.baseURL+"/v1/images/generations?async=true", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Authorization", s.apiKey)
	response, err := s.client.Do(httpRequest)
	if err != nil {
		return nil, err
	}
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("image API %s: %s", response.Status, strings.TrimSpace(string(raw)))
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &created); err != nil || created.ID == "" {
		return nil, errors.New("image API returned no task id")
	}
	return s.wait(ctx, created.ID)
}

func (s *Service) wait(ctx context.Context, taskID string) ([]Image, error) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	deadline := time.NewTimer(5 * time.Minute)
	defer deadline.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			return nil, errors.New("image generation timed out")
		case <-ticker.C:
			request, err := http.NewRequestWithContext(ctx, http.MethodGet,
				s.baseURL+"/v1/tasks/"+taskID, nil)
			if err != nil {
				return nil, err
			}
			request.Header.Set("Authorization", s.apiKey)
			response, err := s.client.Do(request)
			if err != nil {
				return nil, err
			}
			raw, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
			response.Body.Close()
			if response.StatusCode < 200 || response.StatusCode >= 300 {
				return nil, fmt.Errorf("image task %s: %s", response.Status, strings.TrimSpace(string(raw)))
			}
			var task struct {
				State string `json:"state"`
				Data  struct {
					Images []Image `json:"images"`
				} `json:"data"`
			}
			if err := json.Unmarshal(raw, &task); err != nil {
				return nil, err
			}
			switch task.State {
			case "succeeded":
				if len(task.Data.Images) == 0 {
					return nil, errors.New("image task succeeded without images")
				}
				return task.Data.Images, nil
			case "error":
				return nil, fmt.Errorf("image generation failed: %s", strings.TrimSpace(string(raw)))
			}
		}
	}
}
