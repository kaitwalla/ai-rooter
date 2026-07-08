package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func isOllamaProvider(provider Provider) bool {
	return provider.Type == "ollama" || provider.Type == "ollama_cloud"
}

func (a *App) proxyToOllama(w http.ResponseWriter, r *http.Request, provider Provider, publicModel string, body []byte) {
	switch r.URL.Path {
	case "/v1/chat/completions":
		a.handleOllamaChat(w, r, provider, publicModel, body)
	case "/v1/completions":
		a.handleOllamaCompletion(w, r, provider, publicModel, body)
	case "/v1/embeddings":
		a.handleOllamaEmbeddings(w, r, provider, publicModel, body)
	default:
		writeOpenAIError(w, http.StatusBadRequest, "unsupported_endpoint", "Ollama providers support /v1/chat/completions, /v1/completions, and /v1/embeddings")
	}
}

func (a *App) handleOllamaChat(w http.ResponseWriter, r *http.Request, provider Provider, publicModel string, body []byte) {
	var in map[string]json.RawMessage
	if err := json.Unmarshal(body, &in); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	reqBody := map[string]any{}
	copyJSONFields(reqBody, in, "model", "messages", "tools", "format", "keep_alive", "logprobs", "top_logprobs")
	copyOpenAIResponseFormat(reqBody, in)
	copyThinking(reqBody, in)
	copyOptions(reqBody, in)
	stream := rawBool(in["stream"], false)
	reqBody["stream"] = stream

	resp, err := a.callOllama(r, provider, "chat", reqBody)
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "upstream_error", err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		copyUpstreamError(w, resp)
		return
	}
	if stream {
		streamOllamaChatAsOpenAI(w, resp.Body, publicModel)
		return
	}
	var out ollamaChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "upstream_decode_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":      "chatcmpl-" + randomID(),
		"object":  "chat.completion",
		"created": createdUnix(out.CreatedAt),
		"model":   publicModel,
		"choices": []map[string]any{{
			"index": 0,
			"message": map[string]any{
				"role":       "assistant",
				"content":    out.Message.Content,
				"tool_calls": out.Message.ToolCalls,
			},
			"finish_reason": finishReason(out.DoneReason),
		}},
		"usage": usageObject(out.PromptEvalCount, out.EvalCount),
	})
}

func (a *App) handleOllamaCompletion(w http.ResponseWriter, r *http.Request, provider Provider, publicModel string, body []byte) {
	var in map[string]json.RawMessage
	if err := json.Unmarshal(body, &in); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	var prompt string
	if err := json.Unmarshal(in["prompt"], &prompt); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "Ollama completion prompts must be strings")
		return
	}
	reqBody := map[string]any{"model": mustRaw(in["model"]), "prompt": prompt}
	copyJSONFields(reqBody, in, "suffix", "images", "format", "system", "raw", "keep_alive", "logprobs", "top_logprobs")
	copyOpenAIResponseFormat(reqBody, in)
	copyThinking(reqBody, in)
	copyOptions(reqBody, in)
	stream := rawBool(in["stream"], false)
	reqBody["stream"] = stream

	resp, err := a.callOllama(r, provider, "generate", reqBody)
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "upstream_error", err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		copyUpstreamError(w, resp)
		return
	}
	if stream {
		streamOllamaCompletionAsOpenAI(w, resp.Body, publicModel)
		return
	}
	var out ollamaGenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "upstream_decode_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":      "cmpl-" + randomID(),
		"object":  "text_completion",
		"created": createdUnix(out.CreatedAt),
		"model":   publicModel,
		"choices": []map[string]any{{
			"text":          out.Response,
			"index":         0,
			"logprobs":      nil,
			"finish_reason": finishReason(out.DoneReason),
		}},
		"usage": usageObject(out.PromptEvalCount, out.EvalCount),
	})
}

