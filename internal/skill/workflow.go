package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	StartImport = "import"
	StartCheck  = "check"

	optionReviewYes = "Run LLM review"
	optionReviewNo  = "Skip review"
	optionEnableYes = "Enable"
	optionEnableNo  = "Keep disabled"
)

type StartResult struct {
	Name        string           `json:"name"`
	Action      string           `json:"action"`
	Valid       bool             `json:"valid"`
	Reasons     []string         `json:"reasons,omitempty"`
	Review      *LLMReviewResult `json:"review,omitempty"`
	Enabled     bool             `json:"enabled"`
	ReviewAsk   string           `json:"review_ask,omitempty"`
	EnableAsk   string           `json:"enable_ask,omitempty"`
	Error       string           `json:"error,omitempty"`
	Description string           `json:"description,omitempty"`
}

func (r *Runtime) Start(ctx context.Context, params map[string]any) (StartResult, error) {
	action, _ := params["action"].(string)
	action = strings.TrimSpace(action)
	switch action {
	case StartImport:
		source, _ := params["source"].(string)
		name, _ := params["name"].(string)
		imported, err := r.Import(ctx, source, name)
		if err != nil {
			return StartResult{}, err
		}
		return r.finishStart(ctx, startResultFromCheck(imported.Name, action, imported.Check))
	case StartCheck:
		name, _ := params["name"].(string)
		name = strings.TrimSpace(name)
		if name == "" {
			return StartResult{}, fmt.Errorf("name is required")
		}
		check, err := r.Check(ctx, name)
		if err != nil {
			return StartResult{}, err
		}
		if !check.Valid {
			return startResultFromCheck(name, action, check), nil
		}
		_ = r.Disable(ctx, name)
		return r.finishStart(ctx, startResultFromCheck(name, action, check))
	default:
		return StartResult{}, fmt.Errorf("invalid skill_start action")
	}
}

func startResultFromCheck(name, action string, check CheckResult) StartResult {
	return StartResult{Name: name, Action: action, Valid: check.Valid, Reasons: append([]string(nil), check.Reasons...), Error: check.Error, Description: check.Description}
}

func checkFromStartResult(result StartResult) CheckResult {
	return CheckResult{Name: result.Name, Valid: result.Valid, Reasons: append([]string(nil), result.Reasons...), Description: result.Description, Error: result.Error}
}

func (r *Runtime) finishStart(ctx context.Context, result StartResult) (StartResult, error) {
	if !result.Valid {
		return result, nil
	}
	reviewChoice, err := r.askChoice(ctx, formatCheckQuestion(checkFromStartResult(result)), []string{optionReviewYes, optionReviewNo})
	if err != nil {
		return result, err
	}
	result.ReviewAsk = reviewChoice
	if reviewChoice == optionReviewYes {
		review, err := r.Review(ctx, result.Name)
		if err != nil {
			return result, err
		}
		result.Review = &review
	}
	enableChoice, err := r.askChoice(ctx, formatEnableQuestion(result), []string{optionEnableYes, optionEnableNo})
	if err != nil {
		return result, err
	}
	result.EnableAsk = enableChoice
	result.Enabled = enableChoice == optionEnableYes
	if err := r.saveWorkflowDecisionLocked(ctx, result); err != nil {
		return result, err
	}
	return result, nil
}

func (r *Runtime) askChoice(ctx context.Context, question string, options []string) (string, error) {
	r.mu.Lock()
	prompter := r.prompter
	r.mu.Unlock()
	if prompter == nil {
		return "", fmt.Errorf("skill workflow prompter is not configured")
	}
	for attempt := 0; attempt < 2; attempt++ {
		q := question
		if attempt > 0 {
			q = "Please choose one of the provided options to continue the Skill workflow.\n" + question
		}
		answer, err := prompter.AskChoice(ctx, q, options)
		if err != nil {
			return "", err
		}
		for _, opt := range options {
			if strings.TrimSpace(answer) == opt {
				return opt, nil
			}
		}
	}
	return "", fmt.Errorf("invalid choice")
}

func formatCheckQuestion(check CheckResult) string {
	var b strings.Builder
	b.WriteString("Skill static check completed: ")
	b.WriteString(check.Name)
	if len(check.Reasons) == 0 {
		b.WriteString("\nNo obvious issues found.")
	} else {
		b.WriteString("\nPotential issues found:")
		for _, reason := range check.Reasons {
			b.WriteString("\n- ")
			b.WriteString(reason)
		}
	}
	b.WriteString("\nDo you want Suna to run an additional LLM review for this Skill?")
	return b.String()
}

func formatEnableQuestion(result StartResult) string {
	var b strings.Builder
	if result.Review != nil {
		b.WriteString("LLM review completed. ")
	}
	b.WriteString("Enable Skill ")
	b.WriteString(result.Name)
	b.WriteString("?")
	return b.String()
}

func startJSONResult(v any) string {
	if result, ok := v.(StartResult); ok {
		b, err := json.Marshal(summarizeStartResult(result))
		if err != nil {
			return err.Error()
		}
		return string(b)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err.Error()
	}
	return string(b)
}

type startResultSummary struct {
	Name           string   `json:"name"`
	Action         string   `json:"action"`
	Valid          bool     `json:"valid"`
	Reasons        []string `json:"reasons,omitempty"`
	ReviewRan      bool     `json:"review_ran,omitempty"`
	NeedsAttention bool     `json:"needs_attention,omitempty"`
	ReviewSummary  string   `json:"review_summary,omitempty"`
	Enabled        bool     `json:"enabled"`
	ReviewAsk      string   `json:"review_ask,omitempty"`
	EnableAsk      string   `json:"enable_ask,omitempty"`
	Error          string   `json:"error,omitempty"`
	Description    string   `json:"description,omitempty"`
}

func summarizeStartResult(result StartResult) startResultSummary {
	s := startResultSummary{Name: result.Name, Action: result.Action, Valid: result.Valid, Reasons: append([]string(nil), result.Reasons...), Enabled: result.Enabled, ReviewAsk: result.ReviewAsk, EnableAsk: result.EnableAsk, Error: result.Error, Description: result.Description}
	if result.Review != nil {
		s.ReviewRan = true
		s.NeedsAttention = result.Review.NeedsAttention
		s.ReviewSummary = firstReviewLine(result.Review.Review)
		if result.Review.Error != "" && s.Error == "" {
			s.Error = result.Review.Error
		}
	}
	return s
}

func firstReviewLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.Trim(line, "#*- `"))
		if line != "" {
			if len(line) > 240 {
				return line[:240] + "..."
			}
			return line
		}
	}
	return ""
}
