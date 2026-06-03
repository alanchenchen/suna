package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/alanchenchen/suna/internal/skill"
)

// Config 表示 Suna 的持久化配置，对应设计文档中的“单二进制 + daemon/TUI 共享配置”。
// 这里不保存运行态字段；纯界面设置统一放在 UI，模型密钥统一放在 credentials.toml。
type Config struct {
	ActiveModel string                  `toml:"active_model"`
	Models      []ModelConfig           `toml:"models"`
	Guard       GuardConfig             `toml:"guard"`
	UI          UIConfig                `toml:"ui"`
	Skills      map[string]skill.Record `toml:"skills"`
	Hooks       []HookConfig            `toml:"hooks"`
	MaxModelRPS int                     `toml:"max_model_rps,omitempty"`
	DataDir     string                  `toml:"-"`
}

func (c *Config) Clone() *Config {
	if c == nil {
		return nil
	}
	cp := *c
	cp.Models = append([]ModelConfig(nil), c.Models...)
	for i := range cp.Models {
		cp.Models[i].Reasoning = cloneMap(cp.Models[i].Reasoning)
	}
	cp.Guard.Blocked = append([]GuardRule(nil), c.Guard.Blocked...)
	cp.Guard.Allowed = append([]GuardAllowRule(nil), c.Guard.Allowed...)
	cp.Skills = cloneSkillRecords(c.Skills)
	cp.Hooks = append([]HookConfig(nil), c.Hooks...)
	return &cp
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneSkillRecords(in map[string]skill.Record) map[string]skill.Record {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]skill.Record, len(in))
	for k, v := range in {
		v.Reasons = append([]string(nil), v.Reasons...)
		out[k] = v
	}
	return out
}

// DefaultMaxModelRPS 是每个模型 ref 的默认请求限速，避免 subtask 并发打爆供应商。
const DefaultMaxModelRPS = 15

func (c *Config) GetMaxModelRPS() int {
	if c.MaxModelRPS <= 0 {
		return DefaultMaxModelRPS
	}
	return c.MaxModelRPS
}

type ModelConfig struct {
	Provider      string         `toml:"provider"`
	Model         string         `toml:"model"`
	BaseURL       string         `toml:"base_url,omitempty"`
	ContextWindow int            `toml:"context_window,omitempty"`
	Strengths     []string       `toml:"strengths,omitempty"`
	Reasoning     map[string]any `toml:"reasoning,omitempty"`
	APIKey        string         `toml:"-"`
}

type configTOML struct {
	ActiveModel string                  `toml:"active_model"`
	Models      []modelConfigTOML       `toml:"models"`
	Guard       GuardConfig             `toml:"guard"`
	UI          UIConfig                `toml:"ui"`
	Skills      map[string]skill.Record `toml:"skills,omitempty"`
	Hooks       []HookConfig            `toml:"hooks"`
	MaxModelRPS int                     `toml:"max_model_rps,omitzero"`
}

type modelConfigTOML struct {
	Provider      string          `toml:"provider"`
	Model         string          `toml:"model"`
	BaseURL       string          `toml:"base_url,omitempty"`
	ContextWindow int             `toml:"context_window,omitempty"`
	Strengths     []string        `toml:"strengths,omitempty"`
	Reasoning     inlineTOMLTable `toml:"reasoning,omitempty"`
}

type inlineTOMLTable map[string]any

// GuardConfig 保存本地安全规则配置，对应 plans/04-guard.md。
type GuardConfig struct {
	Mode      string           `toml:"mode,omitempty"`
	Workspace string           `toml:"workspace,omitempty"`
	Blocked   []GuardRule      `toml:"blocked"`
	Allowed   []GuardAllowRule `toml:"allowed"`
}

func (g GuardConfig) ModeOrDefault() string {
	switch strings.ToLower(strings.TrimSpace(g.Mode)) {
	case "readonly", "ask", "auto", "smart":
		return strings.ToLower(strings.TrimSpace(g.Mode))
	default:
		return "ask"
	}
}

