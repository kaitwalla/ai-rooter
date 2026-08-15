package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

func (a *App) proxyOllamaResponses(w http.ResponseWriter, r *http.Request, provider Provider, body []byte) {
	target, err := ollamaOpenAIEndpointURL(provider.BaseURL, "responses")
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "upstream_url_error", err.Error())
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "upstream_request_error", err.Error())
		return
	}
	copyForwardHeaders(req.Header, r.Header)
	req.Header.Del("Authorization")
	if provider.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+provider.APIKey)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Length", fmt.Sprint(len(body)))

	resp, err := a.client.Do(req)
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "upstream_error", err.Error())
		return
	}
	defer resp.Body.Close()
	copyResponseHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)

	var reqBody map[string]any
	if json.Unmarshal(body, &reqBody) == nil {
		if stream, _ := reqBody["stream"].(bool); stream {
			flusher := extractFlusher(w)
			if flusher != nil {
				copyWithFlush(w, resp.Body, flusher)
				return
			}
		}
	}
	_, _ = io.Copy(w, resp.Body)
}

func extractFlusher(w http.ResponseWriter) http.Flusher {
	for {
		if f, ok := w.(http.Flusher); ok {
			return f
		}
		if u, ok := w.(interface{ Unwrap() http.ResponseWriter }); ok {
			w = u.Unwrap()
		} else {
			return nil
		}
	}
}

func copyWithFlush(dst io.Writer, src io.Reader, flusher http.Flusher) {
	buf := make([]byte, 4096)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			_, _ = dst.Write(buf[:n])
			if bytes.Contains(buf[:n], []byte("\n\n")) {
				flusher.Flush()
			}
		}
		if err != nil {
			break
		}
	}
}

func ollamaOpenAIEndpointURL(baseURL, endpoint string) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	scheme := strings.ToLower(base.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("unsupported URL scheme %q; expected http or https", base.Scheme)
	}
	path := strings.TrimRight(base.Path, "/")
	switch {
	case strings.HasSuffix(path, "/v1/api"):
		path = strings.TrimSuffix(path, "/api")
	case strings.HasSuffix(path, "/api"):
		path = strings.TrimSuffix(path, "/api") + "/v1"
	case strings.HasSuffix(path, "/v1"):
	default:
		path += "/v1"
	}
	base.Path = strings.TrimRight(path, "/") + "/" + endpoint
	base.RawQuery = ""
	return base.String(), nil
}
