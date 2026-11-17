package memory

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestLocalEmbeddingByDefault(t *testing.T) {
	service := NewService(nil, "", "", "")
	if service.Configured() {
		t.Fatal("service without an API key must not be configured")
	}
	if got := service.ModelName(); got != localModel {
		t.Fatalf("ModelName() = %q, want %q", got, localModel)
	}
	literal, err := service.EmbedText(context.Background(), "项目进度与风险")
	if err != nil {
		t.Fatalf("EmbedText: %v", err)
	}
	if !strings.HasPrefix(literal, "[") || !strings.HasSuffix(literal, "]") {
		t.Fatalf("unexpected pgvector literal: %s", literal)
	}
	if count := strings.Count(literal, ",") + 1; count != dims {
		t.Fatalf("vector dims = %d, want %d", count, dims)
	}
}

func TestRemoteEmbedding(t *testing.T) {
	var sawAuth, sawModel, sawInput string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("path = %s", r.URL.Path)
		}
		sawAuth = r.Header.Get("Authorization")
		var payload embeddingRequest
		_ = json.NewDecoder(r.Body).Decode(&payload)
		sawModel = payload.Model
		sawInput, _ = payload.Input.(string)
		vector := make([]float32, dims)
		for i := range vector {
			vector[i] = float32(i%7) + 1
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"embedding": vector}},
		})
	}))
	defer server.Close()

	service := NewService(nil, "test-key", server.URL, "text-embedding-3-small")
	if !service.Configured() {
		t.Fatal("service with an API key must be configured")
	}
	if got := service.ModelName(); got != "remote:text-embedding-3-small" {
		t.Fatalf("ModelName() = %q", got)
	}
	literal, err := service.EmbedText(context.Background(), "季度目标")
	if err != nil {
		t.Fatalf("EmbedText: %v", err)
	}
	if sawAuth != "Bearer test-key" {
		t.Fatalf("Authorization = %q", sawAuth)
	}
	if sawModel != "text-embedding-3-small" || sawInput != "季度目标" {
		t.Fatalf("request model=%q input=%q", sawModel, sawInput)
	}
	values := parseLiteral(t, literal)
	if len(values) != dims {
		t.Fatalf("dims = %d", len(values))
	}
	var norm float64
	for _, value := range values {
		norm += value * value
	}
	if math.Abs(norm-1) > 1e-4 {
		t.Fatalf("vector is not normalized: norm=%f", norm)
	}
}

func TestRemoteEmbeddingDimensionMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"embedding": []float32{1, 2, 3}}},
		})
	}))
	defer server.Close()
	service := NewService(nil, "test-key", server.URL, "small")
	_, err := service.EmbedText(context.Background(), "x")
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected dimension mismatch error, got %v", err)
	}
}

func TestRemoteEmbeddingAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"bad key"}}`))
	}))
	defer server.Close()
	service := NewService(nil, "test-key", server.URL, "small")
	_, err := service.EmbedText(context.Background(), "x")
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected API error, got %v", err)
	}
}

func parseLiteral(t *testing.T, literal string) []float64 {
	t.Helper()
	trimmed := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(literal), "["), "]")
	parts := strings.Split(trimmed, ",")
	values := make([]float64, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil {
			t.Fatalf("parse %q: %v", part, err)
		}
		values = append(values, value)
	}
	return values
}
