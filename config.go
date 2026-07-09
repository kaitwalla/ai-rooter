package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
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
	Chains        []ModelChain   `json:"chains,omitempty"`
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
	PublicName   string      `json:"public_name"`
	ProviderID   string      `json:"provider_id"`
	UpstreamName string      `json:"upstream_name"`
	ChainID      string      `json:"chain_id,omitempty"`
	Chain        []ChainStep `json:"chain,omitempty"`
	Enabled      bool        `json:"enabled"`
	Order        int         `json:"order"`
}

type ModelChain struct {
	ID    string      `json:"id"`
	Name  string      `json:"name"`
	Steps []ChainStep `json:"steps"`
	Order int         `json:"order"`
}

type ChainStep struct {
	ProviderID   string `json:"provider_id"`
	UpstreamName string `json:"upstream_name"`
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
			AdminToken: generateAdminToken(),
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
	if normalized.AdminToken != cfg.AdminToken {
		if err := writeConfigAtomic(s.path, normalized); err != nil {
			return err
		}
	}
	s.cfg = normalized
	return nil
}

func normalizeConfig(cfg Config) (Config, error) {
	cfg.AdminToken = strings.TrimSpace(cfg.AdminToken)
	if cfg.AdminToken == "" {
		cfg.AdminToken = generateAdminToken()
	}
	cfg.PublicAPIKeys = compactUnique(cfg.PublicAPIKeys)
	if cfg.Providers == nil {
		cfg.Providers = []Provider{}
	}
	if cfg.Chains == nil {
		cfg.Chains = []ModelChain{}
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
		if err := validateProviderBaseURL(p.BaseURL); err != nil {
			return cfg, fmt.Errorf("provider %q has invalid base URL: %w", p.ID, err)
		}
	}

	chainIDs := map[string]bool{}
	modelKeys := map[string]bool{}
	for i := range cfg.Chains {
		c := &cfg.Chains[i]
		c.ID = slugify(c.ID)
		c.Name = strings.TrimSpace(c.Name)
		if c.ID == "" {
			c.ID = slugify(c.Name)
		}
		if c.ID == "" {
			return cfg, fmt.Errorf("chain %d needs an id or name", i+1)
		}
		if chainIDs[c.ID] {
			return cfg, fmt.Errorf("duplicate chain id %q", c.ID)
		}
		chainIDs[c.ID] = true
		if c.Name == "" {
			c.Name = c.ID
		}
		key := strings.ToLower(c.Name)
		if modelKeys[key] {
			return cfg, fmt.Errorf("duplicate public model name %q", c.Name)
		}
		modelKeys[key] = true
		for j := range c.Steps {
			step := &c.Steps[j]
			step.ProviderID = slugify(step.ProviderID)
			step.UpstreamName = strings.TrimSpace(step.UpstreamName)
			if step.ProviderID == "" || !providerIDs[step.ProviderID] {
				return cfg, fmt.Errorf("chain %q step %d references unknown provider %q", c.Name, j+1, step.ProviderID)
			}
			if step.UpstreamName == "" {
				return cfg, fmt.Errorf("chain %q step %d needs an upstream model", c.Name, j+1)
			}
		}
	}
	slices.SortStableFunc(cfg.Chains, func(a, b ModelChain) int {
		if a.Order == b.Order {
			return strings.Compare(a.Name, b.Name)
		}
		return a.Order - b.Order
	})
	for i := range cfg.Chains {
		cfg.Chains[i].Order = i + 1
	}

	for i := range cfg.Models {
		m := &cfg.Models[i]
		m.PublicName = strings.TrimSpace(m.PublicName)
		m.ProviderID = slugify(m.ProviderID)
		m.UpstreamName = strings.TrimSpace(m.UpstreamName)
		m.ChainID = slugify(m.ChainID)
		if m.PublicName == "" {
			return cfg, fmt.Errorf("model row %d needs a public name", i+1)
		}
		if m.ChainID != "" && !chainIDs[m.ChainID] {
			return cfg, fmt.Errorf("model %q references unknown chain %q", m.PublicName, m.ChainID)
		}
		if m.ChainID != "" && (m.ProviderID == "" || m.UpstreamName == "") {
			if chain, ok := findConfigChain(cfg.Chains, m.ChainID); ok && len(chain.Steps) > 0 {
				m.ProviderID = chain.Steps[0].ProviderID
				m.UpstreamName = chain.Steps[0].UpstreamName
			}
		}
		if m.ProviderID == "" || !providerIDs[m.ProviderID] {
			return cfg, fmt.Errorf("model %q references unknown provider %q", m.PublicName, m.ProviderID)
		}
		if m.UpstreamName == "" {
			m.UpstreamName = m.PublicName
		}
		for j := range m.Chain {
			step := &m.Chain[j]
			step.ProviderID = slugify(step.ProviderID)
			step.UpstreamName = strings.TrimSpace(step.UpstreamName)
			if step.ProviderID == "" || !providerIDs[step.ProviderID] {
				return cfg, fmt.Errorf("model %q chain step %d references unknown provider %q", m.PublicName, j+1, step.ProviderID)
			}
			if step.UpstreamName == "" {
				return cfg, fmt.Errorf("model %q chain step %d needs an upstream model", m.PublicName, j+1)
			}
		}
		key := strings.ToLower(m.PublicName)
		if modelKeys[key] {
			if m.ChainID != "" {
				continue
			}
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

func generateAdminToken() string {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		panic(fmt.Sprintf("generate admin token: %v", err))
	}
	return "rta_" + hex.EncodeToString(bytes[:])
}

func validateProviderBaseURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("scheme must be http or https")
	}
	if parsed.Host == "" || parsed.Hostname() == "" {
		return errors.New("host is required")
	}
	if parsed.User != nil {
		return errors.New("credentials in URLs are not allowed")
	}
	host := parsed.Hostname()
	if isBlockedLinkLocalHost(host) {
		return errors.New("link-local metadata addresses are not allowed")
	}
	return nil
}

func isBlockedLinkLocalHost(host string) bool {
	if strings.EqualFold(host, "metadata.google.internal") {
		return true
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return isLinkLocalMetadataIP(ip)
	}
	return false
}

func isLinkLocalMetadataIP(ip netip.Addr) bool {
	return ip.IsLinkLocalUnicast()
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
	out.Chains = slices.Clone(cfg.Chains)
	for i := range out.Chains {
		out.Chains[i].Steps = slices.Clone(cfg.Chains[i].Steps)
	}
	out.Models = slices.Clone(cfg.Models)
	for i := range out.Models {
		out.Models[i].Chain = slices.Clone(cfg.Models[i].Chain)
	}
	return out
}

func findConfigChain(chains []ModelChain, id string) (ModelChain, bool) {
	id = slugify(id)
	for _, chain := range chains {
		if chain.ID == id {
			return chain, true
		}
	}
	return ModelChain{}, false
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
