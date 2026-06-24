package protocol

type SendMessageParams struct {
	ClientMsgID string `json:"client_msg_id,omitempty"`
	// Parts 是唯一输入载体；纯文本也必须作为 text part 传入，避免回到旧的 content string 分支。
	Parts []MessagePart `json:"parts,omitempty"`
}

type StreamParams struct {
	Chunk string `json:"chunk"`
	ID    string `json:"id"`
	Done  bool   `json:"done,omitempty"`
	// Error / ResumeAvailable 是结构化运行结果；TUI 不解析 Chunk 文本来判断错误或重试能力。
	Error           bool `json:"error,omitempty"`
	ResumeAvailable bool `json:"resume_available,omitempty"`
	ContextTokens   int  `json:"context_tokens,omitempty"`
	ContextWindow   int  `json:"context_window,omitempty"`
}

type SessionRestoreStatus struct {
	Messages    int                 `json:"messages"`
	Compacted   bool                `json:"compacted"`
	ToolSummary *ToolSummaryPayload `json:"tool_summary,omitempty"`
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
	Root  string `json:"root"`
	Bytes int64  `json:"bytes"`
	Count int    `json:"count"`
}

type AttachmentClearResult struct {
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
