package builtin

import (
	"context"
	"testing"

	"github.com/alanchenchen/suna/internal/tools"
)

type providerTestTool struct {
	name   string
	result string
}

func (t providerTestTool) Spec() tools.Spec {
	return builtinSpec(t.name, "test tool", tools.Act, map[string]any{"type": "object"})
}

func (t providerTestTool) Execute(context.Context, map[string]any) tools.Result {
	return tools.TextResult(t.result)
}

func TestProviderExecuteUsesFirstToolForDuplicateName(t *testing.T) {
	provider := NewProvider(
		providerTestTool{name: "duplicate", result: "first"},
		providerTestTool{name: "duplicate", result: "second"},
	)
	t.Cleanup(func() { _ = provider.Close(context.Background()) })

	got, ok := provider.Execute(context.Background(), tools.Call{Name: "duplicate"})
	if !ok || got.Content != "first" {
		t.Fatalf("Execute() = %#v, %v, want first tool result", got, ok)
	}
	if _, ok := provider.Execute(context.Background(), tools.Call{Name: "missing"}); ok {
		t.Fatal("Execute() handled unknown tool")
	}
}
