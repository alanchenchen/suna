package ipc

// JSON-RPC 2.0 消息定义
// 协议规范：https://www.jsonrpc.org/specification
//
// Daemon 和 TUI 之间通过 NDJSON (每行一条 JSON) 传输。
// 每条 JSON 必须单行，JSON 内不能有裸换行符（用 \n 转义）。

// Request JSON-RPC 请求
type Request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// Response JSON-RPC 响应
type Response struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id,omitempty"`
	Result  any    `json:"result,omitempty"`
	Error   *Error `json:"error,omitempty"`
}

// Notification JSON-RPC 通知（无 ID，不需要响应）
type Notification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// Error JSON-RPC 错误
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// 标准错误码
const (
	ErrParse         = -32700
	ErrInvalid       = -32600
	ErrNotFound      = -32601
	ErrInvalidParams = -32602
	ErrInternal      = -32603
)

// === TUI → Daemon 方法 ===

const (
	MethodSendMessage    = "agent.sendMessage"
	MethodCancel         = "agent.cancel"
	MethodMemorySearch   = "memory.search"
	MethodMemoryFacts    = "memory.facts"
	MethodTriggerList    = "trigger.list"
	MethodTriggerAdd     = "trigger.add"
	MethodTriggerRemove  = "trigger.remove"
	MethodDaemonStatus   = "daemon.status"
	MethodDaemonStop     = "daemon.stop"
	MethodDaemonRestart  = "daemon.restart"
	MethodConfigGet      = "config.get"
	MethodConfigSet      = "config.set"
	MethodSkillList      = "skill.list"
	MethodSkillValidate  = "skill.validate"
	MethodSessionNew     = "session.new"
	MethodSessionRestore = "session.restore"
	MethodCompact        = "session.compact"
	MethodUsage          = "session.usage"
)

const (
	ConfigActionUpsertModel   = "upsert_model"
	ConfigActionDeleteModel   = "delete_model"
	ConfigActionActivateModel = "activate_model"
	ConfigActionUpdateGeneral = "update_general"
)

// === Daemon → TUI 通知 ===

const (
	NotifyStream              = "agent.stream"
	NotifyReasoning           = "agent.reasoning"
	NotifyToolStart           = "agent.tool_start"
	NotifyToolEnd             = "agent.tool_end"
	NotifyAskUser             = "agent.ask_user"
	NotifyDaemonState         = "daemon.state"
	NotifyPerception          = "perception.event"
	NotifyMemoryUpdated       = "memory.updated"
	NotifyCompactResult       = "session.compact_result"
	NotifyMemorySearchResult  = "memory.search_result"
	NotifySessionRestoreMsg   = "session.restore_message"
	NotifySessionRestoreInput = "session.restore_input"
)

// StreamParams 是 daemon 向 TUI 推送单轮对话进度的统一载荷。
// token/cache/context/speed 必须来自 LLM usage；服务不返回 usage 时 HasUsage=false，TUI 展示未知，
// 不在 daemon 或 TUI 本地估算，避免把近似值伪装成接口真实用量。
type StreamParams struct {
	Chunk         string  `json:"chunk"`
	ID            string  `json:"id"`
	Done          bool    `json:"done,omitempty"`
	InputTokens   int     `json:"input_tokens,omitempty"`
	OutputTokens  int     `json:"output_tokens,omitempty"`
	CachedTokens  int     `json:"cached_tokens,omitempty"`
	HasUsage      bool    `json:"has_usage,omitempty"`
	ContextTokens int     `json:"context_tokens,omitempty"`
	ContextWindow int     `json:"context_window,omitempty"`
	TokensPerSec  float64 `json:"tokens_per_sec,omitempty"`
}

// ToolStartParams 工具开始执行通知
type ToolStartParams struct {
	ID     string         `json:"id"`
	Tool   string         `json:"tool"`
	Params map[string]any `json:"params"`
	Intent string         `json:"intent,omitempty"`
}

type ToolEndParams struct {
	ID     string `json:"id"`
	Tool   string `json:"tool"`
	Result string `json:"result"`
	Error  bool   `json:"error,omitempty"`
}

