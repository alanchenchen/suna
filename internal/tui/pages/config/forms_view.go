package config

import "strings"

type FormView struct {
	Title  string
	Help   string
	Error  string
	Notice string
	Width  int
}

func (m Model) ProviderFormView(title, setupTitle, help string, width int) FormView {
	if m.SetupMode {
		title = setupTitle
	}
	return FormView{Title: title, Help: help, Error: m.Error, Notice: m.Notice, Width: width}
}

func (m Model) WorkspaceFormView(title, help, formHelp string, width int) FormView {
	parts := []string{help}
	if formHelp != "" {
		parts = append(parts, formHelp)
	}
	return FormView{Title: title, Help: strings.Join(parts, "\n"), Error: m.Error, Notice: m.Notice, Width: width}
}
