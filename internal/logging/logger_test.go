package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeCategoryAllowsPerf(t *testing.T) {
	if got, want := normalizeCategory("perf"), "perf"; got != want {
		t.Fatalf("normalizeCategory(perf) = %q, want %q", got, want)
	}
}

func TestCompactLogFileKeepsTailLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "perf.log")
	content := strings.Join([]string{
		"old-1",
		"old-2",
		"new-1",
		"new-2",
		"new-3",
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	compactLogFileIfNeeded(path, int64(len(content)-1), 18)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if strings.Contains(got, "old-1") || strings.Contains(got, "old-2") {
		t.Fatalf("compacted log kept old lines: %q", got)
	}
	if !strings.Contains(got, "new-2") || !strings.Contains(got, "new-3") {
		t.Fatalf("compacted log missing recent lines: %q", got)
	}
	if strings.HasPrefix(got, "-") {
		t.Fatalf("compacted log starts with a partial line: %q", got)
	}
}
