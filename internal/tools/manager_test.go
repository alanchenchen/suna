package tools

import "testing"

func TestToolDefsDoesNotMutateStoredSpecParameters(t *testing.T) {
	m := NewManager()
	m.specs = map[string]Spec{"demo": {Name: "demo", Parameters: map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}}}}

	defs := m.ToolDefs(func(params map[string]any) map[string]any {
		props := params["properties"].(map[string]any)
		props["intent"] = map[string]any{"type": "string"}
		return params
	})
	if len(defs) != 1 {
		t.Fatalf("ToolDefs len = %d, want 1", len(defs))
	}
	if _, ok := defs[0].Parameters["properties"].(map[string]any)["intent"]; !ok {
		t.Fatalf("returned schema missing injected intent")
	}
	if _, ok := m.specs["demo"].Parameters["properties"].(map[string]any)["intent"]; ok {
		t.Fatalf("ToolDefs mutated stored spec parameters")
	}
}
