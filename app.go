package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

type App struct {
	store         *Store
	adminTokenEnv string
	client        *http.Client
}

func NewApp(store *Store, adminTokenEnv string) *App {
	return &App{
		store:         store,
		adminTokenEnv: strings.TrimSpace(adminTokenEnv),
		client: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", a.handleAdminPage)
	mux.HandleFunc("/admin/api/config", a.requireAdmin(a.handleAdminConfig))
	mux.HandleFunc("/admin/api/activate", a.requireAdmin(a.handleActivate))
	mux.HandleFunc("/admin/api/discover", a.requireAdmin(a.handleDiscover))
	mux.HandleFunc("/healthz", a.handleHealth)
	mux.HandleFunc("/v1/models", a.handleModels)
	mux.HandleFunc("/v1/models/", a.handleModel)
	mux.HandleFunc("/v1/", a.handleProxy)
	return logRequests(mux)
}

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) handleAdminPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, adminHTML)
}

func (a *App) handleAdminConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, a.store.Snapshot())
	case http.MethodPut:
		defer r.Body.Close()
		var next Config
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&next); err != nil {
			writeProblem(w, http.StatusBadRequest, "invalid JSON", err.Error())
			return
		}
		current := a.store.Snapshot()
		mergeRedactedSecrets(&next, current)
		if err := a.store.Replace(next); err != nil {
			writeProblem(w, http.StatusBadRequest, "invalid config", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, a.store.Snapshot())
	default:
		w.Header().Set("Allow", "GET, PUT")
		writeProblem(w, http.StatusMethodNotAllowed, "method not allowed", "")
	}
}

func (a *App) handleDiscover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeProblem(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	defer r.Body.Close()
	var body struct {
		ProviderID string `json:"provider_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid JSON", err.Error())
		return
	}
	cfg := a.store.Snapshot()
	provider, ok := findProvider(cfg, body.ProviderID)
	if !ok {
		writeProblem(w, http.StatusNotFound, "provider not found", body.ProviderID)
		return
	}
	models, err := a.discoverModels(r.Context(), provider)
	if err != nil {
		writeProblem(w, http.StatusBadGateway, "model discovery failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

func (a *App) handleActivate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeProblem(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	defer r.Body.Close()
	var body struct {
		ProviderID string   `json:"provider_id"`
		Models     []string `json:"models"`
		Pull       bool     `json:"pull"`
		Enable     bool     `json:"enable"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512<<10)).Decode(&body); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid JSON", err.Error())
		return
	}
	models := compactUnique(body.Models)
	if len(models) == 0 {
		writeProblem(w, http.StatusBadRequest, "no models", "Add at least one model name.")
		return
	}

	cfg := a.store.Snapshot()
	provider, ok := findProvider(cfg, body.ProviderID)
	if !ok {
		writeProblem(w, http.StatusNotFound, "provider not found", body.ProviderID)
		return
	}

	results := make([]map[string]any, 0, len(models))
	for _, model := range models {
		result := map[string]any{"model": model, "pulled": false, "ok": true}
		if body.Pull {
			if !isOllamaProvider(provider) {
				result["ok"] = false
				result["error"] = "pull is only available for Ollama providers"
			} else if err := a.pullOllamaModel(r.Context(), provider, model); err != nil {
				result["ok"] = false
				result["error"] = err.Error()
			} else {
				result["pulled"] = true
			}
		}
		results = append(results, result)
		if result["ok"] == false {
			continue
		}
		cfg.Models = addOrUpdateModelMapping(cfg.Models, ModelMapping{
			PublicName:   model,
			ProviderID:   provider.ID,
			UpstreamName: model,
			Enabled:      body.Enable,
			Order:        len(cfg.Models) + 1,
		})
	}
	if err := a.store.Replace(cfg); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid config", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"config":  a.store.Snapshot(),
		"results": results,
	})
}

func (a *App) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeProblem(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	if !a.requirePublicAPIKey(w, r) {
		return
	}
	cfg := a.store.Snapshot()
	data := []map[string]any{}
	for _, model := range cfg.Models {
		provider, ok := findProvider(cfg, model.ProviderID)
		if !model.Enabled || !ok || !provider.Enabled {
			continue
		}
		data = append(data, openAIModel(model.PublicName, provider.Name))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   data,
	})
}

