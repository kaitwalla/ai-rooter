package main

import (
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

var (
	errAdminNotFound = errors.New("resource not found")
	errAdminConflict = errors.New("resource already exists")
)

type providerPatch struct {
	ID      *string `json:"id"`
	Name    *string `json:"name"`
	Type    *string `json:"type"`
	BaseURL *string `json:"base_url"`
	APIKey  *string `json:"api_key"`
	Enabled *bool   `json:"enabled"`
}

type chainPatch struct {
	ID    *string      `json:"id"`
	Name  *string      `json:"name"`
	Steps *[]ChainStep `json:"steps"`
	Order *int         `json:"order"`
}

type modelPatch struct {
	PublicName   *string      `json:"public_name"`
	ProviderID   *string      `json:"provider_id"`
	UpstreamName *string      `json:"upstream_name"`
	ChainID      *string      `json:"chain_id"`
	Chain        *[]ChainStep `json:"chain"`
	Enabled      *bool        `json:"enabled"`
	Order        *int         `json:"order"`
}

func (a *App) registerAdminAPI(mux *http.ServeMux) {
	mux.HandleFunc("/admin/api/providers", a.requireAdmin(a.handleAdminProviders))
	mux.HandleFunc("/admin/api/providers/", a.requireAdmin(a.handleAdminProvider))
	mux.HandleFunc("/admin/api/chains", a.requireAdmin(a.handleAdminChains))
	mux.HandleFunc("/admin/api/chains/", a.requireAdmin(a.handleAdminChain))
	mux.HandleFunc("/admin/api/models", a.requireAdmin(a.handleAdminModels))
	mux.HandleFunc("/admin/api/models/", a.requireAdmin(a.handleAdminModel))
}

func (a *App) handleAdminProviders(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := a.store.Snapshot()
		providers := slices.Clone(cfg.Providers)
		for i := range providers {
			if providers[i].APIKey != "" {
				providers[i].APIKey = redactedSecret
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": providers})
	case http.MethodPost:
		defer r.Body.Close()
		var provider Provider
		if err := decodeAdminJSON(w, r, &provider); err != nil {
			writeProblem(w, http.StatusBadRequest, "invalid JSON", err.Error())
			return
		}
		cfg, err := a.store.Update(func(cfg *Config) error {
			id := slugify(provider.ID)
			if id == "" {
				id = slugify(provider.Name)
			}
			if _, ok := findProvider(*cfg, id); ok {
				return fmt.Errorf("%w: provider %q", errAdminConflict, id)
			}
			cfg.Providers = append(cfg.Providers, provider)
			return nil
		})
		if err != nil {
			writeAdminMutationError(w, err)
			return
		}
		id := slugify(provider.ID)
		if id == "" {
			id = slugify(provider.Name)
		}
		created, _ := findProvider(cfg, id)
		writeJSON(w, http.StatusCreated, redactProvider(created))
	default:
		w.Header().Set("Allow", "GET, POST")
		writeProblem(w, http.StatusMethodNotAllowed, "method not allowed", "")
	}
}

func (a *App) handleAdminProvider(w http.ResponseWriter, r *http.Request) {
	id, err := pathResourceID(r.URL, "/admin/api/providers/")
	if err != nil {
		writeProblem(w, http.StatusNotFound, "provider not found", "")
		return
	}
	cfg := a.store.Snapshot()
	current, ok := findProvider(cfg, id)
	if !ok {
		writeProblem(w, http.StatusNotFound, "provider not found", id)
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, redactProvider(current))
	case http.MethodPut:
		defer r.Body.Close()
		var replacement Provider
		if err := decodeAdminJSON(w, r, &replacement); err != nil {
			writeProblem(w, http.StatusBadRequest, "invalid JSON", err.Error())
			return
		}
		if replacement.ID == "" {
			replacement.ID = current.ID
		}
		if replacement.APIKey == redactedSecret {
			replacement.APIKey = current.APIKey
		}
		updated, err := a.replaceProvider(id, replacement)
		if err != nil {
			writeAdminMutationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, redactProvider(updated))
	case http.MethodPatch:
		defer r.Body.Close()
		var patch providerPatch
		if err := decodeAdminJSON(w, r, &patch); err != nil {
			writeProblem(w, http.StatusBadRequest, "invalid JSON", err.Error())
			return
		}
		updated, err := a.patchProvider(id, patch)
		if err != nil {
			writeAdminMutationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, redactProvider(updated))
	case http.MethodDelete:
		_, err := a.store.Update(func(cfg *Config) error {
			index := providerIndex(*cfg, id)
			if index < 0 {
				return fmt.Errorf("%w: provider %q", errAdminNotFound, id)
			}
			for _, chain := range cfg.Chains {
				for _, step := range chain.Steps {
					if slugify(step.ProviderID) == id {
						return fmt.Errorf("provider %q is still referenced by chain %q", id, chain.Name)
					}
				}
			}
			for _, model := range cfg.Models {
				if slugify(model.ProviderID) == id {
					return fmt.Errorf("provider %q is still referenced by model %q", id, model.PublicName)
				}
				for _, step := range model.Chain {
					if slugify(step.ProviderID) == id {
						return fmt.Errorf("provider %q is still referenced by model %q chain", id, model.PublicName)
					}
				}
			}
			cfg.Providers = append(cfg.Providers[:index], cfg.Providers[index+1:]...)
			return nil
		})
		if err != nil {
			writeAdminMutationError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, PUT, PATCH, DELETE")
		writeProblem(w, http.StatusMethodNotAllowed, "method not allowed", "")
	}
}

func (a *App) replaceProvider(id string, replacement Provider) (Provider, error) {
	cfg, err := a.store.Update(func(cfg *Config) error {
		index := providerIndex(*cfg, id)
		if index < 0 {
			return fmt.Errorf("%w: provider %q", errAdminNotFound, id)
		}
		newID := slugify(replacement.ID)
		if newID != id && providerIndex(*cfg, newID) >= 0 {
			return fmt.Errorf("%w: provider %q", errAdminConflict, newID)
		}
		if newID != id {
			rewriteProviderReferences(cfg, id, newID)
		}
		cfg.Providers[index] = replacement
		return nil
	})
	if err != nil {
		return Provider{}, err
	}
	provider, ok := findProvider(cfg, replacement.ID)
	if !ok {
		return Provider{}, errAdminNotFound
	}
	return provider, nil
}

func (a *App) patchProvider(id string, patch providerPatch) (Provider, error) {
	var resolvedID string
	cfg, err := a.store.Update(func(cfg *Config) error {
		index := providerIndex(*cfg, id)
		if index < 0 {
			return fmt.Errorf("%w: provider %q", errAdminNotFound, id)
		}
		provider := cfg.Providers[index]
		if patch.ID != nil {
			provider.ID = *patch.ID
		}
		if patch.Name != nil {
			provider.Name = *patch.Name
		}
		if patch.Type != nil {
			provider.Type = *patch.Type
		}
		if patch.BaseURL != nil {
			provider.BaseURL = *patch.BaseURL
		}
		if patch.APIKey != nil && *patch.APIKey != redactedSecret {
			provider.APIKey = *patch.APIKey
		}
		if patch.Enabled != nil {
			provider.Enabled = *patch.Enabled
		}
		newID := slugify(provider.ID)
		if newID == "" {
			newID = slugify(provider.Name)
		}
		if newID != id && providerIndex(*cfg, newID) >= 0 {
			return fmt.Errorf("%w: provider %q", errAdminConflict, newID)
		}
		if newID != id {
			rewriteProviderReferences(cfg, id, newID)
		}
		cfg.Providers[index] = provider
		resolvedID = newID
		return nil
	})
	if err != nil {
		return Provider{}, err
	}
	provider, ok := findProvider(cfg, resolvedID)
	if !ok {
		return Provider{}, errAdminNotFound
	}
	return provider, nil
}

func (a *App) handleAdminChains(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": a.store.Snapshot().Chains})
	case http.MethodPost:
		defer r.Body.Close()
		var chain ModelChain
		if err := decodeAdminJSON(w, r, &chain); err != nil {
			writeProblem(w, http.StatusBadRequest, "invalid JSON", err.Error())
			return
		}
		cfg, err := a.store.Update(func(cfg *Config) error {
			id := slugify(chain.ID)
			if id == "" {
				id = slugify(chain.Name)
			}
			if chainIndex(*cfg, id) >= 0 {
				return fmt.Errorf("%w: chain %q", errAdminConflict, id)
			}
			if chain.Order <= 0 {
				chain.Order = len(cfg.Chains) + 1
			}
			cfg.Chains = append(cfg.Chains, chain)
			return nil
		})
		if err != nil {
			writeAdminMutationError(w, err)
			return
		}
		id := slugify(chain.ID)
		if id == "" {
			id = slugify(chain.Name)
		}
		created, _ := findChainByID(cfg, id)
		writeJSON(w, http.StatusCreated, created)
	default:
		w.Header().Set("Allow", "GET, POST")
		writeProblem(w, http.StatusMethodNotAllowed, "method not allowed", "")
	}
}

func (a *App) handleAdminChain(w http.ResponseWriter, r *http.Request) {
	id, err := pathResourceID(r.URL, "/admin/api/chains/")
	if err != nil {
		writeProblem(w, http.StatusNotFound, "chain not found", "")
		return
	}
	current, ok := findChainByID(a.store.Snapshot(), id)
	if !ok {
		writeProblem(w, http.StatusNotFound, "chain not found", id)
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, current)
	case http.MethodPut:
		defer r.Body.Close()
		var replacement ModelChain
		if err := decodeAdminJSON(w, r, &replacement); err != nil {
			writeProblem(w, http.StatusBadRequest, "invalid JSON", err.Error())
			return
		}
		if replacement.ID == "" {
			replacement.ID = current.ID
		}
		if replacement.Order <= 0 {
			replacement.Order = current.Order
		}
		updated, err := a.replaceChain(id, replacement)
		if err != nil {
			writeAdminMutationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, updated)
	case http.MethodPatch:
		defer r.Body.Close()
		var patch chainPatch
		if err := decodeAdminJSON(w, r, &patch); err != nil {
			writeProblem(w, http.StatusBadRequest, "invalid JSON", err.Error())
			return
		}
		updated, err := a.patchChain(id, patch)
		if err != nil {
			writeAdminMutationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, updated)
	case http.MethodDelete:
		_, err := a.store.Update(func(cfg *Config) error {
			index := chainIndex(*cfg, id)
			if index < 0 {
				return fmt.Errorf("%w: chain %q", errAdminNotFound, id)
			}
			for _, model := range cfg.Models {
				if slugify(model.ChainID) == id {
					return fmt.Errorf("chain %q is still referenced by model %q", id, model.PublicName)
				}
			}
			cfg.Chains = append(cfg.Chains[:index], cfg.Chains[index+1:]...)
			return nil
		})
		if err != nil {
			writeAdminMutationError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, PUT, PATCH, DELETE")
		writeProblem(w, http.StatusMethodNotAllowed, "method not allowed", "")
	}
}

func (a *App) replaceChain(id string, replacement ModelChain) (ModelChain, error) {
	cfg, err := a.store.Update(func(cfg *Config) error {
		index := chainIndex(*cfg, id)
		if index < 0 {
			return fmt.Errorf("%w: chain %q", errAdminNotFound, id)
		}
		newID := slugify(replacement.ID)
		if newID == "" {
			newID = slugify(replacement.Name)
		}
		if newID != id && chainIndex(*cfg, newID) >= 0 {
			return fmt.Errorf("%w: chain %q", errAdminConflict, newID)
		}
		if newID != id {
			for i := range cfg.Models {
				if slugify(cfg.Models[i].ChainID) == id {
					cfg.Models[i].ChainID = newID
				}
			}
		}
		cfg.Chains[index] = replacement
		return nil
	})
	if err != nil {
		return ModelChain{}, err
	}
	chain, ok := findChainByID(cfg, replacement.ID)
	if !ok {
		return ModelChain{}, errAdminNotFound
	}
	return chain, nil
}

func (a *App) patchChain(id string, patch chainPatch) (ModelChain, error) {
	newID := id
	cfg, err := a.store.Update(func(cfg *Config) error {
		index := chainIndex(*cfg, id)
		if index < 0 {
			return fmt.Errorf("%w: chain %q", errAdminNotFound, id)
		}
		chain := cfg.Chains[index]
		if patch.ID != nil {
			chain.ID = *patch.ID
		}
		if patch.Name != nil {
			chain.Name = *patch.Name
		}
		if patch.Steps != nil {
			chain.Steps = *patch.Steps
		}
		if patch.Order != nil {
			chain.Order = *patch.Order
		}
		newID = slugify(chain.ID)
		if newID == "" {
			newID = slugify(chain.Name)
		}
		if newID != id && chainIndex(*cfg, newID) >= 0 {
			return fmt.Errorf("%w: chain %q", errAdminConflict, newID)
		}
		if newID != id {
			for i := range cfg.Models {
				if slugify(cfg.Models[i].ChainID) == id {
					cfg.Models[i].ChainID = newID
				}
			}
		}
		cfg.Chains[index] = chain
		return nil
	})
	if err != nil {
		return ModelChain{}, err
	}
	chain, ok := findChainByID(cfg, newID)
	if !ok {
		return ModelChain{}, errAdminNotFound
	}
	return chain, nil
}

func (a *App) handleAdminModels(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": a.store.Snapshot().Models})
	case http.MethodPost:
		defer r.Body.Close()
		var model ModelMapping
		if err := decodeAdminJSON(w, r, &model); err != nil {
			writeProblem(w, http.StatusBadRequest, "invalid JSON", err.Error())
			return
		}
		cfg, err := a.store.Update(func(cfg *Config) error {
			if modelIndex(*cfg, model.PublicName) >= 0 || publicNameUsedByChain(*cfg, model.PublicName) {
				return fmt.Errorf("%w: model %q", errAdminConflict, model.PublicName)
			}
			if model.Order <= 0 {
				model.Order = len(cfg.Models) + 1
			}
			cfg.Models = append(cfg.Models, model)
			return nil
		})
		if err != nil {
			writeAdminMutationError(w, err)
			return
		}
		created, ok := findModelByPublicName(cfg, strings.TrimSpace(model.PublicName))
		if !ok {
			writeProblem(w, http.StatusInternalServerError, "model creation failed", "")
			return
		}
		writeJSON(w, http.StatusCreated, created)
	default:
		w.Header().Set("Allow", "GET, POST")
		writeProblem(w, http.StatusMethodNotAllowed, "method not allowed", "")
	}
}

