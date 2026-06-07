package skilltools

import (
	"testing"

	"github.com/alanchenchen/suna/internal/tools"
)

func TestLoadNotificationFromResult(t *testing.T) {
	res := tools.TextResult("loaded")
	res.Metadata = map[string]any{"skill_name": "writer"}

	name, ok := LoadNotificationFromResult(ToolLoad, map[string]any{}, res)
	if !ok {
		t.Fatalf("LoadNotificationFromResult() ok = false, want true")
	}
	if name != "writer" {
		t.Fatalf("name = %q, want %q", name, "writer")
	}
}
