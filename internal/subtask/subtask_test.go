package subtask

import (
	"testing"

	"github.com/alanchenchen/suna/internal/model"
)

func TestToolDefsReturnsAllowedToolDefinitions(t *testing.T) {
	st := New(Request{ToolDefs: []model.ToolDef{{
		Name:        "readfile",
		Description: "read a file",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}},
	}}})

	defs := st.toolDefs()
	if len(defs) != 1 || defs[0].Name != "readfile" {
		t.Fatalf("toolDefs = %#v, want readfile", defs)
	}
	props := defs[0].Parameters["properties"].(map[string]any)
	props["path"] = map[string]any{"type": "number"}

	again := st.toolDefs()
	againProps := again[0].Parameters["properties"].(map[string]any)
	path := againProps["path"].(map[string]any)
	if path["type"] != "string" {
		t.Fatalf("toolDefs aliases request schema, path type = %v", path["type"])
	}
}
