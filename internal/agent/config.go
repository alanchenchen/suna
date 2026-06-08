package agent

import (
	"context"
	"fmt"
	"os"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/protocol"
)

type ConfigSetParams struct {
	Action       string
	Model        ConfigModel
	ModelRef     string
	ActiveModel  string
	APIKey       string
	DeleteAPIKey bool
	Locale       string
	Theme        string
	GuardMode    string
	Workspace    *string
}

type ConfigModel struct {
	Provider      string
	Model         string
	BaseURL       string
	ContextWindow int
	Strengths     []string
	Reasoning     map[string]any
}

func (a *Agent) Config() *config.Config {
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	return a.cfg.Clone()
}

func (a *Agent) ReloadConfigFromDiskIfNeeded() (*config.Config, error) {
	a.configMu.Lock()
	defer a.configMu.Unlock()
	info, err := os.Stat(a.cfg.ConfigPath())
	if err != nil {
		// 首次启动时 config.toml 可能还不存在，这是正常的配置向导状态；
		// 保持内存中的空配置即可，不要向上返回错误导致 daemon 状态请求刷 ERROR。
		if os.IsNotExist(err) {
			return a.cfg.Clone(), nil
		}
		return a.cfg.Clone(), err
	}
	if !info.ModTime().After(a.configModTime) && len(a.cfg.Models) > 0 {
		return a.cfg.Clone(), nil
	}
	loaded, err := config.Load(a.cfg.ConfigPath())
	if err != nil {
		return a.cfg.Clone(), err
	}
	a.cfg = loaded
	a.configModTime = info.ModTime()
	if err := a.reloadRouterLocked(loaded); err != nil {
		return nil, err
	}
	a.guard = a.newGuardForSession(a.sessionID)
	a.reloadSkillsLocked()
	if a.mcp != nil {
		a.mcp.SetConfig(loaded.MCP)
	}
	if a.tools != nil {
		if err := a.tools.Reload(context.Background()); err != nil {
			return a.cfg.Clone(), err
		}
	}
	return a.cfg.Clone(), nil
}

func (a *Agent) UpdateConfig(params ConfigSetParams) (*config.Config, error) {
	a.configMu.Lock()
	defer a.configMu.Unlock()
	cfg := a.cfg.Clone()
	if cfg == nil {
		return nil, fmt.Errorf("config not loaded")
	}
	switch params.Action {
	case protocol.ConfigActionUpsertModel:
		mc := config.ModelConfig{Provider: params.Model.Provider, Model: params.Model.Model, BaseURL: params.Model.BaseURL, ContextWindow: params.Model.ContextWindow, Strengths: append([]string(nil), params.Model.Strengths...), Reasoning: cloneMap(params.Model.Reasoning)}
		if mc.Provider == "" || mc.Model == "" {
			return nil, fmt.Errorf("provider and model are required")
		}
		ref := mc.Ref()
		updated := false
		for i, existing := range cfg.Models {
			if existing.Ref() == params.ModelRef || existing.Ref() == ref {
				mc.APIKey = existing.APIKey
				cfg.Models[i] = mc
				updated = true
				break
			}
		}
		if !updated {
			cfg.Models = append(cfg.Models, mc)
		}
		if cfg.ActiveModel == "" || cfg.ActiveModel == params.ModelRef {
			cfg.ActiveModel = ref
		}
		if params.ActiveModel != "" {
			cfg.ActiveModel = params.ActiveModel
		}
		if params.APIKey != "" {
			if err := config.SaveCredential(cfg.DataDir, mc.Provider, params.APIKey); err != nil {
				return nil, err
			}
		}
	case protocol.ConfigActionDeleteModel:
		if params.ModelRef == "" {
			return nil, fmt.Errorf("model_ref is required")
		}
		deletedProvider := ""
		filtered := cfg.Models[:0]
		for _, mc := range cfg.Models {
			if mc.Ref() != params.ModelRef {
				filtered = append(filtered, mc)
			} else {
				deletedProvider = mc.Provider
			}
		}
		cfg.Models = filtered
		if params.DeleteAPIKey && deletedProvider != "" && !providerStillUsed(cfg.Models, deletedProvider) {
			if err := config.DeleteCredential(cfg.DataDir, deletedProvider); err != nil {
				return nil, err
			}
		}
		if cfg.ActiveModel == params.ModelRef {
			cfg.ActiveModel = ""
			if len(cfg.Models) > 0 {
				cfg.ActiveModel = cfg.Models[0].Ref()
			}
		}
	case protocol.ConfigActionActivateModel:
		if _, ok := cfg.ModelByRef(params.ActiveModel); !ok {
			return nil, fmt.Errorf("model %q not found", params.ActiveModel)
		}
		cfg.ActiveModel = params.ActiveModel
	case protocol.ConfigActionUpdateGeneral:
		if params.Locale != "" {
			cfg.UI.Locale = params.Locale
		}
		if params.Theme != "" {
			cfg.UI.Theme = params.Theme
		}
		if params.GuardMode != "" {
			cfg.Guard.Mode = config.GuardConfig{Mode: params.GuardMode}.ModeOrDefault()
		}
		if params.Workspace != nil {
			cfg.Guard.Workspace = *params.Workspace
		}
	default:
		return nil, fmt.Errorf("unknown config action %q", params.Action)
	}
	if err := cfg.Save(cfg.ConfigPath()); err != nil {
		return nil, err
	}
	if err := config.LoadCredentials(cfg); err != nil {
		return nil, err
	}
	a.cfg = cfg
	if err := a.reloadRouterLocked(cfg); err != nil {
		return nil, err
	}
	a.guard = a.newGuardForSession(a.sessionID)
	a.reloadSkillsLocked()
	if a.tools != nil {
		if err := a.tools.Reload(context.Background()); err != nil {
			return nil, err
		}
	}
	if info, err := os.Stat(cfg.ConfigPath()); err == nil {
		a.configModTime = info.ModTime()
	}
	return cfg, nil
}

func (a *Agent) reloadSkillsLocked() {
	if a.cfg == nil || a.skills == nil {
		return
	}
	a.skills.SetRoot(a.cfg.SkillsDir())
	a.skills.SetStore(a.cfg)
	a.skills.SetReviewer(agentSkillReviewer{})
	a.skills.SetPrompter(agentSkillPrompter{})
	_ = a.skills.Reload(context.Background())
}

func providerStillUsed(models []config.ModelConfig, provider string) bool {
	for _, mc := range models {
		if mc.Provider == provider {
			return true
		}
	}
	return false
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (a *Agent) reloadRouterLocked(cfg *config.Config) error {
	if len(cfg.Models) == 0 || cfg.ActiveModel == "" {
		a.router = nil
		a.compressor = memory.NewCompressor(nil)
		if a.prompts != nil {
			a.compressor.SetPrompts(a.prompts)
		}
		if a.extractWorker != nil {
			a.extractWorker.SetProvider(nil)
		}
		return nil
	}
	router, err := model.NewRouter(cfg, a.mediaStore)
	if err != nil {
		return err
	}
	a.router = router
	if a.prompts != nil {
		router.SetPrompts(a.prompts)
	}
	provider := backgroundProvider(router)
	a.compressor = memory.NewCompressor(provider)
	if a.prompts != nil {
		a.compressor.SetPrompts(a.prompts)
	}
	if a.extractWorker != nil {
		a.extractWorker.SetProvider(provider)
	}
	return nil
}
