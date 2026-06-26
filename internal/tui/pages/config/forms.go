package config

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	coreconfig "github.com/alanchenchen/suna/internal/config"
)

const ProviderFormFieldCount = 9

const ProviderFormProtocolIndex = 1

type ProviderFormSpec struct {
	Labels       []string
	Placeholders []string
	Values       []string
	PasswordAt   int
}

type ProviderFormLabels struct {
	Provider        string
	Protocol        string
	Model           string
	APIKey          string
	Endpoint        string
	ContextWindow   string
	MaxOutputTokens string
	Strengths       string
	SubtaskFor      string
	StrengthsHint   string
	SubtaskForHint  string
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
	fieldLabels := []string{labels.Provider, labels.Protocol, labels.Model, labels.APIKey, labels.Endpoint, labels.ContextWindow, labels.MaxOutputTokens, labels.Strengths, labels.SubtaskFor}
	placeholders := []string{"Zhipu", "OpenAI Chat", "glm-5.1", "sk-...", "https://api.example.com/v1", "128000", "8192", labels.StrengthsHint, labels.SubtaskForHint}
	values := []string{"", string(coreconfig.ModelProtocolOpenAIChat), "", "", "", "", "", "", ""}
	if mc != nil {
		values[0] = mc.Provider
		values[1] = string(coreconfig.NormalizeModelProtocol(mc.Protocol))
		values[2] = mc.Model
		values[4] = mc.BaseURL
		if mc.ContextWindow > 0 {
			values[5] = strconv.Itoa(mc.ContextWindow)
		}
		if mc.MaxOutputTokens > 0 {
			values[6] = strconv.Itoa(mc.MaxOutputTokens)
		}
		values[7] = strings.Join(mc.Strengths, ", ")
		values[8] = strings.Join(mc.SubtaskFor, ", ")
	} else {
		switch m.ProviderKind {
		case "openai":
			values[0] = "openai"
			values[1] = string(coreconfig.ModelProtocolOpenAIResponses)
			values[4] = "https://api.openai.com/v1"
			placeholders[2] = "gpt-4o-mini"
		case "anthropic":
			values[0] = "anthropic"
			values[1] = string(coreconfig.ModelProtocolAnthropic)
			values[4] = "https://api.anthropic.com"
			placeholders[2] = "claude-sonnet-4-20250514"
			placeholders[5] = "200000"
			placeholders[6] = "8192"
		}
	}
	return ProviderFormSpec{Labels: fieldLabels, Placeholders: placeholders, Values: values, PasswordAt: 3}
}

func ProviderFormValuesFromStrings(values []string) ProviderFormValues {
	vals := make([]string, ProviderFormFieldCount)
	for i := range vals {
		if i < len(values) {
			vals[i] = strings.TrimSpace(values[i])
		}
	}
	return ProviderFormValues{Provider: vals[0], Protocol: coreconfig.NormalizeModelProtocol(coreconfig.ModelProtocol(vals[1])), Model: vals[2], APIKey: vals[3], Endpoint: vals[4], ContextWindow: vals[5], MaxOutputTokens: vals[6], Strengths: vals[7], SubtaskFor: vals[8]}
}

type ProviderValidationLabels struct {
	Required               string
	APIKeyRequired         string
	EndpointRequired       string
	InvalidEndpoint        string
	InvalidContextWindow   string
	InvalidMaxOutputTokens string
	InvalidProtocol        string
}

func ValidateProviderForm(v ProviderFormValues, setupMode bool, labels ProviderValidationLabels) error {
	if v.Provider == "" || v.Model == "" {
		return fmt.Errorf("%s", labels.Required)
	}
	if !coreconfig.IsSupportedModelProtocol(v.Protocol) {
		return fmt.Errorf("%s", labels.InvalidProtocol)
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

func ProviderProtocolOptions() []coreconfig.ModelProtocol {
	return coreconfig.SupportedModelProtocols()
}

func NextProviderProtocol(current coreconfig.ModelProtocol, delta int) coreconfig.ModelProtocol {
	options := ProviderProtocolOptions()
	if len(options) == 0 {
		return coreconfig.ModelProtocolOpenAIChat
	}
	current = coreconfig.NormalizeModelProtocol(current)
	idx := 0
	for i, option := range options {
		if option == current {
			idx = i
			break
		}
	}
	idx = (idx + delta) % len(options)
	if idx < 0 {
		idx += len(options)
	}
	return options[idx]
}

func ModelProtocolValue(value string) coreconfig.ModelProtocol {
	return coreconfig.ModelProtocol(value)
}