func (a *App) handleOllamaEmbeddings(w http.ResponseWriter, r *http.Request, provider Provider, publicModel string, body []byte) {
	var in map[string]json.RawMessage
	if err := json.Unmarshal(body, &in); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	reqBody := map[string]any{}
	copyJSONFields(reqBody, in, "model", "input", "truncate", "dimensions", "keep_alive")
	copyOptions(reqBody, in)

	resp, err := a.callOllama(r, provider, "embed", reqBody)
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "upstream_error", err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		copyUpstreamError(w, resp)
		return
	}
	var out ollamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "upstream_decode_error", err.Error())
		return
	}
	data := make([]map[string]any, 0, len(out.Embeddings))
	for i, embedding := range out.Embeddings {
		data = append(data, map[string]any{
			"object":    "embedding",
			"embedding": embedding,
			"index":     i,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"model":  publicModel,
		"data":   data,
		"usage":  usageObject(out.PromptEvalCount, 0),
	})
}

func (a *App) callOllama(r *http.Request, provider Provider, endpoint string, reqBody map[string]any) (*http.Response, error) {
	target, err := ollamaEndpointURL(provider.BaseURL, endpoint)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, target, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if provider.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+provider.APIKey)
	}
	return a.client.Do(req)
}

func (a *App) pullOllamaModel(ctx context.Context, provider Provider, model string) error {
	target, err := ollamaEndpointURL(provider.BaseURL, "pull")
	if err != nil {
		return err
	}
	data, err := json.Marshal(map[string]any{
		"model":  model,
		"stream": false,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if provider.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+provider.APIKey)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("POST %s returned %s: %s", target, resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func ollamaEndpointURL(baseURL, endpoint string) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	base.Path = ollamaAPIPath(base.Path, endpoint)
	base.RawQuery = ""
	return base.String(), nil
}

func ollamaAPIPath(basePath, endpoint string) string {
	trimmed := strings.TrimRight(basePath, "/")
	switch {
	case strings.HasSuffix(trimmed, "/api"):
		return trimmed + "/" + endpoint
	case strings.HasSuffix(trimmed, "/v1"):
		return strings.TrimSuffix(trimmed, "/v1") + "/api/" + endpoint
	case trimmed == "":
		return "/api/" + endpoint
	default:
		return trimmed + "/api/" + endpoint
	}
}

func streamOllamaChatAsOpenAI(w http.ResponseWriter, body io.Reader, publicModel string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var item ollamaChatResponse
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
			continue
		}
		finish := any(nil)
		if item.Done {
			finish = finishReason(item.DoneReason)
		}
		chunk := map[string]any{
			"id":      "chatcmpl-" + randomID(),
			"object":  "chat.completion.chunk",
			"created": createdUnix(item.CreatedAt),
			"model":   publicModel,
			"choices": []map[string]any{{
				"index":         0,
				"delta":         map[string]any{"content": item.Message.Content},
				"finish_reason": finish,
			}},
		}
		writeSSE(w, chunk)
		if flusher != nil {
			flusher.Flush()
		}
	}
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

func streamOllamaCompletionAsOpenAI(w http.ResponseWriter, body io.Reader, publicModel string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var item ollamaGenerateResponse
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
			continue
		}
		finish := any(nil)
		if item.Done {
			finish = finishReason(item.DoneReason)
		}
		chunk := map[string]any{
			"id":      "cmpl-" + randomID(),
			"object":  "text_completion",
			"created": createdUnix(item.CreatedAt),
			"model":   publicModel,
			"choices": []map[string]any{{
				"text":          item.Response,
				"index":         0,
				"logprobs":      nil,
				"finish_reason": finish,
			}},
		}
		writeSSE(w, chunk)
		if flusher != nil {
			flusher.Flush()
		}
	}
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

func writeSSE(w io.Writer, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
}

func copyJSONFields(dst map[string]any, src map[string]json.RawMessage, fields ...string) {
	for _, field := range fields {
		if raw, ok := src[field]; ok {
			dst[field] = mustRaw(raw)
		}
	}
}

