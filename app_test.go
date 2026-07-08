package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestModelsEndpointReturnsEnabledModelsInOrder(t *testing.T) {
	app := testApp(t, Config{
		PublicAPIKeys: []string{"client-key"},
		Providers: []Provider{
			{ID: "a", Name: "Provider A", Type: "openai", BaseURL: "http://example.test/v1", Enabled: true},
			{ID: "b", Name: "Provider B", Type: "openai", BaseURL: "http://example.test/v1", Enabled: false},
		},
		Models: []ModelMapping{
			{PublicName: "z", ProviderID: "a", UpstreamName: "z-up", Enabled: true, Order: 2},
			{PublicName: "hidden", ProviderID: "a", UpstreamName: "hidden", Enabled: false, Order: 1},
			{PublicName: "disabled-provider", ProviderID: "b", UpstreamName: "x", Enabled: true, Order: 3},
			{PublicName: "a", ProviderID: "a", UpstreamName: "a-up", Chain: []ChainStep{{ProviderID: "a", UpstreamName: "a-fallback"}}, Enabled: true, Order: 0},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer client-key")
	rec := httptest.NewRecorder()
	app.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, model := range payload.Data {
		got = append(got, model.ID)
	}
	want := "a,z"
	if strings.Join(got, ",") != want {
		t.Fatalf("models = %q want %q", strings.Join(got, ","), want)
	}
}

func TestProxyRewritesModelAndProviderAuth(t *testing.T) {
	var upstreamAuth, upstreamPath, upstreamBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamAuth = r.Header.Get("Authorization")
		upstreamPath = r.URL.Path
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		upstreamBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"ok"}`))
	}))
	defer upstream.Close()

	app := testApp(t, Config{
		PublicAPIKeys: []string{"client-key"},
		Providers: []Provider{
			{ID: "p", Name: "Provider", Type: "openai", BaseURL: upstream.URL + "/v1", APIKey: "provider-key", Enabled: true},
		},
		Models: []ModelMapping{
			{PublicName: "public", ProviderID: "p", UpstreamName: "real", Enabled: true, Order: 1},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"public","messages":[]}`))
	req.Header.Set("Authorization", "Bearer client-key")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if upstreamAuth != "Bearer provider-key" {
		t.Fatalf("upstream auth = %q", upstreamAuth)
	}
	if upstreamPath != "/v1/chat/completions" {
		t.Fatalf("upstream path = %q", upstreamPath)
	}
	if !strings.Contains(upstreamBody, `"model":"real"`) {
		t.Fatalf("upstream body = %s", upstreamBody)
	}
}

func TestChainFallsBackOnAnyFourHundredStatus(t *testing.T) {
	var primaryBody, fallbackBody string
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		primaryBody = string(body)
		writeOpenAIError(w, http.StatusForbidden, "forbidden", "no")
	}))
	defer primary.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		fallbackBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"fallback-ok"}`))
	}))
	defer fallback.Close()

	app := testApp(t, Config{
		PublicAPIKeys: []string{"client-key"},
		Providers: []Provider{
			{ID: "primary", Name: "Primary", Type: "openai", BaseURL: primary.URL + "/v1", Enabled: true},
			{ID: "fallback", Name: "Fallback", Type: "openai", BaseURL: fallback.URL + "/v1", Enabled: true},
		},
		Models: []ModelMapping{
			{
				PublicName:   "public",
				ProviderID:   "primary",
				UpstreamName: "primary-real",
				Chain:        []ChainStep{{ProviderID: "fallback", UpstreamName: "fallback-real"}},
				Enabled:      true,
				Order:        1,
			},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"public","messages":[]}`))
	req.Header.Set("Authorization", "Bearer client-key")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(primaryBody, `"model":"primary-real"`) {
		t.Fatalf("primary body = %s", primaryBody)
	}
	if !strings.Contains(fallbackBody, `"model":"fallback-real"`) {
		t.Fatalf("fallback body = %s", fallbackBody)
	}
	if !strings.Contains(rec.Body.String(), "fallback-ok") {
		t.Fatalf("response body = %s", rec.Body.String())
	}
}

