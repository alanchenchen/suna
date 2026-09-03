package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

// 旧 daemon 不返回 kind 字段时，反序列化应得到零值（空字符串），
// 客户端按普通消息处理，不 panic。
func TestSnapshotMessageKindMissingFieldCompat(t *testing.T) {
	raw := `[{"role":"user","content":"旧消息"}]`
	var msgs []SnapshotMessage
	if err := json.Unmarshal([]byte(raw), &msgs); err != nil {
		t.Fatalf("json.Unmarshal() error: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Kind != "" {
		t.Fatalf("unmarshal = %+v, want zero kind for missing field", msgs)
	}
}
func TestSnapshotMessageKindJSON(t *testing.T) {
	msgs := []SnapshotMessage{
		{Role: "user", Content: "普通消息", Kind: SnapshotMessageKindText},
		{Role: "user", Content: "[image: a.png, image/png, 1.2MB, source=attachment:a.png]", Kind: SnapshotMessageKindMedia},
		{Role: "assistant", Content: "回答", Kind: SnapshotMessageKindText},
	}
	b, err := json.Marshal(msgs)
	if err != nil {
		t.Fatalf("json.Marshal() error: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, `"kind":"text"`) || !strings.Contains(s, `"kind":"media"`) {
		t.Fatalf("json = %s, want kind text and media", s)
	}
	// 反序列化验证字段完整。
	var back []SnapshotMessage
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("json.Unmarshal() error: %v", err)
	}
	if len(back) != 3 || back[1].Kind != SnapshotMessageKindMedia {
		t.Fatalf("unmarshal = %+v, want 3 messages with media kind at index 1", back)
	}
}