func (a *App) handleAdminModel(w http.ResponseWriter, r *http.Request) {
	name, err := pathResourceID(r.URL, "/admin/api/models/")
	if err != nil {
		writeProblem(w, http.StatusNotFound, "model not found", "")
		return
	}
	current, ok := findModelByPublicName(a.store.Snapshot(), name)
	if !ok {
		writeProblem(w, http.StatusNotFound, "model not found", name)
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, current)
	case http.MethodPut:
		defer r.Body.Close()
		var replacement ModelMapping
		if err := decodeAdminJSON(w, r, &replacement); err != nil {
			writeProblem(w, http.StatusBadRequest, "invalid JSON", err.Error())
			return
		}
		if strings.TrimSpace(replacement.PublicName) == "" {
			replacement.PublicName = current.PublicName
		}
		if replacement.Order <= 0 {
			replacement.Order = current.Order
		}
		updated, err := a.replaceModel(name, replacement)
		if err != nil {
			writeAdminMutationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, updated)
	case http.MethodPatch:
		defer r.Body.Close()
		var patch modelPatch
		if err := decodeAdminJSON(w, r, &patch); err != nil {
			writeProblem(w, http.StatusBadRequest, "invalid JSON", err.Error())
			return
		}
		updated, err := a.patchModel(name, patch)
		if err != nil {
			writeAdminMutationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, updated)
	case http.MethodDelete:
		_, err := a.store.Update(func(cfg *Config) error {
			index := modelIndex(*cfg, name)
			if index < 0 {
				return fmt.Errorf("%w: model %q", errAdminNotFound, name)
			}
			cfg.Models = append(cfg.Models[:index], cfg.Models[index+1:]...)
			return nil
		})
		if err != nil {
			writeAdminMutationError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, PUT, PATCH, DELETE")
		writeProblem(w, http.StatusMethodNotAllowed, "method not allowed", "")
	}
}

