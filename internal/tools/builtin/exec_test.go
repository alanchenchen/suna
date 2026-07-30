package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alanchenchen/suna/internal/tools"
)

func TestExecUsesExecutionContextCWD(t *testing.T) {
	root := t.TempDir()
	ctx := tools.WithExecutionContext(context.Background(), tools.ExecutionContext{CWD: root})

	res := Exec{}.Execute(ctx, map[string]any{"command": "pwd", "shell": "bash"})
	if res.IsError {
		t.Fatalf("Exec.Execute() error = %s", res.Error)
	}
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	if got := strings.TrimSpace(res.Content); got != root {
		t.Fatalf("Exec.Execute() cwd = %q, want %q", got, root)
	}
}

func TestExecResolvesRelativeCWDFromExecutionContext(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0755); err != nil {
		t.Fatal(err)
	}
	ctx := tools.WithExecutionContext(context.Background(), tools.ExecutionContext{CWD: root})

	res := Exec{}.Execute(ctx, map[string]any{"command": "pwd", "cwd": "child", "shell": "bash"})
	if res.IsError {
		t.Fatalf("Exec.Execute() error = %s", res.Error)
	}
	if real, err := filepath.EvalSymlinks(child); err == nil {
		child = real
	}
	if got := strings.TrimSpace(res.Content); got != child {
		t.Fatalf("Exec.Execute() cwd = %q, want %q", got, child)
	}
}

func TestExecLimitsLargeStdout(t *testing.T) {
	res := Exec{}.Execute(context.Background(), map[string]any{
		"command": "yes x | head -c 200000",
		"timeout": float64(5),
		"shell":   "bash",
	})
	if res.IsError {
		t.Fatalf("Exec.Execute() error = %s", res.Error)
	}
	if !res.Truncated {
		t.Fatalf("Exec.Execute().Truncated = false, want true")
	}
	if got, wantMax := len(res.Content), maxExecOutput+100; got > wantMax {
		t.Fatalf("len(Exec.Execute().Content) = %d, want <= %d", got, wantMax)
	}
	if !strings.Contains(res.Content, "truncated") {
		start := len(res.Content) - 80
		if start < 0 {
			start = 0
		}
		t.Fatalf("Exec.Execute().Content suffix = %q, want truncation marker", res.Content[start:])
	}
}
