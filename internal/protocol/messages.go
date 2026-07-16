package protocol

type RuntimeHelloParams struct {
	// ProtocolVersion 是客户端期望的协议版本；为空时按当前默认 0.2 处理。
	ProtocolVersion string `json:"protocol_version,omitempty"`
	// Transport 由 JSON-RPC transport 层注入并覆盖客户端输入，用于 runtime.hello 返回真实承载方式。
	Transport string `json:"transport,omitempty"`
	// Client 是第三方 UI/插件的自描述信息，只用于诊断和未来能力协商。
	Client RuntimeClient `json:"client,omitempty"`
}

type RuntimeClient struct {
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
	Type    string `json:"type,omitempty"`
}

type RuntimeHelloResult struct {
	ProtocolVersion string `json:"protocol_version"`
	RuntimeVersion  string `json:"runtime_version"`
	Transport       string `json:"transport"`
	// Capabilities 是运行时能力开关；客户端应按 key 判断，不要从版本号推断能力。
	Capabilities map[string]bool `json:"capabilities"`
	// ContentSources 声明 agent.sendMessage 支持的内容来源，第三方 UI v0 主要使用 text/path/url。
	ContentSources map[string]bool `json:"content_sources"`
	// Limits 暴露协议层稳定限制，例如 tool result 截断阈值。
	Limits map[string]int `json:"limits,omitempty"`
	// Metadata 预留给未来非关键诊断字段；客户端不应依赖其存在。
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

type SendMessageParams struct {
	ClientMsgID string `json:"client_msg_id,omitempty"`
	// Parts 是唯一输入载体；纯文本也必须作为 text part 传入，避免回到旧的 content string 分支。
	Parts []MessagePart `json:"parts,omitempty"`
}

type AgentDeltaKind string

const (
	AgentDeltaAssistant AgentDeltaKind = "assistant"
	AgentDeltaReasoning AgentDeltaKind = "reasoning"
)

type AgentDeltaParams struct {
	RunID   string         `json:"run_id,omitempty"`
	Kind    AgentDeltaKind `json:"kind"`
	Content string         `json:"content"`
}

type AgentRunState string

const (
	AgentRunRunning   AgentRunState = "running"
	AgentRunRetrying  AgentRunState = "retrying"
	AgentRunDone      AgentRunState = "done"
	AgentRunFailed    AgentRunState = "failed"
	AgentRunCancelled AgentRunState = "cancelled"
)

type AgentRunPhase string

const (
	AgentRunPhaseModel   AgentRunPhase = "model"
	AgentRunPhaseTool    AgentRunPhase = "tool"
	AgentRunPhaseCompact AgentRunPhase = "compact"
	AgentRunPhaseGuard   AgentRunPhase = "guard"
	AgentRunPhaseAsk     AgentRunPhase = "ask"
	AgentRunPhaseSkill   AgentRunPhase = "skill"
)

type ProtocolErrorData struct {
	// Kind 是稳定错误分类，UI/SDK 只能依赖它做分支，不应解析 message。
	Kind string `json:"kind"`
	// Reason 是可选机器可读补充原因，例如 unsupported protocol_version。
	Reason string `json:"reason,omitempty"`
	// Retryable 表示同一请求在条件不变时是否值得重试。
	Retryable bool `json:"retryable,omitempty"`
	// StatusCode 保留上游 HTTP/模型错误状态码，便于客户端展示和诊断。
	StatusCode int `json:"status_code,omitempty"`
}

type ModelErrorKind string

const (
	ModelErrorUnknown   ModelErrorKind = "unknown"
	ModelErrorHTTP      ModelErrorKind = "http"
	ModelErrorNetwork   ModelErrorKind = "network"
	ModelErrorCancelled ModelErrorKind = "cancelled"
	ModelErrorInternal  ModelErrorKind = "internal"
)

type ModelError struct {
	Kind       ModelErrorKind `json:"kind"`
	Message    string         `json:"message"`
	StatusCode int            `json:"status_code,omitempty"`
	Code       string         `json:"code,omitempty"`
	Type       string         `json:"type,omitempty"`
	Provider   string         `json:"provider,omitempty"`
	Model      string         `json:"model,omitempty"`
}

type RunErrorKind string

const (
	RunErrorNoModelConfigured       RunErrorKind = "no_model_configured"
	RunErrorSessionModelUnavailable RunErrorKind = "session_model_unavailable"
)

// RunError 表示模型请求开始前无法满足的运行前置条件。
// UI/SDK 应只根据 Kind 分支，并使用 ModelRef 作为展示或恢复上下文。
type RunError struct {
	Kind     RunErrorKind `json:"kind"`
	ModelRef string       `json:"model_ref,omitempty"`
}

type AgentRunParams struct {
	RunID string        `json:"run_id,omitempty"`
	State AgentRunState `json:"state"`
	Phase AgentRunPhase `json:"phase,omitempty"`
	// CanControl 表示接收该通知的 client 是否拥有当前 run 的控制权。
	CanControl bool `json:"can_control"`

	Message string `json:"message,omitempty"`

	Attempt     int   `json:"attempt,omitempty"`
	MaxAttempts int   `json:"max_attempts,omitempty"`
	DelayMs     int64 `json:"delay_ms,omitempty"`

	Error           *ModelError `json:"error,omitempty"`
	RunError        *RunError   `json:"run_error,omitempty"`
	ResumeAvailable bool        `json:"resume_available,omitempty"`
}

type UserMessageParams struct {
	SessionID string        `json:"session_id,omitempty"`
	Parts     []MessagePart `json:"parts,omitempty"`
}

type SessionStateParams struct {
	Session SessionInfo `json:"session"`
}

type SessionStatus string

const (
	SessionStatusIdle       SessionStatus = "idle"
	SessionStatusRunning    SessionStatus = "running"
	SessionStatusWaiting    SessionStatus = "waiting"
	SessionStatusCompacting SessionStatus = "compacting"
)

type SessionInfo struct {
	ID             string        `json:"id"`
	Title          string        `json:"title,omitempty"`
	CWD            string        `json:"cwd"`
	ModelRef       string        `json:"model_ref,omitempty"`
	MessageCount   int           `json:"message_count"`
	CreatedAt      string        `json:"created_at"`
	UpdatedAt      string        `json:"updated_at"`
	LastAttachedAt string        `json:"last_attached_at,omitempty"`
	Status         SessionStatus `json:"status"`
	ClientCount    int           `json:"client_count"`
}

type SessionListParams struct {
	CWD        string `json:"cwd,omitempty"`
	ActiveOnly bool   `json:"active_only,omitempty"`
}

type SessionListResult struct {
	Sessions []SessionInfo `json:"sessions"`
}

type SessionCreateParams struct {
	CWD   string `json:"cwd"`
	Title string `json:"title,omitempty"`
}

type SessionAttachParams struct {
	SessionID string `json:"session_id"`
	// RequireActive 只用于 Join Active 的陈旧 UI 防护；Resume/普通 attach 必须保持 false。
	RequireActive bool `json:"require_active"`
}

type SessionUpdateParams struct {
	SessionID string  `json:"session_id"`
	Title     *string `json:"title,omitempty"`
	ModelRef  *string `json:"model_ref,omitempty"`
}

type SessionDeleteParams struct {
	SessionID string `json:"session_id"`
}

type SessionDeleteResult struct {
	Deleted bool `json:"deleted"`
}

type SnapshotMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type RunWaitingType string

const (
	RunWaitingAsk   RunWaitingType = "ask"
	RunWaitingGuard RunWaitingType = "guard"
)

type CurrentRunView struct {
	Status          SessionStatus  `json:"status"`
	Phase           AgentRunPhase  `json:"phase,omitempty"`
	AssistantBuffer string         `json:"assistant_buffer,omitempty"`
	ReasoningBuffer string         `json:"reasoning_buffer,omitempty"`
	WaitingType     RunWaitingType `json:"waiting_type,omitempty"`
	CanControl      bool           `json:"can_control"`
}

type SessionSnapshot struct {
	Session     SessionInfo         `json:"session"`
	Messages    []SnapshotMessage   `json:"messages,omitempty"`
	Compacted   bool                `json:"compacted,omitempty"`
	ToolSummary *ToolSummaryPayload `json:"tool_summary,omitempty"`
	CurrentRun  *CurrentRunView     `json:"current_run,omitempty"`
}

type ToolSummaryPayload struct {
	Total    int               `json:"total"`
	Success  int               `json:"success"`
	Failed   int               `json:"failed"`
	Changes  []ToolChangeItem  `json:"changes,omitempty"`
	Failures []ToolSummaryItem `json:"failures,omitempty"`
	Recent   []ToolSummaryItem `json:"recent,omitempty"`
	Omitted  int               `json:"omitted,omitempty"`
}

type ToolChangeItem struct {
	Tool  string `json:"tool"`
	Count int    `json:"count"`
}

type ToolSummaryItem struct {
	Tool    string `json:"tool"`
	Status  string `json:"status"`
	Summary string `json:"summary,omitempty"`
}

type UsageParams struct {
	RunID                  string  `json:"run_id,omitempty"`
	InputTokens            int     `json:"input_tokens"`
	OutputTokens           int     `json:"output_tokens"`
	CachedTokens           int     `json:"cached_tokens,omitempty"`
	ContextTokens          int     `json:"context_tokens,omitempty"`
	EstimatedContextTokens int     `json:"estimated_context_tokens,omitempty"`
	ContextWindow          int     `json:"context_window,omitempty"`
	DurationMs             int64   `json:"duration_ms,omitempty"`
	TokensPerSec           float64 `json:"tokens_per_sec,omitempty"`
}

type ToolStartParams struct {
	ID     string         `json:"id"`
	Tool   string         `json:"tool"`
	Params map[string]any `json:"params"`
	Intent string         `json:"intent,omitempty"`
}

type ToolEndParams struct {
	ID              string         `json:"id"`
	Tool            string         `json:"tool"`
	Result          string         `json:"result"`
	Error           bool           `json:"error,omitempty"`
	ResultTruncated bool           `json:"result_truncated,omitempty"`
	ResultBytes     int            `json:"result_bytes,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
}

type ToolGuardParams struct {
	ToolCallID    string `json:"tool_call_id"`
	Tool          string `json:"tool"`
	Risk          string `json:"risk"`
	Decision      string `json:"decision"`
	Source        string `json:"source"`
	Reason        string `json:"reason,omitempty"`
	Suggestion    string `json:"suggestion,omitempty"`
	ReviewCode    string `json:"review_code,omitempty"`
	ReviewMessage string `json:"review_message,omitempty"`
}

type AskUserParams struct {
	Question    string   `json:"question"`
	Options     []string `json:"options,omitempty"`
	ID          string   `json:"id"`
	SessionID   string   `json:"session_id,omitempty"`
	CanReply    bool     `json:"can_reply"`
	AllowCustom bool     `json:"allow_custom"`
}

type GuardConfirmParams struct {
	ID            string         `json:"id"`
	ToolCallID    string         `json:"tool_call_id,omitempty"`
	Tool          string         `json:"tool"`
	Params        map[string]any `json:"params"`
	Risk          string         `json:"risk"`
	Reason        string         `json:"reason"`
	Suggestion    string         `json:"suggestion,omitempty"`
	ReviewCode    string         `json:"review_code,omitempty"`
	ReviewMessage string         `json:"review_message,omitempty"`
	SessionID     string         `json:"session_id,omitempty"`
	CanReply      bool           `json:"can_reply"`
}

type InteractionResolvedParams struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id,omitempty"`
}

type GuardReplyParams struct {
	ID       string `json:"id"`
	Decision string `json:"decision"`
}

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
	GuardMode   string        `json:"guard_mode,omitempty"`
	Workspace   string        `json:"workspace,omitempty"`
}

