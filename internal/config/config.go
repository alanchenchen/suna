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
	MCP         MCPConfig               `toml:"mcp"`
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
		cp.Models[i].Strengths = append([]string(nil), cp.Models[i].Strengths...)
		cp.Models[i].SubtaskFor = append([]string(nil), cp.Models[i].SubtaskFor...)
	}
	cp.Guard.Blocked = append([]GuardRule(nil), c.Guard.Blocked...)
	cp.Guard.Allowed = append([]GuardAllowRule(nil), c.Guard.Allowed...)
	cp.Skills = cloneSkillRecords(c.Skills)
	cp.MCP.Servers = cloneMCPServers(c.MCP.Servers)
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

func cloneMCPServers(in map[string]MCPServerConfig) map[string]MCPServerConfig {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]MCPServerConfig, len(in))
	for k, v := range in {
		v.Args = append([]string(nil), v.Args...)
		v.Env = cloneStringMap(v.Env)
		v.Headers = cloneStringMap(v.Headers)
		out[k] = v
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// MCPConfig 保存外部 MCP server 连接配置；server id 来自 [mcp.servers.<id>]。
type MCPConfig struct {
	Servers map[string]MCPServerConfig `toml:"servers,omitempty"`
}

type MCPServerConfig struct {
	Enabled        bool              `toml:"enabled"`
	Transport      string            `toml:"transport,omitempty"`
	Command        string            `toml:"command,omitempty"`
	Args           []string          `toml:"args,omitempty"`
	Env            map[string]string `toml:"env,omitempty"`
	CWD            string            `toml:"cwd,omitempty"`
	URL            string            `toml:"url,omitempty"`
	Headers        map[string]string `toml:"headers,omitempty"`
	TimeoutSeconds int               `toml:"timeout_seconds,omitempty"`
}

// DefaultMaxModelRPS 是每个模型 ref 的默认请求限速，避免 subtask 并发打爆供应商。
const DefaultMaxModelRPS = 10

func (c *Config) GetMaxModelRPS() int {
	if c.MaxModelRPS <= 0 {
		return DefaultMaxModelRPS
	}
	return c.MaxModelRPS
}

type ModelConfig struct {
	Provider        string         `toml:"provider"`
	Protocol        ModelProtocol  `toml:"protocol,omitempty"`
	Model           string         `toml:"model"`
	BaseURL         string         `toml:"base_url,omitempty"`
	ContextWindow   int            `toml:"context_window,omitempty"`
	MaxOutputTokens int            `toml:"max_output_tokens,omitempty"`
	Strengths       []string       `toml:"strengths,omitempty"`
	SubtaskFor      []string       `toml:"subtask_for,omitempty"`
	Reasoning       map[string]any `toml:"reasoning,omitempty"`
	APIKey          string         `toml:"-"`
}

type configTOML struct {
	ActiveModel string            `toml:"active_model"`
	Models      []modelConfigTOML `toml:"models"`
	Guard       GuardConfig       `toml:"guard"`
	UI          UIConfig          `toml:"ui"`
	MaxModelRPS int               `toml:"max_model_rps,omitzero"`
}

type modelConfigTOML struct {
	Provider        string          `toml:"provider"`
	Protocol        ModelProtocol   `toml:"protocol"`
	Model           string          `toml:"model"`
	BaseURL         string          `toml:"base_url,omitempty"`
	ContextWindow   int             `toml:"context_window,omitempty"`
	MaxOutputTokens int             `toml:"max_output_tokens,omitempty"`
	Strengths       []string        `toml:"strengths,omitempty"`
	SubtaskFor      []string        `toml:"subtask_for,omitempty"`
	Reasoning       inlineTOMLTable `toml:"reasoning,omitempty"`
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

// Load 从 TOML 加载配置并校验模型引用；缺失或非法字段直接返回错误。
// 旧配置兼容只允许模型 protocol 缺省，并在 NormalizeModels 中显式归一为 openai_chat。
func Load(path string) (*Config, error) {
	return LoadFromDataDir(path, DefaultDataDir())
}

// LoadFromDataDir 从指定数据目录加载配置与凭证，避免调用方的作用域被默认目录覆盖。
func LoadFromDataDir(path, dataDir string) (*Config, error) {
	cfg := &Config{UI: UIConfig{Theme: "auto", Locale: "en"}, DataDir: dataDir}

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
	if err := cfg.NormalizeModels(); err != nil {
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
	if err := cfg.ValidateModelLimits(); err != nil {
		return nil, err
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

func (c *Config) ValidateModelLimits() error {
	for _, mc := range c.Models {
		if mc.ContextWindow <= 0 {
			return fmt.Errorf("model %q context_window is required", mc.Ref())
		}
		if mc.MaxOutputTokens <= 0 {
			return fmt.Errorf("model %q max_output_tokens is required", mc.Ref())
		}
		if mc.MaxOutputTokens >= mc.ContextWindow {
			return fmt.Errorf("model %q max_output_tokens must be smaller than context_window", mc.Ref())
		}
	}
	return nil
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
	if err := c.NormalizeModels(); err != nil {
		return err
	}
	if err := c.ValidateModelLimits(); err != nil {
		return err
	}
	if err := c.NormalizeGuard(); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(c.tomlView()); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	writeSkillRecordsTOML(&buf, c.Skills)
	writeMCPConfigTOML(&buf, c.MCP)
	writeHooksTOML(&buf, c.Hooks)
	return os.WriteFile(path, buf.Bytes(), 0644)
}

func (c *Config) tomlView() configTOML {
	models := make([]modelConfigTOML, 0, len(c.Models))
	for _, mc := range c.Models {
		models = append(models, modelConfigTOML{
			Provider:        mc.Provider,
			Protocol:        mc.ProtocolOrDefault(),
			Model:           mc.Model,
			BaseURL:         mc.BaseURL,
			ContextWindow:   mc.ContextWindow,
			MaxOutputTokens: mc.MaxOutputTokens,
			Strengths:       mc.Strengths,
			SubtaskFor:      mc.SubtaskFor,
			Reasoning:       inlineTOMLTable(mc.Reasoning),
		})
	}
	return configTOML{
		ActiveModel: c.ActiveModel,
		Models:      models,
		Guard:       c.Guard,
		UI:          c.UI,
		MaxModelRPS: c.MaxModelRPS,
	}
}

func writeSkillRecordsTOML(buf *bytes.Buffer, records map[string]skill.Record) {
	if len(records) == 0 {
		return
	}
	names := make([]string, 0, len(records))
	for name := range records {
		names = append(names, name)
	}
	sort.Strings(names)
	ensureTOMLGap(buf)
	for _, name := range names {
		record := records[name]
		buf.WriteString("[skills.")
		buf.WriteString(formatTOMLKey(name))
		buf.WriteString("]\n")
		writeBoolField(buf, "enabled", record.Enabled)
		if len(record.Reasons) > 0 {
			writeIndentedKey(buf, "reasons")
			buf.WriteString(formatInlineTOMLValue(record.Reasons))
			buf.WriteString("\n")
		}
		buf.WriteString("\n")
	}
}

func writeMCPConfigTOML(buf *bytes.Buffer, cfg MCPConfig) {
	if len(cfg.Servers) == 0 {
		return
	}
	names := make([]string, 0, len(cfg.Servers))
	for name := range cfg.Servers {
		names = append(names, name)
	}
	sort.Strings(names)
	ensureTOMLGap(buf)
	for _, name := range names {
		server := cfg.Servers[name]
		buf.WriteString("[mcp.servers.")
		buf.WriteString(formatTOMLKey(name))
		buf.WriteString("]\n")
		writeBoolField(buf, "enabled", server.Enabled)
		writeStringField(buf, "transport", server.Transport)
		writeStringField(buf, "command", server.Command)
		if len(server.Args) > 0 {
			writeIndentedKey(buf, "args")
			buf.WriteString(formatInlineTOMLValue(server.Args))
			buf.WriteString("\n")
		}
		writeStringField(buf, "cwd", server.CWD)
		writeStringField(buf, "url", server.URL)
		if server.TimeoutSeconds > 0 {
			writeIndentedKey(buf, "timeout_seconds")
			buf.WriteString(strconv.FormatInt(int64(server.TimeoutSeconds), 10))
			buf.WriteString("\n")
		}
		if len(server.Env) > 0 {
			writeStringMapSectionTOML(buf, "mcp.servers."+formatTOMLKey(name)+".env", server.Env)
		}
		if len(server.Headers) > 0 {
			writeStringMapSectionTOML(buf, "mcp.servers."+formatTOMLKey(name)+".headers", server.Headers)
		}
		buf.WriteString("\n")
	}
}

func writeHooksTOML(buf *bytes.Buffer, hooks []HookConfig) {
	if len(hooks) == 0 {
		return
	}
	ensureTOMLGap(buf)
	for _, hook := range hooks {
		buf.WriteString("[[hooks]]\n")
		writeStringField(buf, "event", hook.Event)
		writeStringField(buf, "tool", hook.Tool)
		writeStringField(buf, "command", hook.Command)
		buf.WriteString("\n")
	}
}

func writeStringMapSectionTOML(buf *bytes.Buffer, section string, values map[string]string) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	buf.WriteString("\n[")
	buf.WriteString(section)
	buf.WriteString("]\n")
	for _, key := range keys {
		writeIndentedKey(buf, formatTOMLKey(key))
		buf.WriteString(strconv.Quote(values[key]))
		buf.WriteString("\n")
	}
}

func writeBoolField(buf *bytes.Buffer, key string, value bool) {
	writeIndentedKey(buf, key)
	buf.WriteString(strconv.FormatBool(value))
	buf.WriteString("\n")
}

func writeStringField(buf *bytes.Buffer, key, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	writeIndentedKey(buf, key)
	buf.WriteString(strconv.Quote(value))
	buf.WriteString("\n")
}

func writeIndentedKey(buf *bytes.Buffer, key string) {
	buf.WriteString("  ")
	buf.WriteString(key)
	buf.WriteString(" = ")
}

func ensureTOMLGap(buf *bytes.Buffer) {
	if buf.Len() > 0 && !strings.HasSuffix(buf.String(), "\n\n") {
		buf.WriteString("\n")
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
