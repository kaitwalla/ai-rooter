package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
)

func init() {
	base := http.DefaultTransport
	if base == nil {
		base = &http.Transport{}
	}
	http.DefaultTransport = &providerCompatibilityTransport{base: base}
}

type providerCompatibilityTransport struct {
	base http.RoundTripper
}

func (t *providerCompatibilityTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodPost || req.Body == nil {
		return t.base.RoundTrip(req)
	}
	if isResponsesRequestPath(req.URL.Path) {
		return roundTripResponsesCompat(t.base, req)
	}
	if !isChatRequestPath(req.URL.Path) {
		return t.base.RoundTrip(req)
	}

	// Only apply normalization to known provider hosts
	hostname := req.URL.Hostname()
	if !isAllowedProviderHost(hostname) {
		return t.base.RoundTrip(req)
	}

	// Limit request body size to prevent memory exhaustion
	const maxBodySize = 32 << 20 // 32 MB
	limitedReader := io.LimitReader(req.Body, maxBodySize+1)
	original, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()

	if len(original) > maxBodySize {
		return nil, errors.New("provider compatibility: request body exceeds maximum size of 32MB")
	}

	normalized, err := normalizeProviderChatRequest(hostname, original)
	if err != nil {
		normalized = original
	}

	clone := req.Clone(req.Context())
	clone.Header = req.Header.Clone()
	clone.Body = io.NopCloser(bytes.NewReader(normalized))
	clone.ContentLength = int64(len(normalized))
	clone.Header.Set("Content-Length", strconv.Itoa(len(normalized)))
	return t.base.RoundTrip(clone)
}

func isChatRequestPath(path string) bool {
	path = strings.TrimRight(path, "/")
	return strings.HasSuffix(path, "/chat/completions") || strings.HasSuffix(path, "/api/chat")
}

func normalizeProviderChatRequest(host string, body []byte) ([]byte, error) {
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, err
	}

	// Check for trailing JSON data after the first decoded value
	var trailing struct{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, errors.New("request body contains trailing JSON data")
	}

	// Official OpenAI supports developer-role semantics natively. For the
	// compatible APIs Rooter targets, collapse system/developer instructions
	// into one ordered system message so model-specific chat templates receive
	// the conventional system/user/assistant/tool role set.
	if !isNativeOpenAIHost(host) {
		normalizeInstructionMessages(payload)
	}

	// Groq intentionally omits a few OpenAI request fields. Dropping optional
	// unsupported fields here avoids provider-specific 400s while keeping the
	// public Rooter API stable.
	if isGroqHost(host) {
		delete(payload, "logit_bias")
		delete(payload, "logprobs")
		delete(payload, "top_logprobs")
	}

	return json.Marshal(payload)
}

func normalizeInstructionMessages(payload map[string]any) {
	raw, ok := payload["messages"].([]any)
	if !ok || len(raw) == 0 {
		return
	}

	instructions := make([]string, 0, 2)
	conversation := make([]any, 0, len(raw))
	for _, item := range raw {
		message, ok := item.(map[string]any)
		if !ok {
			conversation = append(conversation, item)
			continue
		}
		role, _ := message["role"].(string)
		if role != "system" && role != "developer" {
			conversation = append(conversation, message)
			continue
		}
		if text := instructionContentText(message["content"]); text != "" {
			instructions = append(instructions, text)
		}
	}
	if len(instructions) == 0 {
		return
	}

	merged := map[string]any{
		"role":    "system",
		"content": strings.Join(instructions, "\n\n"),
	}
	payload["messages"] = append([]any{merged}, conversation...)
}

func instructionContentText(content any) string {
	switch value := content.(type) {
	case string:
		return value
	case []any:
		parts := make([]string, 0, len(value))
		for _, raw := range value {
			part, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			typ, _ := part["type"].(string)
			if typ != "text" && typ != "input_text" {
				continue
			}
			if text, ok := part["text"].(string); ok && text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

func isNativeOpenAIHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return host == "api.openai.com" || strings.HasSuffix(host, ".openai.azure.com")
}

func isGroqHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return host == "api.groq.com" || strings.HasSuffix(host, ".groq.com")
}

func isAllowedProviderHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))

	// OpenAI
	if host == "api.openai.com" || strings.HasSuffix(host, ".openai.azure.com") {
		return true
	}

	// Groq
	if host == "api.groq.com" || strings.HasSuffix(host, ".groq.com") {
		return true
	}

	// Anthropic
	if host == "api.anthropic.com" || strings.HasSuffix(host, ".anthropic.com") {
		return true
	}

	// Common OpenAI-compatible providers
	if host == "api.together.xyz" || strings.HasSuffix(host, ".together.xyz") {
		return true
	}
	if host == "api.fireworks.ai" || strings.HasSuffix(host, ".fireworks.ai") {
		return true
	}
	if host == "api.deepseek.com" || strings.HasSuffix(host, ".deepseek.com") {
		return true
	}
	if host == "api.mistral.ai" || strings.HasSuffix(host, ".mistral.ai") {
		return true
	}
	if host == "api.cohere.ai" || strings.HasSuffix(host, ".cohere.ai") {
		return true
	}
	if host == "api.replicate.com" || strings.HasSuffix(host, ".replicate.com") {
		return true
	}
	if host == "openrouter.ai" || strings.HasSuffix(host, ".openrouter.ai") {
		return true
	}

	// Localhost for development/testing
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}

	return false
}
