package guard

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

type Decision string

const (
	Approve Decision = "approve"
	Reject  Decision = "reject"
	Confirm Decision = "confirm"
)

type Mode string

const (
	ModeReadonly Mode = "readonly"
	ModeAsk      Mode = "ask"
	ModeAuto     Mode = "auto"
	ModeSmart    Mode = "smart"
)

// ReviewContext 是 smart mode 下 LLM review 的轻量意图上下文。
// 它随单次 tool call 传入，避免并发工具调用串用任务或用户确认事实。
type ReviewContext struct {
	Evidence string
}

type ReviewRequest struct {
	ToolName        string
	ParamsJSON      string
	ParamsTruncated bool
	Target          string
	Context         ReviewContext
}

// LLMReviewer 用于 smart mode 的 LLM 审查。接收结构化操作上下文，返回 LLM 原始回复。
type LLMReviewer func(ctx context.Context, req ReviewRequest) (string, error)

type GuardResult struct {
	Decision      Decision
	Reason        string
	ReadOnly      bool
	Source        string
	Audit         string
	ReviewCode    string
	ReviewMessage string
}

type Guard struct {
	db           *sql.DB
	mode         Mode
	blockedRules []blockRule
	userBlocked  []blockRule
	userAllowed  []allowedRule
	workspace    string
	sessionID    string
	llmReviewer  LLMReviewer
}

type blockRule struct {
	pattern *regexp.Regexp
	reason  string
}

type allowedRule struct {
	pattern *regexp.Regexp
	tool    string
	reason  string
}

func NewGuard(db *sql.DB, sessionID string) *Guard {
	return NewGuardWithMode(db, sessionID, ModeSmart)
}

func NewGuardWithMode(db *sql.DB, sessionID string, mode Mode) *Guard {
	g := &Guard{db: db, sessionID: sessionID, mode: NormalizeMode(string(mode))}
	g.blockedRules = g.builtinBlockedRules()
	return g
}

func NewGuardWithConfigModeAndWorkspace(db *sql.DB, sessionID string, mode Mode, workspace string, blockedPatterns []string, blockedReasons []string, allowedPatterns []string, allowedTools []string) *Guard {
	g := &Guard{db: db, sessionID: sessionID, mode: NormalizeMode(string(mode))}
	g.blockedRules = g.builtinBlockedRules()
	g.workspace = normalizeWorkspaceRoot(workspace)
	for i, p := range blockedPatterns {
		re, err := regexp.Compile(p)
		if err != nil {
			continue
		}
		reason := ""
		if i < len(blockedReasons) {
			reason = blockedReasons[i]
		}
		g.userBlocked = append(g.userBlocked, blockRule{pattern: re, reason: reason})
	}
	for i, p := range allowedPatterns {
		re, err := regexp.Compile(p)
		if err != nil {
			continue
		}
		tool := ""
		if i < len(allowedTools) {
			tool = allowedTools[i]
		}
		g.userAllowed = append(g.userAllowed, allowedRule{pattern: re, tool: tool, reason: ""})
	}
	return g
}

func NormalizeMode(mode string) Mode {
	switch Mode(strings.ToLower(strings.TrimSpace(mode))) {
	case ModeReadonly:
		return ModeReadonly
	case ModeAsk:
		return ModeAsk
	case ModeAuto:
		return ModeAuto
	case ModeSmart:
		return ModeSmart
	default:
		return ModeSmart
	}
}

func (g *Guard) Mode() Mode {
	if g == nil || g.mode == "" {
		return ModeSmart
	}
	return g.mode
}

func (g *Guard) Workspace() string {
	if g == nil {
		return ""
	}
	return g.workspace
}

// SetLLMReviewer 注入 LLM 审查函数，由 Agent 在创建 Guard 后调用。
func (g *Guard) SetLLMReviewer(reviewer LLMReviewer) {
	g.llmReviewer = reviewer
}

