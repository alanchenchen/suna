package daemon

import (
	"testing"

	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/protocol"
)

// 用户手动输入以 [image: 开头的普通文本（非系统生成的摘要）时，
// 识别条件（前缀 + source=）应将其判为普通文本，不误判为 media。
func TestSnapshotMessageKindUserTextWithImagePrefix(t *testing.T) {
	got := snapshotMessageKind(model.RoleUser, "[image: 这个方案怎么样]")
	if got != protocol.SnapshotMessageKindText {
		t.Fatalf("snapshotMessageKind() = %q, want text (prefix without source= is not a summary)", got)
	}
}

func TestSnapshotMessageKindClassifiesMessages(t *testing.T) {
	cases := []struct {
		name string
		role model.Role
		text string
		want protocol.SnapshotMessageKind
	}{
		{"assistant", model.RoleAssistant, "回答内容", protocol.SnapshotMessageKindText},
		{"plain user", model.RoleUser, "普通提问", protocol.SnapshotMessageKindText},
		{"image summary", model.RoleUser, "[image: photo.png, image/png, 1.2MB, source=attachment:photo.png]", protocol.SnapshotMessageKindMedia},
		{"legacy uploaded summary", model.RoleUser, "[uploaded image: photo.png, image/png, 1.2MB, source=attachment:photo.png]", protocol.SnapshotMessageKindMedia},
		{"tool role falls back to text", model.RoleTool, "tool text", protocol.SnapshotMessageKindText},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := snapshotMessageKind(tt.role, tt.text)
			if got != tt.want {
				t.Fatalf("snapshotMessageKind(%q, %q) = %q, want %q", tt.role, tt.text, got, tt.want)
			}
		})
	}
}

func TestIsMediaSummaryText(t *testing.T) {
	if !isMediaSummaryText("[image: a.png, image/png, 1.2MB, source=attachment:a.png]") {
		t.Fatal("isMediaSummaryText() = false, want true for image summary")
	}
	if !isMediaSummaryText("[uploaded image: a.png, image/png, 1.2MB, source=attachment:a.png]") {
		t.Fatal("isMediaSummaryText() = false, want true for legacy uploaded summary")
	}
	if isMediaSummaryText("普通用户消息") {
		t.Fatal("isMediaSummaryText() = true, want false for plain text")
	}
	if isMediaSummaryText("image: 无方括号前缀") {
		t.Fatal("isMediaSummaryText() = true, want false for text without bracket prefix")
	}
	// 用户手动输入以 [image: 开头但无 source= 的普通文本，不应误判为媒体摘要。
	if isMediaSummaryText("[image: 这个方案怎么样]") {
		t.Fatal("isMediaSummaryText() = true, want false for user text with image prefix but no source")
	}
}

// 误判逻辑完整边界矩阵：所有输入形态的判定。
func TestSnapshotKindEdgeMatrix(t *testing.T) {
	cases := []struct {
		name string
		text string
		want protocol.SnapshotMessageKind
	}{
		// 系统生成的新格式摘要（三种来源都带 source=）
		{"new attachment", "[image: a.png, image/png, 1.2MB, source=attachment:a.png]", protocol.SnapshotMessageKindMedia},
		{"new path", "[image: a.png, image/png, 270.0KB, source=/abs/a.png]", protocol.SnapshotMessageKindMedia},
		{"new url", "[image: a.png, image/png, source=https://example.com/a.png]", protocol.SnapshotMessageKindMedia},
		// 旧格式摘要（sourceKind 非空时带 source=）
		{"legacy", "[uploaded image: a.png, image/png, 1.2MB, source=attachment]", protocol.SnapshotMessageKindMedia},
		// 用户手动输入撞前缀但无 source= → 不误判
		{"user text with prefix", "[image: 这个方案怎么样]", protocol.SnapshotMessageKindText},
		// 普通消息
		{"plain", "普通用户消息", protocol.SnapshotMessageKindText},
		// 系统不会生成但理论存在的形态：前缀无 source → 安全降级为 text（显示原文，不丢数据）
		{"prefix no source", "[image: a.png, image/png, 1.2MB]", protocol.SnapshotMessageKindText},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := snapshotMessageKind(model.RoleUser, tt.text)
			if got != tt.want {
				t.Fatalf("snapshotMessageKind(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}
