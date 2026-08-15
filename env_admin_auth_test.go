package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEnvironmentAdminTokenSurvivesScopedBridge(t *testing.T) {
	app := testApp(t, Config{
		Providers: []Provider{{ID: "p", Name: "P", Type: "openai", BaseURL: "http://example.test/v1", Enabled: true}},
	})
	app.adminTokenEnv = "env-admin-token"

	req := httptest.NewRequest(http.MethodGet, "/admin/api/providers", nil)
	req.Header.Set("Authorization", "Bearer env-admin-token")
	rec := httptest.NewRecorder()
	app.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin endpoint status=%d body=%s", rec.Code, rec.Body.String())
	}

	// The environment-managed admin token remains a full-access credential for
	// the OpenAI-compatible surface too. That path still uses the internal public
	// sentinel after the scoped middleware authenticates the environment token.
	req = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer env-admin-token")
	rec = httptest.NewRecorder()
	app.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("models endpoint status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestWrongEnvironmentAdminTokenIsRejected(t *testing.T) {
	app := testApp(t, Config{
		Providers: []Provider{{ID: "p", Name: "P", Type: "openai", BaseURL: "http://example.test/v1", Enabled: true}},
	})
	app.adminTokenEnv = "env-admin-token"

	req := httptest.NewRequest(http.MethodGet, "/admin/api/providers", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()
	app.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want 401 body=%s", rec.Code, rec.Body.String())
	}
}