// GuardRule 定义被阻止的文件/命令模式及原因。
type GuardRule struct {
	Pattern string `toml:"pattern"`
	Reason  string `toml:"reason"`
}

// GuardAllowRule 定义特定工具在特定模式下的允许规则。
type GuardAllowRule struct {
	Pattern string `toml:"pattern"`
	Tool    string `toml:"tool"`
	Reason  string `toml:"reason"`
}

// UIConfig 保存纯界面配置；核心 daemon 逻辑不得依赖这里的值。
type UIConfig struct {
	Theme  string `toml:"theme"`
	Locale string `toml:"locale"`
}

// HookConfig 描述生命周期 hook，供后续自动化扩展使用。
type HookConfig struct {
	Event   string `toml:"event"`
	Tool    string `toml:"tool"`
	Command string `toml:"command"`
}

type credentialsFile map[string]struct {
	APIKey string `toml:"api_key"`
}

// Load 从 TOML 加载配置并校验模型引用；缺失或非法字段直接返回错误，不做旧格式兼容。
func Load(path string) (*Config, error) {
	cfg := &Config{UI: UIConfig{Theme: "auto", Locale: "en"}}
	cfg.DataDir = DefaultDataDir()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("config file not found: %s\nPlease create ~/.suna/config.toml with active_model and [[models]] entries", path)
	}
	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	cfg.NormalizeUI()
	if err := cfg.NormalizeGuard(); err != nil {
		return nil, err
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

func (c *Config) NormalizeUI() {
	// ui 仅包含纯界面配置，避免与模型、守护进程等核心配置混在一起。
	if c.UI.Locale == "" {
		c.UI.Locale = "en"
	}
	if c.UI.Theme == "" {
		c.UI.Theme = "auto"
	}
}

func (c *Config) NormalizeGuard() error {
	workspace := strings.TrimSpace(c.Guard.Workspace)
	if workspace == "" {
		c.Guard.Workspace = ""
		return nil
	}
	if strings.HasPrefix(workspace, "~/") {
		home, _ := os.UserHomeDir()
		if home != "" {
			workspace = filepath.Join(home, workspace[2:])
		}
	}
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return fmt.Errorf("guard.workspace %q is invalid: %w", c.Guard.Workspace, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("guard.workspace %q is not accessible: %w", c.Guard.Workspace, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("guard.workspace %q must be a directory", c.Guard.Workspace)
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	c.Guard.Workspace = filepath.Clean(abs)
	return nil
}

// NeedsSetup 判断当前配置是否足以启动 daemon；首次启动时 TUI 会进入配置向导。
func NeedsSetup(path string) bool {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return true
	}
	cfg, err := Load(path)
	return err != nil || len(cfg.Models) == 0
}

// LoadTOML 是少量启动期探测配置的轻量入口，不执行 Config 的业务校验。
func LoadTOML(path string, v any) error {
	_, err := toml.DecodeFile(path, v)
	return err
}

// Save 完整写回当前配置结构，避免 TUI 局部更新时丢失 guard/hooks 等模块配置。
func (c *Config) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	c.NormalizeUI()
	if err := c.NormalizeGuard(); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(c.tomlView()); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	return os.WriteFile(path, buf.Bytes(), 0644)
}

func (c *Config) tomlView() configTOML {
	models := make([]modelConfigTOML, 0, len(c.Models))
	for _, mc := range c.Models {
		models = append(models, modelConfigTOML{
			Provider:      mc.Provider,
			Model:         mc.Model,
			BaseURL:       mc.BaseURL,
			ContextWindow: mc.ContextWindow,
			Strengths:     mc.Strengths,
			Reasoning:     inlineTOMLTable(mc.Reasoning),
		})
	}
	return configTOML{
		ActiveModel: c.ActiveModel,
		Models:      models,
		Guard:       c.Guard,
		UI:          c.UI,
		Skills:      cloneSkillRecords(c.Skills),
		Hooks:       c.Hooks,
		MaxModelRPS: c.MaxModelRPS,
	}
}

func (t inlineTOMLTable) MarshalTOML() ([]byte, error) {
	return []byte(formatInlineTOMLTable(map[string]any(t))), nil
}

func formatInlineTOMLTable(m map[string]any) string {
	keys := make([]string, 0, len(m))
	for k, v := range m {
		if v == nil {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, formatTOMLKey(key)+" = "+formatInlineTOMLValue(m[key]))
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

func formatInlineTOMLValue(v any) string {
	switch x := v.(type) {
	case string:
		return strconv.Quote(x)
	case bool:
		if x {
			return "true"
		}
		return "false"
	case int:
		return strconv.FormatInt(int64(x), 10)
	case int8:
		return strconv.FormatInt(int64(x), 10)
	case int16:
		return strconv.FormatInt(int64(x), 10)
	case int32:
		return strconv.FormatInt(int64(x), 10)
	case int64:
		return strconv.FormatInt(x, 10)
	case uint:
		return strconv.FormatUint(uint64(x), 10)
	case uint8:
		return strconv.FormatUint(uint64(x), 10)
	case uint16:
		return strconv.FormatUint(uint64(x), 10)
	case uint32:
		return strconv.FormatUint(uint64(x), 10)
	case uint64:
		return strconv.FormatUint(x, 10)
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case []any:
		return formatInlineTOMLArray(x)
	case []string:
		items := make([]any, 0, len(x))
		for _, item := range x {
			items = append(items, item)
		}
		return formatInlineTOMLArray(items)
	case map[string]any:
		return formatInlineTOMLTable(x)
	default:
		rv := reflect.ValueOf(v)
		if rv.IsValid() && rv.Kind() == reflect.Map && rv.Type().Key().Kind() == reflect.String {
			out := make(map[string]any, rv.Len())
			for _, key := range rv.MapKeys() {
				out[key.String()] = rv.MapIndex(key).Interface()
			}
			return formatInlineTOMLTable(out)
		}
		return strconv.Quote(fmt.Sprint(v))
	}
}

func formatInlineTOMLArray(items []any) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, formatInlineTOMLValue(item))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func formatTOMLKey(key string) string {
	if isBareTOMLKey(key) {
		return key
	}
	return strconv.Quote(key)
}

func isBareTOMLKey(key string) bool {
	if key == "" {
		return false
	}
	for _, r := range key {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

// SaveCredential 按 provider 保存密钥；同一 provider 下的多模型共享同一个 API key。
func SaveCredential(dataDir, provider, apiKey string) error {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return err
	}
	creds, err := readCredentials(dataDir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if creds == nil {
		creds = credentialsFile{}
	}
	creds[provider] = struct {
		APIKey string `toml:"api_key"`
	}{APIKey: apiKey}
	return writeCredentials(dataDir, creds)
}

// DeleteCredential removes a stored provider credential. Missing files or absent providers are no-ops.
func DeleteCredential(dataDir, provider string) error {
	creds, err := readCredentials(dataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if _, ok := creds[provider]; !ok {
		return nil
	}
	delete(creds, provider)
	return writeCredentials(dataDir, creds)
}

// LoadCredentials 将 credentials.toml 中的密钥注入到对应 ModelConfig；解析错误会直接返回。
func LoadCredentials(cfg *Config) error {
	creds, err := readCredentials(cfg.DataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
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
	for _, ref := range keys {
		buf.WriteString(fmt.Sprintf("[%q]\n", ref))
		buf.WriteString(fmt.Sprintf("api_key = %q\n\n", creds[ref].APIKey))
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
	dirs := []string{c.DataDir, c.SkillsDir(), c.LogsDir()}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("create dir %s: %w", d, err)
		}
	}
	return nil
}

func (c *Config) EnsureDataDirs() error {
	for _, d := range []string{
		c.DataDir,
		c.LogsDir(),
		c.SkillsDir(),
	} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return err
		}
	}
	return nil
}

func (c *Config) LoadSkillRecords() map[string]skill.Record {
	return cloneSkillRecords(c.Skills)
}

func (c *Config) SaveSkillRecords(trust map[string]skill.Record) error {
	c.Skills = cloneSkillRecords(trust)
	return c.Save(c.ConfigPath())
}

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
