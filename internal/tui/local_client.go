package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/alanchenchen/suna/internal/protocol"
	"github.com/alanchenchen/suna/internal/transport/local"
)

/*
localClient 是当前 TUI 使用的 local transport 客户端。

职责：
  - 连接 daemon 的本地入口（Unix socket / Windows named pipe）
  - 用 local transport 的 JSON-RPC framing 承载 protocol request
  - 接收 daemon 的 protocol event notification，分发给 UI 回调

设计原则（01-architecture.md I/O 抽象层）：

	TUI 不持有任何业务逻辑、状态、数据库连接。
	TUI 只负责渲染 UI，并通过 protocol schema 与 daemon 交互。
*/
type localClient struct {
	client   *local.Client
	onNotify func(method string, params json.RawMessage)
}

// NewLocalClient 创建当前 TUI 的 local transport 客户端。
func NewLocalClient() *localClient {
	return &localClient{}
}

func (c *localClient) Connect() error {
	client, err := local.DialDefault(5 * time.Second)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w\nIs sunad running? Start it with: suna daemon", err)
	}
	c.client = client
	if c.onNotify != nil {
		c.client.OnNotify(c.onNotify)
	}
	return nil
}

func (c *localClient) Close() error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

func (c *localClient) Connected() bool {
	return c.client != nil && c.client.Connected()
}

func (c *localClient) OnNotify(fn func(method string, params json.RawMessage)) {
	c.onNotify = fn
	if c.client != nil {
		c.client.OnNotify(fn)
	}
}

func (c *localClient) SendRequestNotify(method string, params any) error {
	if c.client == nil || !c.client.Connected() {
		return fmt.Errorf("not connected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := c.client.InvokeRaw(ctx, method, params)
	if err != nil {
		c.handleError(method, err)
		return err
	}
	c.handleResult(method, result)
	return nil
}

func (c *localClient) SendMessage(content string, attachments []attachmentItem) error {
	parts := []protocol.MessagePart{{Type: "text", Text: content}}
	for _, attachment := range attachments {
		parts = append(parts, attachment.toPart())
	}
	return c.SendRequestNotify(protocol.MethodSendMessage, protocol.SendMessageParams{Parts: parts})
}

func (c *localClient) Cancel() error {
	return c.SendRequestNotify(protocol.MethodCancel, nil)
}

func (c *localClient) AskReply(askID, answer string) error {
	return c.SendRequestNotify(protocol.MethodAskReply, map[string]string{
		"id":     askID,
		"answer": answer,
	})
}

func (c *localClient) GuardReply(guardID, decision string) error {
	return c.SendRequestNotify(protocol.MethodGuardReply, protocol.GuardReplyParams{ID: guardID, Decision: decision})
}

func (c *localClient) NewSession() error {
	return c.SendRequestNotify(protocol.MethodSessionNew, nil)
}

func (c *localClient) RestoreSession() error {
	return c.SendRequestNotify(protocol.MethodSessionRestore, nil)
}

func (c *localClient) ListMemory() error {
	return c.SendRequestNotify(protocol.MethodMemoryList, nil)
}

func (c *localClient) ListSkills() error {
	return c.SendRequestNotify(protocol.MethodSkillList, nil)
}

func (c *localClient) SetSkill(params protocol.SkillSetParams) error {
	return c.SendRequestNotify(protocol.MethodSkillSet, params)
}

func (c *localClient) Compact() error {
	return c.SendRequestNotify(protocol.MethodCompact, nil)
}

func (c *localClient) DaemonStatus() error {
	return c.SendRequestNotify(protocol.MethodDaemonStatus, nil)
}

func (c *localClient) ConfigGet() error {
	return c.SendRequestNotify(protocol.MethodConfigGet, nil)
}

func (c *localClient) ConfigSet(params protocol.ConfigSetParams) error {
	return c.SendRequestNotify(protocol.MethodConfigSet, params)
}

func (c *localClient) AttachmentStatus() error {
	return c.SendRequestNotify(protocol.MethodAttachmentStatus, nil)
}

func (c *localClient) AttachmentClear() error {
	return c.SendRequestNotify(protocol.MethodAttachmentClear, nil)
}

func (c *localClient) handleError(method string, err error) {
	if c.onNotify == nil {
		return
	}
	data, _ := json.Marshal(map[string]string{"message": err.Error()})
	if method == protocol.MethodCompact {
		c.onNotify("compact.error", data)
	} else {
		c.onNotify("config.error", data)
	}
}

func (c *localClient) handleResult(method string, rawResult json.RawMessage) {
	if rawResult == nil || c.onNotify == nil {
		return
	}
	if method == protocol.MethodAttachmentStatus || method == protocol.MethodAttachmentClear {
		c.onNotify(protocol.MethodAttachmentStatus, rawResult)
		return
	}
	if method == protocol.MethodSkillList {
		c.onNotify(protocol.MethodSkillList, rawResult)
		return
	}
	if looksLikeDaemonStatus(rawResult) {
		c.onNotify(protocol.NotifyDaemonFullStatus, rawResult)
		return
	}
	if looksLikeConfig(rawResult) {
		c.onNotify(protocol.NotifyConfigState, rawResult)
	}
}

func looksLikeDaemonStatus(raw json.RawMessage) bool {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return false
	}
	_, hasPID := m["pid"]
	_, hasProvider := m["provider"]
	_, hasModel := m["model"]
	return hasPID && (hasProvider || hasModel)
}

func looksLikeConfig(raw json.RawMessage) bool {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return false
	}
	_, hasModels := m["models"]
	return hasModels
}
