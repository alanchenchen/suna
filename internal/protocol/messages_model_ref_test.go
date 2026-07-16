package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAgentRunErrorProtocolFields(t *testing.T) {
	payload, err := json.Marshal(AgentRunParams{
		State: AgentRunFailed,
		RunError: &RunError{
			Kind:     RunErrorSessionModelUnavailable,
			ModelRef: "openai/gpt-5.6",
		},
	})
	if err != nil {
		t.Fatalf("marshal AgentRunParams error = %v", err)
	}
	for _, want := range []string{`"run_error"`, `"kind":"session_model_unavailable"`, `"model_ref":"openai/gpt-5.6"`} {
		if !strings.Contains(string(payload), want) {
			t.Fatalf("run payload = %s, want %s", payload, want)
		}
	}
}

func TestSessionModelRefProtocolFields(t *testing.T) {
	modelRef := "openai/gpt-4.1"
	payload, err := json.Marshal(SessionUpdateParams{SessionID: "session-1", ModelRef: &modelRef})
	if err != nil {
		t.Fatalf("marshal update params error = %v", err)
	}
	if !strings.Contains(string(payload), `"model_ref":"openai/gpt-4.1"`) {
		t.Fatalf("update payload = %s, want model_ref", payload)
	}

	var params SessionUpdateParams
	if err := json.Unmarshal([]byte(`{"session_id":"session-1","model_ref":"anthropic/claude-sonnet-4"}`), &params); err != nil {
		t.Fatalf("unmarshal update params error = %v", err)
	}
	if params.ModelRef == nil || *params.ModelRef != "anthropic/claude-sonnet-4" {
		t.Fatalf("ModelRef = %#v, want anthropic/claude-sonnet-4", params.ModelRef)
	}

	info, err := json.Marshal(SessionInfo{ID: "session-1", CWD: "/tmp", ModelRef: modelRef})
	if err != nil {
		t.Fatalf("marshal session info error = %v", err)
	}
	if !strings.Contains(string(info), `"model_ref":"openai/gpt-4.1"`) {
		t.Fatalf("session info payload = %s, want model_ref", info)
	}
}
