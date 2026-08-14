package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeProviderChatRequestCollapsesDeveloperInstructions(t *testing.T) {
	body := []byte(`{"model":"qwen","messages":[{"role":"system","content":"system one"},{"role":"developer","content":"developer two"},{"role":"user","content":"hello"}]}`)
	normalized, err := normalizeProviderChatRequest("api.deepseek.com", body)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(normalized, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Messages) != 2 {
		t.Fatalf("messages = %d want 2: %s", len(payload.Messages), normalized)
	}
	if payload.Messages[0].Role != "system" {
		t.Fatalf("first role = %q", payload.Messages[0].Role)
	}
	if payload.Messages[0].Content != "system one\n\ndeveloper two" {
		t.Fatalf("system content = %q", payload.Messages[0].Content)
	}
	if payload.Messages[1].Role != "user" || payload.Messages[1].Content != "hello" {
		t.Fatalf("user message changed: %#v", payload.Messages[1])
	}
}

func TestNormalizeProviderChatRequestPreservesOpenAIDeveloperRole(t *testing.T) {
	body := []byte(`{"messages":[{"role":"developer","content":"keep me"},{"role":"user","content":"hello"}]}`)
	normalized, err := normalizeProviderChatRequest("api.openai.com", body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(normalized), `"role":"developer"`) {
		t.Fatalf("developer role was normalized for native OpenAI: %s", normalized)
	}
}

func TestNormalizeProviderChatRequestHandlesTypedInstructionContent(t *testing.T) {
	body := []byte(`{"messages":[{"role":"developer","content":[{"type":"text","text":"first"},{"type":"input_text","text":"second"}]},{"role":"user","content":"hello"}]}`)
	normalized, err := normalizeProviderChatRequest("localhost", body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(normalized), `"content":"first\nsecond"`) {
		t.Fatalf("typed instruction content was not preserved: %s", normalized)
	}
}

func TestNormalizeProviderChatRequestDropsUnsupportedGroqFields(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"hi"}],"logit_bias":{"1":1},"logprobs":true,"top_logprobs":3,"temperature":0.2}`)
	normalized, err := normalizeProviderChatRequest("api.groq.com", body)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(normalized, &payload); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"logit_bias", "logprobs", "top_logprobs"} {
		if _, ok := payload[key]; ok {
			t.Fatalf("Groq field %q was retained: %s", key, normalized)
		}
	}
	if _, ok := payload["temperature"]; !ok {
		t.Fatalf("supported field was removed: %s", normalized)
	}
}

func TestChatRequestPathRecognition(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/chat/completions", "/api/chat", "/api/chat/"} {
		if !isChatRequestPath(path) {
			t.Fatalf("expected chat path %q", path)
		}
	}
	if isChatRequestPath("/v1/embeddings") {
		t.Fatal("embeddings should not be normalized as chat")
	}
}
