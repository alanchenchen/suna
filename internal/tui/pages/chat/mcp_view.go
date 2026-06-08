package chat

import "github.com/alanchenchen/suna/internal/protocol"

type MCPRowView struct {
	Server   protocol.MCPServerInfo
	Selected bool
	Active   bool
	Issue    bool
	Loading  bool
}

type MCPOverlayView struct {
	Rows    []MCPRowView
	Loading bool
	Empty   bool
	Error   string
	Active  int
	Tools   int
	Issues  int
	Total   int
	Width   int
	Inner   int
	Height  int
}

func (m Model) MCPOverlayView(width, overlayMaxHeight int) MCPOverlayView {
	w := maxInt(48, minInt(82, width-4))
	inner := maxInt(28, w-8)
	bodyHeight := maxInt(4, minInt(14, overlayMaxHeight-8))
	active, tools, issues := MCPSummaryCounts(m.MCPServers)
	rows := make([]MCPRowView, 0, len(m.MCPServers))
	for i, s := range m.MCPServers {
		rows = append(rows, MCPRowView{Server: s, Selected: i == m.MCPCursor, Active: s.Active, Issue: s.Error != "", Loading: m.MCPActionServer != "" && m.MCPActionServer == s.Name})
	}
	return MCPOverlayView{
		Rows:    rows,
		Loading: m.MCPLoading && len(m.MCPServers) == 0,
		Empty:   !m.MCPLoading && len(m.MCPServers) == 0,
		Error:   m.MCPError,
		Active:  active,
		Tools:   tools,
		Issues:  issues,
		Total:   len(m.MCPServers),
		Width:   w,
		Inner:   inner,
		Height:  bodyHeight,
	}
}
