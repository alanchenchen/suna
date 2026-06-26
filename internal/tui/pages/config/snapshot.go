package config

import (
	coreconfig "github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/protocol"
)

// SnapshotFromProtocol 将 daemon 配置快照转成 Config 页面自己的展示模型。
func SnapshotFromProtocol(p protocol.ConfigParams) []ModelConfig {
	models := make([]ModelConfig, 0, len(p.Models))
	for _, cm := range p.Models {
		models = append(models, ModelConfig{
			Provider:        cm.Provider,
			Protocol:        coreconfig.ModelProtocol(cm.Protocol),
			Model:           cm.Model,
			BaseURL:         cm.BaseURL,
			ContextWindow:   cm.ContextWindow,
			MaxOutputTokens: cm.MaxOutputTokens,
			Strengths:       cm.Strengths,
			SubtaskFor:      cm.SubtaskFor,
			Reasoning:       cm.Reasoning,
			HasAPIKey:       cm.HasAPIKey,
		})
	}
	return models
}

func ActiveModelRef(p protocol.ConfigParams, providerName, modelName, daemonProvider, daemonModel string) string {
	if p.ActiveModel != "" {
		return p.ActiveModel
	}
	provider, model := providerName, modelName
	if daemonProvider != "" {
		provider = daemonProvider
	}
	if daemonModel != "" {
		model = daemonModel
	}
	if provider != "" && model != "" {
		return provider + "/" + model
	}
	return ""
}

func ActiveModel(models []ModelConfig, ref string) (ModelConfig, bool) {
	for _, mc := range models {
		if mc.Ref() == ref {
			return mc, true
		}
	}
	return ModelConfig{}, false
}
