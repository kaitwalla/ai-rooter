package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const redactedSecret = "********"

type Config struct {
	PublicAPIKeys []string       `json:"public_api_keys"`
	AdminToken    string         `json:"admin_token"`
	Providers     []Provider     `json:"providers"`
	Models        []ModelMapping `json:"models"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

type Provider struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
	Enabled bool   `json:"enabled"`
}

type ModelMapping struct {
	PublicName   string `json:"public_name"`
	ProviderID   string `json:"provider_id"`
	UpstreamName string `json:"upstream_name"`
	Enabled      bool   `json:"enabled"`
	Order        int    `json:"order"`
}

type Store struct {
	path string
	mu   sync.RWMutex
	cfg  Config
}

func NewStore(path string) (*Store, error) {
	store := &Store{path: path}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) Snapshot() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneConfig(s.cfg)
}

func (s *Store) Replace(next Config) error {
	normalized, err := normalizeConfig(next)
	if err != nil {
		return err
	}
	normalized.UpdatedAt = time.Now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := writeConfigAtomic(s.path, normalized); err != nil {
		return err
	}
	s.cfg = normalized
	return nil
}

func (s *Store) load() error {
	if _, err := os.Stat(s.path); errors.Is(err, os.ErrNotExist) {
		cfg := Config{
			Providers: []Provider{
				{
					ID:      "local-ollama",
					Name:    "Local Ollama",
					Type:    "ollama",
					BaseURL: "http://localhost:11434/api",
					Enabled: true,
				},
			},
			Models: []ModelMapping{},
		}
		normalized, err := normalizeConfig(cfg)
		if err != nil {
			return err
		}
		normalized.UpdatedAt = time.Now().UTC()
		if err := writeConfigAtomic(s.path, normalized); err != nil {
			return err
		}
		s.cfg = normalized
		return nil
	} else if err != nil {
		return err
	}

	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("read config %s: %w", s.path, err)
	}
	normalized, err := normalizeConfig(cfg)
	if err != nil {
		return err
	}
	s.cfg = normalized
	return nil
}

func normalizeConfig(cfg Config) (Config, error) {
	cfg.AdminToken = strings.TrimSpace(cfg.AdminToken)
	cfg.PublicAPIKeys = compactUnique(cfg.PublicAPIKeys)
	if cfg.Providers == nil {
		cfg.Providers = []Provider{}
	}
	if cfg.Models == nil {
		cfg.Models = []ModelMapping{}
	}

	providerIDs := map[string]bool{}
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		p.ID = slugify(p.ID)
		p.Name = strings.TrimSpace(p.Name)
		p.Type = strings.TrimSpace(strings.ToLower(p.Type))
		p.BaseURL = strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
		p.APIKey = strings.TrimSpace(p.APIKey)
		if p.ID == "" {
			p.ID = slugify(p.Name)
		}
		if p.ID == "" {
			return cfg, fmt.Errorf("provider %d needs an id or name", i+1)
		}
		if providerIDs[p.ID] {
			return cfg, fmt.Errorf("duplicate provider id %q", p.ID)
		}
		providerIDs[p.ID] = true
		if p.Name == "" {
			p.Name = p.ID
		}
		if p.Type == "" {
			p.Type = "openai"
		}
		switch p.Type {
		case "openai", "ollama", "ollama_cloud":
		default:
			return cfg, fmt.Errorf("provider %q has unsupported type %q", p.ID, p.Type)
		}
		if p.BaseURL == "" {
			return cfg, fmt.Errorf("provider %q needs a base URL", p.ID)
		}
	}

	modelKeys := map[string]bool{}
	for i := range cfg.Models {
		m := &cfg.Models[i]
		m.PublicName = strings.TrimSpace(m.PublicName)
		m.ProviderID = slugify(m.ProviderID)
		m.UpstreamName = strings.TrimSpace(m.UpstreamName)
		if m.PublicName == "" {
			return cfg, fmt.Errorf("model row %d needs a public name", i+1)
		}
		if m.ProviderID == "" || !providerIDs[m.ProviderID] {
			return cfg, fmt.Errorf("model %q references unknown provider %q", m.PublicName, m.ProviderID)
		}
		if m.UpstreamName == "" {
			m.UpstreamName = m.PublicName
		}
		key := strings.ToLower(m.PublicName)
		if modelKeys[key] {
			return cfg, fmt.Errorf("duplicate public model name %q", m.PublicName)
		}
		modelKeys[key] = true
	}
	slices.SortStableFunc(cfg.Models, func(a, b ModelMapping) int {
		if a.Order == b.Order {
			return strings.Compare(a.PublicName, b.PublicName)
		}
		return a.Order - b.Order
	})
	for i := range cfg.Models {
		cfg.Models[i].Order = i + 1
	}
	return cfg, nil
}

func writeConfigAtomic(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func cloneConfig(cfg Config) Config {
	out := cfg
	out.PublicAPIKeys = slices.Clone(cfg.PublicAPIKeys)
	out.Providers = slices.Clone(cfg.Providers)
	out.Models = slices.Clone(cfg.Models)
	return out
}

func compactUnique(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func slugify(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	var b strings.Builder
	previousDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			previousDash = false
		case r == '-' || r == '_' || r == ' ' || r == '.':
			if !previousDash {
				b.WriteByte('-')
				previousDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}
