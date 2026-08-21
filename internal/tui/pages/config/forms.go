package config

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	coreconfig "github.com/alanchenchen/suna/internal/config"
)

const ProviderFormFieldCount = 10

const (
	ProviderFormProviderIndex = iota
	ProviderFormProtocolIndex
	ProviderFormAuthModeIndex
	ProviderFormModelIndex
	ProviderFormAPIKeyIndex
	ProviderFormEndpointIndex
	ProviderFormContextWindowIndex
	ProviderFormMaxOutputTokensIndex
	ProviderFormStrengthsIndex
	ProviderFormSubtaskForIndex
)

type ProviderFormSpec struct {
	Labels       []string
	Placeholders []string
	Values       []string
	PasswordAt   int
}

type ProviderFormLabels struct {
	Provider        string
	Protocol        string
	AuthMode        string
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
	m.FormProvider = ""
	if ref == "" {
		m.FormTitle = "tui.config.provider.add"
	}
}

func (m *Model) OpenProviderModelForm(provider string) {
	m.WorkspaceOpen = false
	m.FormOpen = true
	m.FormTitle = "tui.config.provider.add_model_to_provider"
	m.EditingName = ""
	m.FormProvider = strings.TrimSpace(provider)
}

func (m *Model) ProviderFormSpec(labels ProviderFormLabels, mc *ModelConfig) ProviderFormSpec {
	fieldLabels := []string{labels.Provider, labels.Protocol, labels.AuthMode, labels.Model, labels.APIKey, labels.Endpoint, labels.ContextWindow, labels.MaxOutputTokens, labels.Strengths, labels.SubtaskFor}
	placeholders := []string{"Zhipu", "OpenAI Chat", "Default", "glm-5.1", "<API_KEY>", "https://api.example.com/v1", "128000", "8192", labels.StrengthsHint, labels.SubtaskForHint}
	values := []string{"", string(coreconfig.ModelProtocolOpenAIChat), "", "", "", "", "", "", "", ""}
	if mc != nil {
		values[ProviderFormProviderIndex] = mc.Provider
		values[ProviderFormProtocolIndex] = string(coreconfig.NormalizeModelProtocol(mc.Protocol))
		values[ProviderFormAuthModeIndex] = string(mc.AuthMode)
		values[ProviderFormModelIndex] = mc.Model
		values[ProviderFormEndpointIndex] = mc.BaseURL
		if mc.ContextWindow > 0 {
			values[ProviderFormContextWindowIndex] = strconv.Itoa(mc.ContextWindow)
		}
		if mc.MaxOutputTokens > 0 {
			values[ProviderFormMaxOutputTokensIndex] = strconv.Itoa(mc.MaxOutputTokens)
		}
		values[ProviderFormStrengthsIndex] = strings.Join(mc.Strengths, ", ")
		values[ProviderFormSubtaskForIndex] = strings.Join(mc.SubtaskFor, ", ")
	} else {
		values[ProviderFormProviderIndex] = ""
		values[ProviderFormProtocolIndex] = string(coreconfig.ModelProtocolOpenAIChat)
	}
	if m.FormProvider != "" {
		values[ProviderFormProviderIndex] = m.FormProvider
	}
	return ProviderFormSpec{Labels: fieldLabels, Placeholders: placeholders, Values: values, PasswordAt: ProviderFormAPIKeyIndex}
}

func ProviderFormValuesFromStrings(values []string) ProviderFormValues {
	vals := make([]string, ProviderFormFieldCount)
	for i := range vals {
		if i < len(values) {
			vals[i] = strings.TrimSpace(values[i])
		}
	}
	return ProviderFormValues{
		Provider:        vals[ProviderFormProviderIndex],
		Protocol:        coreconfig.NormalizeModelProtocol(coreconfig.ModelProtocol(vals[ProviderFormProtocolIndex])),
		AuthMode:        coreconfig.AuthMode(vals[ProviderFormAuthModeIndex]),
		Model:           vals[ProviderFormModelIndex],
		APIKey:          vals[ProviderFormAPIKeyIndex],
		Endpoint:        vals[ProviderFormEndpointIndex],
		ContextWindow:   vals[ProviderFormContextWindowIndex],
		MaxOutputTokens: vals[ProviderFormMaxOutputTokensIndex],
		Strengths:       vals[ProviderFormStrengthsIndex],
		SubtaskFor:      vals[ProviderFormSubtaskForIndex],
	}
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
	m.FormProvider = ""
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
	m.FormProvider = ""
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

func AuthModeOptions() []coreconfig.AuthMode {
	return []coreconfig.AuthMode{"", coreconfig.AuthModeBearer, coreconfig.AuthModeBoth}
}

func NextAuthMode(current coreconfig.AuthMode, delta int) coreconfig.AuthMode {
	options := AuthModeOptions()
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