func (a *App) replaceModel(name string, replacement ModelMapping) (ModelMapping, error) {
	cfg, err := a.store.Update(func(cfg *Config) error {
		index := modelIndex(*cfg, name)
		if index < 0 {
			return fmt.Errorf("%w: model %q", errAdminNotFound, name)
		}
		if replacement.PublicName != name && (modelIndex(*cfg, replacement.PublicName) >= 0 || publicNameUsedByChain(*cfg, replacement.PublicName)) {
			return fmt.Errorf("%w: model %q", errAdminConflict, replacement.PublicName)
		}
		cfg.Models[index] = replacement
		return nil
	})
	if err != nil {
		return ModelMapping{}, err
	}
	model, ok := findModelByPublicName(cfg, replacement.PublicName)
	if !ok {
		return ModelMapping{}, errAdminNotFound
	}
	return model, nil
}

func (a *App) patchModel(name string, patch modelPatch) (ModelMapping, error) {
	lookup := name
	cfg, err := a.store.Update(func(cfg *Config) error {
		index := modelIndex(*cfg, name)
		if index < 0 {
			return fmt.Errorf("%w: model %q", errAdminNotFound, name)
		}
		model := cfg.Models[index]
		if patch.PublicName != nil {
			model.PublicName = *patch.PublicName
		}
		if patch.ProviderID != nil {
			model.ProviderID = *patch.ProviderID
		}
		if patch.UpstreamName != nil {
			model.UpstreamName = *patch.UpstreamName
		}
		if patch.ChainID != nil {
			model.ChainID = *patch.ChainID
		}
		if patch.Chain != nil {
			model.Chain = *patch.Chain
		}
		if patch.Enabled != nil {
			model.Enabled = *patch.Enabled
		}
		if patch.Order != nil {
			model.Order = *patch.Order
		}
		lookup = strings.TrimSpace(model.PublicName)
		if lookup != name && (modelIndex(*cfg, lookup) >= 0 || publicNameUsedByChain(*cfg, lookup)) {
			return fmt.Errorf("%w: model %q", errAdminConflict, lookup)
		}
		cfg.Models[index] = model
		return nil
	})
	if err != nil {
		return ModelMapping{}, err
	}
	model, ok := findModelByPublicName(cfg, lookup)
	if !ok {
		return ModelMapping{}, errAdminNotFound
	}
	return model, nil
}

