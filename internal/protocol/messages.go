package protocol

type SendMessageParams struct {
	ClientMsgID string `json:"client_msg_id,omitempty"`
	// Parts 是唯一输入载体；纯文本也必须作为 text part 传入，避免回到旧的 content string 分支。
	Parts []MessagePart `json:"parts,omitempty"`
}

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

type ToolStartParams struct {
	ID     string         `json:"id"`
	Tool   string         `json:"tool"`
	Params map[string]any `json:"params"`
	Intent string         `json:"intent,omitempty"`
}

type ToolEndParams struct {
	ID              string `json:"id"`
	Tool            string `json:"tool"`
	Result          string `json:"result"`
	Error           bool   `json:"error,omitempty"`
	ResultTruncated bool   `json:"result_truncated,omitempty"`
	ResultBytes     int    `json:"result_bytes,omitempty"`
}

type AskUserParams struct {
	Question string   `json:"question"`
	Options  []string `json:"options,omitempty"`
	ID       string   `json:"id"`
}

type GuardConfirmParams struct {
	ID         string         `json:"id"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	Tool       string         `json:"tool"`
	Params     map[string]any `json:"params"`
	Risk       string         `json:"risk"`
	Reason     string         `json:"reason"`
	Suggestion string         `json:"suggestion,omitempty"`
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
	GuardMode   string      `json:"guard_mode,omitempty"`
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
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	Cost         float64 `json:"cost"`
	Requests     int     `json:"requests"`
}

type CompactResult struct {
	BeforeTokens     int `json:"before_tokens"`
	AfterTokens      int `json:"after_tokens"`
	ContextWindow    int `json:"context_window"`
	TurnsCompressed  int `json:"turns_compressed"`
	SummaryTokens    int `json:"summary_tokens"`
	TruncatedOutputs int `json:"truncated_outputs"`
}

type AskUserReply struct {
	ID     string `json:"id"`
	Answer string `json:"answer"`
}