type ConfigModel struct {
	Provider        string         `json:"provider"`
	Protocol        string         `json:"protocol"`
	Model           string         `json:"model"`
	BaseURL         string         `json:"base_url,omitempty"`
	ContextWindow   int            `json:"context_window,omitempty"`
	MaxOutputTokens int            `json:"max_output_tokens,omitempty"`
	Strengths       []string       `json:"strengths,omitempty"`
	SubtaskFor      []string       `json:"subtask_for,omitempty"`
	Reasoning       map[string]any `json:"reasoning,omitempty"`
	HasAPIKey       bool           `json:"has_api_key,omitempty"`
}

type ConfigSetParams struct {
	Action       string      `json:"action"`
	Model        ConfigModel `json:"model,omitempty"`
	ModelRef     string      `json:"model_ref,omitempty"`
	ActiveModel  string      `json:"active_model,omitempty"`
	APIKey       string      `json:"api_key,omitempty"`
	DeleteAPIKey bool        `json:"delete_api_key,omitempty"`
	Locale       string      `json:"locale,omitempty"`
	Theme        string      `json:"theme,omitempty"`
	GuardMode    string      `json:"guard_mode,omitempty"`
	Workspace    *string     `json:"workspace,omitempty"`
}

type MemoryStats struct {
	Active int `json:"active"`
	Core   int `json:"core"`
	Queued int `json:"queued"`
}