func (g *Guard) Check(ctx context.Context, tool string, params map[string]any, reviewCtx ...ReviewContext) *GuardResult {
	// exec run 命令只解析一次 AST，供结构性高危 / 只读判定 / workspace / sensitive 多处消费，
	// 避免同一命令多次解析（设计方案：一次解析多处消费）。
	var execAnalysis *ExecAnalysis
	if tool == "exec" && execAction(params) == "run" {
		if cmd, _ := params["command"].(string); strings.TrimSpace(cmd) != "" {
			shell, _ := params["shell"].(string)
			if a, ok := newExecAnalyzer().Analyze(cmd, shell); ok {
				execAnalysis = &a
			}
		}
	}

	// 硬拦截顺序：结构性高危（组合特征兑底）→ blocked（危险命令）→ workspace（路径边界）→ sensitive（敏感文件）。
	// 结构性高危不依赖规则穷举，所有 mode 一致拦截；auto 模式也只被这一层兑底。
	if tool == "exec" && execAction(params) == "run" {
		if isSystemicallyHighRiskAnalysis(execAnalysis) {
			g.audit(ctx, tool, params, "structural_high_risk", "blocked: systemically dangerous command")
			return &GuardResult{Decision: Reject, Reason: "blocked: systemically dangerous command", Source: "rule", Audit: "structural_high_risk"}
		}
	}
	if blocked, reason := g.checkBlocked(tool, params); blocked {
		g.audit(ctx, tool, params, "blocked", reason)
		return &GuardResult{Decision: Reject, Reason: reason, Source: "rule", Audit: "blocked"}
	}
	if blocked, reason, auditReason := g.checkWorkspaceWithAnalysis(ctx, tool, params, execAnalysis); blocked {
		g.audit(ctx, tool, params, "workspace_reject", auditReason)
		return &GuardResult{Decision: Reject, Reason: reason, Source: "rule", Audit: "workspace_reject"}
	}
	if blocked, reason := g.checkSensitive(tool, params, execAnalysis); blocked {
		g.audit(ctx, tool, params, "sensitive_reject", reason)
		return &GuardResult{Decision: Reject, Reason: reason, Source: "rule", Audit: "sensitive_reject"}
	}
	if allowed, reason := g.checkAllowed(tool, params); allowed {
		if reason == "" {
			reason = "allowed rule"
		}
		g.audit(ctx, tool, params, "allowed", reason)
		return &GuardResult{Decision: Approve, Reason: reason, Source: "rule", Audit: "allowed"}
	}

	// 只读判定：静态可证明无副作用（Perceive 工具或 Act 工具的只读调用）。
	// 无法证明只读的一律非只读，交给 mode policy 处置（readonly 拒 / ask 问 / smart 审 exec）。
	readOnly := g.isReadOnlyCall(tool, params, execAnalysis)

	if g.Mode() == ModeReadonly {
		if readOnly {
			g.audit(ctx, tool, params, "auto_approve", "readonly call")
			return &GuardResult{Decision: Approve, Reason: "readonly call", ReadOnly: true, Source: "static", Audit: "auto_approve"}
		}
		g.audit(ctx, tool, params, "readonly_reject", "readonly mode blocks this operation")
		return &GuardResult{Decision: Reject, Reason: "readonly mode blocks this operation", Source: "static", Audit: "readonly_reject"}
	}
	if readOnly {
		g.audit(ctx, tool, params, "auto_approve", "readonly call")
		return &GuardResult{Decision: Approve, Reason: "readonly call", ReadOnly: true, Source: "static", Audit: "auto_approve"}
	}
	if g.Mode() == ModeAuto {
		g.audit(ctx, tool, params, "auto_approve", "auto mode")
		// Reason 留空：mode 名是内部审计语义，展示层已有决策 badge，不需要重复展示。
		return &GuardResult{Decision: Approve, Reason: "", Source: "static", Audit: "auto_approve"}
	}
	if g.Mode() == ModeAsk {
		g.audit(ctx, tool, params, "confirm", "ask mode")
		return &GuardResult{Decision: Confirm, Reason: "confirm risky operation", Source: "user", Audit: "confirm"}
	}

	// smart mode：只审 exec（Act 中最危险的一类）；其他非只读工具静态放行。
	// 写文件本身不执行，危险在后续执行时，而执行必走 exec 被 LLM 审，链路闭合。
	if tool != "exec" {
		g.audit(ctx, tool, params, "auto_approve", "smart mode non-exec write")
		// Reason 留空：mode 名是内部审计语义，展示层已有决策 badge，不需要重复展示。
		return &GuardResult{Decision: Approve, Reason: "", Source: "static", Audit: "auto_approve"}
	}
	if g.llmReviewer == nil {
		return g.reviewFallback(ctx, tool, params, "review_unavailable", "Smart Guard reviewer is unavailable")
	}
	ctxForReview := ReviewContext{}
	if len(reviewCtx) > 0 {
		ctxForReview = reviewCtx[0]
	}
	return g.llmReview(ctx, tool, params, ctxForReview)
}

