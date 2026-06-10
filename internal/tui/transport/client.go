package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/alanchenchen/suna/internal/protocol"
	"github.com/alanchenchen/suna/internal/transport/local"
	"github.com/alanchenchen/suna/internal/tui/components/attachment"
)

const (
	defaultRequestTimeout = 30 * time.Second
	compactRequestTimeout = 5 * time.Minute
)

// Client 是 TUI 到 daemon 的 local transport 适配层；页面只能通过 tea.Cmd 间接调用它。
type Client struct {
	client   *local.Client
	onNotify func(method string, params json.RawMessage)
}

func NewClient() *Client { return &Client{} }

func (c *Client) Connect() error {
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

func (c *Client) Close() error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

func (c *Client) Connected() bool {
	return c.client != nil && c.client.Connected()
}

func (c *Client) OnNotify(fn func(method string, params json.RawMessage)) {
	c.onNotify = fn
	if c.client != nil {
		c.client.OnNotify(fn)
	}
}

func (c *Client) SendRequestNotify(method string, params any) error {
	if c.client == nil || !c.client.Connected() {
		return fmt.Errorf("not connected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout(method))
	defer cancel()
	result, err := c.client.InvokeRaw(ctx, method, params)
	if err != nil {
		return err
	}
	c.handleResult(method, result)
	return nil
}

func requestTimeout(method string) time.Duration {
	switch method {
	case protocol.MethodCompact:
		// 手动压缩可能调用当前模型总结较长历史；超时时间要长于轻量配置/状态请求，
		// 避免 daemon 仍在处理时 TUI 先报本地 deadline。
		return compactRequestTimeout
	default:
		return defaultRequestTimeout
	}
}

func (c *Client) SendMessage(content string, attachments []attachment.Item) error {
	parts := []protocol.MessagePart{{Type: "text", Text: content}}
	for _, item := range attachments {
		parts = append(parts, item.ToPart())
	}
	return c.SendRequestNotify(protocol.MethodSendMessage, protocol.SendMessageParams{Parts: parts})
}

func (c *Client) ResumeRun() error { return c.SendRequestNotify(protocol.MethodResumeRun, nil) }

func (c *Client) Cancel() error { return c.SendRequestNotify(protocol.MethodCancel, nil) }

func (c *Client) AskReply(askID, answer string) error {
	return c.SendRequestNotify(protocol.MethodAskReply, map[string]string{"id": askID, "answer": answer})
}

func (c *Client) GuardReply(guardID, decision string) error {
	return c.SendRequestNotify(protocol.MethodGuardReply, protocol.GuardReplyParams{ID: guardID, Decision: decision})
}

func (c *Client) NewSession() error { return c.SendRequestNotify(protocol.MethodSessionNew, nil) }
func (c *Client) RestoreSession() error {
	return c.SendRequestNotify(protocol.MethodSessionRestore, nil)
}
func (c *Client) ListMemory() error { return c.SendRequestNotify(protocol.MethodMemoryList, nil) }
func (c *Client) ListSkills() error { return c.SendRequestNotify(protocol.MethodSkillList, nil) }
func (c *Client) SetSkill(params protocol.SkillSetParams) error {
	return c.SendRequestNotify(protocol.MethodSkillSet, params)
}
func (c *Client) ListMCP() error { return c.SendRequestNotify(protocol.MethodMCPList, nil) }
func (c *Client) ToggleMCP(params protocol.MCPSetParams) error {
	return c.SendRequestNotify(protocol.MethodMCPToggle, params)
}
func (c *Client) ReloadMCP(params protocol.MCPReloadParams) error {
	return c.SendRequestNotify(protocol.MethodMCPReload, params)
}
func (c *Client) Compact() error      { return c.SendRequestNotify(protocol.MethodCompact, nil) }
func (c *Client) DaemonStatus() error { return c.SendRequestNotify(protocol.MethodDaemonStatus, nil) }
func (c *Client) ConfigGet() error    { return c.SendRequestNotify(protocol.MethodConfigGet, nil) }
func (c *Client) ConfigSet(params protocol.ConfigSetParams) error {
	return c.SendRequestNotify(protocol.MethodConfigSet, params)
}
func (c *Client) AttachmentStatus() error {
	return c.SendRequestNotify(protocol.MethodAttachmentStatus, nil)
}
func (c *Client) AttachmentClear() error {
	return c.SendRequestNotify(protocol.MethodAttachmentClear, nil)
}

func (c *Client) handleResult(method string, rawResult json.RawMessage) {
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
	if method == protocol.MethodMCPList {
		c.onNotify(protocol.MethodMCPList, rawResult)
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
