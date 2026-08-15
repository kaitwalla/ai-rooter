package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStreamOllamaChatAsOpenAIPreservesToolCallsAndReasoning(t *testing.T) {
	input := strings.Join([]string{
		`{"model":"qwen","created_at":"2026-08-15T04:00:00Z","message":{"role":"assistant","content":"","thinking":"need a lookup","tool_calls":[{"function":{"index":0,"name":"lookup","arguments":{"query":"rooter"}}}]},"done":false}`,
		`{"model":"qwen","created_at":"2026-08-15T04:00:01Z","message":{"role":"assistant","content":""},"done":true,"done_reason":"stop"}`,
	}, "\n")

	rec := httptest.NewRecorder()
	streamOllamaChatAsOpenAI(rec, strings.NewReader(input), "public")

	events := sseEvents(t, rec.Body.String())
	if len(events) != 2 {
		t.Fatalf("events = %d want 2", len(events))
	}

	firstChoice := events[0]["choices"].([]any)[0].(map[string]any)
	delta := firstChoice["delta"].(map[string]any)
	if delta["reasoning"] != "need a lookup" {
		t.Fatalf("reasoning = %#v", delta["reasoning"])
	}
	calls := delta["tool_calls"].([]any)
	call := calls[0].(map[string]any)
	if call["id"] == "" {
		t.Fatal("tool call id is empty")
	}
	if call["type"] != "function" {
		t.Fatalf("type = %#v", call["type"])
	}
	if call["index"].(float64) != 0 {
		t.Fatalf("index = %#v", call["index"])
	}
	fn := call["function"].(map[string]any)
	if fn["name"] != "lookup" {
		t.Fatalf("name = %#v", fn["name"])
	}
	if fn["arguments"] != `{"query":"rooter"}` {
		t.Fatalf("arguments = %#v", fn["arguments"])
	}

	finalChoice := events[1]["choices"].([]any)[0].(map[string]any)
	if finalChoice["finish_reason"] != "tool_calls" {
		t.Fatalf("finish_reason = %#v", finalChoice["finish_reason"])
	}
}

func TestCopyOllamaMessagesNormalizesOpenAIToolHistory(t *testing.T) {
	raw := `{"messages":[{"role":"assistant","content":"","reasoning":"thinking","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"query\":\"rooter\"}"}}]},{"role":"tool","tool_call_id":"call_1","content":"ok"}]}`
	var src map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &src); err != nil {
		t.Fatal(err)
	}
	dst := map[string]any{}
	copyOllamaMessages(dst, src)
	messages := dst["messages"].([]map[string]any)
	assistant := messages[0]
	if assistant["thinking"] != "thinking" {
		t.Fatalf("thinking = %#v", assistant["thinking"])
	}
	if _, exists := assistant["reasoning"]; exists {
		t.Fatal("reasoning was not removed")
	}
	calls := assistant["tool_calls"].([]any)
	call := calls[0].(map[string]any)
	if _, exists := call["type"]; exists {
		t.Fatal("OpenAI tool type leaked to Ollama")
	}
	fn := call["function"].(map[string]any)
	args := fn["arguments"].(map[string]any)
	if args["query"] != "rooter" {
		t.Fatalf("arguments = %#v", args)
	}
	tool := messages[1]
	if tool["tool_name"] != "lookup" {
		t.Fatalf("tool_name = %#v", tool["tool_name"])
	}
}

func sseEvents(t *testing.T, body string) []map[string]any {
	t.Helper()
	var events []map[string]any
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "data: ") || line == "data: [DONE]" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	return events
}
