package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSessionIDSerializedOnRunEvents(t *testing.T) {
	// 多 session 并存的客户端靠 session_id 区分事件归属；字段必须出现在序列化结果中。
	payload, err := json.Marshal(AgentRunParams{SessionID: "session-1", RunID: "run-1", State: AgentRunRunning})
	if err != nil {
		t.Fatalf("marshal AgentRunParams error = %v", err)
	}
	if !strings.Contains(string(payload), `"session_id":"session-1"`) {
		t.Fatalf("run payload = %s, want session_id", payload)
	}

	delta, err := json.Marshal(AgentDeltaParams{SessionID: "session-1", Kind: AgentDeltaAssistant, Content: "hi"})
	if err != nil {
		t.Fatalf("marshal AgentDeltaParams error = %v", err)
	}
	if !strings.Contains(string(delta), `"session_id":"session-1"`) {
		t.Fatalf("delta payload = %s, want session_id", delta)
	}

	usage, err := json.Marshal(UsageParams{SessionID: "session-1", InputTokens: 1, OutputTokens: 1})
	if err != nil {
		t.Fatalf("marshal UsageParams error = %v", err)
	}
	if !strings.Contains(string(usage), `"session_id":"session-1"`) {
		t.Fatalf("usage payload = %s, want session_id", usage)
	}

	tool, err := json.Marshal(ToolStartParams{SessionID: "session-1", ID: "tool-1", Tool: "readfile"})
	if err != nil {
		t.Fatalf("marshal ToolStartParams error = %v", err)
	}
	if !strings.Contains(string(tool), `"session_id":"session-1"`) {
		t.Fatalf("tool payload = %s, want session_id", tool)
	}

	compact, err := json.Marshal(CompactResult{SessionID: "session-1", BeforeTokens: 10, AfterTokens: 5})
	if err != nil {
		t.Fatalf("marshal CompactResult error = %v", err)
	}
	if !strings.Contains(string(compact), `"session_id":"session-1"`) {
		t.Fatalf("compact payload = %s, want session_id", compact)
	}
}
