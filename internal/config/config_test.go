package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/alanchenchen/suna/internal/skill"
)

func TestGuardWorkspaceNormalizesExistingDirectory(t *testing.T) {
	workspace := t.TempDir()
	cfg := &Config{Guard: GuardConfig{Workspace: workspace}}

	if err := cfg.NormalizeGuard(); err != nil {
		t.Fatalf("NormalizeGuard() error = %v", err)
	}
	want := cleanSymlinkPath(t, workspace)
	if got := cfg.Guard.Workspace; got != want {
		t.Fatalf("Guard.Workspace = %q, want %q", got, want)
	}
}

func TestGuardWorkspaceRejectsMissingDirectory(t *testing.T) {
	cfg := &Config{Guard: GuardConfig{Workspace: filepath.Join(t.TempDir(), "missing")}}
	if err := cfg.NormalizeGuard(); err == nil {
		t.Fatalf("NormalizeGuard() error = nil, want non-nil")
	}
}

func TestConfigSaveKeepsGuardWorkspace(t *testing.T) {
	cfg, path, workspace := newSaveTestConfig(t, 0)

	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	data := readFile(t, path)
	want := `workspace = "` + cleanSymlinkPath(t, workspace) + `"`
	if !strings.Contains(data, want) {
		t.Fatalf("saved config = %q, want substring %q", data, want)
	}
}

func TestConfigSaveOmitsDefaultMaxModelRPS(t *testing.T) {
	cfg, path, _ := newSaveTestConfig(t, 0)

	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	data := readFile(t, path)
	if strings.Contains(data, "max_model_rps") {
		t.Fatalf("saved config = %q, should not contain max_model_rps", data)
	}
	if got := cfg.GetMaxModelRPS(); got != DefaultMaxModelRPS {
		t.Fatalf("GetMaxModelRPS() = %d, want %d", got, DefaultMaxModelRPS)
	}
}

func TestConfigSaveKeepsCustomMaxModelRPS(t *testing.T) {
	cfg, path, _ := newSaveTestConfig(t, 20)

	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	data := readFile(t, path)
	if !strings.Contains(data, "max_model_rps = 20") {
		t.Fatalf("saved config = %q, want custom max_model_rps", data)
	}
}

func TestConfigReasoningRoundTripsInlineTable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	workspace := mkdir(t, filepath.Join(dir, "workspace"))
	cfg := &Config{
		ActiveModel: "openai/gpt-5",
		Models: []ModelConfig{{
			Provider:        "openai",
			Model:           "gpt-5",
			ContextWindow:   128000,
			MaxOutputTokens: 8192,
			Reasoning: map[string]any{
				"reasoning": map[string]any{"effort": "high"},
			},
		}},
		Guard:   GuardConfig{Mode: "ask", Workspace: workspace},
		UI:      UIConfig{Theme: "auto", Locale: "en"},
		DataDir: dir,
	}

	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	data := readFile(t, path)
	if !strings.Contains(data, `reasoning = { reasoning = { effort = "high" } }`) {
		t.Fatalf("saved config = %q, want inline reasoning table", data)
	}
	var loaded Config
	if err := LoadTOML(path, &loaded); err != nil {
		t.Fatalf("LoadTOML() error = %v", err)
	}
	got := loaded.Models[0].Reasoning["reasoning"].(map[string]any)["effort"]
	if got != "high" {
		t.Fatalf("reasoning.effort = %#v, want %q", got, "high")
	}
}

