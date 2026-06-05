package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/prompt"
	"github.com/alanchenchen/suna/internal/skill"
)

type agentSkillReviewer struct{}

type agentSkillPrompter struct{}

func emitSkillReviewEvent(ctx context.Context, name, status, review, errText string) {
	events := skillEventsFromContext(ctx)
	if events == nil {
		return
	}
	events <- Event{Type: EventSkillReview, SkillName: name, SkillReviewStatus: status, SkillReview: review, Content: errText}
}

func (agentSkillReviewer) ReviewSkill(ctx context.Context, req skill.LLMReviewRequest) (string, error) {
	emitSkillReviewEvent(ctx, req.Name, "running", "", "")
	ag := skillAgentFromContext(ctx)
	if ag == nil || ag.router == nil {
		emitSkillReviewEvent(ctx, req.Name, "error", "", "skill review requires configured agent model")
		return "", fmt.Errorf("skill review requires configured agent model")
	}
	files := make([]prompt.SkillReviewFile, 0, len(req.Files))
	for _, file := range req.Files {
		files = append(files, prompt.SkillReviewFile{Path: file.Path, Content: file.Content, Truncated: file.Truncated})
	}
	reviewPrompt, err := ag.prompts.RenderSkillReview(prompt.SkillReviewData{Name: req.Name, Description: req.Description, Reasons: req.Reasons, Files: files, UserRequest: ag.working.LastUserText()})
	if err != nil {
		emitSkillReviewEvent(ctx, req.Name, "error", "", err.Error())
		return "", err
	}
	modelRef := ag.router.ActiveRef()
	modelID := resolveModelID(ag.cfg, modelRef)
	request := &model.CompletionRequest{Model: modelID, Purpose: "skill_review", RequestID: uuid.New().String(), System: "You are reviewing an Agent Skill. Be concise, practical, and safety-focused.", Messages: []model.Message{model.NewTextMessage(model.RoleUser, reviewPrompt)}, MaxTokens: 700, Temperature: 0}
	ch, err := ag.router.Complete(ctx, modelRef, request)
	if err != nil {
		emitSkillReviewEvent(ctx, req.Name, "error", "", err.Error())
		return "", err
	}
	var out string
	for chunk := range ch {
		if chunk.Error != "" {
			emitSkillReviewEvent(ctx, req.Name, "error", "", chunk.Error)
			return "", fmt.Errorf("%s", chunk.Error)
		}
		out += chunk.Content
	}
	out = strings.TrimSpace(out)
	emitSkillReviewEvent(ctx, req.Name, "done", out, "")
	return out, nil
}

func (agentSkillPrompter) AskChoice(ctx context.Context, question string, options []string) (string, error) {
	ag := skillAgentFromContext(ctx)
	if ag == nil {
		return "", fmt.Errorf("skill workflow requires main agent context")
	}
	events := skillEventsFromContext(ctx)
	if events == nil {
		return "", fmt.Errorf("skill workflow requires main agent event stream")
	}
	replyCh := make(chan string, 1)
	events <- Event{Type: EventAskUser, Question: question, Options: append([]string(nil), options...), AllowCustom: false, Reply: replyCh}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case answer := <-replyCh:
		return answer, nil
	}
}

type skillRuntimeContextKey struct{}
type skillRuntimeEventsContextKey struct{}

func contextWithSkillRuntime(ctx context.Context, a *Agent, events chan<- Event) context.Context {
	ctx = context.WithValue(ctx, skillRuntimeContextKey{}, a)
	return context.WithValue(ctx, skillRuntimeEventsContextKey{}, events)
}

func skillAgentFromContext(ctx context.Context) *Agent {
	if a, ok := ctx.Value(skillRuntimeContextKey{}).(*Agent); ok {
		return a
	}
	return nil
}

func skillEventsFromContext(ctx context.Context) chan<- Event {
	if ch, ok := ctx.Value(skillRuntimeEventsContextKey{}).(chan<- Event); ok {
		return ch
	}
	return nil
}
