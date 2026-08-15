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
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("messages = %#v", messages)
	}
	msg0, ok0 := messages[0].(map[string]any)
	msg1, ok1 := messages[1].(map[string]any)
	if !ok0 || !ok1 || msg0["role"] != "developer" || msg1["role"] != "user" {
		t.Fatalf("roles = %#v", messages)
	}
	maxTokens, ok := payload["max_tokens"].(float64)
	if !ok || maxTokens != 2048 {
		t.Fatalf("max_tokens = %#v", payload["max_tokens"])
	}
	if payload["reasoning_effort"] != "high" {
		t.Fatalf("reasoning_effort = %#v", payload["reasoning_effort"])
	}
	tools, ok := payload["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Fatalf("tools = %#v", payload["tools"])
	}
	tool0, ok := tools[0].(map[string]any)
	if !ok {
		t.Fatalf("tool[0] = %#v", tools[0])
	}
	tool, ok := tool0["function"].(map[string]any)
	if !ok {
		t.Fatalf("tool function = %#v", tool0["function"])
	}
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
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) < 2 {
		t.Fatalf("messages = %#v", payload["messages"])
	}
	assistant, ok := messages[0].(map[string]any)
	if !ok {
		t.Fatalf("assistant = %#v", messages[0])
	}
	toolCalls, ok := assistant["tool_calls"].([]any)
	if !ok || len(toolCalls) == 0 {
		t.Fatalf("tool_calls = %#v", assistant["tool_calls"])
	}
	call, ok := toolCalls[0].(map[string]any)
	if !ok || call["id"] != "call_123" {
		t.Fatalf("call id = %#v", call)
	}
	tool, ok := messages[1].(map[string]any)
	if !ok || tool["tool_call_id"] != "call_123" || tool["content"] != "package main" {
		t.Fatalf("tool message = %#v", tool)
	}
}

func TestChatResponseAsResponsesPreservesTextToolsAndUsage(t *testing.T) {
	chat := map[string]any{
		"model": "coding-real",
		"choices": []any{map[string]any{"message": map[string]any{
			"content":    "I need a file.",
			"tool_calls": []any{map[string]any{"id": "call_7", "type": "function", "function": map[string]any{"name": "read_file", "arguments": "{\"path\":\"x.go\"}"}}},
		}}},
		"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5},
	}
	out := chatObjectToResponse(chat)
	if out["object"] != "response" || out["status"] != "completed" {
		t.Fatalf("response = %#v", out)
	}
	items, ok := out["output"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("output = %#v", out["output"])
	}
	call, ok := items[1].(map[string]any)
	if !ok || call["type"] != "function_call" || call["call_id"] != "call_7" || call["name"] != "read_file" {
		t.Fatalf("call = %#v", call)
	}
	usage, ok := out["usage"].(map[string]any)
	if !ok {
		t.Fatalf("usage = %#v", out["usage"])
	}
	inputTokens, ok1 := usage["input_tokens"].(int64)
	outputTokens, ok2 := usage["output_tokens"].(int64)
	if !ok1 || !ok2 || inputTokens != 10 || outputTokens != 5 {
		t.Fatalf("usage = %#v", usage)
	}
}

func TestResponsesUnsupportedStatefulFieldsFailClearly(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantErr string
	}{
		{
			name:    "previous_response_id",
			body:    `{"model":"x","previous_response_id":"resp_old","input":"hi"}`,
			wantErr: "previous_response_id",
		},
		{
			name:    "background true",
			body:    `{"model":"x","background":true,"input":"hi"}`,
			wantErr: "background",
		},
		{
			name:    "background false",
			body:    `{"model":"x","background":false,"input":"hi"}`,
			wantErr: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := responsesRequestToChat([]byte(tc.body))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("err = %v, want %q", err, tc.wantErr)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected err = %v", err)
				}
			}
		})
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