func TestConfigReasoningSavesInlineThinkingTable(t *testing.T) {
	dir := t.TempDir()
	workspace := mkdir(t, filepath.Join(dir, "workspace"))
	cfg := &Config{
		ActiveModel: "DF/GLM-5.1",
		Models: []ModelConfig{{
			Provider:        "DF",
			Model:           "GLM-5.1",
			BaseURL:         "https://api.example.com/v1",
			ContextWindow:   128000,
			MaxOutputTokens: 8192,
			Strengths:       []string{"文本模型", "coding"},
			Reasoning: map[string]any{
				"thinking": map[string]any{"type": "disabled"},
			},
		}},
		Guard:   GuardConfig{Mode: "ask", Workspace: workspace},
		UI:      UIConfig{Theme: "auto", Locale: "en"},
		DataDir: dir,
	}
	path := filepath.Join(t.TempDir(), "config.toml")

	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	saved := readFile(t, path)
	if !strings.Contains(saved, `reasoning = { thinking = { type = "disabled" } }`) {
		t.Fatalf("saved config = %q, want inline thinking table", saved)
	}
	if strings.Contains(saved, "[models.reasoning") {
		t.Fatalf("saved config = %q, should not contain nested reasoning table", saved)
	}
	var loaded Config
	if err := LoadTOML(path, &loaded); err != nil {
		t.Fatalf("LoadTOML() error = %v", err)
	}
	thinking, ok := loaded.Models[0].Reasoning["thinking"].(map[string]any)
	if !ok {
		t.Fatalf("thinking = %#v, want map", loaded.Models[0].Reasoning["thinking"])
	}
	if got := thinking["type"]; got != "disabled" {
		t.Fatalf("thinking.type = %#v, want %q", got, "disabled")
	}
}

