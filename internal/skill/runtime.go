package skill

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/alanchenchen/suna/internal/tool"
)

const (
	ToolLoad  = "skill_load"
	ToolStart = "skill_start"
)

// Store 由宿主提供配置读写；skill 包不直接依赖 config，避免包循环。
type Store interface {
	LoadSkillRecords() map[string]Record
	SaveSkillRecords(map[string]Record) error
}

type EnableDecision struct {
	Name    string   `json:"name"`
	Enabled bool     `json:"enabled"`
	Reasons []string `json:"reasons,omitempty"`
}

type LLMReviewResult struct {
	Name           string   `json:"name"`
	Valid          bool     `json:"valid"`
	StaticReasons  []string `json:"static_reasons,omitempty"`
	Review         string   `json:"review,omitempty"`
	NeedsAttention bool     `json:"needs_attention,omitempty"`
	Error          string   `json:"error,omitempty"`
}

type LLMReviewer interface {
	ReviewSkill(ctx context.Context, req LLMReviewRequest) (string, error)
}

type UserPrompter interface {
	AskChoice(ctx context.Context, question string, options []string) (string, error)
}

type LLMReviewRequest struct {
	Name        string
	Description string
	Reasons     []string
	Files       []ReviewFile
}

type ReviewFile struct {
	Path      string
	Content   string
	Truncated bool
}

type LoadNotification struct {
	Name string
}

func LoadNotificationFromResult(toolName string, params map[string]any, result tool.Result) (LoadNotification, bool) {
	if toolName != ToolLoad || result.IsError {
		return LoadNotification{}, false
	}
	name, _ := result.Metadata["skill_name"].(string)
	if name == "" {
		name, _ = params["name"].(string)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return LoadNotification{}, false
	}
	return LoadNotification{Name: name}, true
}

type Runtime struct {
	root     string
	store    Store
	manager  *Manager
	reviewer LLMReviewer
	prompter UserPrompter
	mu       sync.Mutex
}

func NewRuntime(root string, store Store) *Runtime {
	r := &Runtime{root: root, store: store}
	r.manager = NewManager(root, r.loadRecords())
	return r
}

func (r *Runtime) SetRoot(root string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.root = root
	if r.manager != nil {
		r.manager.root = root
	}
}

func (r *Runtime) SetStore(store Store) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.store = store
}

func (r *Runtime) SetReviewer(reviewer LLMReviewer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reviewer = reviewer
}

func (r *Runtime) SetPrompter(prompter UserPrompter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prompter = prompter
}

func (r *Runtime) reloadLocked(ctx context.Context) error {
	if r.manager == nil {
		r.manager = NewManager(r.root, r.loadRecords())
	} else {
		r.manager.SetRecords(r.loadRecords())
	}
	if err := r.manager.Reload(ctx); err != nil {
		return err
	}
	return r.syncStoreLocked(ctx)
}

func (r *Runtime) syncStoreLocked(ctx context.Context) error {
	if r.store == nil || r.manager == nil {
		return nil
	}
	records := r.manager.Records()
	changed := false
	for _, info := range r.manager.List() {
		if !info.Valid {
			continue
		}
		if _, ok := records[info.Name]; !ok {
			// 手动放入 skills 目录的新 Skill 视为用户已信任，默认激活；启动期不做静态 check。
			records[info.Name] = Record{Enabled: true}
			changed = true
		}
	}
	if !changed {
		return nil
	}
	if err := r.store.SaveSkillRecords(records); err != nil {
		return err
	}
	r.manager.SetRecords(records)
	return r.manager.Reload(ctx)
}

func (r *Runtime) Reload(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reloadLocked(ctx)
}

func (r *Runtime) List(ctx context.Context) ([]Info, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.reloadLocked(ctx); err != nil {
		return nil, err
	}
	return r.manager.List(), nil
}

func (r *Runtime) Summary() string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.manager == nil {
		return ""
	}
	return r.manager.Summary()
}

func (r *Runtime) Check(ctx context.Context, name string) (CheckResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.reloadLocked(ctx); err != nil {
		return CheckResult{}, err
	}
	return r.manager.Check(strings.TrimSpace(name)), nil
}

func (r *Runtime) Review(ctx context.Context, name string) (LLMReviewResult, error) {
	r.mu.Lock()
	if r.reviewer == nil {
		r.mu.Unlock()
		return LLMReviewResult{}, fmt.Errorf("skill LLM reviewer is not configured")
	}
	if err := r.reloadLocked(ctx); err != nil {
		r.mu.Unlock()
		return LLMReviewResult{}, err
	}
	check := r.manager.Check(strings.TrimSpace(name))
	if !check.Valid {
		r.mu.Unlock()
		return LLMReviewResult{Name: check.Name, Valid: false, StaticReasons: check.Reasons, Error: check.Error}, nil
	}
	req, err := r.reviewRequestLocked(check)
	reviewer := r.reviewer
	r.mu.Unlock()
	if err != nil {
		return LLMReviewResult{}, err
	}
	review, err := reviewer.ReviewSkill(ctx, req)
	if err != nil {
		return LLMReviewResult{}, err
	}
	return LLMReviewResult{Name: check.Name, Valid: true, StaticReasons: check.Reasons, Review: strings.TrimSpace(review), NeedsAttention: len(check.Reasons) > 0}, nil
}

