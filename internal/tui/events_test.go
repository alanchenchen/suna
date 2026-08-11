package tui

import (
	"testing"

	"github.com/alanchenchen/suna/internal/protocol"
)

func TestMCPUpdatedNotificationMergesServerByID(t *testing.T) {
	tui := New(LocaleEN)
	tui.chat.SetMCPServers([]protocol.MCPServerInfo{{ID: "server-a", Name: "server-a", State: protocol.MCPServerStarting}})
	tui.handleMCPUpdatedNotification(protocol.MCPUpdatedParams{Server: protocol.MCPServerInfo{ID: "server-a", Name: "server-a", State: protocol.MCPServerActive, ToolCount: 3}})
	if got := len(tui.chat.MCPServers); got != 1 {
		t.Fatalf("MCP server count = %d, want 1", got)
	}
	server := tui.chat.MCPServers[0]
	if server.State != protocol.MCPServerActive || server.ToolCount != 3 {
		t.Fatalf("MCP server = %#v, want active with 3 tools", server)
	}
}
