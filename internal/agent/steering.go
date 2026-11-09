package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/runner"
)

const (
	MaxSteeringMessages = 32
	MaxSteeringBytes    = 64 * 1024
)

type SteeringState string

const (
	SteeringQueued   SteeringState = "queued"
	SteeringApplied  SteeringState = "applied"
	SteeringRemoved  SteeringState = "removed"
	SteeringRejected SteeringState = "rejected"
)

type SteeringItem struct {
	ID          string
	ClientMsgID string
	Text        string
	State       SteeringState
	Sequence    uint64
}

func (a *Agent) PrepareSteering(runID string) {
	a.setSteeringMailbox(runID)
}

func (a *Agent) ensureSteeringMailbox(runID string) {
	a.steeringMu.Lock()
	if a.steering == nil || a.steering.runID != runID {
		a.steering = newSteeringMailbox(runID)
	}
	a.steeringMu.Unlock()
}

func (a *Agent) setSteeringMailbox(runID string) {
	a.steeringMu.Lock()
	a.steering = newSteeringMailbox(runID)
	a.steeringMu.Unlock()
}

func (a *Agent) clearSteeringMailbox(runID string) {
	a.steeringMu.Lock()
	if a.steering != nil && a.steering.runID == runID {
		a.steering = nil
	}
	a.steeringMu.Unlock()
}

func (a *Agent) EnqueueSteering(runID, clientMsgID, text string) (SteeringItem, bool, error) {
	a.steeringMu.RLock()
	mailbox := a.steering
	a.steeringMu.RUnlock()
	item, created, err := mailbox.enqueue(runID, clientMsgID, text)
	return item, created, err
}

func (a *Agent) RemoveSteering(runID, id string) (SteeringItem, bool, error) {
	a.steeringMu.RLock()
	mailbox := a.steering
	a.steeringMu.RUnlock()
	item, _, removed, err := mailbox.remove(runID, id)
	return item, removed, err
}

func (a *Agent) PendingSteering(runID string) []SteeringItem {
	a.steeringMu.RLock()
	mailbox := a.steering
	a.steeringMu.RUnlock()
	if mailbox == nil || mailbox.runID != runID {
		return nil
	}
	return mailbox.pending()
}

// beginSteeringInteraction 在发出交互前登记等待者；结束函数固定释放原 mailbox，
// 防止旧 run 的迟到收尾影响新 run。同一结束函数可重复调用但只释放一次。
func (a *Agent) beginSteeringInteraction(runID string) func() {
	a.steeringMu.RLock()
	mailbox := a.steering
	a.steeringMu.RUnlock()
	if mailbox == nil || mailbox.runID != runID {
		return func() {}
	}
	return mailbox.beginInteraction()
}

func (a *Agent) RejectPendingSteering(runID string) []SteeringItem {
	a.steeringMu.RLock()
	mailbox := a.steering
	a.steeringMu.RUnlock()
	if mailbox == nil || mailbox.runID != runID {
		return nil
	}
	items := mailbox.rejectPending()
	return items
}

func (a *Agent) sealSteering(runID string) bool {
	a.steeringMu.RLock()
	mailbox := a.steering
	a.steeringMu.RUnlock()
	if mailbox == nil || mailbox.runID != runID {
		return false
	}
	return mailbox.seal()
}

func (a *Agent) takeSteering(runID string, seal bool) []runner.SteeringInput {
	a.steeringMu.RLock()
	mailbox := a.steering
	a.steeringMu.RUnlock()
	if mailbox == nil || mailbox.runID != runID {
		return nil
	}
	return mailbox.take(seal)
}

func (a *Agent) onSteeringApplied(ctx context.Context, input runner.SteeringInput) SteeringItem {
	text := input.Message.Text()
	a.steeringMu.RLock()
	mailbox := a.steering
	a.steeringMu.RUnlock()
	if mailbox != nil {
		mailbox.applied(input.ID)
	}
	a.turnCount++
	a.enqueueMemoryEvent(ctx, model.RoleUser, text, false, false, false, false)
	return SteeringItem{ID: input.ID, ClientMsgID: input.ClientMsgID, Text: text, State: SteeringApplied, Sequence: input.Sequence}
}

type steeringMailbox struct {
	mu               sync.Mutex
	runID            string
	sealed           bool
	interactionCount int
	items            []SteeringItem
	// claimed 是已被 Runner 认领、尚待检查点确认的队首长度；不增加对外状态。
	claimed         int
	byClient        map[string]SteeringItem
	byID            map[string]SteeringItem
	acceptedBytes   int
	nextSequence    uint64
	lastAppliedText string
}

func newSteeringMailbox(runID string) *steeringMailbox {
	return &steeringMailbox{runID: runID, byClient: make(map[string]SteeringItem), byID: make(map[string]SteeringItem)}
}