type SessionStats struct {
	Active    int    `json:"active"`
	Completed int    `json:"completed"`
	LastID    string `json:"last_id,omitempty"`
}

type MemoryListResult struct {
	Memories []MemoryItem `json:"memories"`
}

type MemoryDeleteParams struct {
	ID string `json:"id"`
}

type MemoryDeleteResult struct {
	Deleted bool `json:"deleted"`
}

type MemoryClearResult struct {
	DeletedCount int `json:"deleted_count"`
}

type MemoryItem struct {
	ID       string   `json:"id"`
	Content  string   `json:"content"`
	Kind     string   `json:"kind"`
	Tags     []string `json:"tags,omitempty"`
	Priority int      `json:"priority"`
	IsCore   bool     `json:"is_core"`
}

type UsageResult struct {
	Today UsagePeriod `json:"today"`
	Week  UsagePeriod `json:"week"`
	Month UsagePeriod `json:"month"`
}

type UsagePeriod struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	Requests     int `json:"requests"`
}

type CompactResult struct {
	BeforeTokens     int    `json:"before_tokens"`
	AfterTokens      int    `json:"after_tokens"`
	ContextWindow    int    `json:"context_window"`
	TurnsCompressed  int    `json:"turns_compressed"`
	SummaryTokens    int    `json:"summary_tokens"`
	TruncatedOutputs int    `json:"truncated_outputs"`
	Noop             bool   `json:"noop,omitempty"`
	Running          *bool  `json:"running,omitempty"`
	Error            string `json:"error,omitempty"`
}

