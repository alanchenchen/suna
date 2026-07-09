package config

import "strings"

func (m Model) Title(defaultTitle, modelsTitle, providerTitle string) string {
	switch m.Page {
	case "models":
		return modelsTitle
	case "detail":
		if m.DetailRef != "" {
			return providerTitle + ": " + m.DetailRef
		}
	}
	return defaultTitle
}

type HelpLabels struct {
	OpenModels    string
	Language      string
	Theme         string
	Guard         string
	Workspace     string
	Attachments   string
	OpenConfigDir string
	AddModel      string
	ModelRow      string
	EditModel     string
	Reasoning     string
	ActivateModel string
	DeleteModel   string
	Models        string
	Detail        string
	Home          string
}

func (m Model) Help(rows []Row, labels HelpLabels) string {
	if m.DeleteConfirm != "" || m.ReasoningOpen {
		return ""
	}
	if m.Cursor >= 0 && m.Cursor < len(rows) {
		switch rows[m.Cursor].Kind {
		case "section":
			return labels.OpenModels
		case "general_language":
			return labels.Language
		case "general_theme":
			return labels.Theme
		case "general_guard":
			return labels.Guard
		case "general_workspace":
			return labels.Workspace
		case "clear_attachments":
			return labels.Attachments
		case "open_config_dir":
			return labels.OpenConfigDir
		case "add_model", "provider_add_model", "add_provider_model":
			return labels.AddModel
		case "model":
			return labels.ModelRow
		case "edit_model":
			return labels.EditModel
		case "edit_reasoning":
			return labels.Reasoning
		case "activate_model":
			return labels.ActivateModel
		case "delete_model":
			return labels.DeleteModel
		}
	}
	switch m.Page {
	case "models":
		return labels.Models
	case "detail":
		return labels.Detail
	default:
		return labels.Home
	}
}

type RowTone int

const (
	RowToneDefault RowTone = iota
	RowToneAgent
	RowToneError
	RowToneBrand
)

func RowLabelTone(label, activateLabel, attachmentsLabel, openConfigDirLabel, deleteLabel string) RowTone {
	trimmed := strings.TrimSpace(label)
	if strings.HasPrefix(trimmed, "+") || strings.Contains(label, activateLabel) {
		return RowToneAgent
	}
	if strings.Contains(label, attachmentsLabel) {
		return RowToneError
	}
	if strings.HasPrefix(trimmed, "▸") || strings.Contains(label, openConfigDirLabel) {
		return RowToneBrand
	}
	if strings.Contains(label, deleteLabel) {
		return RowToneError
	}
	return RowToneDefault
}
