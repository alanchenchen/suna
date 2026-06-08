package chat

import "github.com/alanchenchen/suna/internal/protocol"

type MCPAction struct {
	Name   string
	Active bool
}

func (m *Model) OpenMCPOverlay() {
	m.MCPOverlayOpen = true
	m.MCPLoading = true
	m.MCPError = ""
	m.MCPCursor = ClampMCPCursor(m.MCPCursor, len(m.MCPServers))
	m.SkillsOverlayOpen = false
}

func (m *Model) CloseMCPOverlay() {
	m.MCPOverlayOpen = false
	m.MCPError = ""
	m.MCPActionServer = ""
}

func (m *Model) MoveMCPCursor(delta int) {
	m.MCPCursor = ClampMCPCursor(m.MCPCursor+delta, len(m.MCPServers))
}

func (m *Model) SelectMCPForToggle() (MCPAction, bool) {
	if len(m.MCPServers) == 0 || m.MCPCursor < 0 || m.MCPCursor >= len(m.MCPServers) {
		return MCPAction{}, false
	}
	item := m.MCPServers[m.MCPCursor]
	return MCPAction{Name: item.Name, Active: !item.Active}, true
}

func (m *Model) SelectMCPForReload() (string, bool) {
	if len(m.MCPServers) == 0 || m.MCPCursor < 0 || m.MCPCursor >= len(m.MCPServers) {
		return "", false
	}
	return m.MCPServers[m.MCPCursor].Name, true
}

func (m *Model) SetMCPServers(servers []protocol.MCPServerInfo) {
	m.MCPServers = servers
	m.MCPLoading = false
	m.MCPActionServer = ""
	m.MCPCursor = ClampMCPCursor(m.MCPCursor, len(m.MCPServers))
	if m.MCPCursor < m.MCPScroll {
		m.MCPScroll = m.MCPCursor
	}
}

func (m *Model) SetMCPError(err string) {
	m.MCPLoading = false
	m.MCPActionServer = ""
	m.MCPError = err
}

func (m *Model) SetMCPActionServer(name string) {
	m.MCPActionServer = name
	m.MCPError = ""
}

func MCPSummaryCounts(servers []protocol.MCPServerInfo) (active, tools, issues int) {
	for _, s := range servers {
		if s.Active {
			active++
		}
		tools += s.ToolCount
		if s.Error != "" {
			issues++
		}
	}
	return
}

func ClampMCPCursor(cursor, n int) int {
	if n <= 0 {
		return 0
	}
	if cursor < 0 {
		return 0
	}
	if cursor >= n {
		return n - 1
	}
	return cursor
}