// AskUserParams 向用户提问
type AskUserParams struct {
	Question string   `json:"question"`
	Options  []string `json:"options,omitempty"`
	ID       string   `json:"id"`
}

// DaemonStateParams 连接时推送 daemon 状态
type DaemonStateParams struct {
	SessionID    string `json:"session_id"`
	AgentStatus  string `json:"agent_status"`
	CurrentTask  string `json:"current_task,omitempty"`
	PID          int    `json:"pid"`
	Uptime       string `json:"uptime"`
	Connections  int    `json:"connections"`
	ProviderName string `json:"provider_name,omitempty"`
	ModelName    string `json:"model_name,omitempty"`
}

type DaemonStatusParams struct {
	PID           int           `json:"pid"`
	Uptime        string        `json:"uptime"`
	Connections   int           `json:"connections"`
	Triggers      int           `json:"triggers"`
	AgentStatus   string        `json:"agent_status"`
	Provider      string        `json:"provider,omitempty"`
	Model         string        `json:"model,omitempty"`
	ContextTokens int           `json:"context_tokens,omitempty"`
	ContextWindow int           `json:"context_window,omitempty"`
	Memory        *MemoryStats  `json:"memory,omitempty"`
	Sessions      *SessionStats `json:"sessions,omitempty"`
	UsageToday    *UsagePeriod  `json:"usage_today,omitempty"`
}

type ConfigParams struct {
	Models      []ConfigModel `json:"models"`
	ActiveModel string        `json:"active_model"`
	Locale      string        `json:"locale,omitempty"`
	Theme       string        `json:"theme,omitempty"`
}

type ConfigModel struct {
	Provider      string   `json:"provider"`
	Model         string   `json:"model"`
	BaseURL       string   `json:"base_url,omitempty"`
	ContextWindow int      `json:"context_window,omitempty"`
	Strengths     []string `json:"strengths,omitempty"`
	HasAPIKey     bool     `json:"has_api_key,omitempty"`
}

type ConfigSetParams struct {
	Action      string      `json:"action"`
	Model       ConfigModel `json:"model,omitempty"`
	ModelRef    string      `json:"model_ref,omitempty"`
	ActiveModel string      `json:"active_model,omitempty"`
	APIKey      string      `json:"api_key,omitempty"`
	Locale      string      `json:"locale,omitempty"`
	Theme       string      `json:"theme,omitempty"`
}

type MemoryStats struct {
	Episodes int `json:"episodes"`
	Entities int `json:"entities"`
	Facts    int `json:"facts"`
}

type SessionStats struct {
	Active    int    `json:"active"`
	Completed int    `json:"completed"`
	LastID    string `json:"last_id,omitempty"`
}

// SendMessageParams 发送消息参数
type SendMessageParams struct {
	Content string `json:"content"`
}

// MemorySearchParams 记忆搜索参数
type MemorySearchParams struct {
	Query string `json:"query"`
	TopK  int    `json:"top_k,omitempty"`
}

// MemorySearchResult 记忆搜索结果
type MemorySearchResult struct {
	Memories []MemoryItem `json:"memories"`
}

type MemoryItem struct {
	ID        string `json:"id"`
	Content   string `json:"content"`
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
}

// UsageResult 用量查询结果
type UsageResult struct {
	Today UsagePeriod `json:"today"`
	Week  UsagePeriod `json:"week"`
	Month UsagePeriod `json:"month"`
}

type UsagePeriod struct {
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	Cost         float64 `json:"cost"`
	Requests     int     `json:"requests"`
}

// CompactResult 压缩结果
type CompactResult struct {
	BeforeTokens     int `json:"before_tokens"`
	AfterTokens      int `json:"after_tokens"`
	ContextWindow    int `json:"context_window"`
	TurnsCompressed  int `json:"turns_compressed"`
	SummaryTokens    int `json:"summary_tokens"`
	TruncatedOutputs int `json:"truncated_outputs"`
}

// AskUserReply AskUser 的回复
type AskUserReply struct {
	ID     string `json:"id"`
	Answer string `json:"answer"`
}
