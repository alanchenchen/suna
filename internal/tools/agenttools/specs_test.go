package agenttools

import (
	"testing"

	"github.com/alanchenchen/suna/internal/tools"
)

func TestSpawnSpecToolEnumMatchesGrantableCatalog(t *testing.T) {
	catalog := []tools.Spec{
		{Name: "readfile", Source: tools.Source{Kind: tools.SourceBuiltin}},
		{Name: "askuser", Source: tools.Source{Kind: tools.SourceAgent}},
		{Name: "mcp-tool", Source: tools.Source{Kind: tools.SourceMCP}},
		{Name: "skill-tool", Source: tools.Source{Kind: tools.SourceSkill}},
	}
	names := spawnToolNames(catalog)
	// 只有 builtin 和 MCP 可授予 subtask；agent/skill 源被过滤。
	if len(names) != 2 {
		t.Fatalf("spawnToolNames() = %v, want 2 names", names)
	}
	for _, n := range []string{"readfile", "mcp-tool"} {
		found := false
		for _, got := range names {
			if got == n {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("spawnToolNames() missing %q: %v", n, names)
		}
	}

	spec := spawnSpec(names)
	properties, ok := spec.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatal("spawn properties missing")
	}
	toolsParam, ok := properties["tools"].(map[string]any)
	if !ok {
		t.Fatal("spawn tools param missing")
	}
	items, ok := toolsParam["items"].(map[string]any)
	if !ok {
		t.Fatal("spawn tools items missing")
	}
	enum, ok := items["enum"].([]string)
	if !ok {
		t.Fatal("spawn tools enum missing")
	}
	if len(enum) != 2 || enum[0] != "readfile" || enum[1] != "mcp-tool" {
		t.Fatalf("spawn tools enum = %v, want [readfile mcp-tool]", enum)
	}
}

func TestSpawnSpecRequiresTaskModelTools(t *testing.T) {
	spec := spawnSpec(nil)
	required, ok := spec.Parameters["required"].([]string)
	if !ok {
		t.Fatal("spawn required missing")
	}
	if len(required) != 3 {
		t.Fatalf("spawn required = %v, want 3 entries", required)
	}
}

func TestSpawnSpecEmptyToolNamesAllowsModelOnly(t *testing.T) {
	spec := spawnSpec([]string{})
	properties, ok := spec.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatal("spawn properties missing")
	}
	toolsParam, ok := properties["tools"].(map[string]any)
	if !ok {
		t.Fatal("spawn tools param missing")
	}
	items, ok := toolsParam["items"].(map[string]any)
	if !ok {
		t.Fatal("spawn tools items missing")
	}
	enum, ok := items["enum"].([]string)
	if !ok || len(enum) != 0 {
		t.Fatalf("spawn tools enum = %v, want empty (model-only allowed)", enum)
	}
}

func TestAskUserSpecGuardNeverAndQuestionRequired(t *testing.T) {
	spec := askUserSpec()
	if spec.Guard != tools.GuardNever {
		t.Fatalf("askUserSpec Guard = %v, want GuardNever", spec.Guard)
	}
	required, ok := spec.Parameters["required"].([]string)
	if !ok || len(required) != 1 || required[0] != "question" {
		t.Fatalf("askUserSpec required = %v, want [question]", required)
	}
}
