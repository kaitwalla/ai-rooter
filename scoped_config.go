package main

import (
	"encoding/json"
	"net/http"
)

// handleScopedConfig keeps credential management separate from bulk routing
// configuration. API keys are managed exclusively through /admin/api/api-keys,
// and provider secrets are redacted on reads and preserved on writes.
func (a *App) handleScopedConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, adminConfigView(a.store.Snapshot()))
	case http.MethodPut:
		defer r.Body.Close()
		var next Config
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&next); err != nil {
			writeProblem(w, http.StatusBadRequest, "invalid JSON", err.Error())
			return
		}

		current := a.store.Snapshot()
		// Scoped API keys are deliberately not part of bulk config import/export.
		// Preserve them even when clients send an older config document.
		next.APIKeys = current.APIKeys
		next.PublicAPIKeys = nil
		next.AdminToken = ""
		mergeRedactedSecrets(&next, current)
		if err := a.store.Replace(next); err != nil {
			writeProblem(w, http.StatusBadRequest, "invalid config", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, adminConfigView(a.store.Snapshot()))
	default:
		w.Header().Set("Allow", "GET, PUT")
		writeProblem(w, http.StatusMethodNotAllowed, "method not allowed", "")
	}
}

func adminConfigView(cfg Config) Config {
	view := cloneConfig(cfg)
	view.PublicAPIKeys = nil
	view.AdminToken = ""
	view.APIKeys = nil
	for i := range view.Providers {
		if view.Providers[i].APIKey != "" {
			view.Providers[i].APIKey = redactedSecret
		}
	}
	return view
}
