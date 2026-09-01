package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestConfigModelDistinguishesMissingAndExplicitEmptyFields(t *testing.T) {
	var missing ConfigSetParams
	if err := json.Unmarshal([]byte(`{"action":"upsert_model","model":{"base_url":"https://api.example.com"}}`), &missing); err != nil {
		t.Fatalf("Unmarshal(missing) error = %v", err)
	}
	for _, field := range []ConfigModelField{ConfigModelFieldAuthMode, ConfigModelFieldStrengths, ConfigModelFieldSubtaskFor, ConfigModelFieldReasoning} {
		if missing.Model.Has(field) {
			t.Fatalf("missing model unexpectedly has field %d", field)
		}
	}

	var explicit ConfigSetParams
	if err := json.Unmarshal([]byte(`{"action":"upsert_model","model":{"auth_mode":"default","strengths":[],"subtask_for":[],"reasoning":{}}}`), &explicit); err != nil {
		t.Fatalf("Unmarshal(explicit) error = %v", err)
	}
	for _, field := range []ConfigModelField{ConfigModelFieldAuthMode, ConfigModelFieldStrengths, ConfigModelFieldSubtaskFor, ConfigModelFieldReasoning} {
		if !explicit.Model.Has(field) {
			t.Fatalf("explicit model missing field %d", field)
		}
	}
	if explicit.Model.AuthMode != "default" || len(explicit.Model.Strengths) != 0 || len(explicit.Model.SubtaskFor) != 0 || len(explicit.Model.Reasoning) != 0 {
		t.Fatalf("explicit empty model = %#v", explicit.Model)
	}
}

// api_key_hint 是脱敏展示字段：非空时序列化输出，空时省略；
// 反序列化不能把它当作用户输入（presence 不记录它，回写不会覆盖配置）。
func TestConfigModelAPIKeyHintWireSemantics(t *testing.T) {
	encoded, err := json.Marshal(ConfigModel{Provider: "provider-a", Model: "model-a", APIKeyHint: "sk-****abcd"})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if !strings.Contains(string(encoded), `"api_key_hint":"sk-****abcd"`) {
		t.Fatalf("Marshal() = %s, want api_key_hint field", encoded)
	}

	encoded, err = json.Marshal(ConfigModel{Provider: "provider-a", Model: "model-a"})
	if err != nil {
		t.Fatalf("Marshal() empty hint error = %v", err)
	}
	if strings.Contains(string(encoded), "api_key_hint") {
		t.Fatalf("Marshal() = %s, want empty hint omitted", encoded)
	}
}

func TestConfigModelMarshalIncludesExplicitEmptyValues(t *testing.T) {
	encoded, err := json.Marshal(ConfigModel{Provider: "provider-a", Protocol: "anthropic", Model: "model-a"})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	got := string(encoded)
	for _, want := range []string{`"auth_mode":""`, `"strengths":[]`, `"subtask_for":[]`, `"reasoning":{}`} {
		if !strings.Contains(got, want) {
			t.Fatalf("Marshal() = %s, want %s", got, want)
		}
	}
}
