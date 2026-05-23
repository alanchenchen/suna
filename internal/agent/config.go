package agent

import (
	"fmt"
	"os"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/memory"
	"github.com/alanchenchen/suna/internal/model"
)

type ConfigSetParams struct {
	Action      string
	Model       ConfigModel
	ModelRef    string
	ActiveModel string
	APIKey      string
	Locale      string
	Theme       string
	GuardMode   string
	Workspace   *string
}

type ConfigModel struct {
	Provider      string
	Model         string
	BaseURL       string
	ContextWindow int
	Strengths     []string
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
	case "upsert_model":
		mc := config.ModelConfig{Provider: params.Model.Provider, Model: params.Model.Model, BaseURL: params.Model.BaseURL, ContextWindow: params.Model.ContextWindow, Strengths: append([]string(nil), params.Model.Strengths...)}
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
	case "delete_model":
		if params.ModelRef == "" {
			return nil, fmt.Errorf("model_ref is required")
		}
		filtered := cfg.Models[:0]
		for _, mc := range cfg.Models {
			if mc.Ref() != params.ModelRef {
				filtered = append(filtered, mc)
			}
		}
		cfg.Models = filtered
		if cfg.ActiveModel == params.ModelRef {
			cfg.ActiveModel = ""
			if len(cfg.Models) > 0 {
				cfg.ActiveModel = cfg.Models[0].Ref()
			}
		}
	case "activate_model":
		if _, ok := cfg.ModelByRef(params.ActiveModel); !ok {
			return nil, fmt.Errorf("model %q not found", params.ActiveModel)
		}
		cfg.ActiveModel = params.ActiveModel
	case "update_general":
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
	if info, err := os.Stat(cfg.ConfigPath()); err == nil {
		a.configModTime = info.ModTime()
	}
	return cfg, nil
}

func (a *Agent) reloadRouterLocked(cfg *config.Config) error {
	if len(cfg.Models) == 0 || cfg.ActiveModel == "" {
		a.router = nil
		a.compressor = memory.NewCompressor(nil)
		return nil
	}
	router, err := model.NewRouter(cfg)
	if err != nil {
		return err
	}
	a.router = router
	if a.prompts != nil {
		router.SetPrompts(a.prompts)
	}
	if p := router.DefaultProvider(); p != nil {
		a.compressor = memory.NewCompressor(p)
		if a.prompts != nil {
			a.compressor.SetPrompts(a.prompts)
		}
	}
	return nil
}
