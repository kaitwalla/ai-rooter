package main

import (
	"io"
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

func TestEnvironmentAdminTokenLoopbackPreflight(t *testing.T) {
	app := testApp(t, Config{
		Providers: []Provider{{ID: "p", Name: "P", Type: "openai", BaseURL: "http://example.test/v1", Enabled: true}},
	})
	app.adminTokenEnv = "env-admin-token"

	// Match the production handler composition in main: app routes wrapped by
	// the SSE flushing middleware, then exercised over a real loopback socket.
	server := httptest.NewServer(sseFlushMiddleware(app.routes()))
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/admin/api/providers", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer env-admin-token")
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("loopback status=%d body=%s", resp.StatusCode, body)
	}
}
