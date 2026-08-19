package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"

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
	if err == nil && created {
		a.updateGuardTaskInput(text)
	}
	return item, created, err
}

func (a *Agent) RemoveSteering(runID, id string) (SteeringItem, bool, error) {
	a.steeringMu.RLock()
	mailbox := a.steering
	a.steeringMu.RUnlock()
	item, latest, removed, err := mailbox.remove(runID, id)
	if err == nil && removed {
		if latest == "" {
			latest = a.activeGuardTaskText()
		}
		a.updateGuardTaskInput(latest)
	}
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

func (a *Agent) SetSteeringInteractionPending(runID string, pending bool) {
	a.steeringMu.RLock()
	mailbox := a.steering
	a.steeringMu.RUnlock()
	if mailbox != nil && mailbox.runID == runID {
		mailbox.setInteractionPending(pending)
	}
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
	a.turnCount++
	a.updateGuardTaskInput(text)
	a.enqueueMemoryEvent(ctx, model.RoleUser, text, false, false, false, false)
	return SteeringItem{ID: input.ID, ClientMsgID: input.ClientMsgID, Text: text, State: SteeringApplied, Sequence: input.Sequence}
}

type steeringMailbox struct {
	mu              sync.Mutex
	runID           string
	sealed          bool
	interaction     bool
	items           []SteeringItem
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
	if m.interaction {
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

func (m *steeringMailbox) setInteractionPending(pending bool) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.interaction = pending
	m.mu.Unlock()
}

func (m *steeringMailbox) remove(runID, id string) (SteeringItem, string, bool, error) {
	if m == nil || runID == "" || runID != m.runID || id == "" {
		return SteeringItem{}, "", false, fmt.Errorf("steering_not_found")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.interaction {
		return SteeringItem{}, "", false, fmt.Errorf("interaction_pending")
	}
	for i := len(m.items) - 1; i >= 0; i-- {
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
	if len(m.items) == 0 {
		if seal {
			m.sealed = true
		}
		return nil
	}
	items := append([]SteeringItem(nil), m.items...)
	m.items = nil
	out := make([]runner.SteeringInput, 0, len(items))
	for _, item := range items {
		item.State = SteeringApplied
		m.byClient[item.ClientMsgID] = item
		m.byID[item.ID] = item
		m.lastAppliedText = item.Text
		out = append(out, runner.SteeringInput{ID: item.ID, ClientMsgID: item.ClientMsgID, Sequence: item.Sequence, Message: model.NewTextMessage(model.RoleUser, item.Text)})
	}
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
