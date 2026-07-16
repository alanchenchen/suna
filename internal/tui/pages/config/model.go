package config

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"

	coreconfig "github.com/alanchenchen/suna/internal/config"
	uipage "github.com/alanchenchen/suna/internal/tui/pages/page"
)

type Model struct {
	Cursor          int
	SetupMode       bool
	FormOpen        bool
	WorkspaceOpen   bool
	DeleteCursor    int
	ReasoningOpen   bool
	ReasoningCursor int
	ReasoningFamily string
	Page            string
	DeleteConfirm   string
	DetailRef       string
	FormTitle       string
	FormProvider    string
	Inputs          []textinput.Model
	InputFocus      int
	Error           string
	Notice          string
	FromMode        uipage.Page
	Models          []string
	EditingName     string
}

func (m *Model) EnsureDefaults() {
	if m.Page == "" {
		m.Page = "home"
	}
}

type Row struct {
	Kind  string
	Name  string
	Label string
	Value string
}

func (r Row) Selectable() bool {
	switch r.Kind {
	case "section", "general_language", "general_theme", "general_guard", "general_workspace", "clear_attachments", "open_config_dir", "add_model", "provider_add_model", "add_provider_model", "edit_model", "edit_reasoning", "activate_model", "delete_model", "model", "empty":
		return true
	default:
		return false
	}
}

type ProviderFormValues struct {
	Provider        string
	Protocol        coreconfig.ModelProtocol
	Model           string
	APIKey          string
	Endpoint        string
	ContextWindow   string
	MaxOutputTokens string
	Strengths       string
	SubtaskFor      string
}

type ModelConfig struct {
	Provider        string
	Protocol        coreconfig.ModelProtocol
	Model           string
	BaseURL         string
	ContextWindow   int
	MaxOutputTokens int
	Strengths       []string
	SubtaskFor      []string
	Reasoning       map[string]any
	HasAPIKey       bool
}

func (m ModelConfig) Ref() string { return m.Provider + "/" + m.Model }

func ModelNeedsAttention(mc ModelConfig) bool {
	return !mc.HasAPIKey || mc.Model == "" || mc.BaseURL == "" || mc.ContextWindow <= 0 || mc.MaxOutputTokens <= 0 || mc.MaxOutputTokens >= mc.ContextWindow
}

func ModelStatusMark(mc ModelConfig, active bool) string {
	if ModelNeedsAttention(mc) {
		return "!"
	}
	if active {
		return "◉"
	}
	return "○"
}

func ModelSummary(mc ModelConfig, _ bool, fmtTok func(int) string) string {
	var parts []string
	if !mc.HasAPIKey {
		parts = append(parts, "missing_api_key")
	} else if mc.Ref() == "" {
		parts = append(parts, "invalid")
	}
	if mc.ContextWindow > 0 {
		parts = append(parts, "ctx "+fmtTok(mc.ContextWindow))
	}
	if mc.MaxOutputTokens > 0 {
		parts = append(parts, "out "+fmtTok(mc.MaxOutputTokens))
	}
	if len(mc.Strengths) > 0 {
		parts = append(parts, strings.Join(mc.Strengths, ", "))
	}
	return strings.Join(parts, " · ")
}

func NormalizeGuardMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "readonly", "auto", "smart":
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return "ask"
	}
}

func NextGuardMode(mode string) string {
	switch NormalizeGuardMode(mode) {
	case "ask":
		return "smart"
	case "smart":
		return "auto"
	case "auto":
		return "readonly"
	default:
		return "ask"
	}
}

func SplitCSV(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func ParsePositiveInt(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	if n < 0 {
		return 0
	}
	return n
}