func TestConfigSaveWritesSkillsAsFlatObjectMap(t *testing.T) {
	dir := t.TempDir()
	workspace := mkdir(t, filepath.Join(dir, "workspace"))
	cfg := &Config{
		ActiveModel: "test/model",
		Models:      []ModelConfig{{Provider: "test", Model: "model", ContextWindow: 128000, MaxOutputTokens: 8192}},
		Guard:       GuardConfig{Mode: "ask", Workspace: workspace},
		UI:          UIConfig{Theme: "auto", Locale: "en"},
		Skills: map[string]skill.Record{
			"img":         {Enabled: true, Reasons: []string{"contains binary or obfuscated content", "includes scripts/ helper files"}},
			"code-review": {Enabled: true},
			"needs.quote": {Enabled: false, Reasons: []string{"mentions sensitive environment variables or tokens"}},
		},
		DataDir: dir,
	}
	path := filepath.Join(dir, "config.toml")

	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	data := readFile(t, path)
	if strings.Contains(data, "[skills]\n") {
		t.Fatalf("saved config = %q, should not contain standalone [skills] table", data)
	}
	for _, section := range []string{"[skills.code-review]", "[skills.img]", "[skills.\"needs.quote\"]"} {
		if !strings.Contains(data, section) {
			t.Fatalf("saved config = %q, want section %s", data, section)
		}
	}
	if !strings.Contains(data, `reasons = ["contains binary or obfuscated content", "includes scripts/ helper files"]`) {
		t.Fatalf("saved config = %q, want inline reasons array", data)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !loaded.Skills["img"].Enabled || len(loaded.Skills["img"].Reasons) != 2 {
		t.Fatalf("loaded img skill = %#v, want enabled with reasons", loaded.Skills["img"])
	}
	if loaded.Skills["needs.quote"].Enabled {
		t.Fatalf("loaded needs.quote skill = %#v, want disabled", loaded.Skills["needs.quote"])
	}
}

func TestConfigSaveWritesMCPLikeConfigurationDocs(t *testing.T) {
	cfg, path, _ := newSaveTestConfig(t, 0)
	cfg.MCP.Servers = map[string]MCPServerConfig{
		"filesystem": {
			Enabled:        false,
			Transport:      "stdio",
			Command:        "npx",
			Args:           []string{"-y", "@modelcontextprotocol/server-filesystem", "/Users/me/project"},
			CWD:            "/Users/me/project",
			TimeoutSeconds: 30,
		},
		"github": {
			Enabled:        true,
			Transport:      "stdio",
			Command:        "npx",
			Args:           []string{"-y", "@modelcontextprotocol/server-github"},
			Env:            map[string]string{"GITHUB_TOKEN": "ghp_xxx"},
			TimeoutSeconds: 30,
		},
		"context7": {
			Enabled:        false,
			Transport:      "streamable_http",
			URL:            "https://mcp.context7.com/mcp",
			Headers:        map[string]string{"Authorization": "Bearer xxx"},
			TimeoutSeconds: 30,
		},
	}

	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	data := readFile(t, path)
	for _, want := range []string{
		"[mcp.servers.context7]",
		`  transport = "streamable_http"`,
		`  url = "https://mcp.context7.com/mcp"`,
		"[mcp.servers.context7.headers]",
		`  Authorization = "Bearer xxx"`,
		"[mcp.servers.filesystem]",
		`  args = ["-y", "@modelcontextprotocol/server-filesystem", "/Users/me/project"]`,
		`  cwd = "/Users/me/project"`,
		"[mcp.servers.github]",
		"[mcp.servers.github.env]",
		`  GITHUB_TOKEN = "ghp_xxx"`,
	} {
		if !strings.Contains(data, want) {
			t.Fatalf("saved config = %q, want substring %q", data, want)
		}
	}
	if strings.Contains(data, "[mcp]\n") || strings.Contains(data, "[mcp.servers]\n") {
		t.Fatalf("saved config = %q, should not contain wrapper mcp tables", data)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := loaded.MCP.Servers["github"].Env["GITHUB_TOKEN"]; got != "ghp_xxx" {
		t.Fatalf("loaded github token = %q, want ghp_xxx", got)
	}
}

func TestConfigSaveWritesHooksLikeConfigurationDocs(t *testing.T) {
	cfg, path, _ := newSaveTestConfig(t, 0)
	cfg.Hooks = []HookConfig{{Event: "before_tool", Tool: "exec", Command: "echo checking"}}

	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	data := readFile(t, path)
	for _, want := range []string{"[[hooks]]", `  event = "before_tool"`, `  tool = "exec"`, `  command = "echo checking"`} {
		if !strings.Contains(data, want) {
			t.Fatalf("saved config = %q, want substring %q", data, want)
		}
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded.Hooks) != 1 || loaded.Hooks[0].Command != "echo checking" {
		t.Fatalf("loaded hooks = %#v, want one hook", loaded.Hooks)
	}
}

func TestDeleteCredentialRemovesOnlyRequestedProvider(t *testing.T) {
	dir := t.TempDir()
	mustSaveCredential(t, dir, "openai", "sk-openai")
	mustSaveCredential(t, dir, "anthropic", "sk-anthropic")

	if err := DeleteCredential(dir, "openai"); err != nil {
		t.Fatalf("DeleteCredential() error = %v", err)
	}
	creds := loadCredentials(t, dir)
	if _, ok := creds["openai"]; ok {
		t.Fatalf("credentials[openai] exists in %#v, want absent", creds)
	}
	if got := creds["anthropic"].APIKey; got != "sk-anthropic" {
		t.Fatalf("credentials[anthropic].APIKey = %q, want %q", got, "sk-anthropic")
	}
}

func TestDeleteCredentialMissingProviderIsNoop(t *testing.T) {
	dir := t.TempDir()
	mustSaveCredential(t, dir, "openai", "sk-openai")

	if err := DeleteCredential(dir, "missing"); err != nil {
		t.Fatalf("DeleteCredential() error = %v", err)
	}
	creds := loadCredentials(t, dir)
	if got := creds["openai"].APIKey; got != "sk-openai" {
		t.Fatalf("credentials[openai].APIKey = %q, want %q", got, "sk-openai")
	}
}

func newSaveTestConfig(t *testing.T, maxModelRPS int) (*Config, string, string) {
	t.Helper()
	dir := t.TempDir()
	workspace := mkdir(t, filepath.Join(dir, "workspace"))
	cfg := &Config{
		ActiveModel: "test/model",
		Models:      []ModelConfig{{Provider: "test", Model: "model", ContextWindow: 128000, MaxOutputTokens: 8192}},
		Guard:       GuardConfig{Mode: "ask", Workspace: workspace},
		UI:          UIConfig{Theme: "auto", Locale: "en"},
		MaxModelRPS: maxModelRPS,
		DataDir:     dir,
	}
	return cfg, filepath.Join(dir, "config.toml"), workspace
}

func mkdir(t *testing.T, path string) string {
	t.Helper()
	if err := os.Mkdir(path, 0755); err != nil {
		t.Fatalf("Mkdir(%q) error = %v", path, err)
	}
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	return string(data)
}

func cleanSymlinkPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q) error = %v", path, err)
	}
	return filepath.Clean(resolved)
}