func TestChainDoesNotFallBackOnNonFourHundredStatus(t *testing.T) {
	fallbackCalled := false
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeOpenAIError(w, http.StatusInternalServerError, "upstream_down", "down")
	}))
	defer primary.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackCalled = true
		_, _ = w.Write([]byte(`{"id":"should-not-run"}`))
	}))
	defer fallback.Close()

	app := testApp(t, Config{
		PublicAPIKeys: []string{"client-key"},
		Providers: []Provider{
			{ID: "primary", Name: "Primary", Type: "openai", BaseURL: primary.URL + "/v1", Enabled: true},
			{ID: "fallback", Name: "Fallback", Type: "openai", BaseURL: fallback.URL + "/v1", Enabled: true},
		},
		Models: []ModelMapping{
			{
				PublicName:   "public",
				ProviderID:   "primary",
				UpstreamName: "primary-real",
				Chain:        []ChainStep{{ProviderID: "fallback", UpstreamName: "fallback-real"}},
				Enabled:      true,
				Order:        1,
			},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"public","messages":[]}`))
	req.Header.Set("Authorization", "Bearer client-key")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if fallbackCalled {
		t.Fatal("fallback was called for a non-4xx response")
	}
}

func TestChainSkipsRateLimitedStepUntilRetryAfter(t *testing.T) {
	primaryCalls := 0
	fallbackCalls := 0
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryCalls++
		w.Header().Set("Retry-After", "60")
		writeOpenAIError(w, http.StatusTooManyRequests, "rate_limited", "try later")
	}))
	defer primary.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"fallback-ok"}`))
	}))
	defer fallback.Close()

	app := testApp(t, Config{
		PublicAPIKeys: []string{"client-key"},
		Providers: []Provider{
			{ID: "primary", Name: "Primary", Type: "openai", BaseURL: primary.URL + "/v1", Enabled: true},
			{ID: "fallback", Name: "Fallback", Type: "openai", BaseURL: fallback.URL + "/v1", Enabled: true},
		},
		Models: []ModelMapping{
			{
				PublicName:   "public",
				ProviderID:   "primary",
				UpstreamName: "primary-real",
				Chain:        []ChainStep{{ProviderID: "fallback", UpstreamName: "fallback-real"}},
				Enabled:      true,
				Order:        1,
			},
		},
	})

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"public","messages":[]}`))
		req.Header.Set("Authorization", "Bearer client-key")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		app.routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d status = %d body = %s", i+1, rec.Code, rec.Body.String())
		}
	}
	if primaryCalls != 1 {
		t.Fatalf("primary calls = %d want 1", primaryCalls)
	}
	if fallbackCalls != 2 {
		t.Fatalf("fallback calls = %d want 2", fallbackCalls)
	}
}

func TestParseRetryAfterDefaultsToThirtyMinutes(t *testing.T) {
	now := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	if got := parseRetryAfter("", now); !got.Equal(now.Add(30 * time.Minute)) {
		t.Fatalf("empty retry-after = %s", got)
	}
	if got := parseRetryAfter("120", now); !got.Equal(now.Add(2 * time.Minute)) {
		t.Fatalf("seconds retry-after = %s", got)
	}
	date := "Wed, 08 Jul 2026 12:05:00 GMT"
	if got := parseRetryAfter(date, now); !got.Equal(now.Add(5 * time.Minute)) {
		t.Fatalf("date retry-after = %s", got)
	}
}

func TestOllamaCloudAdapterUsesNativeAPI(t *testing.T) {
	var upstreamAuth, upstreamPath, upstreamBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamAuth = r.Header.Get("Authorization")
		upstreamPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		upstreamBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model":"gpt-oss:120b",
			"created_at":"2026-07-08T18:00:00Z",
			"message":{"role":"assistant","content":"hello"},
			"done":true,
			"done_reason":"stop",
			"prompt_eval_count":3,
			"eval_count":2
		}`))
	}))
	defer upstream.Close()

	app := testApp(t, Config{
		PublicAPIKeys: []string{"client-key"},
		Providers: []Provider{
			{ID: "oc", Name: "Ollama Cloud", Type: "ollama_cloud", BaseURL: upstream.URL + "/api", APIKey: "ollama-key", Enabled: true},
		},
		Models: []ModelMapping{
			{PublicName: "fast", ProviderID: "oc", UpstreamName: "gpt-oss:120b", Enabled: true, Order: 1},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"fast","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer client-key")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if upstreamAuth != "Bearer ollama-key" {
		t.Fatalf("upstream auth = %q", upstreamAuth)
	}
	if upstreamPath != "/api/chat" {
		t.Fatalf("upstream path = %q", upstreamPath)
	}
	if !strings.Contains(upstreamBody, `"model":"gpt-oss:120b"`) {
		t.Fatalf("upstream body = %s", upstreamBody)
	}
	if !strings.Contains(upstreamBody, `"stream":false`) {
		t.Fatalf("upstream body did not force non-stream default = %s", upstreamBody)
	}
	if !strings.Contains(rec.Body.String(), `"model":"fast"`) {
		t.Fatalf("response body = %s", rec.Body.String())
	}
}

func TestActivateAddsModelsAndPullsOllama(t *testing.T) {
	var pullPath, pullAuth, pullBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pullPath = r.URL.Path
		pullAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		pullBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success"}`))
	}))
	defer upstream.Close()

	app := testApp(t, Config{
		Providers: []Provider{
			{ID: "oc", Name: "Ollama Cloud", Type: "ollama_cloud", BaseURL: upstream.URL + "/api", APIKey: "ollama-key", Enabled: true},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/admin/api/activate", strings.NewReader(`{
		"provider_id":"oc",
		"models":["gpt-oss:120b","gpt-oss:20b"],
		"pull":true,
		"enable":true
	}`))
	authorizeAdmin(req, app)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if pullPath != "/api/pull" {
		t.Fatalf("pull path = %q", pullPath)
	}
	if pullAuth != "Bearer ollama-key" {
		t.Fatalf("pull auth = %q", pullAuth)
	}
	if !strings.Contains(pullBody, `"stream":false`) {
		t.Fatalf("pull body = %s", pullBody)
	}

	cfg := app.store.Snapshot()
	if len(cfg.Models) != 2 {
		t.Fatalf("models = %d", len(cfg.Models))
	}
	if cfg.Models[0].PublicName != "gpt-oss:120b" || !cfg.Models[0].Enabled {
		t.Fatalf("first model = %#v", cfg.Models[0])
	}
}

func TestActivateDoesNotOverwriteSamePublicNameFromDifferentProvider(t *testing.T) {
	app := testApp(t, Config{
		Providers: []Provider{
			{ID: "a", Name: "Provider A", Type: "openai", BaseURL: "http://a.example/v1", Enabled: true},
			{ID: "b", Name: "Provider B", Type: "openai", BaseURL: "http://b.example/v1", Enabled: true},
		},
		Models: []ModelMapping{
			{PublicName: "shared", ProviderID: "a", UpstreamName: "shared", Enabled: true, Order: 1},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/admin/api/activate", strings.NewReader(`{
		"provider_id":"b",
		"models":["shared"],
		"pull":false,
		"enable":true
	}`))
	authorizeAdmin(req, app)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	cfg := app.store.Snapshot()
	if len(cfg.Models) != 2 {
		t.Fatalf("models = %d", len(cfg.Models))
	}
	if cfg.Models[0].PublicName != "shared" || cfg.Models[0].ProviderID != "a" {
		t.Fatalf("first model overwritten: %#v", cfg.Models[0])
	}
	if cfg.Models[1].PublicName != "shared-b" || cfg.Models[1].ProviderID != "b" {
		t.Fatalf("second model = %#v", cfg.Models[1])
	}
}

func TestRejectsClientKeyWhenNoPublicKeysConfigured(t *testing.T) {
	app := testApp(t, Config{
		Providers: []Provider{{ID: "p", Name: "Provider", Type: "openai", BaseURL: "http://example.test/v1", Enabled: true}},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	app.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestAdminAPIRequiresToken(t *testing.T) {
	app := testApp(t, Config{
		Providers: []Provider{{ID: "p", Name: "Provider", Type: "openai", BaseURL: "http://example.test/v1", Enabled: true}},
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/api/config", nil)
	rec := httptest.NewRecorder()
	app.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestNormalizeConfigRejectsUnsafeProviderURLs(t *testing.T) {
	tests := []string{
		"file:///tmp/provider",
		"https://user:pass@example.test/v1",
		"http://169.254.169.254/latest",
		"http://[fe80::1]/api",
		"http://metadata.google.internal/computeMetadata/v1",
	}
	for _, baseURL := range tests {
		t.Run(baseURL, func(t *testing.T) {
			_, err := normalizeConfig(Config{
				Providers: []Provider{{ID: "p", Name: "Provider", Type: "openai", BaseURL: baseURL, Enabled: true}},
			})
			if err == nil {
				t.Fatal("normalizeConfig accepted unsafe provider URL")
			}
		})
	}
}

func authorizeAdmin(req *http.Request, app *App) {
	req.Header.Set("Authorization", "Bearer "+app.adminToken())
}

func testApp(t *testing.T, cfg Config) *App {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	normalized, err := normalizeConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeConfigAtomic(path, normalized); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return NewApp(store, "")
}
