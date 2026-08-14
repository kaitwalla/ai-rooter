package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestResponsesRequestToChatTranslatesInstructionsAndTools(t *testing.T) {
	body := []byte(`{
		"model":"coding-real",
		"instructions":"Work carefully.",
		"input":"Fix the parser.",
		"tools":[{"type":"function","name":"read_file","description":"Read a file","parameters":{"type":"object"}}],
		"tool_choice":{"type":"function","name":"read_file"},
		"max_output_tokens":2048,
		"reasoning":{"effort":"high"}
	}`)
	out, stream, err := responsesRequestToChat(body)
	if err != nil {
		t.Fatal(err)
	}
	if stream {
		t.Fatal("stream should default false")
	}
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatal(err)
	}
	messages := payload["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("messages = %#v", messages)
	}
	if messages[0].(map[string]any)["role"] != "developer" || messages[1].(map[string]any)["role"] != "user" {
		t.Fatalf("roles = %#v", messages)
	}
	if payload["max_tokens"].(float64) != 2048 {
		t.Fatalf("max_tokens = %#v", payload["max_tokens"])
	}
	if payload["reasoning_effort"] != "high" {
		t.Fatalf("reasoning_effort = %#v", payload["reasoning_effort"])
	}
	tool := payload["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)
	if tool["name"] != "read_file" {
		t.Fatalf("tool = %#v", tool)
	}
}

func TestResponsesFunctionCallRoundTripInput(t *testing.T) {
	body := []byte(`{
		"model":"coding-real",
		"input":[
			{"type":"function_call","call_id":"call_123","name":"read_file","arguments":"{\"path\":\"main.go\"}"},
			{"type":"function_call_output","call_id":"call_123","output":"package main"}
		]
	}`)
	out, _, err := responsesRequestToChat(body)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatal(err)
	}
	messages := payload["messages"].([]any)
	assistant := messages[0].(map[string]any)
	call := assistant["tool_calls"].([]any)[0].(map[string]any)
	if call["id"] != "call_123" {
		t.Fatalf("call id = %#v", call["id"])
	}
	tool := messages[1].(map[string]any)
	if tool["tool_call_id"] != "call_123" || tool["content"] != "package main" {
		t.Fatalf("tool message = %#v", tool)
	}
}

func TestChatResponseAsResponsesPreservesTextToolsAndUsage(t *testing.T) {
	chat := map[string]any{
		"model": "coding-real",
		"choices": []any{map[string]any{"message": map[string]any{
			"content": "I need a file.",
			"tool_calls": []any{map[string]any{"id":"call_7","type":"function","function":map[string]any{"name":"read_file","arguments":"{\"path\":\"x.go\"}"}}},
		}}},
		"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5},
	}
	out := chatObjectToResponse(chat)
	if out["object"] != "response" || out["status"] != "completed" {
		t.Fatalf("response = %#v", out)
	}
	items := out["output"].([]any)
	if len(items) != 2 {
		t.Fatalf("output = %#v", items)
	}
	call := items[1].(map[string]any)
	if call["type"] != "function_call" || call["call_id"] != "call_7" || call["name"] != "read_file" {
		t.Fatalf("call = %#v", call)
	}
	usage := out["usage"].(map[string]any)
	if usage["input_tokens"].(int64) != 10 || usage["output_tokens"].(int64) != 5 {
		t.Fatalf("usage = %#v", usage)
	}
}

func TestResponsesUnsupportedStatefulFieldsFailClearly(t *testing.T) {
	_, _, err := responsesRequestToChat([]byte(`{"model":"x","previous_response_id":"resp_old","input":"hi"}`))
	if err == nil || !strings.Contains(err.Error(), "previous_response_id") {
		t.Fatalf("err = %v", err)
	}
}

func TestResponsesPathTranslation(t *testing.T) {
	if got := responsesPathToChat("/v1/responses"); got != "/v1/chat/completions" {
		t.Fatalf("path = %q", got)
	}
	if got := responsesPathToChat("/api/responses"); got != "/api/chat" {
		t.Fatalf("path = %q", got)
	}
}
