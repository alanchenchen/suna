package memory

import (
	"sync"

	"github.com/alanchenchen/suna/internal/model"
)

type WorkingMemory struct {
	mu        sync.RWMutex
	messages  []model.Message
	taskState map[string]any
}

func NewWorkingMemory() *WorkingMemory {
	return &WorkingMemory{
		taskState: make(map[string]any),
	}
}

func (w *WorkingMemory) AddMessage(msg model.Message) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.messages = append(w.messages, msg)
}

func (w *WorkingMemory) Messages() []model.Message {
	w.mu.RLock()
	defer w.mu.RUnlock()
	cp := make([]model.Message, len(w.messages))
	copy(cp, w.messages)
	return cp
}

func (w *WorkingMemory) SetMessages(msgs []model.Message) {
	w.mu.Lock()
	defer w.mu.Unlock()
	// SetMessages 是 compact 后改写上下文的内存边界；必须复制 slice，避免保留旧历史的 backing array。
	cp := make([]model.Message, len(msgs))
	copy(cp, msgs)
	w.messages = cp
}

func (w *WorkingMemory) LastN(n int) []model.Message {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if len(w.messages) <= n {
		cp := make([]model.Message, len(w.messages))
		copy(cp, w.messages)
		return cp
	}
	cp := make([]model.Message, n)
	copy(cp, w.messages[len(w.messages)-n:])
	return cp
}

func (w *WorkingMemory) Len() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return len(w.messages)
}

func (w *WorkingMemory) SetState(key string, value any) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.taskState[key] = value
}

func (w *WorkingMemory) GetState(key string) (any, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	v, ok := w.taskState[key]
	return v, ok
}

func (w *WorkingMemory) Clear() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.messages = nil
	w.taskState = make(map[string]any)
}

func (w *WorkingMemory) EstimatedTokens() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return model.EstimateMessagesTokens(w.messages)
}

// LastUserText 返回最后一条用户消息的文本内容
func (w *WorkingMemory) LastUserText() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	for i := len(w.messages) - 1; i >= 0; i-- {
		if w.messages[i].Role == model.RoleUser {
			return w.messages[i].Text()
		}
	}
	return ""
}
