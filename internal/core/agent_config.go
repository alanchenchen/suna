package core

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
}

type ConfigModel struct {
	Provider      string
	Model         string
	BaseURL       string
	ContextWindow int
	Strengths     []string
}

// Config 返回当前 daemon 正在使用的配置快照，调用方不得修改其内容。
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
	return a.cfg.Clone(), nil
}

// UpdateConfig 是 daemon 侧配置写入口。TUI 只能通过 IPC 调用这里，避免 UI 直接读写核心配置。
// 模型配置更新后会立即重建 Router，因此 active_model 在当前 daemon 中即时生效。
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
	return cfg, nil
}

func (a *Agent) reloadRouterLocked(cfg *config.Config) error {
	// 模型配置变更必须立即反映到 daemon 内存态，否则 TUI 激活模型会“写了但不生效”。
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
	if p := router.DefaultProvider(); p != nil {
		a.compressor = memory.NewCompressor(p)
	}
	return nil
}
