package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	ActiveModel string        `toml:"active_model"`
	Models      []ModelConfig `toml:"models"`
	Guard       GuardConfig   `toml:"guard"`
	TUI         TUIConfig     `toml:"tui"`
	Hooks       []HookConfig  `toml:"hooks"`
	Locale      string        `toml:"locale"`
	DataDir     string        `toml:"-"`
}

type ModelConfig struct {
	Provider      string   `toml:"provider"`
	Model         string   `toml:"model"`
	BaseURL       string   `toml:"base_url,omitempty"`
	ContextWindow int      `toml:"context_window,omitempty"`
	CostPer1K     float64  `toml:"cost_per_1k,omitempty"`
	Strengths     []string `toml:"strengths,omitempty"`
	APIKey        string   `toml:"-"`
}

type GuardConfig struct {
	Enabled     bool             `toml:"enabled"`
	ReviewModel string           `toml:"review_model"`
	Blocked     []GuardRule      `toml:"blocked"`
	Allowed     []GuardAllowRule `toml:"allowed"`
}

type GuardRule struct {
	Pattern string `toml:"pattern"`
	Reason  string `toml:"reason"`
}

type GuardAllowRule struct {
	Pattern string `toml:"pattern"`
	Tool    string `toml:"tool"`
	Reason  string `toml:"reason"`
}

type TUIConfig struct {
	Theme string `toml:"theme"`
}

type HookConfig struct {
	Event   string `toml:"event"`
	Tool    string `toml:"tool"`
	Command string `toml:"command"`
}

type credentialsFile map[string]struct {
	APIKey string `toml:"api_key"`
}

func Load(path string) (*Config, error) {
	cfg := &Config{TUI: TUIConfig{Theme: "dark"}, Locale: "en"}
	homeDir, _ := os.UserHomeDir()
	cfg.DataDir = filepath.Join(homeDir, ".suna")

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("config file not found: %s\nPlease create ~/.suna/config.toml with active_model and [[models]] entries", path)
	}
	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if len(cfg.Models) == 0 {
		return nil, fmt.Errorf("config has no [[models]] entries")
	}
	if cfg.ActiveModel == "" {
		cfg.ActiveModel = cfg.Models[0].Ref()
	}
	if _, ok := cfg.ModelByRef(cfg.ActiveModel); !ok {
		return nil, fmt.Errorf("active_model %q not found in [[models]]", cfg.ActiveModel)
	}
	if err := LoadCredentials(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func NeedsSetup(path string) bool {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return true
	}
	cfg, err := Load(path)
	return err != nil || len(cfg.Models) == 0
}

func (c *Config) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	var buf strings.Builder
	if c.ActiveModel != "" {
		buf.WriteString(fmt.Sprintf("active_model = %q\n\n", c.ActiveModel))
	}
	if c.Locale != "" && c.Locale != "en" {
		buf.WriteString(fmt.Sprintf("locale = %q\n\n", c.Locale))
	}
	for _, mc := range c.Models {
		buf.WriteString("[[models]]\n")
		buf.WriteString(fmt.Sprintf("provider = %q\n", mc.Provider))
		buf.WriteString(fmt.Sprintf("model = %q\n", mc.Model))
		if mc.BaseURL != "" {
			buf.WriteString(fmt.Sprintf("base_url = %q\n", mc.BaseURL))
		}
		if mc.ContextWindow > 0 {
			buf.WriteString(fmt.Sprintf("context_window = %d\n", mc.ContextWindow))
		}
		if len(mc.Strengths) > 0 {
			buf.WriteString("strengths = [")
			for i, s := range mc.Strengths {
				if i > 0 {
					buf.WriteString(", ")
				}
				buf.WriteString(fmt.Sprintf("%q", s))
			}
			buf.WriteString("]\n")
		}
		buf.WriteString("\n")
	}
	return os.WriteFile(path, []byte(buf.String()), 0644)
}

func SaveCredential(dataDir, provider, apiKey string) error {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return err
	}
	creds, _ := readCredentials(dataDir)
	if creds == nil {
		creds = credentialsFile{}
	}
	creds[provider] = struct {
		APIKey string `toml:"api_key"`
	}{APIKey: apiKey}
	return writeCredentials(dataDir, creds)
}

func LoadCredentials(cfg *Config) error {
	creds, err := readCredentials(cfg.DataDir)
	if err != nil {
		return nil
	}
	for i := range cfg.Models {
		if c, ok := creds[cfg.Models[i].Provider]; ok {
			cfg.Models[i].APIKey = c.APIKey
		}
	}
	return nil
}

func readCredentials(dataDir string) (credentialsFile, error) {
	credPath := filepath.Join(dataDir, "credentials.toml")
	var creds credentialsFile
	if _, err := toml.DecodeFile(credPath, &creds); err != nil {
		return nil, err
	}
	return creds, nil
}

func writeCredentials(dataDir string, creds credentialsFile) error {
	var buf strings.Builder
	keys := make([]string, 0, len(creds))
	for k := range creds {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, provider := range keys {
		buf.WriteString(fmt.Sprintf("[%s]\n", provider))
		buf.WriteString(fmt.Sprintf("api_key = %q\n\n", creds[provider].APIKey))
	}
	return os.WriteFile(filepath.Join(dataDir, "credentials.toml"), []byte(buf.String()), 0600)
}

func (c *Config) ValidateAPIKeys() error {
	seen := map[string]bool{}
	for _, mc := range c.Models {
		if seen[mc.Provider] {
			continue
		}
		seen[mc.Provider] = true
		if mc.APIKey == "" {
			return fmt.Errorf("provider %q: missing api_key in credentials.toml", mc.Provider)
		}
	}
	return nil
}

func (c *Config) EnsureDataDir() error {
	dirs := []string{c.DataDir, filepath.Join(c.DataDir, "capabilities"), filepath.Join(c.DataDir, "logs")}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("create dir %s: %w", d, err)
		}
	}
	return nil
}

func (c *Config) DBPath() string     { return filepath.Join(c.DataDir, "memory.db") }
func (c *Config) ConfigPath() string { return filepath.Join(c.DataDir, "config.toml") }

func (c *Config) ModelByRef(ref string) (ModelConfig, bool) {
	for _, mc := range c.Models {
		if mc.Ref() == ref {
			return mc, true
		}
	}
	return ModelConfig{}, false
}

func (c *Config) ActiveModelConfig() (ModelConfig, bool) { return c.ModelByRef(c.ActiveModel) }

func (mc ModelConfig) Ref() string { return mc.Provider + "/" + mc.Model }

func (mc ModelConfig) ResolveAPIKey() (string, error) {
	if mc.APIKey == "" {
		return "", fmt.Errorf("provider %q missing api_key in credentials.toml", mc.Provider)
	}
	return mc.APIKey, nil
}

func (mc ModelConfig) IsAnthropic() bool { return mc.Provider == "anthropic" }
func (mc ModelConfig) IsOpenAI() bool    { return mc.Provider == "openai" }

func (mc ModelConfig) EffectiveBaseURL() string {
	if mc.BaseURL != "" {
		return mc.BaseURL
	}
	if mc.Provider == "openai" {
		return ""
	}
	return mc.BaseURL
}