func (m *steeringMailbox) enqueue(runID, clientMsgID, text string) (SteeringItem, bool, error) {
	if m == nil {
		return SteeringItem{}, false, fmt.Errorf("run_not_steerable")
	}
	text = strings.TrimSpace(text)
	clientMsgID = strings.TrimSpace(clientMsgID)
	if runID == "" || runID != m.runID || clientMsgID == "" || text == "" {
		return SteeringItem{}, false, fmt.Errorf("invalid_steering")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.byClient[clientMsgID]; ok {
		if existing.Text != text {
			return SteeringItem{}, false, fmt.Errorf("client_msg_conflict")
		}
		return existing, false, nil
	}
	if m.sealed {
		return SteeringItem{}, false, fmt.Errorf("run_not_steerable")
	}
	if m.interactionCount > 0 {
		return SteeringItem{}, false, fmt.Errorf("interaction_pending")
	}
	if len(m.byClient) >= MaxSteeringMessages || m.acceptedBytes+len(text) > MaxSteeringBytes {
		return SteeringItem{}, false, fmt.Errorf("steering_queue_full")
	}
	m.nextSequence++
	item := SteeringItem{ID: uuid.NewString(), ClientMsgID: clientMsgID, Text: text, State: SteeringQueued, Sequence: m.nextSequence}
	m.items = append(m.items, item)
	m.byClient[clientMsgID] = item
	m.byID[item.ID] = item
	m.acceptedBytes += len(text)
	return item, true, nil
}

func (m *steeringMailbox) beginInteraction() func() {
	// 计数增减和 steering 的检查、队列修改共用短锁，不持锁等待用户。
	m.mu.Lock()
	m.interactionCount++
	m.mu.Unlock()
	return sync.OnceFunc(func() {
		m.mu.Lock()
		m.interactionCount--
		m.mu.Unlock()
	})
}

func (m *steeringMailbox) remove(runID, id string) (SteeringItem, string, bool, error) {
	if m == nil || runID == "" || runID != m.runID || id == "" {
		return SteeringItem{}, "", false, fmt.Errorf("steering_not_found")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.interactionCount > 0 {
		return SteeringItem{}, "", false, fmt.Errorf("interaction_pending")
	}
	// 认领与撤回共用短锁；已交给 Runner 的副本不能再报告撤回成功。
	for i := len(m.items) - 1; i >= m.claimed; i-- {
		if m.items[i].ID != id {
			continue
		}
		item := m.items[i]
		item.State = SteeringRemoved
		m.items = append(m.items[:i], m.items[i+1:]...)
		m.byClient[item.ClientMsgID] = item
		m.byID[item.ID] = item
		latest := m.lastAppliedText
		if len(m.items) > 0 {
			latest = m.items[len(m.items)-1].Text
		}
		return item, latest, true, nil
	}
	if existing, ok := m.byID[id]; ok && existing.State == SteeringRemoved {
		return existing, "", false, nil
	}
	return SteeringItem{}, "", false, fmt.Errorf("steering_not_found")
}

func (m *steeringMailbox) seal() bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sealed = true
	return len(m.items) > 0
}

func (m *steeringMailbox) take(seal bool) []runner.SteeringInput {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.items) == m.claimed {
		if seal && len(m.items) == 0 {
			m.sealed = true
		}
		return nil
	}
	// 在锁内一次认领，检查点与广播留在锁外；失败时仍由 rejectPending 明确退回。
	out := make([]runner.SteeringInput, 0, len(m.items)-m.claimed)
	for _, item := range m.items[m.claimed:] {
		out = append(out, runner.SteeringInput{ID: item.ID, ClientMsgID: item.ClientMsgID, Sequence: item.Sequence, Message: memory.NewTextMessage(model.RoleUser, item.Text)})
	}
	m.claimed = len(m.items)
	return out
}

func (m *steeringMailbox) rejectPending() []SteeringItem {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	items := append([]SteeringItem(nil), m.items...)
	m.items = nil
	m.claimed = 0
	m.sealed = true
	for i := range items {
		items[i].State = SteeringRejected
		m.byClient[items[i].ClientMsgID] = items[i]
		m.byID[items[i].ID] = items[i]
	}
	return items
}

func (m *steeringMailbox) pending() []SteeringItem {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]SteeringItem(nil), m.items...)
}

func (m *steeringMailbox) applied(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, item := range m.items[:m.claimed] {
		if item.ID != id {
			continue
		}
		item.State = SteeringApplied
		m.byClient[item.ClientMsgID] = item
		m.byID[id] = item
		m.lastAppliedText = item.Text
		m.items = append(m.items[:i], m.items[i+1:]...)
		m.claimed--
		return
	}
}
