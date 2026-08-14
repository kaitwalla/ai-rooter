package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func isResponsesRequestPath(path string) bool {
	path = strings.TrimRight(path, "/")
	return strings.HasSuffix(path, "/responses")
}

func isNativeResponsesHost(host string) bool {
	return isNativeOpenAIHost(host) || isGroqHost(host)
}

func roundTripResponsesCompat(base http.RoundTripper, req *http.Request) (*http.Response, error) {
	if isNativeResponsesHost(req.URL.Hostname()) {
		return base.RoundTrip(req)
	}

	original, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()

	chatBody, stream, err := responsesRequestToChat(original)
	if err != nil {
		return jsonTransportError(req, http.StatusBadRequest, "invalid_request_error", err.Error()), nil
	}

	clone := req.Clone(req.Context())
	urlCopy := *clone.URL
	clone.URL = &urlCopy
	clone.URL.Path = responsesPathToChat(clone.URL.Path)
	clone.Header = req.Header.Clone()
	clone.Body = io.NopCloser(bytes.NewReader(chatBody))
	clone.ContentLength = int64(len(chatBody))
	clone.Header.Set("Content-Length", strconv.Itoa(len(chatBody)))

	resp, err := base.RoundTrip(clone)
	if err != nil || resp == nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp, err
	}

	if stream {
		return chatStreamAsResponses(resp), nil
	}
	return chatResponseAsResponses(resp)
}

func responsesPathToChat(path string) string {
	trimmed := strings.TrimRight(path, "/")
	if strings.HasSuffix(trimmed, "/api/responses") {
		return strings.TrimSuffix(trimmed, "/responses") + "/chat"
	}
	return strings.TrimSuffix(trimmed, "/responses") + "/chat/completions"
}

func responsesRequestToChat(body []byte) ([]byte, bool, error) {
	var in map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&in); err != nil {
		return nil, false, err
	}

	model, _ := in["model"].(string)
	if strings.TrimSpace(model) == "" {
		return nil, false, fmt.Errorf("model must be a non-empty string")
	}
	for _, unsupported := range []string{"previous_response_id", "background", "conversation"} {
		if value, ok := in[unsupported]; ok && value != nil && value != "" {
			return nil, false, fmt.Errorf("%s is not supported by this provider through Rooter", unsupported)
		}
	}

	messages := make([]any, 0, 4)
	if text := responseInstructionText(in["instructions"]); text != "" {
		messages = append(messages, map[string]any{"role": "developer", "content": text})
	}
	inputMessages, err := responseInputToMessages(in["input"])
	if err != nil {
		return nil, false, err
	}
	messages = append(messages, inputMessages...)

	out := map[string]any{
		"model":    model,
		"messages": messages,
	}
	copyResponseRequestFields(out, in)

	if rawTools, ok := in["tools"].([]any); ok {
		tools, err := responseToolsToChat(rawTools)
		if err != nil {
			return nil, false, err
		}
		if len(tools) > 0 {
			out["tools"] = tools
		}
	}
	if choice, ok := in["tool_choice"]; ok {
		out["tool_choice"] = responseToolChoiceToChat(choice)
	}
	if maxTokens, ok := in["max_output_tokens"]; ok {
		out["max_tokens"] = maxTokens
	}
	if reasoning, ok := in["reasoning"].(map[string]any); ok {
		if effort, ok := reasoning["effort"]; ok {
			out["reasoning_effort"] = effort
		}
	}
	if text, ok := in["text"].(map[string]any); ok {
		if format, ok := text["format"].(map[string]any); ok {
			out["response_format"] = responseFormatToChat(format)
		}
	}
	stream, _ := in["stream"].(bool)
	out["stream"] = stream

	encoded, err := json.Marshal(out)
	return encoded, stream, err
}

func copyResponseRequestFields(dst, src map[string]any) {
	for _, key := range []string{"temperature", "top_p", "parallel_tool_calls", "seed", "user"} {
		if value, ok := src[key]; ok {
			dst[key] = value
		}
	}
}

func responseInstructionText(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if text := instructionContentText(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n\n")
	default:
		return ""
	}
}