// llmReview 调用 LLM 进行安全审查。LLM 只做二元决策：approve / reject。
// 审风险不审意图：意图对齐归 Agent（有完整上下文），Guard 只判断操作本身是否危险。
func (g *Guard) llmReview(ctx context.Context, toolName string, params map[string]any, reviewCtx ReviewContext) *GuardResult {
	target := guardTarget(toolName, params)
	paramsJSON, paramsTruncated := marshalReviewParams(params)
	resp, err := g.llmReviewer(ctx, ReviewRequest{ToolName: toolName, ParamsJSON: paramsJSON, ParamsTruncated: paramsTruncated, Target: target, Context: reviewCtx})
	if err != nil {
		code, msg := classifyReviewError(err)
		return g.reviewFallback(ctx, toolName, params, code, msg)
	}
	jsonText := extractJSON(resp)
	if strings.TrimSpace(jsonText) == "" {
		return g.reviewFallback(ctx, toolName, params, "review_empty_response", "Smart Guard review returned an empty response")
	}
	var decision struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(jsonText), &decision); err != nil {
		return g.reviewFallback(ctx, toolName, params, "review_parse_failed", "Smart Guard review returned invalid JSON")
	}
	decision.Decision = strings.ToLower(strings.TrimSpace(decision.Decision))
	switch decision.Decision {
	case "reject":
		g.audit(ctx, toolName, params, "llm_reject", decision.Reason)
		return &GuardResult{Decision: Reject, Reason: decision.Reason, Source: "llm", Audit: "llm_reject"}
	case "approve":
		g.audit(ctx, toolName, params, "llm_approve", decision.Reason)
		return &GuardResult{Decision: Approve, Reason: decision.Reason, Source: "llm", Audit: "llm_approve"}
	default:
		// LLM 表达不确定或返回未知决策：硬拦截已兜底确定性危险，按 approve 放行并留痕。
		g.audit(ctx, toolName, params, "llm_approve_uncertain", decision.Reason)
		return &GuardResult{Decision: Approve, Reason: "smart guard: " + decision.Reason, Source: "llm", Audit: "llm_approve_uncertain"}
	}
}

// reviewFallback 在 LLM 审核不可用时 fail-closed：审核能力缺失不放行。
func (g *Guard) reviewFallback(ctx context.Context, tool string, params map[string]any, code, message string) *GuardResult {
	if strings.TrimSpace(code) == "" {
		code = "review_unavailable"
	}
	if strings.TrimSpace(message) == "" {
		message = "Smart Guard review failed"
	}
	g.audit(ctx, tool, params, code, message)
	return &GuardResult{Decision: Reject, Reason: message, Source: "fallback", Audit: code, ReviewCode: code, ReviewMessage: message}
}

func classifyReviewError(err error) (string, string) {
	if errors.Is(err, context.DeadlineExceeded) {
		return "review_timeout", "Smart Guard review timed out"
	}
	if errors.Is(err, context.Canceled) {
		return "review_canceled", "Smart Guard review was canceled"
	}
	return "review_provider_error", "Smart Guard review request failed"
}

// extractJSON 从 LLM 回复中提取第一个完整 JSON 对象。
// 先定位首个 {（容忍前导文本），再用 json.Decoder 严格取第一个完整对象，
// 避免旧实现（首 { 到末 }）在多对象或正文含 {} 时取错。
func extractJSON(s string) string {
	start := strings.Index(s, "{")
	if start < 0 {
		return ""
	}
	dec := json.NewDecoder(strings.NewReader(s[start:]))
	for {
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return ""
		}
		if len(v) > 0 && v[0] == '{' {
			return string(v)
		}
	}
}

func (g *Guard) checkBlocked(tool string, params map[string]any) (bool, string) {
	target := ruleTarget(tool, params)
	if target == "" {
		return false, ""
	}
	for _, rule := range g.blockedRules {
		if rule.pattern.MatchString(target) {
			return true, rule.reason
		}
	}
	for _, rule := range g.userBlocked {
		if rule.pattern.MatchString(target) {
			return true, rule.reason
		}
	}
	return false, ""
}

// checkSensitive 是硬拦截层的一部分：敏感文件读/写一律拒绝，所有 mode 一致，
// 与 workspace 无关（敏感数据永远在 guard 层拒绝）。
// 敏感路径规则由 IsSensitivePath 维护（凭证、密钥、SSH 目录等），此处只做调用位置提升，
// 让 guard 审计能看到"敏感文件拦截"，而不是在 agent 层静默拦截。
// exec 是敏感检查的绕过口（readfile 拦截但 exec cat 放行），这里复用 Check 已解析的
// ExecAnalysis（一次解析多处消费），避免同一命令重复 AST 解析。
func (g *Guard) checkSensitive(tool string, params map[string]any, analysis *ExecAnalysis) (bool, string) {
	var paths []string
	switch tool {
	case "readfile", "writefile", "editfile", "listdir", "search":
		if p, _ := params["path"].(string); p != "" {
			paths = append(paths, p)
		}
	case "filesystem":
		if p, _ := params["path"].(string); p != "" {
			paths = append(paths, p)
		}
		if d, _ := params["destination"].(string); d != "" {
			paths = append(paths, d)
		}
	case "exec":
		paths = g.execSensitivePaths(params, analysis)
	}
	for _, p := range paths {
		if sensitive, reason := IsSensitivePath(p); sensitive {
			return true, fmt.Sprintf("blocked: sensitive file (%s). Accessing credential/secret files is not allowed.", reason)
		}
	}
	return false, ""
}

