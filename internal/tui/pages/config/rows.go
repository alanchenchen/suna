package config

import "sort"

type RowsDeps struct {
	Tr               func(string) string
	ProvidersSummary func(total, needs int) string
	Models           []ModelConfig
	ActiveModel      string
	IsActive         func(string) bool
	NeedsAttention   func(ModelConfig) bool
	ModelSummary     func(ModelConfig) string
	CurrentLanguage  string
	Theme            string
	GuardMode        string
	Workspace        string
	ConfigPath       string
	CredentialsPath  string
	AttachmentUsage  string
	ConfigDir        string
	DisplayEndpoint  func(string) string
	ContextDisplay   func(ModelConfig) string
	ReasoningDisplay func(ModelConfig) string
}

func (m *Model) Rows(deps RowsDeps) []Row {
	m.EnsureDefaults()
	switch m.Page {
	case "models":
		return m.ModelRows(deps)
	case "detail":
		return m.DetailRows(deps)
	default:
		return m.HomeRows(deps)
	}
}

func (m *Model) HomeRows(deps RowsDeps) []Row {
	active := deps.ActiveModel
	if active == "" {
		active = deps.Tr("tui.config.none")
	}
	needs := 0
	for _, mc := range deps.Models {
		if deps.NeedsAttention != nil && deps.NeedsAttention(mc) {
			needs++
		}
	}
	rows := []Row{
		{"section", "models", "▸ " + deps.Tr("tui.config.model_connections"), ""},
		{"info", "", "  " + deps.Tr("tui.config.active"), active},
		{"info", "", "  " + deps.Tr("tui.config.providers"), deps.ProvidersSummary(len(deps.Models), needs)},
		{"info", "", "", ""},
		{"label", "", deps.Tr("tui.config.general.section"), ""},
		{"general_language", "", "  " + deps.Tr("tui.config.language"), deps.CurrentLanguage},
		{"general_theme", "", "  " + deps.Tr("tui.config.theme"), deps.Theme},
		{"general_guard", "", "  " + deps.Tr("tui.config.guard_mode"), deps.GuardMode},
		{"general_workspace", "", "  " + deps.Tr("tui.config.workspace"), deps.Workspace},
		{"info", "", "", ""},
		{"label", "", deps.Tr("tui.config.local_files"), ""},
		{"info", "", "  " + deps.Tr("tui.config.config_path"), deps.ConfigPath},
		{"info", "", "  " + deps.Tr("tui.config.credentials_path"), deps.CredentialsPath},
		{"clear_attachments", "", "  " + deps.Tr("tui.config.attachments"), deps.AttachmentUsage},
		{"open_config_dir", "", "  " + deps.Tr("tui.config.open_config_folder"), deps.ConfigDir},
	}
	m.EnsureCursor(rows)
	return rows
}

func (m *Model) ModelRows(deps RowsDeps) []Row {
	models := append([]ModelConfig(nil), deps.Models...)
	sort.Slice(models, func(i, j int) bool { return models[i].Ref() < models[j].Ref() })
	m.Models = nil
	var rows []Row
	if len(models) == 0 {
		rows = append(rows, Row{"add_model", "", deps.Tr("tui.config.add_first_model"), ""})
	}
	for _, mc := range models {
		ref := mc.Ref()
		m.Models = append(m.Models, ref)
		active := deps.IsActive != nil && deps.IsActive(ref)
		label := ModelStatusMark(mc, active) + " " + ref
		rows = append(rows, Row{"model", ref, label, deps.ModelSummary(mc)})
	}
	if len(models) > 0 {
		rows = append(rows, Row{"add_model", "", "+ " + deps.Tr("tui.config.add_model"), ""})
	}
	m.EnsureCursor(rows)
	return rows
}

func (m *Model) DetailRows(deps RowsDeps) []Row {
	var mc ModelConfig
	ok := false
	for _, model := range deps.Models {
		if model.Ref() == m.DetailRef {
			mc, ok = model, true
			break
		}
	}
	if !ok {
		return []Row{{"empty", "", deps.Tr("tui.config.model_not_found"), ""}}
	}
	status := deps.Tr("tui.config.deactivated_status")
	if deps.IsActive != nil && deps.IsActive(mc.Ref()) {
		status = deps.Tr("tui.config.activated_status")
	}
	apiKey := deps.Tr("tui.config.missing")
	if mc.HasAPIKey {
		apiKey = deps.Tr("tui.config.configured")
	}
	rows := []Row{
		{"info", "", deps.Tr("tui.config.status"), status},
		{"info", "", deps.Tr("tui.config.provider.type"), mc.Provider},
		{"info", "", deps.Tr("tui.config.provider.endpoint"), deps.DisplayEndpoint(mc.BaseURL)},
		{"info", "", deps.Tr("tui.config.provider.api_key"), apiKey},
		{"info", "", deps.Tr("tui.config.provider.model"), ModelStatusMark(mc, deps.IsActive != nil && deps.IsActive(mc.Ref())) + " " + mc.Model},
		{"info", "", deps.Tr("tui.config.provider.context_window"), deps.ContextDisplay(mc)},
		{"info", "", deps.Tr("tui.config.reasoning"), deps.ReasoningDisplay(mc)},
		{"info", "", "", ""},
		{"edit_model", "", "  " + deps.Tr("tui.config.edit_model"), ""},
		{"edit_reasoning", "", "  " + deps.Tr("tui.config.edit_reasoning"), ""},
	}
	if deps.IsActive != nil && !deps.IsActive(mc.Ref()) {
		rows = append(rows, Row{"activate_model", mc.Ref(), "  " + deps.Tr("tui.config.activate_model"), ""})
	}
	rows = append(rows, Row{"delete_model", "", "  " + deps.Tr("tui.config.delete_model"), ""})
	return rows
}

func (m *Model) EnsureCursor(rows []Row) {
	if len(rows) == 0 {
		m.Cursor = 0
		return
	}
	if m.Cursor >= len(rows) {
		m.Cursor = len(rows) - 1
	}
	if m.Cursor < 0 {
		m.Cursor = 0
	}
	if !rows[m.Cursor].Selectable() {
		m.MoveCursor(rows, 1)
	}
}

func (m *Model) MoveCursor(rows []Row, delta int) {
	if len(rows) == 0 {
		m.Cursor = 0
		return
	}
	idx := m.Cursor
	for step := 0; step < len(rows); step++ {
		idx += delta
		if idx < 0 {
			idx = len(rows) - 1
		}
		if idx >= len(rows) {
			idx = 0
		}
		if rows[idx].Selectable() {
			m.Cursor = idx
			return
		}
	}
}

func (m Model) SelectedModel(rows []Row) (string, bool) {
	if m.Cursor < 0 || m.Cursor >= len(rows) || rows[m.Cursor].Kind != "model" {
		return "", false
	}
	return rows[m.Cursor].Name, true
}
