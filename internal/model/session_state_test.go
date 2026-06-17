package model

import (
	"context"
	"strings"
	"testing"
)

func TestOpenAIChatBuildMessagesIncludesSessionStateBeforeHistory(t *testing.T) {
	p := NewOpenAIChatProvider("test-key", "", "gpt-test", 128000, 8192, nil)
	req := &CompletionRequest{
		System:       "system prompt",
		SessionState: "早期决策：保持独立字段。",
		Messages:     []Message{NewTextMessage(RoleUser, "继续")},
	}

	msgs, err := p.buildMessages(context.Background(), req)
	if err != nil {
		t.Fatalf("buildMessages() error = %v", err)
	}
	if got, want := len(msgs), 3; got != want {
		t.Fatalf("len(msgs) = %d, want %d", got, want)
	}
	stateBytes, err := msgs[1].MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON(session state) error = %v", err)
	}
	state := string(stateBytes)
	if !strings.Contains(state, "<session_state>") || !strings.Contains(state, "早期决策") {
		t.Fatalf("session state message = %s, want wrapped state", state)
	}
	historyBytes, err := msgs[2].MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON(history) error = %v", err)
	}
	if !strings.Contains(string(historyBytes), "继续") {
		t.Fatalf("history message = %s, want user history", string(historyBytes))
	}
}

func TestOpenAIResponsesBuildInputIncludesSessionStateBeforeHistory(t *testing.T) {
	p := NewOpenAIResponsesProvider("test-key", "", "gpt-test", 128000, 8192, nil)
	req := &CompletionRequest{
		SessionState: "早期事实：已经完成 compact。",
		Messages:     []Message{NewTextMessage(RoleUser, "继续")},
	}

	input, err := p.buildInput(context.Background(), req)
	if err != nil {
		t.Fatalf("buildInput() error = %v", err)
	}
	if got, want := len(input), 2; got != want {
		t.Fatalf("len(input) = %d, want %d", got, want)
	}
	stateBytes, err := input[0].MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON(session state) error = %v", err)
	}
	if state := string(stateBytes); !strings.Contains(state, "<session_state>") || !strings.Contains(state, "早期事实") {
		t.Fatalf("session state input = %s, want wrapped state", state)
	}
}

func TestAnthropicBuildMessagesIncludesSessionStateBeforeHistory(t *testing.T) {
	p := NewAnthropicProvider("test-key", "", "claude-test", 128000, 8192, nil)
	req := &CompletionRequest{
		SessionState: "早期约束：不要污染 WorkingMemory。",
		Messages:     []Message{NewTextMessage(RoleUser, "继续")},
	}

	msgs, err := p.buildMessages(context.Background(), req)
	if err != nil {
		t.Fatalf("buildMessages() error = %v", err)
	}
	if got, want := len(msgs), 2; got != want {
		t.Fatalf("len(msgs) = %d, want %d", got, want)
	}
	stateBytes, err := msgs[0].MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON(session state) error = %v", err)
	}
	if state := string(stateBytes); !strings.Contains(state, "session_state") || !strings.Contains(state, "早期约束") {
		t.Fatalf("session state message = %s, want wrapped state", state)
	}
}
