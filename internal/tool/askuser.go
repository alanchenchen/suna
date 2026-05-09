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
	return "Ask the user a question and wait for their reply. Used for confirmation, information gathering, or branching decisions."
}
func (a *AskUser) Category() Category { return Communicate }
func (a *AskUser) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"question": map[string]any{"type": "string", "description": "Question to ask the user"},
			"options":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "List of options"},
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