func (a *App) handleModel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		writeProblem(w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	if !a.requirePublicAPIKey(w, r) {
		return
	}
	publicName := strings.TrimPrefix(r.URL.Path, "/v1/models/")
	cfg := a.store.Snapshot()
	model, provider, ok := findModelAndProvider(cfg, publicName)
	if !ok || !model.Enabled || !provider.Enabled {
		writeOpenAIError(w, http.StatusNotFound, "model_not_found", fmt.Sprintf("model %q is not available", publicName))
		return
	}
	writeJSON(w, http.StatusOK, openAIModel(model.PublicName, provider.Name))
}

func (a *App) handleProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeProblem(w, http.StatusNotFound, "not found", "")
		return
	}
	if !a.requirePublicAPIKey(w, r) {
		return
	}
	defer r.Body.Close()

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32<<20))
	if err != nil {
		writeOpenAIError(w, http.StatusRequestEntityTooLarge, "request_too_large", err.Error())
		return
	}
	rewritten, publicModel, err := rewriteModel(body, func(name string) (string, bool) {
		cfg := a.store.Snapshot()
		model, _, ok := findModelAndProvider(cfg, name)
		if !ok || !model.Enabled {
			return "", false
		}
		return model.UpstreamName, true
	})
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	cfg := a.store.Snapshot()
	model, provider, ok := findModelAndProvider(cfg, publicModel)
	if !ok || !model.Enabled || !provider.Enabled {
		writeOpenAIError(w, http.StatusNotFound, "model_not_found", fmt.Sprintf("model %q is not available", publicModel))
		return
	}
	if isOllamaProvider(provider) {
		a.proxyToOllama(w, r, provider, publicModel, rewritten)
		return
	}
	a.proxyToProvider(w, r, provider, rewritten)
}

func (a *App) proxyToProvider(w http.ResponseWriter, r *http.Request, provider Provider, body []byte) {
	upstream, err := upstreamURL(provider, r.URL)
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "upstream_url_error", err.Error())
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, upstream, bytes.NewReader(body))
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "upstream_request_error", err.Error())
		return
	}
	copyForwardHeaders(req.Header, r.Header)
	req.Header.Del("Authorization")
	if provider.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+provider.APIKey)
	}
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

func (a *App) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		expected := a.adminToken()
		if expected == "" {
			next(w, r)
			return
		}
		if bearerToken(r.Header.Get("Authorization")) == expected || r.Header.Get("X-Rooter-Admin-Token") == expected {
			next(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="rooter-admin"`)
		if bearerToken(r.Header.Get("Authorization")) != "" || r.Header.Get("X-Rooter-Admin-Token") != "" {
			writeProblem(w, http.StatusUnauthorized, "wrong admin token", "")
			return
		}
		writeProblem(w, http.StatusUnauthorized, "admin token required", "Enter the saved admin token.")
	}
}

func (a *App) adminToken() string {
	if a.adminTokenEnv != "" {
		return a.adminTokenEnv
	}
	return a.store.Snapshot().AdminToken
}

func (a *App) requirePublicAPIKey(w http.ResponseWriter, r *http.Request) bool {
	cfg := a.store.Snapshot()
	if len(cfg.PublicAPIKeys) == 0 {
		writeOpenAIError(w, http.StatusUnauthorized, "invalid_api_key", "no public API keys are configured")
		return false
	}
	token := bearerToken(r.Header.Get("Authorization"))
	if slices.Contains(cfg.PublicAPIKeys, token) {
		return true
	}
	w.Header().Set("WWW-Authenticate", `Bearer realm="rooter"`)
	writeOpenAIError(w, http.StatusUnauthorized, "invalid_api_key", "missing or invalid API key")
	return false
}

func (a *App) discoverModels(ctx context.Context, provider Provider) ([]string, error) {
	if isOllamaProvider(provider) {
		return a.discoverOllamaNative(ctx, provider)
	}
	return a.discoverOpenAIModels(ctx, provider)
}

func (a *App) discoverOpenAIModels(ctx context.Context, provider Provider) ([]string, error) {
	base, err := url.Parse(provider.BaseURL)
	if err != nil {
		return nil, err
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/models"
	base.RawQuery = ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
	if err != nil {
		return nil, err
	}
	if provider.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+provider.APIKey)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GET %s returned %s", base.String(), resp.Status)
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	models := make([]string, 0, len(payload.Data))
	for _, item := range payload.Data {
		if item.ID != "" {
			models = append(models, item.ID)
		}
	}
	slices.Sort(models)
	return models, nil
}

func (a *App) discoverOllamaNative(ctx context.Context, provider Provider) ([]string, error) {
	base, err := url.Parse(provider.BaseURL)
	if err != nil {
		return nil, err
	}
	base.Path = ollamaAPIPath(base.Path, "tags")
	base.RawQuery = ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
	if err != nil {
		return nil, err
	}
	if provider.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+provider.APIKey)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GET %s returned %s", base.String(), resp.Status)
	}
	var payload struct {
		Models []struct {
			Name  string `json:"name"`
			Model string `json:"model"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	models := make([]string, 0, len(payload.Models))
	for _, item := range payload.Models {
		name := item.Name
		if name == "" {
			name = item.Model
		}
		if name != "" {
			models = append(models, name)
		}
	}
	slices.Sort(models)
	return models, nil
}

func rewriteModel(body []byte, resolve func(string) (string, bool)) ([]byte, string, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, "", err
	}
	rawModel, ok := payload["model"]
	if !ok {
		return nil, "", errors.New("request body needs a model")
	}
	var publicModel string
	if err := json.Unmarshal(rawModel, &publicModel); err != nil || publicModel == "" {
		return nil, "", errors.New("model must be a non-empty string")
	}
	upstream, ok := resolve(publicModel)
	if !ok {
		return nil, publicModel, fmt.Errorf("model %q is not configured", publicModel)
	}
	upstreamJSON, _ := json.Marshal(upstream)
	payload["model"] = upstreamJSON
	rewritten, err := json.Marshal(payload)
	return rewritten, publicModel, err
}

