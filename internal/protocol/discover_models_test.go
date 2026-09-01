package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestConfigDiscoverModelsProtocolFields(t *testing.T) {
	payload, err := json.Marshal(ConfigDiscoverModelsParams{Provider: "provider-a"})
	if err != nil {
		t.Fatalf("marshal params error = %v", err)
	}
	if !strings.Contains(string(payload), `"provider":"provider-a"`) {
		t.Fatalf("params payload = %s, want provider", payload)
	}

	result, err := json.Marshal(ConfigModelsResultParams{Provider: "provider-a", Models: []string{"gpt-5.2", "gpt-5.6-sol"}})
	if err != nil {
		t.Fatalf("marshal result error = %v", err)
	}
	for _, want := range []string{`"provider":"provider-a"`, `"models":["gpt-5.2","gpt-5.6-sol"]`} {
		if !strings.Contains(string(result), want) {
			t.Fatalf("result payload = %s, want %s", result, want)
		}
	}

	// 错误信息不应携带 API Key 明文（daemon 侧已脱敏，协议层保证字段存在即可）。
	errPayload, err := json.Marshal(ConfigModelsResultParams{Provider: "provider-a", ErrorMessage: "unauthorized"})
	if err != nil {
		t.Fatalf("marshal error result error = %v", err)
	}
	if !strings.Contains(string(errPayload), `"error_message":"unauthorized"`) {
		t.Fatalf("error payload = %s, want error_message", errPayload)
	}
}
