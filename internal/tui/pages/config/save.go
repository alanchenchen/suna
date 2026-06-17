package config

import "github.com/alanchenchen/suna/internal/protocol"

type ProviderSave struct {
	Params protocol.ConfigSetParams
	Ref    string
}

func (m Model) BuildProviderSave(v ProviderFormValues, existingReasoning map[string]any) ProviderSave {
	params := protocol.ConfigSetParams{
		Action:   protocol.ConfigActionUpsertModel,
		ModelRef: m.EditingName,
		APIKey:   v.APIKey,
		Model: protocol.ConfigModel{
			Provider:        v.Provider,
			Model:           v.Model,
			BaseURL:         v.Endpoint,
			ContextWindow:   ParsePositiveInt(v.ContextWindow),
			MaxOutputTokens: ParsePositiveInt(v.MaxOutputTokens),
			Strengths:       SplitCSV(v.Strengths),
			Reasoning:       existingReasoning,
		},
	}
	if m.SetupMode {
		params.ActiveModel = v.Provider + "/" + v.Model
	}
	return ProviderSave{Params: params, Ref: v.Provider + "/" + v.Model}
}

func BuildWorkspaceSave(workspace, locale, theme, guardMode string) protocol.ConfigSetParams {
	return protocol.ConfigSetParams{Action: protocol.ConfigActionUpdateGeneral, Locale: locale, Theme: theme, GuardMode: guardMode, Workspace: &workspace}
}
