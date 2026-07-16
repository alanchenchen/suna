package daemon

import (
	"context"
	"time"
)

const defaultRuntimeUnloadDelay = 2 * time.Second

// runtimeCanUnloadNoLock 判断 session runtime 是否已完全脱离交互和执行路径。
// 调用方必须持有 m.mu；这里仅释放内存 runtime，不涉及任何持久化 session 数据。
func (m *sessionManager) runtimeCanUnloadNoLock(sessionID string, rt *sessionRuntime) bool {
	return rt != nil &&
		m.runtime[sessionID] == rt &&
		m.creating[sessionID] == "" &&
		!m.deleting[sessionID] &&
		len(rt.clients) == 0 &&
		rt.status == sessionIdle &&
		rt.stateOps == 0
}

// invalidateRuntimeUnloadNoLock 使此前安排的延迟卸载失效。
// 连接恢复、开始运行、状态读取或删除都会调用它，避免旧延迟任务误删新使用的 runtime。
// 调用方必须持有 m.mu。
func (m *sessionManager) invalidateRuntimeUnloadNoLock(sessionID string) {
	m.runtimeUnloadVersion[sessionID]++
}

// scheduleRuntimeUnload 安排无客户端空闲 runtime 的短暂防抖卸载。
// 防抖窗口覆盖误触返回 Welcome、短暂重连和本地 client 交接；到期仍未使用才释放内存。
func (m *sessionManager) scheduleRuntimeUnload(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.scheduleRuntimeUnloadNoLock(sessionID)
}

// scheduleRuntimeUnloadNoLock 的延迟任务只保存 session runtime 的身份和版本，不持有持久化 state。
// 调用方必须持有 m.mu。
func (m *sessionManager) scheduleRuntimeUnloadNoLock(sessionID string) {
	rt := m.runtime[sessionID]
	if !m.runtimeCanUnloadNoLock(sessionID, rt) {
		return
	}
	m.runtimeUnloadVersion[sessionID]++
	version := m.runtimeUnloadVersion[sessionID]
	delay := m.runtimeUnloadDelay
	if delay <= 0 {
		delay = defaultRuntimeUnloadDelay
	}
	go m.unloadRuntimeAfter(sessionID, rt, version, delay)
}

func (m *sessionManager) unloadRuntimeAfter(sessionID string, rt *sessionRuntime, version uint64, delay time.Duration) {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	<-timer.C

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.runtimeUnloadVersion[sessionID] != version || !m.runtimeCanUnloadNoLock(sessionID, rt) {
		return
	}
	// session state 已在 run 结束时持久化；删除 runtime map 引用即可让 Agent、working memory
	// 和 transcript 在后续 GC 中释放。重新 attach 时会从 SQLite 恢复，不删除 session/state/附件。
	delete(m.runtime, sessionID)
	delete(m.runtimeUnloadVersion, sessionID)
}

func (m *sessionManager) finishStateOpNoLock(sessionID string, rt *sessionRuntime) {
	if rt == nil || m.runtime[sessionID] != rt || rt.stateOps <= 0 {
		return
	}
	rt.stateOps--
	m.scheduleRuntimeUnloadNoLock(sessionID)
}

// handleDetachedSession 保留空会话的既有自动清理语义；有持久化消息的会话仅卸载 runtime。
func (m *sessionManager) handleDetachedSession(sessionID string, shouldCheck bool) {
	if sessionID == "" || !shouldCheck {
		return
	}
	meta, err := m.store.Get(context.Background(), sessionID)
	if err == nil && meta != nil && meta.MessageCount == 0 {
		m.deleteInactive(context.Background(), sessionID)
		return
	}
	m.scheduleRuntimeUnload(sessionID)
}
