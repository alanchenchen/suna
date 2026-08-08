package daemon

import (
	"fmt"

	"github.com/alanchenchen/suna/internal/protocol"
	"github.com/google/uuid"
)

// publish 在 runNotifyMu 下执行，保证 cancelling 一定排在随后的终态之前。
func (m *sessionManager) markCancelling(connID string, publish func(*sessionRuntime, string, string, protocol.AgentRunPhase)) (newlyMarked bool, err error) {
	m.mu.Lock()
	id := m.attached[connID]
	rt := m.runtime[id]
	m.mu.Unlock()
	if id == "" {
		return false, fmt.Errorf("session_required")
	}
	if rt == nil {
		return false, fmt.Errorf("session not running")
	}
	rt.runNotifyMu.Lock()
	defer rt.runNotifyMu.Unlock()
	m.mu.Lock()
	if m.attached[connID] != id || m.runtime[id] != rt || rt.status == sessionIdle || rt.runID == "" {
		m.mu.Unlock()
		return false, fmt.Errorf("session not running")
	}
	if rt.runOwner != connID {
		m.mu.Unlock()
		return false, fmt.Errorf("session_busy")
	}
	if rt.runState == protocol.AgentRunCancelling {
		m.mu.Unlock()
		return false, nil
	}
	// 终态已经完成本轮 lifecycle，迟到的 cancel 不能把它重新打开为 cancelling。
	switch rt.runState {
	case protocol.AgentRunDone, protocol.AgentRunFailed, protocol.AgentRunCancelled:
		m.mu.Unlock()
		return false, fmt.Errorf("session not running")
	}
	rt.runState = protocol.AgentRunCancelling
	runID, phase := rt.runID, rt.phase
	m.mu.Unlock()
	publish(rt, id, runID, phase)
	return true, nil
}

// transitionRunState 在锁内收敛 Agent 事件与取消 RPC 的竞争。进入 cancelling 后，
// running/retrying 不再回退状态，任一终态都统一变为 cancelled，且终态只广播一次。
func (m *sessionManager) transitionRunState(sessionID, runID string, next protocol.AgentRunState, publish func(protocol.AgentRunState)) bool {
	m.mu.RLock()
	rt := m.runtime[sessionID]
	m.mu.RUnlock()
	if rt == nil {
		return false
	}
	rt.runNotifyMu.Lock()
	defer rt.runNotifyMu.Unlock()
	m.mu.Lock()
	if m.runtime[sessionID] != rt || rt.runID == "" || rt.runID != runID {
		m.mu.Unlock()
		return false
	}
	state, accepted := next, true
	switch rt.runState {
	case protocol.AgentRunCancelling:
		switch next {
		case protocol.AgentRunRunning, protocol.AgentRunRetrying:
			accepted = false
		default:
			state = protocol.AgentRunCancelled
			rt.runState = state
		}
	case protocol.AgentRunDone, protocol.AgentRunFailed, protocol.AgentRunCancelled:
		accepted = false
	default:
		rt.runState = next
	}
	m.mu.Unlock()
	if accepted && publish != nil {
		publish(state)
	}
	return accepted
}

// finishCancelling 只在 event stream 关闭但未携带终态时补齐取消终态。
func (m *sessionManager) finishCancelling(sessionID, runID string, publish func()) bool {
	m.mu.RLock()
	rt := m.runtime[sessionID]
	m.mu.RUnlock()
	if rt == nil {
		return false
	}
	rt.runNotifyMu.Lock()
	defer rt.runNotifyMu.Unlock()
	m.mu.Lock()
	if m.runtime[sessionID] != rt || rt.runID != runID || rt.runState != protocol.AgentRunCancelling {
		m.mu.Unlock()
		return false
	}
	rt.runState = protocol.AgentRunCancelled
	m.mu.Unlock()
	publish()
	return true
}

func (m *sessionManager) beginRun(connID string) (*sessionRuntime, string, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.beginRunNoLock(connID)
}

// beginRunWithNotification 在释放 lifecycle 通知锁前同步发布初始 running，
// 因而并发 cancel 只能在 running 之后进入 cancelling。所有 lifecycle 路径统一
// 先取 runNotifyMu、再取 manager 锁；初次读取只用于定位锁，正式 begin 前必须复验。
func (m *sessionManager) beginRunWithNotification(connID string, publish func(*sessionRuntime, string, string)) (*sessionRuntime, string, string, error) {
	m.mu.RLock()
	id := m.attached[connID]
	rt := m.runtime[id]
	m.mu.RUnlock()
	if id == "" {
		return nil, "", "", fmt.Errorf("session_required")
	}
	if rt == nil {
		return nil, "", "", fmt.Errorf("session not loaded")
	}

	if m.beforeBeginRunNotifyLock != nil {
		m.beforeBeginRunNotifyLock()
	}
	rt.runNotifyMu.Lock()
	defer rt.runNotifyMu.Unlock()
	m.mu.Lock()
	// 定位锁后 attachment/runtime 可能已切换；不能拿旧 runtime 的通知锁启动新 runtime。
	if m.attached[connID] != id || m.runtime[id] != rt {
		m.mu.Unlock()
		return nil, "", "", fmt.Errorf("session_busy")
	}
	rt, id, runID, err := m.beginRunNoLock(connID)
	m.mu.Unlock()
	if err != nil {
		return nil, "", "", err
	}
	publish(rt, id, runID)
	return rt, id, runID, nil
}

func (m *sessionManager) beginRunNoLock(connID string) (*sessionRuntime, string, string, error) {
	id := m.attached[connID]
	if id == "" {
		return nil, "", "", fmt.Errorf("session_required")
	}
	rt := m.runtime[id]
	if rt == nil {
		return nil, "", "", fmt.Errorf("session not loaded")
	}
	if rt.status != sessionIdle || rt.stateOps > 0 {
		return nil, "", "", fmt.Errorf("session_busy")
	}
	rt.runOwner = connID
	rt.runID = uuid.NewString()
	m.invalidateRuntimeUnloadNoLock(id)
	rt.runState = protocol.AgentRunRunning
	rt.phase = protocol.AgentRunPhaseModel
	rt.status = sessionRunning
	rt.waitingType = ""
	rt.assistant.Reset()
	rt.reasoning.Reset()
	return rt, id, rt.runID, nil
}
