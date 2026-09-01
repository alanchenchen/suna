package protocol

import "encoding/json"

// ConfigModelField 标识 config.set/upsert_model 请求中显式出现的模型字段。
type ConfigModelField uint16

const (
	ConfigModelFieldProvider ConfigModelField = 1 << iota
	ConfigModelFieldProtocol
	ConfigModelFieldAuthMode
	ConfigModelFieldModel
	ConfigModelFieldBaseURL
	ConfigModelFieldContextWindow
	ConfigModelFieldMaxOutputTokens
	ConfigModelFieldStrengths
	ConfigModelFieldSubtaskFor
	ConfigModelFieldReasoning
)

var configModelFields = map[string]ConfigModelField{
	"provider": ConfigModelFieldProvider, "protocol": ConfigModelFieldProtocol,
	"auth_mode": ConfigModelFieldAuthMode, "model": ConfigModelFieldModel,
	"base_url": ConfigModelFieldBaseURL, "context_window": ConfigModelFieldContextWindow,
	"max_output_tokens": ConfigModelFieldMaxOutputTokens, "strengths": ConfigModelFieldStrengths,
	"subtask_for": ConfigModelFieldSubtaskFor, "reasoning": ConfigModelFieldReasoning,
}

// ConfigModel 同时承载配置完整快照和 upsert_model 输入。
// present 只记录 JSON 请求实际提供的字段，不进入响应。
type ConfigModel struct {
	Provider        string         `json:"provider"`
	Protocol        string         `json:"protocol"`
	AuthMode        string         `json:"auth_mode"`
	Model           string         `json:"model"`
	BaseURL         string         `json:"base_url"`
	ContextWindow   int            `json:"context_window"`
	MaxOutputTokens int            `json:"max_output_tokens"`
	Strengths       []string       `json:"strengths"`
	SubtaskFor      []string       `json:"subtask_for"`
	Reasoning       map[string]any `json:"reasoning"`
	HasAPIKey       bool           `json:"has_api_key,omitempty"`
	// APIKeyHint 是脱敏后的 key 提示（如 sk-••••abcd），只用于展示；
	// 响应专有字段，不参与 upsert_model 的 presence 语义，客户端不应回写。
	APIKeyHint string `json:"api_key_hint,omitempty"`
	present    ConfigModelField
	decoded    bool
}

func (m ConfigModel) MarshalJSON() ([]byte, error) {
	type modelWire ConfigModel
	wire := modelWire(m)
	if wire.Strengths == nil {
		wire.Strengths = []string{}
	}
	if wire.SubtaskFor == nil {
		wire.SubtaskFor = []string{}
	}
	if wire.Reasoning == nil {
		wire.Reasoning = map[string]any{}
	}
	return json.Marshal(wire)
}

func (m *ConfigModel) UnmarshalJSON(data []byte) error {
	type modelWire ConfigModel
	var decoded modelWire
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*m = ConfigModel(decoded)
	m.decoded = true
	for name, field := range configModelFields {
		if _, ok := raw[name]; ok {
			m.present |= field
		}
	}
	return nil
}

// Has 报告模型字段是否由客户端显式提供；进程内构造的完整模型默认所有字段均已提供。
func (m ConfigModel) Has(field ConfigModelField) bool {
	return !m.decoded || m.present&field != 0
}
