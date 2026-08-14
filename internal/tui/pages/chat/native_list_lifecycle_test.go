package chat

import (
	"testing"

	"github.com/alanchenchen/suna/internal/protocol"
	"github.com/alanchenchen/suna/internal/tui/components/overlaylist"
)

func TestSetSkillsBeforeNativeListInitializationPreservesData(t *testing.T) {
	m := Model{}
	skills := []protocol.SkillInfo{{Name: "example-skill", Valid: true}}

	m.SetSkills(skills)

	if len(m.Skills) != 1 || m.Skills[0].Name != "example-skill" {
		t.Fatalf("Skills = %#v, want saved skill notification data", m.Skills)
	}
	if m.SkillsList.Initialized() {
		t.Fatal("SkillsList unexpectedly initialized")
	}
}

func TestSetMCPServersBeforeNativeListInitializationPreservesData(t *testing.T) {
	m := Model{}
	servers := []protocol.MCPServerInfo{{Name: "example-server", State: protocol.MCPServerActive}}

	m.SetMCPServers(servers)

	if len(m.MCPServers) != 1 || m.MCPServers[0].Name != "example-server" {
		t.Fatalf("MCPServers = %#v, want saved MCP notification data", m.MCPServers)
	}
	if m.MCPList.Initialized() {
		t.Fatal("MCPList unexpectedly initialized")
	}
}

func testListText() ListText {
	return ListText{
		SkillsTitle: "Skills", MCPTitle: "MCP Servers", ModelsTitle: "Models", CountSuffix: " items",
		Filter: "Filter: ", Skill: "skill", Skills: "skills", Server: "server", Servers: "servers", Model: "model", Models: "models",
		Toggle: "toggle", Reload: "reload", Select: "select", Close: "close", Tools: "tools", GlobalScope: "GLOBAL", ProjectScope: "PROJECT",
		Up: "up", Down: "down", FilterHelp: "filter", ClearFilter: "clear filter", Cancel: "cancel",
	}
}
func TestResetRuntimeClearsInitializedNativeLists(t *testing.T) {
	m := Model{}
	m.InitComponents(ComponentDeps{})
	m.SetSkills([]protocol.SkillInfo{{Name: "previous-skill", Valid: true}})
	m.SetMCPServers([]protocol.MCPServerInfo{{Name: "previous-server", State: protocol.MCPServerActive}})
	m.InitNativeLists(false, ListStyles{}, testListText())
	m.ModelList.SetItems(modelItems([]ModelPickerRow{{Ref: "provider-a/model-a"}}))

	m.SkillsList.List().SetFilterText("previous")
	m.MCPList.List().SetFilterText("previous")
	m.ModelList.List().SetFilterText("previous")
	m.ResetRuntime()

	for name, listModel := range map[string]*overlaylist.Model{"skills": &m.SkillsList, "MCP": &m.MCPList, "models": &m.ModelList} {
		if got := listModel.List().FilterValue(); got != "" {
			t.Fatalf("%s filter after reset = %q, want empty", name, got)
		}
	}

	if got := len(m.SkillsList.List().Items()); got != 0 {
		t.Fatalf("skill list item count after reset = %d, want 0", got)
	}
	if got := len(m.MCPList.List().Items()); got != 0 {
		t.Fatalf("MCP list item count after reset = %d, want 0", got)
	}
	if got := len(m.ModelList.List().Items()); got != 0 {
		t.Fatalf("model list item count after reset = %d, want 0", got)
	}
}

func TestNativeListInitializationUsesPreviouslyReceivedData(t *testing.T) {
	m := Model{}
	m.SetSkills([]protocol.SkillInfo{{Name: "example-skill", Valid: true}})
	m.SetMCPServers([]protocol.MCPServerInfo{{Name: "example-server", State: protocol.MCPServerActive}})

	m.InitNativeLists(false, ListStyles{}, testListText())

	if got := len(m.SkillsList.List().Items()); got != 1 {
		t.Fatalf("skill list item count = %d, want 1", got)
	}
	if got := len(m.MCPList.List().Items()); got != 1 {
		t.Fatalf("MCP list item count = %d, want 1", got)
	}
}
