package agenttools

import "testing"

func TestSpawnSpecExposesOnlySupportedPromptInputs(t *testing.T) {
	spec := spawnSpec([]string{"readfile"})
	properties, ok := spec.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatal("spawn properties missing")
	}
	if _, ok := properties["system"]; ok {
		t.Fatal("spawn schema exposes unsupported system prompt override")
	}
	for _, name := range []string{"task", "model", "tools"} {
		if _, ok := properties[name]; !ok {
			t.Fatalf("spawn property %q missing", name)
		}
	}
}
