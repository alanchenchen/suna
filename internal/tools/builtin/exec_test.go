package builtin

import (
	"context"
	"strings"
	"testing"
)

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
