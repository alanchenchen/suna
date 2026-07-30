package tools

import (
	"context"
	"path/filepath"
	"testing"
)

func TestEffectiveCWDUsesExecutionContextAndResolvesRelativeRequest(t *testing.T) {
	root := t.TempDir()
	ctx := WithExecutionContext(context.Background(), ExecutionContext{CWD: root})

	got, err := EffectiveCWD(ctx, "nested")
	if err != nil {
		t.Fatalf("EffectiveCWD() error = %v", err)
	}
	if want := filepath.Join(root, "nested"); got != want {
		t.Fatalf("EffectiveCWD() = %q, want %q", got, want)
	}
}

func TestEffectiveCWDUsesExecutionContextWhenRequestIsEmpty(t *testing.T) {
	root := t.TempDir()
	ctx := WithExecutionContext(context.Background(), ExecutionContext{CWD: root})

	got, err := EffectiveCWD(ctx, "")
	if err != nil {
		t.Fatalf("EffectiveCWD() error = %v", err)
	}
	if got != root {
		t.Fatalf("EffectiveCWD() = %q, want %q", got, root)
	}
}
