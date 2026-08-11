package chat

import (
	"sort"

	"github.com/alanchenchen/suna/internal/protocol"
)

type MCPAction struct {
	Name   string
	Active bool
}

func (m *Model) OpenMCPOverlay() {
	m.MCPOverlayOpen = true
	m.MCPLoading = true
	m.MCPError = ""
	m.SkillsOverlayOpen = false
	m.ModelPickerOpen = false
}

func (m *Model) CloseMCPOverlay() {
	m.MCPOverlayOpen = false
	m.MCPError = ""
	m.MCPActionServer = ""
}

func (m *Model) SelectMCPForToggle() (MCPAction, bool) {
	selected, ok := m.MCPList.Selected()
	if !ok {
		return MCPAction{}, false
	}
	row, ok := selected.(mcpItem)
	if !ok {
		return MCPAction{}, false
	}
	return MCPAction{Name: row.server.Name, Active: row.server.State != protocol.MCPServerActive && row.server.State != protocol.MCPServerStarting}, true
}

func (m *Model) SelectMCPForReload() (string, bool) {
	selected, ok := m.MCPList.Selected()
	if !ok {
		return "", false
	}
	row, ok := selected.(mcpItem)
	if !ok {
		return "", false
	}
	return row.server.Name, true
}

func (m *Model) SetMCPServers(servers []protocol.MCPServerInfo) {
	m.MCPServers = servers
	m.MCPLoading = false
	m.MCPActionServer = ""
	// 初始通知可能在 Chat 列表组件创建前抵达；原始数据已保存，初始化后会统一构建列表。
	if m.MCPList.Initialized() {
		m.MCPList.SetItems(mcpItems(servers))
	}
}

func (m *Model) UpdateMCPServer(server protocol.MCPServerInfo) {
	updated := false
	for index := range m.MCPServers {
		if m.MCPServers[index].ID == server.ID {
			m.MCPServers[index] = server
			updated = true
			break
		}
	}
	if !updated {
		m.MCPServers = append(m.MCPServers, server)
	}
	sort.Slice(m.MCPServers, func(i, j int) bool { return m.MCPServers[i].Name < m.MCPServers[j].Name })
	if m.MCPList.Initialized() {
		m.MCPList.SetItems(mcpItems(m.MCPServers))
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
