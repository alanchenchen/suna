package config

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const ProviderFormFieldCount = 7

type ProviderFormSpec struct {
	Labels       []string
	Placeholders []string
	Values       []string
	PasswordAt   int
}

type ProviderFormLabels struct {
	Provider        string
	Model           string
	APIKey          string
	Endpoint        string
	ContextWindow   string
	MaxOutputTokens string
	Strengths       string
	StrengthsHint   string
}

func (m *Model) OpenProviderForm(ref string, mc *ModelConfig) {
	m.WorkspaceOpen = false
	m.FormOpen = true
	m.FormTitle = "tui.config.provider.edit"
	m.EditingName = ref
	if ref == "" {
		m.FormTitle = "tui.config.provider.add"
	}
}

func (m *Model) ProviderFormSpec(labels ProviderFormLabels, mc *ModelConfig) ProviderFormSpec {
	fieldLabels := []string{labels.Provider, labels.Model, labels.APIKey, labels.Endpoint, labels.ContextWindow, labels.MaxOutputTokens, labels.Strengths}
	placeholders := []string{"Zhipu", "glm-5.1", "sk-...", "https://api.example.com/v1", "128000", "8192", labels.StrengthsHint}
	values := []string{"", "", "", "", "", "", ""}
	if mc != nil {
		values[0] = mc.Provider
		values[1] = mc.Model
		values[3] = mc.BaseURL
		if mc.ContextWindow > 0 {
			values[4] = strconv.Itoa(mc.ContextWindow)
		}
		if mc.MaxOutputTokens > 0 {
			values[5] = strconv.Itoa(mc.MaxOutputTokens)
		}
		values[6] = strings.Join(mc.Strengths, ", ")
	} else {
		switch m.ProviderKind {
		case "openai":
			values[0] = "openai"
			values[3] = "https://api.openai.com/v1"
			placeholders[1] = "gpt-4o-mini"
		case "anthropic":
			values[0] = "anthropic"
			values[3] = "https://api.anthropic.com"
			placeholders[1] = "claude-sonnet-4-20250514"
			placeholders[4] = "200000"
			placeholders[5] = "8192"
		}
	}
	return ProviderFormSpec{Labels: fieldLabels, Placeholders: placeholders, Values: values, PasswordAt: 2}
}

func ProviderFormValuesFromStrings(values []string) ProviderFormValues {
	vals := make([]string, ProviderFormFieldCount)
	for i := range vals {
		if i < len(values) {
			vals[i] = strings.TrimSpace(values[i])
		}
	}
	return ProviderFormValues{Provider: vals[0], Model: vals[1], APIKey: vals[2], Endpoint: vals[3], ContextWindow: vals[4], MaxOutputTokens: vals[5], Strengths: vals[6]}
}

type ProviderValidationLabels struct {
	Required               string
	APIKeyRequired         string
	EndpointRequired       string
	InvalidEndpoint        string
	InvalidContextWindow   string
	InvalidMaxOutputTokens string
}

func ValidateProviderForm(v ProviderFormValues, setupMode bool, labels ProviderValidationLabels) error {
	if v.Provider == "" || v.Model == "" {
		return fmt.Errorf("%s", labels.Required)
	}
	if setupMode && v.APIKey == "" {
		return fmt.Errorf("%s", labels.APIKeyRequired)
	}
	if v.Endpoint == "" {
		return fmt.Errorf("%s", labels.EndpointRequired)
	}
	u, err := url.Parse(v.Endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("%s", labels.InvalidEndpoint)
	}
	ctx, err := strconv.Atoi(v.ContextWindow)
	if err != nil || ctx <= 0 {
		return fmt.Errorf("%s", labels.InvalidContextWindow)
	}
	out, err := strconv.Atoi(v.MaxOutputTokens)
	if err != nil || out <= 0 || out >= ctx {
		return fmt.Errorf("%s", labels.InvalidMaxOutputTokens)
	}
	return nil
}

func (m *Model) CloseProviderForm() bool {
	if m.SetupMode {
		m.FormOpen = false
		return true
	}
	m.FormOpen = false
	return false
}

func (m *Model) FocusInput(idx, count int) bool {
	if idx < 0 || idx >= count {
		return false
	}
	m.InputFocus = idx
	return true
}

func (m *Model) NextInput(count int) (int, bool) {
	if count <= 0 {
		return 0, false
	}
	idx := m.InputFocus + 1
	if idx >= count {
		idx = count - 1
	}
	return idx, true
}

func (m *Model) PrevInput(count int) (int, bool) {
	if count <= 0 {
		return 0, false
	}
	idx := m.InputFocus - 1
	if idx < 0 {
		idx = 0
	}
	return idx, true
}

func (m *Model) OpenWorkspaceForm() {
	m.WorkspaceOpen = true
	m.FormOpen = true
	m.FormTitle = "tui.config.workspace.edit"
	m.EditingName = ""
}

func (m *Model) CloseFormToWelcome() {
	m.FormOpen = false
}

func (m *Model) CloseForm() {
	m.FormOpen = false
	m.WorkspaceOpen = false
}