func responseInputToMessages(value any) ([]any, error) {
	switch input := value.(type) {
	case nil:
		return nil, nil
	case string:
		return []any{map[string]any{"role": "user", "content": input}}, nil
	case []any:
		messages := make([]any, 0, len(input))
		for _, raw := range input {
			item, ok := raw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("response input items must be objects")
			}
			typ, _ := item["type"].(string)
			switch typ {
			case "", "message":
				role, _ := item["role"].(string)
				if role == "" {
					role = "user"
				}
				content, err := responseContentToChat(item["content"])
				if err != nil {
					return nil, err
				}
				messages = append(messages, map[string]any{"role": role, "content": content})
			case "function_call":
				callID, _ := item["call_id"].(string)
				name, _ := item["name"].(string)
				arguments, _ := item["arguments"].(string)
				if callID == "" || name == "" {
					return nil, fmt.Errorf("function_call requires call_id and name")
				}
				messages = append(messages, map[string]any{
					"role": "assistant",
					"content": nil,
					"tool_calls": []any{map[string]any{
						"id": callID,
						"type": "function",
						"function": map[string]any{"name": name, "arguments": arguments},
					}},
				})
			case "function_call_output":
				callID, _ := item["call_id"].(string)
				if callID == "" {
					return nil, fmt.Errorf("function_call_output requires call_id")
				}
				messages = append(messages, map[string]any{
					"role": "tool",
					"tool_call_id": callID,
					"content": responseOutputText(item["output"]),
				})
			default:
				return nil, fmt.Errorf("unsupported response input item type %q", typ)
			}
		}
		return messages, nil
	default:
		return nil, fmt.Errorf("input must be a string or array")
	}
}

func responseContentToChat(value any) (any, error) {
	if text, ok := value.(string); ok {
		return text, nil
	}
	parts, ok := value.([]any)
	if !ok {
		return value, nil
	}
	out := make([]any, 0, len(parts))
	for _, raw := range parts {
		part, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("content parts must be objects")
		}
		typ, _ := part["type"].(string)
		switch typ {
		case "input_text", "output_text", "text":
			out = append(out, map[string]any{"type": "text", "text": part["text"]})
		case "input_image":
			url := part["image_url"]
			if url == nil {
				url = part["url"]
			}
			if url == nil {
				return nil, fmt.Errorf("input_image requires image_url")
			}
			out = append(out, map[string]any{"type": "image_url", "image_url": map[string]any{"url": url}})
		default:
			return nil, fmt.Errorf("unsupported response content type %q", typ)
		}
	}
	return out, nil
}

func responseOutputText(value any) string {
	switch v := value.(type) {
	case string:
		return v
	default:
		data, _ := json.Marshal(v)
		return string(data)
	}
}

func responseToolsToChat(tools []any) ([]any, error) {
	out := make([]any, 0, len(tools))
	for _, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("tools must be objects")
		}
		typ, _ := tool["type"].(string)
		if typ != "function" {
			return nil, fmt.Errorf("provider-translated Responses currently supports function tools only")
		}
		fn := map[string]any{}
		for _, key := range []string{"name", "description", "parameters", "strict"} {
			if value, ok := tool[key]; ok {
				fn[key] = value
			}
		}
		out = append(out, map[string]any{"type": "function", "function": fn})
	}
	return out, nil
}

func responseToolChoiceToChat(choice any) any {
	obj, ok := choice.(map[string]any)
	if !ok {
		return choice
	}
	if typ, _ := obj["type"].(string); typ == "function" {
		if name, _ := obj["name"].(string); name != "" {
			return map[string]any{"type": "function", "function": map[string]any{"name": name}}
		}
	}
	return choice
}

func responseFormatToChat(format map[string]any) any {
	typ, _ := format["type"].(string)
	switch typ {
	case "json_schema":
		jsonSchema := map[string]any{}
		for _, key := range []string{"name", "schema", "strict", "description"} {
			if value, ok := format[key]; ok {
				jsonSchema[key] = value
			}
		}
		return map[string]any{"type": "json_schema", "json_schema": jsonSchema}
	case "json_object":
		return map[string]any{"type": "json_object"}
	default:
		return format
	}
}

func chatResponseAsResponses(resp *http.Response) (*http.Response, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	_ = resp.Body.Close()

	var chat map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&chat); err != nil {
		return nil, err
	}
	converted := chatObjectToResponse(chat)
	encoded, err := json.Marshal(converted)
	if err != nil {
		return nil, err
	}

	clone := *resp
	clone.Header = resp.Header.Clone()
	clone.Header.Set("Content-Type", "application/json")
	clone.Header.Set("Content-Length", strconv.Itoa(len(encoded)))
	clone.ContentLength = int64(len(encoded))
	clone.Body = io.NopCloser(bytes.NewReader(encoded))
	return &clone, nil
}

