package builtin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFileStreamsPastInitialByteWindow(t *testing.T) {
	path := writeTempFile(t, buildLines(7000, 40))

	res := ReadFile{}.Execute(context.Background(), map[string]any{"path": path, "start_line": float64(6500), "line_count": float64(3)})
	if res.IsError {
		t.Fatalf("ReadFile.Execute() error = %s", res.Error)
	}
	if !strings.Contains(res.Content, "6500: line-6500") {
		t.Fatalf("ReadFile.Execute() content = %q, want line 6500", res.Content)
	}
	if strings.Contains(res.Content, "1: line-0001") {
		t.Fatalf("ReadFile.Execute() content = %q, should not contain first line", res.Content)
	}
}

func TestReadFileTruncatesByResultBytesWithNextOffset(t *testing.T) {
	path := writeTempFile(t, buildLines(1000, 300))

	res := ReadFile{}.Execute(context.Background(), map[string]any{"path": path, "start_line": float64(1), "line_count": float64(1000)})
	if res.IsError {
		t.Fatalf("ReadFile.Execute() error = %s", res.Error)
	}
	if !res.Truncated {
		t.Fatalf("ReadFile.Execute().Truncated = false, want true")
	}
	if got, wantMax := len(res.Content), maxReadResultBytes+200; got > wantMax {
		t.Fatalf("len(ReadFile.Execute().Content) = %d, want <= %d", got, wantMax)
	}
	if !strings.Contains(res.Content, "Use start_line=") {
		t.Fatalf("ReadFile.Execute() content = %q, want continuation hint", res.Content)
	}
}

func TestReadFileTruncatesVeryLongLine(t *testing.T) {
	path := writeTempFile(t, strings.Repeat("x", maxReadLineBytes*2)+"\nsecond\n")

	res := ReadFile{}.Execute(context.Background(), map[string]any{"path": path, "start_line": float64(1), "line_count": float64(2)})
	if res.IsError {
		t.Fatalf("ReadFile.Execute() error = %s", res.Error)
	}
	if !strings.Contains(res.Content, "line truncated") {
		t.Fatalf("ReadFile.Execute() content = %q, want line truncation marker", res.Content)
	}
	if !strings.Contains(res.Content, "2: second") {
		t.Fatalf("ReadFile.Execute() content = %q, want second line after long line", res.Content)
	}
}

func buildLines(count, payloadLen int) string {
	var b strings.Builder
	payload := strings.Repeat("x", payloadLen)
	for i := 1; i <= count; i++ {
		b.WriteString(fmt.Sprintf("line-%04d %s\n", i, payload))
	}
	return b.String()
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input.txt")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
	return path
}
