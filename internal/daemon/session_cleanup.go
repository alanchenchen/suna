package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alanchenchen/suna/internal/logging"
)

func (m *sessionManager) delete(ctx context.Context, connID, sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("session_required")
	}
	m.mu.Lock()
	currentID := m.attached[connID]
	if currentID == sessionID {
		m.mu.Unlock()
		return fmt.Errorf("cannot delete current session")
	}
	rt := m.runtime[sessionID]
	clientCount := 0
	status := sessionIdle
	stateOps := 0
	if rt != nil {
		clientCount = len(rt.clients)
		status = rt.status
		stateOps = rt.stateOps
	}
	deleteInProgress := m.deleting[sessionID]
	canDelete := status == sessionIdle && stateOps == 0 && clientCount == 0 && !deleteInProgress
	if canDelete {
		m.deleting[sessionID] = true
	}
	if canDelete && rt != nil {
		rt.stateOps++
	}
	m.mu.Unlock()
	if canDelete {
		defer func() {
			m.mu.Lock()
			delete(m.deleting, sessionID)
			m.mu.Unlock()
		}()
	}
	if deleteInProgress {
		return fmt.Errorf("session is busy")
	}
	if status != sessionIdle || stateOps > 0 {
		return fmt.Errorf("session is busy")
	}
	if clientCount > 0 {
		return fmt.Errorf("session has attached clients")
	}
	meta, err := m.store.Get(ctx, sessionID)
	if err != nil {
		if rt != nil {
			m.finishStateOp(sessionID)
		}
		return err
	}
	if meta == nil {
		if rt != nil {
			m.finishStateOp(sessionID)
		}
		return fmt.Errorf("session not found")
	}
	if err := m.deletePersistedSession(ctx, sessionID); err != nil {
		if rt != nil {
			m.finishStateOp(sessionID)
		}
		return err
	}
	m.mu.Lock()
	m.deleteVersion[sessionID]++
	delete(m.runtime, sessionID)
	m.mu.Unlock()
	m.removeSessionAttachments(sessionID)
	return nil
}

func (m *sessionManager) deletePersistedSession(ctx context.Context, sessionID string) error {
	return m.store.DeleteWithState(ctx, sessionID)
}

func (m *sessionManager) removeSessionAttachments(sessionID string) {
	attachmentRoot := filepath.Join(m.root.Config().AttachmentsDir(), sessionID)
	if err := os.RemoveAll(attachmentRoot); err != nil {
		// 元数据已提交删除，不能把附件清理失败伪装成会话仍存在；保留 orphan 并记录错误供诊断与后续人工清理。
		logging.Error("session", "remove_attachments_failed", err, logging.Event{"session_id": sessionID})
	}
}

func (m *sessionManager) pruneInactive(ctx context.Context, age time.Duration) {
	if m == nil || m.store == nil || age <= 0 {
		return
	}
	items, err := m.store.List(ctx)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-age)
	for _, item := range items {
		if item.MessageCount <= 0 {
			m.deleteInactive(ctx, item.ID)
			continue
		}
		if item.UpdatedAt.IsZero() || item.UpdatedAt.After(cutoff) {
			continue
		}
		m.deleteInactive(ctx, item.ID)
	}
}

func (m *sessionManager) deleteInactive(ctx context.Context, sessionID string) {
	m.mu.Lock()
	if m.deleting[sessionID] {
		m.mu.Unlock()
		return
	}
	rt := m.runtime[sessionID]
	active := rt != nil && (len(rt.clients) > 0 || rt.status != sessionIdle || rt.stateOps > 0)
	if active {
		m.mu.Unlock()
		return
	}
	// prune 与 attach 共用 deleting 标记，避免后台清理删除正在 attach 的 session 存储。
	m.deleting[sessionID] = true
	m.mu.Unlock()
	deleted := false
	defer func() {
		m.mu.Lock()
		delete(m.deleting, sessionID)
		if deleted {
			m.deleteVersion[sessionID]++
			delete(m.runtime, sessionID)
		}
		m.mu.Unlock()
	}()
	if err := m.deletePersistedSession(ctx, sessionID); err != nil {
		return
	}
	deleted = true
	m.removeSessionAttachments(sessionID)
}

func canonicalCWD(cwd string) string {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	if abs, err := filepath.Abs(cwd); err == nil {
		cwd = abs
	}
	if real, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = real
	}
	return cwd
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
