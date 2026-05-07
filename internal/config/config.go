package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Models  map[string]ModelConfig `toml:"models"`
	Router  RouterConfig           `toml:"router"`
	Guard   GuardConfig            `toml:"guard"`
	TUI     TUIConfig              `toml:"tui"`
	Hooks   []HookConfig           `toml:"hooks"`
	Locale  string                 `toml:"locale"`
	DataDir string                 `toml:"-"`
}

type ModelConfig struct {
	Provider      string   `toml:"provider"`
	ProviderName  string   `toml:"provider_name,omitempty"`
	Model         string   `toml:"model"`
	BaseURL       string   `toml:"base_url"`
	APIKeyEnv     string   `toml:"api_key_env"`
	ContextWindow int      `toml:"context_window"`
	CostPer1K     float64  `toml:"cost_per_1k"`
	Strengths     []string `toml:"strengths"`
}

type RouterConfig struct {
	Default string       `toml:"default"`
	Rules   []RouterRule `toml:"rules"`
}

type RouterRule struct {
	Pattern string `toml:"pattern"`
	Model   string `toml:"model"`
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

// Load 加载配置文件。不提供任何默认模型——用户必须自己配置。
// 如果配置文件不存在或缺少必要字段，返回明确的错误信息。
func Load(path string) (*Config, error) {
	cfg := &Config{
		Models: make(map[string]ModelConfig),
		TUI:    TUIConfig{Theme: "dark"},
		Locale: "en",
	}

	homeDir, _ := os.UserHomeDir()
	cfg.DataDir = filepath.Join(homeDir, ".suna")

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("config file not found: %s\nPlease create it with at least one [models.default] section.\nExample:\n\n[models.default]\nprovider = \"openai\"\nmodel = \"your-model-name\"\nbase_url = \"https://api.example.com/v1\"\napi_key_env = \"YOUR_API_KEY_ENV\"", path)
	}

	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	if len(cfg.Models) == 0 {
		return nil, fmt.Errorf("config has no [models.*] sections. At least [models.default] is required.\nExample:\n\n[models.default]\nprovider = \"openai\"\nmodel = \"your-model-name\"\nbase_url = \"https://api.example.com/v1\"\napi_key_env = \"YOUR_API_KEY_ENV\"")
	}

	if _, ok := cfg.Models["default"]; !ok {
		return nil, fmt.Errorf("config must have a [models.default] section")
	}

	if cfg.Router.Default == "" {
		cfg.Router.Default = "default"
	}

	return cfg, nil
}

// NeedsSetup 检查是否需要引导配置（无配置文件或缺少必要字段）
func NeedsSetup(path string) bool {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return true
	}
	cfg, err := Load(path)
	if err != nil {
		return true
	}
	return len(cfg.Models) == 0
}

// Save 保存配置到文件
func (c *Config) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	var buf strings.Builder
	buf.WriteString("[models.default]\n")
	mc := c.Models["default"]
	buf.WriteString(fmt.Sprintf("provider = %q\n", mc.Provider))
	if mc.ProviderName != "" {
		buf.WriteString(fmt.Sprintf("provider_name = %q\n", mc.ProviderName))
	}
	buf.WriteString(fmt.Sprintf("model = %q\n", mc.Model))
	if mc.BaseURL != "" {
		buf.WriteString(fmt.Sprintf("base_url = %q\n", mc.BaseURL))
	}
	buf.WriteString(fmt.Sprintf("api_key_env = %q\n", mc.APIKeyEnv))
	if mc.ContextWindow > 0 {
		buf.WriteString(fmt.Sprintf("context_window = %d\n", mc.ContextWindow))
	}
	buf.WriteString("\n")
	if c.Locale != "" && c.Locale != "en" {
		buf.WriteString(fmt.Sprintf("locale = %q\n", c.Locale))
	}

	return os.WriteFile(path, []byte(buf.String()), 0644)
}

// SaveCredentials 将 API key 写入 ~/.suna/.credentials 文件。
// 格式：每行 KEY_NAME=actual_key_value
// 文件权限 0600，仅当前用户可读写。
func SaveCredentials(dataDir, envName, apiKey string) error {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return err
	}
	credPath := filepath.Join(dataDir, ".credentials")

	// 读取已有内容
	existing := map[string]string{}
	if data, err := os.ReadFile(credPath); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if idx := strings.Index(line, "="); idx > 0 {
				existing[strings.TrimSpace(line[:idx])] = strings.TrimSpace(line[idx+1:])
			}
		}
	}

	existing[envName] = apiKey

	var buf strings.Builder
	for k, v := range existing {
		buf.WriteString(k + "=" + v + "\n")
	}
	return os.WriteFile(credPath, []byte(buf.String()), 0600)
}

// LoadCredentials 从 ~/.suna/.credentials 加载 API keys 到环境变量
func LoadCredentials(dataDir string) error {
	credPath := filepath.Join(dataDir, ".credentials")
	data, err := os.ReadFile(credPath)
	if err != nil {
		return nil
	}
	for _, line := range strings.Split(string(data), "\n") {
		if idx := strings.Index(line, "="); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			if key != "" && val != "" {
				os.Setenv(key, val)
			}
		}
	}
	return nil
}

// ValidateAPIKeys 检查所有配置模型的 API key 环境变量是否已设置。
// 返回第一个缺失的 key 的错误信息。
func (c *Config) ValidateAPIKeys() error {
	for name, mc := range c.Models {
		if mc.APIKeyEnv == "" {
			return fmt.Errorf("model %q: api_key_env is not set in config", name)
		}
		if os.Getenv(mc.APIKeyEnv) == "" {
			return fmt.Errorf("model %q: environment variable %q is not set", name, mc.APIKeyEnv)
		}
	}
	return nil
}

func (c *Config) EnsureDataDir() error {
	dirs := []string{
		c.DataDir,
		filepath.Join(c.DataDir, "capabilities"),
		filepath.Join(c.DataDir, "logs"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("create dir %s: %w", d, err)
		}
	}
	return nil
}

func (c *Config) DBPath() string {
	return filepath.Join(c.DataDir, "memory.db")
}

func (c *Config) ConfigPath() string {
	return filepath.Join(c.DataDir, "config.toml")
}

func (mc *ModelConfig) ResolveAPIKey() (string, error) {
	if mc.APIKeyEnv == "" {
		return "", fmt.Errorf("api_key_env not set")
	}
	key := os.Getenv(mc.APIKeyEnv)
	if key == "" {
		return "", fmt.Errorf("environment variable %q not set", mc.APIKeyEnv)
	}
	return key, nil
}

func (mc *ModelConfig) IsAnthropic() bool {
	return mc.Provider == "anthropic"
}
