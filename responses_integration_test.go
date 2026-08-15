package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResponsesEndpointTranslatesForCompatibleProvider(t *testing.T) {
	var upstreamPath string
	var upstreamBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamPath = r.URL.Path
		upstreamBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl_1","model":"real-model","choices":[{"index":0,"message":{"role":"assistant","content":"fixed"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`)
	}))
	defer upstream.Close()

	app := testApp(t, Config{
		PublicAPIKeys: []string{"client-key"},
		Providers:     []Provider{{ID: "p", Name: "Provider", Type: "openai", BaseURL: upstream.URL + "/v1", Enabled: true}},
		Models:        []ModelMapping{{PublicName: "Coding", ProviderID: "p", UpstreamName: "real-model", Enabled: true, Order: 1}},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"Coding","instructions":"Be concise","input":"fix it"}`))
	req.Header.Set("Authorization", "Bearer client-key")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if upstreamPath != "/v1/chat/completions" {
		t.Fatalf("upstream path = %q", upstreamPath)
	}
	var sent map[string]any
	if err := json.Unmarshal(upstreamBody, &sent); err != nil {
		t.Fatal(err)
	}
	if sent["model"] != "real-model" {
		t.Fatalf("upstream model = %#v", sent["model"])
	}
	messages := sent["messages"].([]any)
	if got := messages[0].(map[string]any)["role"]; got != "system" {
		t.Fatalf("instruction role after provider normalization = %#v", got)
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["object"] != "response" || out["status"] != "completed" {
		t.Fatalf("response = %#v", out)
	}
	items := out["output"].([]any)
	content := items[0].(map[string]any)["content"].([]any)[0].(map[string]any)
	if content["text"] != "fixed" {
		t.Fatalf("output = %#v", items)
	}
}

func TestChatSSETranslatesTextAndToolCalls(t *testing.T) {
	input := strings.Join([]string{
		`data: {"model":"real-model","choices":[{"delta":{"content":"Hel"}}]}`,
		"",
		`data: {"model":"real-model","choices":[{"delta":{"content":"lo"}}]}`,
		"",
		`data: {"model":"real-model","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\\\"pa"}}]}}]}`,
		"",
		`data: {"model":"real-model","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"th\\\":\\\"x.go\\\"}"}}]}}]}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	var out strings.Builder
	translateChatSSEToResponses(strings.NewReader(input), &out)
	body := out.String()
	for _, want := range []string{"event: response.created", "response.output_text.delta", "Hel", "lo", "response.function_call_arguments.delta", "read_file", "call_1", "response.completed"} {
		if !strings.Contains(body, want) {
			t.Fatalf("stream missing %q:\n%s", want, body)
		}
	}
}

func TestChatSSEPreservesOutputIndexAndSequence(t *testing.T) {
	input := strings.Join([]string{
		`data: {"model":"m","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"tool_a","arguments":"a"}}]}}]}`,
		"",
		`data: {"model":"m","choices":[{"delta":{"content":"text"}}]}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")
	var out strings.Builder
	translateChatSSEToResponses(strings.NewReader(input), &out)
	body := out.String()

	lines := strings.Split(body, "\n")
	var events []map[string]any
	for i := 0; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(lines[i], "data:"))
			var event map[string]any
			if err := json.Unmarshal([]byte(data), &event); err == nil {
				events = append(events, event)
			}
		}
	}

	seenIndices := make(map[int]bool)
	lastSequence := -1
	for _, event := range events {
		seq, ok := event["sequence_number"].(float64)
		if !ok {
			t.Fatalf("missing sequence_number in event: %#v", event)
		}
		if int(seq) <= lastSequence {
			t.Fatalf("sequence not increasing: %d after %d", int(seq), lastSequence)
		}
		lastSequence = int(seq)

		if idx, ok := event["output_index"].(float64); ok {
			seenIndices[int(idx)] = true
		}
	}

	if len(seenIndices) < 2 {
		t.Fatalf("expected at least 2 distinct output_index values, got: %v", seenIndices)
	}
}

func TestResponsesEndToEndWithTranslation(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl_x","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
	}))
	defer upstream.Close()

	req := httptest.NewRequest(http.MethodPost, upstream.URL+"/v1/responses", strings.NewReader(`{"model":"m","input":"test"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := roundTripResponsesCompat(http.DefaultTransport, req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	if result["object"] != "response" {
		t.Fatalf("object = %v", result["object"])
	}
}