func upstreamURL(provider Provider, incoming *url.URL) (string, error) {
	base, err := url.Parse(provider.BaseURL)
	if err != nil {
		return "", err
	}
	path := strings.TrimPrefix(incoming.Path, "/v1")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	base.Path = strings.TrimRight(base.Path, "/") + path
	base.RawQuery = incoming.RawQuery
	return base.String(), nil
}

func findProvider(cfg Config, id string) (Provider, bool) {
	id = slugify(id)
	for _, provider := range cfg.Providers {
		if provider.ID == id {
			return provider, true
		}
	}
	return Provider{}, false
}

func findModelAndProvider(cfg Config, publicName string) (ModelMapping, Provider, bool) {
	for _, model := range cfg.Models {
		if model.PublicName != publicName {
			continue
		}
		provider, ok := findProvider(cfg, model.ProviderID)
		return model, provider, ok
	}
	return ModelMapping{}, Provider{}, false
}

func addOrUpdateModelMapping(models []ModelMapping, next ModelMapping) []ModelMapping {
	for i := range models {
		if models[i].ProviderID == next.ProviderID && models[i].UpstreamName == next.UpstreamName {
			models[i].Enabled = next.Enabled
			if models[i].PublicName == "" {
				models[i].PublicName = next.PublicName
			}
			return models
		}
	}
	for i := range models {
		if models[i].PublicName == next.PublicName {
			next.PublicName = uniqueModelName(models, next.PublicName+"-"+next.ProviderID)
			break
		}
	}
	return append(models, next)
}

func uniqueModelName(models []ModelMapping, base string) string {
	used := map[string]bool{}
	for _, model := range models {
		used[model.PublicName] = true
	}
	if !used[base] {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if !used[candidate] {
			return candidate
		}
	}
}

func openAIModel(id, owner string) map[string]any {
	return map[string]any{
		"id":       id,
		"object":   "model",
		"created":  0,
		"owned_by": owner,
	}
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if len(header) >= len(prefix) && strings.EqualFold(header[:len(prefix)], prefix) {
		return strings.TrimSpace(header[len(prefix):])
	}
	return ""
}

func copyForwardHeaders(dst, src http.Header) {
	for key, values := range src {
		lower := strings.ToLower(key)
		if lower == "host" || lower == "authorization" || lower == "content-length" {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func copyResponseHeaders(dst, src http.Header) {
	for key, values := range src {
		lower := strings.ToLower(key)
		if lower == "content-length" || lower == "connection" {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func mergeRedactedSecrets(next *Config, current Config) {
	if next.AdminToken == redactedSecret {
		next.AdminToken = current.AdminToken
	}
	currentProviders := map[string]Provider{}
	for _, provider := range current.Providers {
		currentProviders[provider.ID] = provider
	}
	for i := range next.Providers {
		if next.Providers[i].APIKey != redactedSecret {
			continue
		}
		if current, ok := currentProviders[slugify(next.Providers[i].ID)]; ok {
			next.Providers[i].APIKey = current.APIKey
		}
	}
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		fmt.Printf("%s %s %d %s\n", r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Millisecond))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeProblem(w http.ResponseWriter, status int, title, detail string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"message": strings.TrimSpace(title + " " + detail),
			"type":    "rooter_error",
		},
	})
}

func writeOpenAIError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    "invalid_request_error",
			"code":    code,
		},
	})
}
