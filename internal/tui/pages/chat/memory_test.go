package chat

import (
	"testing"

	"github.com/alanchenchen/suna/internal/protocol"
)

// 清空确认与删除确认一致：enter 直接生效，不需要输入额外文本。
func TestConfirmMemoryClearDirectlyOnEnter(t *testing.T) {
	m := Model{}
	m.Memories = []protocol.MemoryItem{{ID: "m1", Kind: "preference", Content: "prefer concise"}}
	m.MemoryCursor = 0

	if !m.BeginMemoryClear() {
		t.Fatal("BeginMemoryClear should start clear confirm")
	}
	if m.MemoryConfirm != MemoryConfirmClear {
		t.Fatalf("got confirm mode %d, want MemoryConfirmClear", m.MemoryConfirm)
	}
	if !m.ConfirmMemoryClear() {
		t.Fatal("ConfirmMemoryClear should succeed directly on enter")
	}
	if m.MemoryConfirm != MemoryConfirmNone {
		t.Fatalf("got confirm mode %d after confirm, want MemoryConfirmNone", m.MemoryConfirm)
	}
}

// 未进入清空确认时，ConfirmMemoryClear 不应误触发。
func TestConfirmMemoryClearRequiresConfirmMode(t *testing.T) {
	m := Model{}
	if m.ConfirmMemoryClear() {
		t.Fatal("ConfirmMemoryClear should fail without clear confirm mode")
	}
}

// 空列表时清空无意义，BeginMemoryClear 应拒绝。
func TestBeginMemoryClearRejectsEmpty(t *testing.T) {
	m := Model{}
	if m.BeginMemoryClear() {
		t.Fatal("BeginMemoryClear should reject empty memory list")
	}
	if m.MemoryConfirm != MemoryConfirmNone {
		t.Fatalf("got confirm mode %d, want MemoryConfirmNone", m.MemoryConfirm)
	}
}
