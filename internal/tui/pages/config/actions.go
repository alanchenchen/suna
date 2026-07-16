package config

import uipage "github.com/alanchenchen/suna/internal/tui/pages/page"

func (m *Model) OpenDetail(ref string, defaultCursor int) {
	m.DetailRef = ref
	m.Page = "detail"
	m.Cursor = defaultCursor
}

// ReturnToModels 在详情 ref 失效或删除后回到模型列表。
func (m *Model) ReturnToModels(cursor int) {
	m.Page = "models"
	m.DetailRef = ""
	m.Cursor = cursor
}

func (m *Model) BeginDelete(ref string) {
	if ref == "" {
		return
	}
	m.DeleteConfirm = ref
	m.DeleteCursor = 0
}

func (m *Model) LeaveTarget() uipage.Page {
	if m.SetupMode {
		m.SetupMode = false
		m.FormOpen = false
		m.Page = "home"
		return uipage.Welcome
	}
	if m.DeleteConfirm != "" {
		m.DeleteConfirm = ""
		return uipage.None
	}
	if m.WorkspaceOpen {
		m.WorkspaceOpen = false
		m.FormOpen = false
		return uipage.None
	}
	if m.Page == "detail" {
		m.Page = "models"
		m.Cursor = 0
		return uipage.None
	}
	if m.Page == "models" {
		m.Page = "home"
		m.Cursor = 0
		return uipage.None
	}
	if m.FromMode != uipage.None {
		return m.FromMode
	}
	return uipage.Welcome
}

func ProviderFormRef(v ProviderFormValues) string {
	if v.Provider == "" || v.Model == "" {
		return ""
	}
	return v.Provider + "/" + v.Model
}

func ModelCursorForActive(rows []Row, active string) int {
	for i, row := range rows {
		if row.Kind == "model" && row.Name == active {
			return i
		}
	}
	for i, row := range rows {
		if row.Selectable() {
			return i
		}
	}
	return 0
}

func DetailDefaultCursor(rows []Row, preferred string) int {
	for i, row := range rows {
		if row.Kind == preferred {
			return i
		}
	}
	return 0
}
