package main

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmbeddingGuardAllowsSameModelAcrossFallbacks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest("POST", "/v1/embeddings", nil).WithContext(ctx)
	if err := guardEmbeddingModel(req, "qwen3-embedding-4b"); err != nil {
		t.Fatal(err)
	}
	if err := guardEmbeddingModel(req, "qwen3-embedding-4b"); err != nil {
		t.Fatal(err)
	}
}

func TestEmbeddingGuardRejectsDifferentFallbackModel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest("POST", "/v1/embeddings", nil).WithContext(ctx)
	if err := guardEmbeddingModel(req, "qwen3-embedding-4b"); err != nil {
		t.Fatal(err)
	}
	err := guardEmbeddingModel(req, "embeddinggemma")
	if err == nil || !strings.Contains(err.Error(), "cannot change model") {
		t.Fatalf("err = %v", err)
	}
}

func TestEmbeddingGuardAllowsIndependentRequests(t *testing.T) {
	req1 := httptest.NewRequest("POST", "/v1/embeddings", nil)
	req2 := httptest.NewRequest("POST", "/v1/embeddings", nil)

	if err := guardEmbeddingModel(req1, "model-a"); err != nil {
		t.Fatalf("req1 first call: %v", err)
	}
	if err := guardEmbeddingModel(req2, "model-b"); err != nil {
		t.Fatalf("req2 first call: %v", err)
	}
	if err := guardEmbeddingModel(req1, "model-a"); err != nil {
		t.Fatalf("req1 second call: %v", err)
	}
	if err := guardEmbeddingModel(req2, "model-b"); err != nil {
		t.Fatalf("req2 second call: %v", err)
	}
}

func TestEmbeddingRequestPaths(t *testing.T) {
	if !isEmbeddingRequestPath("/v1/embeddings") || !isEmbeddingRequestPath("/api/embed") {
		t.Fatal("expected embedding paths to be recognized")
	}
	if isEmbeddingRequestPath("/v1/chat/completions") {
		t.Fatal("chat path recognized as embeddings")
	}
}
