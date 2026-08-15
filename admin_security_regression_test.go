package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestBulkConfigRedactsSecretsAndPreservesScopedKeys(t *testing.T) {
	app := testApp(t, Config{
		PublicAPIKeys: []string{"client-key"},
		Providers: []Provider{{
			ID: "p", Name: "Provider", Type: "openai", BaseURL: "http://example.test/v1", APIKey: "provider-secret", Enabled: true,
		}},
		Models: []ModelMapping{{PublicName: "m", ProviderID: "p", UpstreamName: "m", Enabled: true}},
	})

	before := app.store.Snapshot()
	if len(before.APIKeys) < 2 { // migrated client key + test admin key
		t.Fatalf("expected migrated scoped keys, got %d", len(before.APIKeys))
	}

	getReq := httptest.NewRequest(http.MethodGet, "/admin/api/config", nil)
	authorizeAdmin(getReq)
	getRec := httptest.NewRecorder()
	app.routes().ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET config status = %d body = %s", getRec.Code, getRec.Body.String())
	}
	body := getRec.Body.String()
	if strings.Contains(body, "provider-secret") {
		t.Fatal("bulk config leaked provider secret")
	}
	for _, key := range before.APIKeys {
		if strings.Contains(body, key.Hash) {
			t.Fatal("bulk config leaked API key hash")
		}
	}

	var exported Config
	if err := json.Unmarshal(getRec.Body.Bytes(), &exported); err != nil {
		t.Fatal(err)
	}
	if len(exported.APIKeys) != 0 {
		t.Fatalf("bulk config returned %d API keys", len(exported.APIKeys))
	}
	if got := exported.Providers[0].APIKey; got != redactedSecret {
		t.Fatalf("provider API key = %q, want redaction", got)
	}
	exported.Providers[0].Name = "Renamed Provider"
	payload, err := json.Marshal(exported)
	if err != nil {
		t.Fatal(err)
	}
	putReq := httptest.NewRequest(http.MethodPut, "/admin/api/config", strings.NewReader(string(payload)))
	putReq.Header.Set("Content-Type", "application/json")
	authorizeAdmin(putReq)
	putRec := httptest.NewRecorder()
	app.routes().ServeHTTP(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("PUT config status = %d body = %s", putRec.Code, putRec.Body.String())
	}
	after := app.store.Snapshot()
	if len(after.APIKeys) != len(before.APIKeys) {
		t.Fatalf("API keys changed after bulk config PUT: before=%d after=%d", len(before.APIKeys), len(after.APIKeys))
	}
	if after.Providers[0].APIKey != "provider-secret" {
		t.Fatalf("provider secret was not preserved: %q", after.Providers[0].APIKey)
	}
}

func TestKeyPatchCannotEscalateBeyondCallerPermissions(t *testing.T) {
	limitedRaw := "limited-key"
	limited := createStoredAPIKey(limitedRaw, "Limited key manager", []string{permKeysRead, permKeysWrite}, nil)
	target := createStoredAPIKey("target-key", "Target", []string{permModelsRead}, nil)
	app := testApp(t, Config{APIKeys: []APIKey{limited, target}})

	req := httptest.NewRequest(http.MethodPatch, "/admin/api/api-keys/"+target.ID, strings.NewReader(`{"permissions":["keys.write","config.write"]}`))
	req.Header.Set("Authorization", "Bearer "+limitedRaw)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("PATCH status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestScopedLastUsedPersistenceDoesNotRaceAdminMutations(t *testing.T) {
	app := testApp(t, Config{
		PublicAPIKeys: []string{"client-key"},
		Providers: []Provider{{ID: "p", Name: "Provider", Type: "openai", BaseURL: "http://example.test/v1", Enabled: true}},
		Models: []ModelMapping{{PublicName: "m", ProviderID: "p", UpstreamName: "m", Enabled: true}},
	})

	var wg sync.WaitGroup
	errs := make(chan string, 64)
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
			req.Header.Set("Authorization", "Bearer client-key")
			rec := httptest.NewRecorder()
			app.routes().ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				errs <- fmt.Sprintf("models status=%d body=%s", rec.Code, rec.Body.String())
			}
		}()
		go func(i int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPatch, "/admin/api/providers/p", strings.NewReader(fmt.Sprintf(`{"name":"Provider %d"}`, i)))
			req.Header.Set("Content-Type", "application/json")
			authorizeAdmin(req)
			rec := httptest.NewRecorder()
			app.routes().ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				errs <- fmt.Sprintf("provider PATCH status=%d body=%s", rec.Code, rec.Body.String())
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