type AttachmentStatusResult struct {
	SessionID string `json:"session_id,omitempty"`
	Root      string `json:"root"`
	Bytes     int64  `json:"bytes"`
	Count     int    `json:"count"`
}

type AttachmentClearResult struct {
	SessionID    string `json:"session_id,omitempty"`
	Root         string `json:"root"`
	BytesRemoved int64  `json:"bytes_removed"`
	CountRemoved int    `json:"count_removed"`
	Bytes        int64  `json:"bytes"`
	Count        int    `json:"count"`
}

type AskUserReply struct {
	ID     string `json:"id"`
	Answer string `json:"answer"`
}

type MCPServerInfo struct {
	ID         string `json:"id,omitempty"`
	Name       string `json:"name"`
	Transport  string `json:"transport,omitempty"`
	Command    string `json:"command,omitempty"`
	Active     bool   `json:"active"`
	Configured bool   `json:"configured"`
	ToolCount  int    `json:"tool_count"`
	Error      string `json:"error,omitempty"`
}

type MCPListResult struct {
	Servers []MCPServerInfo `json:"servers"`
}

type MCPSetParams struct {
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

type MCPSetResult struct {
	Status string `json:"status"`
}

type MCPReloadParams struct {
	Name string `json:"name"`
}

type MCPReloadResult struct {
	Status string `json:"status"`
}

type SkillInfo struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Enabled     bool     `json:"enabled"`
	Valid       bool     `json:"valid"`
	Reasons     []string `json:"reasons,omitempty"`
	Path        string   `json:"path,omitempty"`
	Error       string   `json:"error,omitempty"`
}

type SkillListResult struct {
	Skills []SkillInfo `json:"skills"`
}

type SkillSetParams struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

type SkillSetResult struct {
	Status string `json:"status"`
}

type SkillLoadParams struct {
	Name   string `json:"name"`
	Status string `json:"status,omitempty"`
}

type SkillReviewParams struct {
	Name   string `json:"name"`
	Status string `json:"status,omitempty"`
	Review string `json:"review,omitempty"`
	Error  string `json:"error,omitempty"`
}
