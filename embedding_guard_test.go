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

func TestEmbeddingRequestPaths(t *testing.T) {
	if !isEmbeddingRequestPath("/v1/embeddings") || !isEmbeddingRequestPath("/api/embed") {
		t.Fatal("expected embedding paths to be recognized")
	}
	if isEmbeddingRequestPath("/v1/chat/completions") {
		t.Fatal("chat path recognized as embeddings")
	}
}
