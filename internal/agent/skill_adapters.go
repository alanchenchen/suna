package agent

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/prompt"
	"github.com/alanchenchen/suna/internal/skill"
)

type agentSkillReviewer struct{}

type agentSkillPrompter struct{}

func (agentSkillReviewer) ReviewSkill(ctx context.Context, req skill.LLMReviewRequest) (string, error) {
	ag := skillAgentFromContext(ctx)
	if ag == nil || ag.router == nil {
		return "", fmt.Errorf("skill review requires configured agent model")
	}
	files := make([]prompt.SkillReviewFile, 0, len(req.Files))
	for _, file := range req.Files {
		files = append(files, prompt.SkillReviewFile{Path: file.Path, Content: file.Content, Truncated: file.Truncated})
	}
	reviewPrompt, err := ag.prompts.RenderSkillReview(prompt.SkillReviewData{Name: req.Name, Description: req.Description, Reasons: req.Reasons, Files: files, UserRequest: ag.working.LastUserText()})
	if err != nil {
		return "", err
	}
	modelRef := ag.router.ActiveRef()
	modelID := resolveModelID(ag.cfg, modelRef)
	request := &model.CompletionRequest{Model: modelID, Purpose: "skill_review", RequestID: uuid.New().String(), System: "You are reviewing an Agent Skill. Be concise, practical, and safety-focused.", Messages: []model.Message{model.NewTextMessage(model.RoleUser, reviewPrompt)}, MaxTokens: 700, Temperature: 0}
	ch, err := ag.router.Complete(ctx, modelRef, request)
	if err != nil {
		return "", err
	}
	var out string
	for chunk := range ch {
		if chunk.Error != "" {
			return "", fmt.Errorf("%s", chunk.Error)
		}
		out += chunk.Content
	}
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