func chatObjectToResponse(chat map[string]any) map[string]any {
	id := "resp_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	model, _ := chat["model"].(string)
	output := make([]any, 0, 2)

	if choices, ok := chat["choices"].([]any); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]any); ok {
			if message, ok := choice["message"].(map[string]any); ok {
				if content := responseMessageText(message["content"]); content != "" {
					output = append(output, responseMessageItem("msg_"+strconv.FormatInt(time.Now().UnixNano(), 36), content))
				}
				if calls, ok := message["tool_calls"].([]any); ok {
					for i, raw := range calls {
						if call, ok := raw.(map[string]any); ok {
							output = append(output, responseFunctionCallItem(call, i))
						}
					}
				}
			}
		}
	}

	return map[string]any{
		"id": id,
		"object": "response",
		"created_at": time.Now().Unix(),
		"status": "completed",
		"model": model,
		"output": output,
		"parallel_tool_calls": true,
		"usage": responseUsage(chat["usage"]),
		"error": nil,
		"incomplete_details": nil,
	}
}

func responseMessageText(content any) string {
	if text, ok := content.(string); ok {
		return text
	}
	parts, ok := content.([]any)
	if !ok {
		return ""
	}
	texts := make([]string, 0, len(parts))
	for _, raw := range parts {
		if part, ok := raw.(map[string]any); ok {
			if text, ok := part["text"].(string); ok {
				texts = append(texts, text)
			}
		}
	}
	return strings.Join(texts, "")
}

func responseMessageItem(id, text string) map[string]any {
	return map[string]any{
		"id": id,
		"type": "message",
		"status": "completed",
		"role": "assistant",
		"content": []any{map[string]any{
			"type": "output_text",
			"text": text,
			"annotations": []any{},
		}},
	}
}

func responseFunctionCallItem(call map[string]any, index int) map[string]any {
	callID, _ := call["id"].(string)
	if callID == "" {
		callID = fmt.Sprintf("call_%d_%d", time.Now().UnixNano(), index)
	}
	name := ""
	arguments := ""
	if fn, ok := call["function"].(map[string]any); ok {
		name, _ = fn["name"].(string)
		arguments, _ = fn["arguments"].(string)
	}
	return map[string]any{
		"id": "fc_" + strconv.FormatInt(time.Now().UnixNano()+int64(index), 36),
		"type": "function_call",
		"status": "completed",
		"call_id": callID,
		"name": name,
		"arguments": arguments,
	}
}

func responseUsage(value any) map[string]any {
	usage, _ := value.(map[string]any)
	input := numberValue(usage["prompt_tokens"])
	output := numberValue(usage["completion_tokens"])
	return map[string]any{
		"input_tokens": input,
		"output_tokens": output,
		"total_tokens": input + output,
		"input_tokens_details": map[string]any{"cached_tokens": 0},
		"output_tokens_details": map[string]any{"reasoning_tokens": 0},
	}
}

func numberValue(value any) int64 {
	switch v := value.(type) {
	case json.Number:
		n, _ := v.Int64()
		return n
	case float64:
		return int64(v)
	case int:
		return int64(v)
	case int64:
		return v
	default:
		return 0
	}
}

func chatStreamAsResponses(resp *http.Response) *http.Response {
	reader, writer := io.Pipe()
	clone := *resp
	clone.Header = resp.Header.Clone()
	clone.Header.Set("Content-Type", "text/event-stream")
	clone.Header.Del("Content-Length")
	clone.ContentLength = -1
	clone.Body = reader

	go func() {
		defer writer.Close()
		defer resp.Body.Close()
		translateChatSSEToResponses(resp.Body, writer)
	}()
	return &clone
}

type streamToolCall struct {
	id string
	name string
	arguments strings.Builder
	itemID string
	outputIndex int
	started bool
}

