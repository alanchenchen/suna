package config

import "strings"

type ProviderKindLabels struct {
	Title string
	Help  string
	Name  func(string) string
	Desc  func(string) string
}

type ProviderKindView struct {
	Title   string
	Help    string
	Options []ProviderKindOptionView
	Width   int
}

type ProviderKindOptionView struct {
	Name     string
	Desc     string
	Selected bool
}

func (m Model) ProviderKindView(labels ProviderKindLabels, width int) ProviderKindView {
	options := ProviderKindOptions()
	items := make([]ProviderKindOptionView, 0, len(options))
	for i, opt := range options {
		name, desc := opt, ""
		if labels.Name != nil {
			name = labels.Name(opt)
		}
		if labels.Desc != nil {
			desc = labels.Desc(opt)
		}
		items = append(items, ProviderKindOptionView{Name: name, Desc: desc, Selected: i == m.KindCursor})
	}
	return ProviderKindView{Title: labels.Title, Help: labels.Help, Options: items, Width: width}
}

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
