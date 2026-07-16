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

func (c *Client) InvokeRaw(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if c.client == nil || !c.client.Connected() {
		return nil, fmt.Errorf("not connected")
	}
	return c.client.InvokeRaw(ctx, method, params)
}

func (c *Client) Invoke(ctx context.Context, method string, params any, out any) error {
	raw, err := c.InvokeRaw(ctx, method, params)
	if err != nil {
		return err
	}
	if out == nil || raw == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
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
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout(protocol.MethodSendMessage))
	defer cancel()
	return c.Invoke(ctx, protocol.MethodSendMessage, protocol.SendMessageParams{Parts: parts}, nil)
}

func (c *Client) ResumeRun() error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout(protocol.MethodResumeRun))
	defer cancel()
	return c.Invoke(ctx, protocol.MethodResumeRun, nil, nil)
}

func (c *Client) Cancel() error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout(protocol.MethodCancel))
	defer cancel()
	return c.Invoke(ctx, protocol.MethodCancel, nil, nil)
}

func (c *Client) AskReply(askID, answer string) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout(protocol.MethodAskReply))
	defer cancel()
	return c.Invoke(ctx, protocol.MethodAskReply, map[string]string{"id": askID, "answer": answer}, nil)
}

func (c *Client) GuardReply(guardID, decision string) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout(protocol.MethodGuardReply))
	defer cancel()
	return c.Invoke(ctx, protocol.MethodGuardReply, protocol.GuardReplyParams{ID: guardID, Decision: decision}, nil)
}

func (c *Client) ListSessions(params protocol.SessionListParams) (protocol.SessionListResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout(protocol.MethodSessionList))
	defer cancel()
	var result protocol.SessionListResult
	return result, c.Invoke(ctx, protocol.MethodSessionList, params, &result)
}

func (c *Client) CreateSession(cwd, title string) (protocol.SessionSnapshot, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout(protocol.MethodSessionCreate))
	defer cancel()
	var result protocol.SessionSnapshot
	return result, c.Invoke(ctx, protocol.MethodSessionCreate, protocol.SessionCreateParams{CWD: cwd, Title: title}, &result)
}

func (c *Client) AttachSession(sessionID string, requireActive bool) (protocol.SessionSnapshot, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout(protocol.MethodSessionAttach))
	defer cancel()
	var result protocol.SessionSnapshot
	return result, c.Invoke(ctx, protocol.MethodSessionAttach, protocol.SessionAttachParams{SessionID: sessionID, RequireActive: requireActive}, &result)
}

func (c *Client) UpdateSession(params protocol.SessionUpdateParams) (protocol.SessionSnapshot, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout(protocol.MethodSessionUpdate))
	defer cancel()
	var result protocol.SessionSnapshot
	return result, c.Invoke(ctx, protocol.MethodSessionUpdate, params, &result)
}

func (c *Client) DetachSession() error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout(protocol.MethodSessionDetach))
	defer cancel()
	return c.Invoke(ctx, protocol.MethodSessionDetach, nil, nil)
}

func (c *Client) DeleteSession(sessionID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout(protocol.MethodSessionDelete))
	defer cancel()
	return c.Invoke(ctx, protocol.MethodSessionDelete, protocol.SessionDeleteParams{SessionID: sessionID}, nil)
}

func (c *Client) ListMemory() (protocol.MemoryListResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout(protocol.MethodMemoryList))
	defer cancel()
	var result protocol.MemoryListResult
	return result, c.Invoke(ctx, protocol.MethodMemoryList, nil, &result)
}

func (c *Client) DeleteMemory(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout(protocol.MethodMemoryDelete))
	defer cancel()
	return c.Invoke(ctx, protocol.MethodMemoryDelete, protocol.MemoryDeleteParams{ID: id}, nil)
}

func (c *Client) ClearMemory() error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout(protocol.MethodMemoryClear))
	defer cancel()
	return c.Invoke(ctx, protocol.MethodMemoryClear, nil, nil)
}

func (c *Client) ListSkills() (protocol.SkillListResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout(protocol.MethodSkillList))
	defer cancel()
	var result protocol.SkillListResult
	return result, c.Invoke(ctx, protocol.MethodSkillList, nil, &result)
}

func (c *Client) SetSkill(params protocol.SkillSetParams) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout(protocol.MethodSkillSet))
	defer cancel()
	return c.Invoke(ctx, protocol.MethodSkillSet, params, nil)
}

func (c *Client) ListMCP() (protocol.MCPListResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout(protocol.MethodMCPList))
	defer cancel()
	var result protocol.MCPListResult
	return result, c.Invoke(ctx, protocol.MethodMCPList, nil, &result)
}

func (c *Client) ToggleMCP(params protocol.MCPSetParams) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout(protocol.MethodMCPToggle))
	defer cancel()
	return c.Invoke(ctx, protocol.MethodMCPToggle, params, nil)
}

func (c *Client) ReloadMCP(params protocol.MCPReloadParams) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout(protocol.MethodMCPReload))
	defer cancel()
	return c.Invoke(ctx, protocol.MethodMCPReload, params, nil)
}

func (c *Client) Compact() error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout(protocol.MethodCompact))
	defer cancel()
	return c.Invoke(ctx, protocol.MethodCompact, nil, nil)
}

func (c *Client) DaemonStatus() (protocol.DaemonStatusParams, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout(protocol.MethodDaemonStatus))
	defer cancel()
	var result protocol.DaemonStatusParams
	return result, c.Invoke(ctx, protocol.MethodDaemonStatus, nil, &result)
}

func (c *Client) ConfigGet() (protocol.ConfigParams, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout(protocol.MethodConfigGet))
	defer cancel()
	var result protocol.ConfigParams
	return result, c.Invoke(ctx, protocol.MethodConfigGet, nil, &result)
}

func (c *Client) ConfigSet(params protocol.ConfigSetParams) (protocol.ConfigParams, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout(protocol.MethodConfigSet))
	defer cancel()
	var result protocol.ConfigParams
	return result, c.Invoke(ctx, protocol.MethodConfigSet, params, &result)
}

func (c *Client) AttachmentStatus() (protocol.AttachmentStatusResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout(protocol.MethodAttachmentStatus))
	defer cancel()
	var result protocol.AttachmentStatusResult
	return result, c.Invoke(ctx, protocol.MethodAttachmentStatus, nil, &result)
}

func (c *Client) AttachmentClear() (protocol.AttachmentClearResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout(protocol.MethodAttachmentClear))
	defer cancel()
	var result protocol.AttachmentClearResult
	return result, c.Invoke(ctx, protocol.MethodAttachmentClear, nil, &result)
}
