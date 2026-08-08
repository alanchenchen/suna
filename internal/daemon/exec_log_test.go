package daemon

import (
	"testing"

	"github.com/alanchenchen/suna/internal/logging"
)

func TestAppendExecToolLogFieldsUsesSafeMetadataWhitelist(t *testing.T) {
	fields := logging.Event{"tool": "exec"}
	appendExecToolLogFields(fields, map[string]any{
		"kind":             "exec",
		"action":           "run",
		"exec_status":      "running",
		"job_id":           "job-1",
		"scope":            "session",
		"timeout_seconds":  3600,
		"output_truncated": false,
		"command":          "sensitive command",
		"cwd":              "sensitive path",
		"env":              map[string]any{"TOKEN": "sensitive"},
		"output":           "sensitive output",
		"intent":           "sensitive intent",
	})

	for _, key := range []string{"action", "exec_status", "job_id", "scope", "timeout_seconds", "output_truncated"} {
		if _, ok := fields[key]; !ok {
			t.Fatalf("safe field %q missing: %#v", key, fields)
		}
	}
	for _, key := range []string{"command", "cwd", "env", "output", "intent"} {
		if _, ok := fields[key]; ok {
			t.Fatalf("sensitive field %q leaked: %#v", key, fields)
		}
	}
}