func (r *Runtime) SetEnabled(ctx context.Context, decision EnableDecision) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.setEnabledLocked(ctx, decision.Name, decision.Enabled, decision.Reasons, true)
}

func (r *Runtime) Disable(ctx context.Context, name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.setEnabledLocked(ctx, name, false, nil, false)
}

func (r *Runtime) setEnabledLocked(ctx context.Context, name string, enabled bool, reasons []string, requireValid bool) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("name is required")
	}
	if err := r.reloadLocked(ctx); err != nil {
		return err
	}
	if requireValid {
		info, ok := r.manager.Info(name)
		if !ok || !info.Valid {
			return fmt.Errorf("skill %q is missing or invalid", name)
		}
	}
	records := r.loadRecords()
	if records == nil {
		records = map[string]Record{}
	}
	current := records[name]
	current.Enabled = enabled
	if reasons != nil {
		current.Reasons = append([]string(nil), reasons...)
	}
	records[name] = current
	return r.saveRecordsLocked(ctx, records)
}

func (r *Runtime) saveWorkflowCheckLocked(ctx context.Context, name string, enabled bool, check CheckResult) error {
	return r.setEnabledLocked(ctx, name, enabled, check.Reasons, true)
}

func (r *Runtime) saveWorkflowDecisionLocked(ctx context.Context, result StartResult) error {
	return r.setEnabledLocked(ctx, result.Name, result.Enabled, result.Reasons, true)
}

func (r *Runtime) saveRecordsLocked(ctx context.Context, records map[string]Record) error {
	if r.store == nil {
		return fmt.Errorf("skill record store is not configured")
	}
	if err := r.store.SaveSkillRecords(records); err != nil {
		return err
	}
	r.manager.SetRecords(records)
	return r.manager.Reload(ctx)
}

type ToolDef struct {
	Name        string
	Description string
	Parameters  map[string]any
}

func (r *Runtime) ToolDefs(withIntent func(map[string]any) map[string]any) []ToolDef {
	loadParams := map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string", "description": "Exact skill name from Available Skills"}}, "required": []string{"name"}}
	startParams := map[string]any{"type": "object", "properties": map[string]any{
		"action": map[string]any{"type": "string", "enum": []string{"import", "check"}, "description": "Skill workflow action. Use import for a source path/URL, check after you prepared files under the skills directory."},
		"name":   map[string]any{"type": "string", "description": "Skill name. Required for check; optional for import."},
		"source": map[string]any{"type": "string", "description": "Local directory path, zip path, or git/http/ssh URL for import."},
	}, "required": []string{"action"}}
	if withIntent != nil {
		loadParams = withIntent(loadParams)
		startParams = withIntent(startParams)
	}
	return []ToolDef{
		{Name: ToolLoad, Description: "Load the full SKILL.md instructions for an enabled skill listed in Available Skills. Only use when the skill is relevant to the current task.", Parameters: loadParams},
		{Name: ToolStart, Description: "Start the built-in Skill verification workflow. Use import to import a Skill source, or check after you have prepared a new Skill directory with file tools. The workflow runs static check, asks the user whether to run LLM review, and asks whether to enable.", Parameters: startParams},
	}
}

func (r *Runtime) ExecuteTool(ctx context.Context, name string, params map[string]any) (tool.Result, bool) {
	switch name {
	case ToolLoad:
		return r.executeLoad(params), true
	case ToolStart:
		res, err := r.Start(ctx, params)
		if err != nil {
			return tool.ErrorResult(err.Error()), true
		}
		return tool.TextResult(startJSONResult(res)), true
	default:
		return tool.Result{}, false
	}
}

func (r *Runtime) executeLoad(params map[string]any) tool.Result {
	if r == nil {
		return tool.ErrorResult("skill runtime is not initialized")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.manager == nil {
		return tool.ErrorResult("skill runtime is not initialized")
	}
	skillName, _ := params["name"].(string)
	skillName = strings.TrimSpace(skillName)
	if skillName == "" {
		return tool.ErrorResult("name is required")
	}
	content, ok, reason := r.manager.Load(skillName)
	if !ok {
		return tool.ErrorResult(reason)
	}
	res := tool.TextResult(fmt.Sprintf("[Skill: %s]\n%s", skillName, content))
	res.Metadata = map[string]any{"skill_name": skillName}
	return res
}

func ToolParamKeys(name string) map[string]bool {
	switch name {
	case ToolLoad:
		return map[string]bool{"name": true}
	case ToolStart:
		return map[string]bool{"action": true, "name": true, "source": true}
	default:
		return nil
	}
}

func (r *Runtime) loadRecords() map[string]Record {
	if r == nil || r.store == nil {
		return nil
	}
	return r.store.LoadSkillRecords()
}
