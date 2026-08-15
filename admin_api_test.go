package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGranularAdminAPIRequiresToken(t *testing.T) {
	app := testApp(t, Config{
		Providers: []Provider{{ID: "p", Name: "Provider", Type: "openai", BaseURL: "http://example.test/v1", Enabled: true}},
	})

	for _, path := range []string{
		"/admin/api/providers",
		"/admin/api/chains",
		"/admin/api/models",
		"/admin/api/public-api-keys",
		"/admin/api/admin-token",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		app.routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s status = %d body = %s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestProviderAPIRedactsSecretAndRenameRewritesReferences(t *testing.T) {
	app := testApp(t, Config{
		Providers: []Provider{
			{ID: "p", Name: "Provider", Type: "openai", BaseURL: "http://example.test/v1", APIKey: "super-secret", Enabled: true},
		},
		Chains: []ModelChain{
			{ID: "coding", Name: "Coding", Steps: []ChainStep{{ProviderID: "p", UpstreamName: "up"}}, Order: 1},
		},
		Models: []ModelMapping{
			{PublicName: "direct", ProviderID: "p", UpstreamName: "up", Chain: []ChainStep{{ProviderID: "p", UpstreamName: "fallback"}}, Enabled: true, Order: 1},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/api/providers/p", nil)
	authorizeAdmin(req)
	rec := httptest.NewRecorder()
	app.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d body = %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "super-secret") || !strings.Contains(rec.Body.String(), redactedSecret) {
		t.Fatalf("provider response did not redact API key: %s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPatch, "/admin/api/providers/p", strings.NewReader(`{"id":"renamed"}`))
	req.Header.Set("Content-Type", "application/json")
	authorizeAdmin(req)
	rec = httptest.NewRecorder()
	app.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH status = %d body = %s", rec.Code, rec.Body.String())
	}

	cfg := app.store.Snapshot()
	if cfg.Providers[0].ID != "renamed" || cfg.Providers[0].APIKey != "super-secret" {
		t.Fatalf("provider = %#v", cfg.Providers[0])
	}
	if cfg.Chains[0].Steps[0].ProviderID != "renamed" {
		t.Fatalf("chain reference = %#v", cfg.Chains[0].Steps[0])
	}
	if cfg.Models[0].ProviderID != "renamed" || cfg.Models[0].Chain[0].ProviderID != "renamed" {
		t.Fatalf("model references = %#v", cfg.Models[0])
	}
}

func TestChainAndModelCRUD(t *testing.T) {
	app := testApp(t, Config{
		Providers: []Provider{{ID: "p", Name: "Provider", Type: "openai", BaseURL: "http://example.test/v1", Enabled: true}},
	})

	postJSON(t, app, "/admin/api/chains", `{"id":"coding","name":"Coding","steps":[{"provider_id":"p","upstream_name":"coder"}]}`, http.StatusCreated)
	postJSON(t, app, "/admin/api/models", `{"public_name":"Direct","provider_id":"p","upstream_name":"direct","enabled":true}`, http.StatusCreated)

	req := httptest.NewRequest(http.MethodPatch, "/admin/api/chains/coding", strings.NewReader(`{"name":"Coding Prime"}`))
	req.Header.Set("Content-Type", "application/json")
	authorizeAdmin(req)
	rec := httptest.NewRecorder()
	app.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("chain PATCH status = %d body = %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPatch, "/admin/api/models/Direct", strings.NewReader(`{"enabled":false,"upstream_name":"direct-v2"}`))
	req.Header.Set("Content-Type", "application/json")
	authorizeAdmin(req)
	rec = httptest.NewRecorder()
	app.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("model PATCH status = %d body = %s", rec.Code, rec.Body.String())
	}

	cfg := app.store.Snapshot()
	if cfg.Chains[0].Name != "Coding Prime" {
		t.Fatalf("chain = %#v", cfg.Chains[0])
	}
	if cfg.Models[0].Enabled || cfg.Models[0].UpstreamName != "direct-v2" {
		t.Fatalf("model = %#v", cfg.Models[0])
	}
}

func TestDeletingReferencedProviderIsRejected(t *testing.T) {
	app := testApp(t, Config{
		Providers: []Provider{{ID: "p", Name: "Provider", Type: "openai", BaseURL: "http://example.test/v1", Enabled: true}},
		Models:    []ModelMapping{{PublicName: "Direct", ProviderID: "p", UpstreamName: "direct", Enabled: true}},
	})

	req := httptest.NewRequest(http.MethodDelete, "/admin/api/providers/p", nil)
	authorizeAdmin(req)
	rec := httptest.NewRecorder()
	app.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if _, ok := findProvider(app.store.Snapshot(), "p"); !ok {
		t.Fatal("referenced provider was deleted")
	}
}

func TestLegacyPublicKeyAliasAndScopedAPIKeys(t *testing.T) {
	app := testApp(t, Config{
		Providers: []Provider{{ID: "p", Name: "Provider", Type: "openai", BaseURL: "http://example.test/v1", Enabled: true}},
	})

	req := httptest.NewRequest(http.MethodPost, "/admin/api/public-api-keys", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	authorizeAdmin(req)
	rec := httptest.NewRecorder()
	app.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body = %s", rec.Code, rec.Body.String())
	}
	var created map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(created["key"], "rtk_") {
		t.Fatalf("generated key = %q", created["key"])
	}

	req = httptest.NewRequest(http.MethodDelete, "/admin/api/public-api-keys", strings.NewReader(`{"key":"`+created["key"]+`"}`))
	req.Header.Set("Content-Type", "application/json")
	authorizeAdmin(req)
	rec = httptest.NewRecorder()
	app.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d body = %s", rec.Code, rec.Body.String())
	}
	deletedHash := hashAPIKey(created["key"])
	for _, key := range app.store.Snapshot().APIKeys {
		if key.Hash == deletedHash {
			t.Fatalf("deleted key is still present: %#v", key)
		}
	}

	req = httptest.NewRequest(http.MethodPost, "/admin/api/api-keys", strings.NewReader(`{"name":"Test key"}`))
	req.Header.Set("Content-Type", "application/json")
	authorizeAdmin(req)
	rec = httptest.NewRecorder()
	app.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("api-keys create status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func postJSON(t *testing.T, app *App, path, body string, want int) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	authorizeAdmin(req)
	rec := httptest.NewRecorder()
	app.routes().ServeHTTP(rec, req)
	if rec.Code != want {
		t.Fatalf("POST %s status = %d body = %s", path, rec.Code, rec.Body.String())
	}
}
