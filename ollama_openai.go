package main

import (
	"bytes"
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
	_, _ = io.Copy(w, resp.Body)
}

func ollamaOpenAIEndpointURL(baseURL, endpoint string) (string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	path := strings.TrimRight(base.Path, "/")
	switch {
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