func (g *Guard) checkAllowed(tool string, params map[string]any) (bool, string) {
	target := ruleTarget(tool, params)
	if target == "" {
		return false, ""
	}
	for _, rule := range g.userAllowed {
		if rule.tool != "" && rule.tool != tool {
			continue
		}
		if rule.pattern.MatchString(target) {
			return true, rule.reason
		}
	}
	return false, ""
}

func ruleTarget(tool string, params map[string]any) string {
	switch tool {
	case "exec":
		return execTarget(params, false)
	case "writefile", "editfile", "readfile", "listdir", "search":
		target, _ := params["path"].(string)
		return target
	case "filesystem":
		action, _ := params["action"].(string)
		path, _ := params["path"].(string)
		destination, _ := params["destination"].(string)
		if destination != "" {
			return fmt.Sprintf("%s %s -> %s", action, path, destination)
		}
		return fmt.Sprintf("%s %s", action, path)
	case "http":
		method, _ := params["method"].(string)
		if method == "" {
			method = "GET"
		}
		target, _ := params["url"].(string)
		return strings.ToUpper(method) + " " + target
	default:
		return ""
	}
}

func guardTarget(tool string, params map[string]any) string {
	return SafeTarget(tool, params)
}

// execAction 与执行工具保持一致：未提供 action 时按 run 处理。
func execAction(params map[string]any) string {
	action, _ := params["action"].(string)
	if action == "" {
		return "run"
	}
	return action
}

// execTarget 让命令执行和受管任务操作使用各自准确的规则、审查目标。
func execTarget(params map[string]any, safe bool) string {
	switch action := execAction(params); action {
	case "status", "stop":
		jobID, _ := params["job_id"].(string)
		return action + " " + MaskSensitiveContent(jobID)
	default:
		command, _ := params["command"].(string)
		if safe {
			return MaskSensitiveContent(command)
		}
		return command
	}
}

func (g *Guard) audit(ctx context.Context, tool string, params map[string]any, decision, reason string) {
	if g.db == nil {
		return
	}
	id := uuid.New().String()
	paramsStr := "{}"
	if b, err := marshalParams(params); err == nil {
		paramsStr = b
	}
	g.db.ExecContext(ctx, `
		INSERT INTO audit_log (id, session_id, tool, params, guard_decision, guard_reason)
		VALUES (?, ?, ?, ?, ?, ?)`,
		id, g.sessionID, tool, paramsStr, decision, reason,
	)
}

func (g *Guard) builtinBlockedRules() []blockRule {
	rules := platformBlockedRules()
	genericRules := []struct{ pattern, reason string }{
		{`(?i)\b(curl|wget|iwr|irm|invoke-webrequest|invoke-restmethod)\b.*\|\s*(sh|bash|zsh|fish|iex|invoke-expression|powershell|pwsh)\b`, "blocked: remote script pipe execution"},
		{`(?i)\beval\s*\$\(`, "blocked: command injection pattern"},
	}
	for _, r := range genericRules {
		if re, err := regexp.Compile(r.pattern); err == nil {
			rules = append(rules, blockRule{pattern: re, reason: r.reason})
		}
	}
	return rules
}

// isReadOnlyCall 静态判定调用是否可证明无副作用（只读）。
// 唯一规则：无法证明只读 → 非只读，没有中间态，绝不猜测放行。
// 只读判定必须精准：它是 mode policy 的输入，判错就是严重 bug。
func (g *Guard) isReadOnlyCall(tool string, params map[string]any, analysis *ExecAnalysis) bool {
	switch tool {
	case "readfile", "listdir", "search":
		// Perceive 工具：只读。
		return true
	case "exec":
		switch execAction(params) {
		case "status":
			// 受管任务状态查询只读取已有输出，不执行新命令。
			return true
		case "stop":
			// 停止精确任务会改变进程状态，非只读。
			return false
		default:
			cmd, _ := params["command"].(string)
			shell, _ := params["shell"].(string)
			if analysis == nil {
				// 优先用 ExecAnalysis（AST 精确）；解析失败 fallback 旧分词器（能力不降级）。
				if a, ok := newExecAnalyzer().Analyze(cmd, shell); ok {
					return analyzeExecReadOnly(&a)
				}
				return analyzeExecCommandReadOnly(cmd, shell)
			}
			return analyzeExecReadOnly(analysis)
		}
	case "filesystem":
		action, _ := params["action"].(string)
		return action == "stat"
	case "http":
		method, _ := params["method"].(string)
		method = strings.ToUpper(strings.TrimSpace(method))
		return method == "" || method == "GET" || method == "HEAD"
	default:
		// 未知工具不默认只读：新外部工具必须显式分类。
		return false
	}
}