func translateChatSSEToResponses(src io.Reader, dst io.Writer) {
	responseID := "resp_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	createdAt := time.Now().Unix()
	sequence := 0
	model := ""
	messageID := "msg_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	messageStarted := false
	contentStarted := false
	var text strings.Builder
	tools := map[int]*streamToolCall{}
	nextOutputIndex := 0

	writeResponseSSE(dst, "response.created", &sequence, map[string]any{"response": streamResponseEnvelope(responseID, createdAt, model, "in_progress", nil)})
	writeResponseSSE(dst, "response.in_progress", &sequence, map[string]any{"response": streamResponseEnvelope(responseID, createdAt, model, "in_progress", nil)})

	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var chunk map[string]any
		decoder := json.NewDecoder(strings.NewReader(data))
		decoder.UseNumber()
		if decoder.Decode(&chunk) != nil {
			continue
		}
		if m, ok := chunk["model"].(string); ok && m != "" {
			model = m
		}
		choices, _ := chunk["choices"].([]any)
		if len(choices) == 0 {
			continue
		}
		choice, _ := choices[0].(map[string]any)
		delta, _ := choice["delta"].(map[string]any)
		if content, ok := delta["content"].(string); ok && content != "" {
			if !messageStarted {
				messageStarted = true
				nextOutputIndex++
				writeResponseSSE(dst, "response.output_item.added", &sequence, map[string]any{"output_index": 0, "item": map[string]any{"id": messageID, "type": "message", "status": "in_progress", "role": "assistant", "content": []any{}}})
			}
			if !contentStarted {
				contentStarted = true
				writeResponseSSE(dst, "response.content_part.added", &sequence, map[string]any{"item_id": messageID, "output_index": 0, "content_index": 0, "part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}}})
			}
			text.WriteString(content)
			writeResponseSSE(dst, "response.output_text.delta", &sequence, map[string]any{"item_id": messageID, "output_index": 0, "content_index": 0, "delta": content})
		}

		if calls, ok := delta["tool_calls"].([]any); ok {
			for _, raw := range calls {
				call, _ := raw.(map[string]any)
				idx := int(numberValue(call["index"]))
				state := tools[idx]
				if state == nil {
					state = &streamToolCall{itemID: "fc_" + strconv.FormatInt(time.Now().UnixNano()+int64(idx), 36), outputIndex: nextOutputIndex}
					nextOutputIndex++
					tools[idx] = state
				}
				if id, ok := call["id"].(string); ok && id != "" {
					state.id = id
				}
				fn, _ := call["function"].(map[string]any)
				if name, ok := fn["name"].(string); ok && name != "" {
					state.name = name
				}
				if !state.started && state.name != "" {
					state.started = true
					writeResponseSSE(dst, "response.output_item.added", &sequence, map[string]any{"output_index": state.outputIndex, "item": map[string]any{"id": state.itemID, "type": "function_call", "status": "in_progress", "call_id": state.id, "name": state.name, "arguments": ""}})
				}
				if args, ok := fn["arguments"].(string); ok && args != "" {
					state.arguments.WriteString(args)
					writeResponseSSE(dst, "response.function_call_arguments.delta", &sequence, map[string]any{"item_id": state.itemID, "output_index": state.outputIndex, "delta": args})
				}
			}
		}
	}

	output := make([]any, 0, nextOutputIndex)
	if messageStarted {
		if contentStarted {
			writeResponseSSE(dst, "response.output_text.done", &sequence, map[string]any{"item_id": messageID, "output_index": 0, "content_index": 0, "text": text.String()})
			writeResponseSSE(dst, "response.content_part.done", &sequence, map[string]any{"item_id": messageID, "output_index": 0, "content_index": 0, "part": map[string]any{"type": "output_text", "text": text.String(), "annotations": []any{}}})
		}
		item := responseMessageItem(messageID, text.String())
		output = append(output, item)
		writeResponseSSE(dst, "response.output_item.done", &sequence, map[string]any{"output_index": 0, "item": item})
	}
	for i := 0; i < len(tools); i++ {
		state := tools[i]
		if state == nil {
			continue
		}
		writeResponseSSE(dst, "response.function_call_arguments.done", &sequence, map[string]any{"item_id": state.itemID, "output_index": state.outputIndex, "arguments": state.arguments.String()})
		item := map[string]any{"id": state.itemID, "type": "function_call", "status": "completed", "call_id": state.id, "name": state.name, "arguments": state.arguments.String()}
		output = append(output, item)
		writeResponseSSE(dst, "response.output_item.done", &sequence, map[string]any{"output_index": state.outputIndex, "item": item})
	}
	final := streamResponseEnvelope(responseID, createdAt, model, "completed", output)
	writeResponseSSE(dst, "response.completed", &sequence, map[string]any{"response": final})
}

func streamResponseEnvelope(id string, createdAt int64, model, status string, output []any) map[string]any {
	if output == nil {
		output = []any{}
	}
	return map[string]any{
		"id": id,
		"object": "response",
		"created_at": createdAt,
		"status": status,
		"model": model,
		"output": output,
		"parallel_tool_calls": true,
		"error": nil,
		"incomplete_details": nil,
	}
}

func writeResponseSSE(w io.Writer, event string, sequence *int, fields map[string]any) {
	fields["type"] = event
	fields["sequence_number"] = *sequence
	*sequence++
	data, err := json.Marshal(fields)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
}

func jsonTransportError(req *http.Request, status int, code, message string) *http.Response {
	body, _ := json.Marshal(map[string]any{"error": map[string]any{"message": message, "type": "invalid_request_error", "code": code}})
	return &http.Response{
		StatusCode: status,
		Status: fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header: http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
		Request: req,
	}
}