func copyOptions(dst map[string]any, src map[string]json.RawMessage) {
	options := map[string]any{}
	if raw, ok := src["options"]; ok {
		if decoded, err := decodeRawObject(raw); err == nil {
			for k, v := range decoded {
				options[k] = v
			}
		}
	}
	fieldMap := map[string]string{
		"temperature":       "temperature",
		"top_p":             "top_p",
		"frequency_penalty": "frequency_penalty",
		"presence_penalty":  "presence_penalty",
		"seed":              "seed",
		"stop":              "stop",
		"max_tokens":        "num_predict",
		"max_output_tokens": "num_predict",
	}
	for openAIField, ollamaField := range fieldMap {
		if raw, ok := src[openAIField]; ok {
			options[ollamaField] = mustRaw(raw)
		}
	}
	if len(options) > 0 {
		dst["options"] = options
	}
}

func copyOpenAIResponseFormat(dst map[string]any, src map[string]json.RawMessage) {
	raw, ok := src["response_format"]
	if !ok {
		return
	}
	var format struct {
		Type       string         `json:"type"`
		JSONSchema map[string]any `json:"json_schema"`
	}
	if err := json.Unmarshal(raw, &format); err != nil {
		return
	}
	switch format.Type {
	case "json_object":
		dst["format"] = "json"
	case "json_schema":
		if schema, ok := format.JSONSchema["schema"]; ok {
			dst["format"] = schema
		}
	}
}

func copyThinking(dst map[string]any, src map[string]json.RawMessage) {
	if raw, ok := src["reasoning_effort"]; ok {
		dst["think"] = mustRaw(raw)
		return
	}
	if raw, ok := src["reasoning"]; ok {
		var reasoning map[string]json.RawMessage
		if err := json.Unmarshal(raw, &reasoning); err == nil {
			if effort, ok := reasoning["effort"]; ok {
				dst["think"] = mustRaw(effort)
			}
		}
	}
}

func mustRaw(raw json.RawMessage) any {
	var value any
	_ = json.Unmarshal(raw, &value)
	return value
}

func decodeRawObject(raw json.RawMessage) (map[string]any, error) {
	var value map[string]any
	err := json.Unmarshal(raw, &value)
	return value, err
}

func rawBool(raw json.RawMessage, fallback bool) bool {
	var value bool
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return fallback
	}
	return value
}

func copyUpstreamError(w http.ResponseWriter, resp *http.Response) {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = resp.Status
	}
	writeOpenAIError(w, resp.StatusCode, "upstream_error", message)
}

func createdUnix(value string) int64 {
	if value == "" {
		return time.Now().Unix()
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Now().Unix()
	}
	return parsed.Unix()
}

func finishReason(value string) string {
	switch value {
	case "", "stop", "unload":
		return "stop"
	case "length":
		return "length"
	default:
		return value
	}
}

func usageObject(prompt, completion int) map[string]int {
	return map[string]int{
		"prompt_tokens":     prompt,
		"completion_tokens": completion,
		"total_tokens":      prompt + completion,
	}
}

func randomID() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}

type ollamaChatResponse struct {
	Model     string `json:"model"`
	CreatedAt string `json:"created_at"`
	Message   struct {
		Role      string `json:"role"`
		Content   string `json:"content"`
		ToolCalls any    `json:"tool_calls,omitempty"`
	} `json:"message"`
	Done            bool   `json:"done"`
	DoneReason      string `json:"done_reason"`
	PromptEvalCount int    `json:"prompt_eval_count"`
	EvalCount       int    `json:"eval_count"`
}

type ollamaGenerateResponse struct {
	Model           string `json:"model"`
	CreatedAt       string `json:"created_at"`
	Response        string `json:"response"`
	Done            bool   `json:"done"`
	DoneReason      string `json:"done_reason"`
	PromptEvalCount int    `json:"prompt_eval_count"`
	EvalCount       int    `json:"eval_count"`
}

type ollamaEmbedResponse struct {
	Model           string      `json:"model"`
	Embeddings      [][]float64 `json:"embeddings"`
	PromptEvalCount int         `json:"prompt_eval_count"`
}
