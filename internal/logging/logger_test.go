package logging

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNormalizeCategoryAllowsIPC(t *testing.T) {
	if got, want := normalizeCategory("ipc"), "ipc"; got != want {
		t.Fatalf("normalizeCategory(ipc) = %q, want %q", got, want)
	}
}

func TestLogTimestampsUseExplicitUTC(t *testing.T) {
	var app bytes.Buffer
	message := []byte("standard log\n")
	n, err := (utcLogWriter{&app}).Write(message)
	if err != nil || n != len(message) {
		t.Fatalf("Write = %d, %v, want %d, nil", n, err, len(message))
	}
	for _, line := range []string{app.String(), formatLine("INFO", "app", "test", Event{"id": "one"})} {
		stamp, _, ok := strings.Cut(line, " ")
		parsed, err := time.Parse(logTimeLayout, stamp)
		if !ok || err != nil || parsed.Location() != time.UTC || time.Since(parsed).Abs() > time.Minute {
			t.Fatalf("log = %q, parse error = %v, want current explicit UTC", line, err)
		}
	}
	if !strings.HasSuffix(app.String(), string(message)) {
		t.Fatalf("standard log = %q, want unchanged message", app.String())
	}
}

type shortLogWriter struct{}

func (shortLogWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

func TestStandardLogReportsShortWrite(t *testing.T) {
	n, err := (utcLogWriter{shortLogWriter{}}).Write([]byte("message"))
	if n != 0 || !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("Write = %d, %v, want 0, short write", n, err)
	}
}

func TestCompactLogFileKeepsTailLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
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