func (s *Store) Update(mutate func(*Config) error) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := cloneConfig(s.cfg)
	if err := mutate(&next); err != nil {
		return Config{}, err
	}
	normalized, err := normalizeConfig(next)
	if err != nil {
		return Config{}, err
	}
	normalized.UpdatedAt = time.Now().UTC()
	if err := writeConfigAtomic(s.path, normalized); err != nil {
		return Config{}, err
	}
	s.cfg = normalized
	return cloneConfig(normalized), nil
}

func rewriteProviderReferences(cfg *Config, oldID, newID string) {
	for i := range cfg.Chains {
		for j := range cfg.Chains[i].Steps {
			if slugify(cfg.Chains[i].Steps[j].ProviderID) == oldID {
				cfg.Chains[i].Steps[j].ProviderID = newID
			}
		}
	}
	for i := range cfg.Models {
		if slugify(cfg.Models[i].ProviderID) == oldID {
			cfg.Models[i].ProviderID = newID
		}
		for j := range cfg.Models[i].Chain {
			if slugify(cfg.Models[i].Chain[j].ProviderID) == oldID {
				cfg.Models[i].Chain[j].ProviderID = newID
			}
		}
	}
}

func providerIndex(cfg Config, id string) int {
	id = slugify(id)
	for i := range cfg.Providers {
		if cfg.Providers[i].ID == id {
			return i
		}
	}
	return -1
}