func mustSaveCredential(t *testing.T, dir, provider, key string) {
	t.Helper()
	if err := SaveCredential(dir, provider, key); err != nil {
		t.Fatalf("SaveCredential(%q) error = %v", provider, err)
	}
}

func loadCredentials(t *testing.T, dir string) credentialsFile {
	t.Helper()
	creds, err := readCredentials(dir)
	if err != nil {
		t.Fatalf("readCredentials() error = %v", err)
	}
	return creds
}

func TestModelConfigAvailableAsSubtaskFor(t *testing.T) {
	tests := []struct {
		name      string
		model     ModelConfig
		activeRef string
		want      bool
	}{
		{name: "empty matches all", model: ModelConfig{Provider: "DF", Model: "MiniMax-M3"}, activeRef: "Froghire/gpt-5.5", want: true},
		{name: "self always matches", model: ModelConfig{Provider: "DF", Model: "MiniMax-M3", SubtaskFor: []string{"Froghire/**"}}, activeRef: "DF/MiniMax-M3", want: true},
		{name: "exact match", model: ModelConfig{Provider: "DF", Model: "MiniMax-M3", SubtaskFor: []string{"Froghire/gpt-5.5"}}, activeRef: "Froghire/gpt-5.5", want: true},
		{name: "provider glob", model: ModelConfig{Provider: "DF", Model: "MiniMax-M3", SubtaskFor: []string{"Froghire/**"}}, activeRef: "Froghire/gpt-5.4", want: true},
		{name: "model glob", model: ModelConfig{Provider: "DF", Model: "MiniMax-M3", SubtaskFor: []string{"DF/glm-*"}}, activeRef: "DF/glm-5.2", want: true},
		{name: "or semantics miss", model: ModelConfig{Provider: "DF", Model: "MiniMax-M3", SubtaskFor: []string{"Froghire/**", "Oio/**"}}, activeRef: "DF/glm-5.2", want: false},
		{name: "star matches all", model: ModelConfig{Provider: "DF", Model: "MiniMax-M3", SubtaskFor: []string{"*"}}, activeRef: "Any/model", want: true},
		{name: "provider double star crosses slash", model: ModelConfig{Provider: "DF", Model: "MiniMax-M3", SubtaskFor: []string{"Froghire/**"}}, activeRef: "Froghire/family/gpt-5.5", want: true},
		{name: "star does not cross slash", model: ModelConfig{Provider: "DF", Model: "MiniMax-M3", SubtaskFor: []string{"Froghire/*"}}, activeRef: "Froghire/family/gpt-5.5", want: false},
		{name: "invalid-looking pattern treated literal", model: ModelConfig{Provider: "DF", Model: "MiniMax-M3", SubtaskFor: []string{"["}}, activeRef: "Froghire/gpt-5.5", want: false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.model.AvailableAsSubtaskFor(tt.activeRef); got != tt.want {
				t.Fatalf("AvailableAsSubtaskFor() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfigSubtaskForRoundTrips(t *testing.T) {
	dir := t.TempDir()
	workspace := mkdir(t, filepath.Join(dir, "workspace"))
	cfg := &Config{
		ActiveModel: "DF/MiniMax-M3",
		Models: []ModelConfig{{
			Provider:        "DF",
			Model:           "MiniMax-M3",
			BaseURL:         "https://api.example.com/v1",
			ContextWindow:   1000000,
			MaxOutputTokens: 8192,
			SubtaskFor:      []string{"Froghire/**", "Oio/**"},
		}},
		Guard:   GuardConfig{Mode: "ask", Workspace: workspace},
		UI:      UIConfig{Theme: "auto", Locale: "en"},
		DataDir: dir,
	}
	path := filepath.Join(dir, "config.toml")

	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	saved := readFile(t, path)
	if !strings.Contains(saved, `subtask_for = ["Froghire/**", "Oio/**"]`) {
		t.Fatalf("saved config = %q, want subtask_for", saved)
	}
	var loaded Config
	if err := LoadTOML(path, &loaded); err != nil {
		t.Fatalf("LoadTOML() error = %v", err)
	}
	got := loaded.Models[0].SubtaskFor
	want := []string{"Froghire/**", "Oio/**"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SubtaskFor = %#v, want %#v", got, want)
	}
}
