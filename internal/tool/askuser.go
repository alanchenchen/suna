package tool

import (
	"context"
	"encoding/json"
	"fmt"
)

type AskUser struct {
	handler AskUserHandler
}

type AskUserHandler func(ctx context.Context, question string, options []string) (string, error)

func NewAskUser(handler AskUserHandler) *AskUser {
	return &AskUser{handler: handler}
}

func (a *AskUser) Name() string { return "askuser" }
func (a *AskUser) Description() string {
	return "向用户提问并等待回复。用于需要确认、获取信息或选择分支。"
}
func (a *AskUser) Category() Category { return Communicate }
func (a *AskUser) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"question": map[string]any{"type": "string", "description": "要问用户的问题"},
			"options":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "选项列表"},
		},
		"required": []string{"question"},
	}
}

func (a *AskUser) Execute(ctx context.Context, params map[string]any) Result {
	question, _ := params["question"].(string)
	if question == "" {
		return ErrorResult("question is required")
	}

	var options []string
	if o, ok := params["options"].([]any); ok {
		for _, v := range o {
			if s, ok := v.(string); ok {
				options = append(options, s)
			}
		}
	}

	if a.handler == nil {
		return ErrorResult("no ask user handler configured")
	}

	answer, err := a.handler(ctx, question, options)
	if err != nil {
		return ErrorResult(fmt.Sprintf("ask user: %s", err))
	}

	result := map[string]string{"answer": answer}
	bytes, _ := json.Marshal(result)
	return TextResult(string(bytes))
}