func chainIndex(cfg Config, id string) int {
	id = slugify(id)
	for i := range cfg.Chains {
		if cfg.Chains[i].ID == id {
			return i
		}
	}
	return -1
}

func modelIndex(cfg Config, name string) int {
	name = strings.TrimSpace(name)
	for i := range cfg.Models {
		if cfg.Models[i].PublicName == name {
			return i
		}
	}
	return -1
}

func findChainByID(cfg Config, id string) (ModelChain, bool) {
	index := chainIndex(cfg, id)
	if index < 0 {
		return ModelChain{}, false
	}
	return cfg.Chains[index], true
}

func findModelByPublicName(cfg Config, name string) (ModelMapping, bool) {
	index := modelIndex(cfg, name)
	if index < 0 {
		return ModelMapping{}, false
	}
	return cfg.Models[index], true
}

func publicNameUsedByChain(cfg Config, name string) bool {
	name = strings.TrimSpace(name)
	for _, chain := range cfg.Chains {
		if chain.Name == name {
			return true
		}
	}
	return false
}

func redactProvider(provider Provider) Provider {
	if provider.APIKey != "" {
		provider.APIKey = redactedSecret
	}
	return provider
}

func pathResourceID(u *url.URL, prefix string) (string, error) {
	escaped := u.EscapedPath()
	if !strings.HasPrefix(escaped, prefix) {
		return "", errAdminNotFound
	}
	value := strings.TrimPrefix(escaped, prefix)
	if value == "" || strings.Contains(value, "/") {
		return "", errAdminNotFound
	}
	decoded, err := url.PathUnescape(value)
	if err != nil {
		return "", err
	}
	decoded = strings.TrimSpace(decoded)
	if decoded == "" {
		return "", errAdminNotFound
	}
	return decoded, nil
}

func decodeAdminJSON(w http.ResponseWriter, r *http.Request, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request body must contain exactly one JSON value")
		}
		return err
	}
	return nil
}

func writeAdminMutationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errAdminNotFound):
		writeProblem(w, http.StatusNotFound, "not found", err.Error())
	case errors.Is(err, errAdminConflict):
		writeProblem(w, http.StatusConflict, "conflict", err.Error())
	default:
		writeProblem(w, http.StatusBadRequest, "invalid configuration", err.Error())
	}
}
