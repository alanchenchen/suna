package agent

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/logging"
	"github.com/alanchenchen/suna/internal/media"
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
	Provider        string
	Protocol        config.ModelProtocol
	Model           string
	BaseURL         string
	ContextWindow   int
	MaxOutputTokens int
	Strengths       []string
	SubtaskFor      []string
	Reasoning       map[string]any
}

func (a *Agent) Config() *config.Config {
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	return a.cfg.Clone()
}

// ReloadConfigFromDiskIfNeeded 仅在候选配置和 Router 均可用后发布运行态快照。
func (a *Agent) ReloadConfigFromDiskIfNeeded() (*config.Config, error) {
	a.configMu.Lock()
	defer a.configMu.Unlock()
	if a.cfg == nil {
		return nil, fmt.Errorf("config not loaded")
	}
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
	loaded, err := config.LoadFromDataDir(a.cfg.ConfigPath(), a.cfg.DataDir)
	if err != nil {
		return a.cfg.Clone(), err
	}
	router, err := newRouterForConfig(loaded)
	if err != nil {
		return a.cfg.Clone(), err
	}

	// cfg/router/modtime 在同一把锁内一次性发布，构建失败不改变任何运行态。
	a.publishConfigLocked(loaded, router, info.ModTime())
	a.reloadRuntimeConfigLocked()
	return a.cfg.Clone(), nil
}

func (a *Agent) UpdateConfig(params ConfigSetParams) (*config.Config, error) {
	a.configMu.Lock()
	defer a.configMu.Unlock()
	if a.cfg == nil {
		return nil, fmt.Errorf("config not loaded")
	}
	cfg := a.cfg.Clone()
	if err := config.LoadCredentials(cfg); err != nil {
		return nil, err
	}

	var credentialChange *stagedCredentialChange
	switch params.Action {
	case protocol.ConfigActionUpsertModel:
		mc := config.ModelConfig{Provider: params.Model.Provider, Protocol: config.ModelProtocol(params.Model.Protocol), Model: params.Model.Model, BaseURL: params.Model.BaseURL, ContextWindow: params.Model.ContextWindow, MaxOutputTokens: params.Model.MaxOutputTokens, Strengths: append([]string(nil), params.Model.Strengths...), SubtaskFor: append([]string(nil), params.Model.SubtaskFor...), Reasoning: cloneMap(params.Model.Reasoning)}
		if mc.Provider == "" || mc.Model == "" {
			return nil, fmt.Errorf("provider and model are required")
		}
		ref := mc.Ref()
		updated := false
		for i, existing := range cfg.Models {
			if existing.Ref() == params.ModelRef || existing.Ref() == ref {
				// 仅同一 provider 可以沿用原模型的凭证，切换 provider 必须重新加载其独立凭证。
				if existing.Provider == mc.Provider {
					mc.APIKey = existing.APIKey
				}
				cfg.Models[i] = mc
				updated = true
				break
			}
		}
		if !updated {
			// 同一 provider 的凭证由 credentials.toml 共享，新模型沿用已加载的密钥。
			mc.APIKey = providerAPIKey(cfg.Models, mc.Provider)
			cfg.Models = append(cfg.Models, mc)
		}
		if cfg.ActiveModel == "" || cfg.ActiveModel == params.ModelRef {
			cfg.ActiveModel = ref
		}
		if params.ActiveModel != "" {
			cfg.ActiveModel = params.ActiveModel
		}
		// 变更后的模型列表必须从当前数据目录重新加载凭证，以便新 provider 只取得自己的密钥。
		if err := config.LoadCredentials(cfg); err != nil {
			return nil, err
		}
		if params.APIKey != "" {
			setProviderAPIKey(cfg.Models, mc.Provider, params.APIKey)
			credentialChange = &stagedCredentialChange{provider: mc.Provider, apiKey: params.APIKey}
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
			credentialChange = &stagedCredentialChange{provider: deletedProvider, delete: true}
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

	if err := normalizeConfigForPublish(cfg); err != nil {
		return nil, err
	}
	router, err := newRouterForConfig(cfg)
	if err != nil {
		return nil, err
	}

	// 所有可预期的失败（校验、Router 构建、暂存写入）都发生在发布前。
	// 配置与凭证分别通过同目录 Rename 提交；若配置提交失败会恢复已提交的凭证。
	modTime, err := stageAndCommitConfig(cfg, credentialChange)
	if err != nil {
		return nil, err
	}
	a.publishConfigLocked(cfg, router, modTime)
	a.reloadRuntimeConfigLocked()
	return a.cfg.Clone(), nil
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

func setProviderAPIKey(models []config.ModelConfig, provider, apiKey string) {
	for i := range models {
		if models[i].Provider == provider {
			models[i].APIKey = apiKey
		}
	}
}

func providerAPIKey(models []config.ModelConfig, provider string) string {
	for _, mc := range models {
		if mc.Provider == provider && mc.APIKey != "" {
			return mc.APIKey
		}
	}
	return ""
}

func normalizeConfigForPublish(cfg *config.Config) error {
	cfg.NormalizeUI()
	if err := cfg.NormalizeModels(); err != nil {
		return err
	}
	if err := cfg.ValidateModelLimits(); err != nil {
		return err
	}
	if cfg.ActiveModel != "" {
		if _, ok := cfg.ModelByRef(cfg.ActiveModel); !ok {
			return fmt.Errorf("active_model %q not found in configured models", cfg.ActiveModel)
		}
	}
	return cfg.NormalizeGuard()
}

type stagedCredentialChange struct {
	provider string
	apiKey   string
	delete   bool
}

// stageAndCommitConfig 先写同目录暂存文件，再通过 Rename 提交。凭证先提交是为了使
// 新 Router 依赖的密钥与配置同时可见；后续配置提交失败时会完整恢复凭证原内容。
func stageAndCommitConfig(cfg *config.Config, credentialChange *stagedCredentialChange) (time.Time, error) {
	if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
		return time.Time{}, err
	}
	stageDir, err := os.MkdirTemp(cfg.DataDir, ".config-update-")
	if err != nil {
		return time.Time{}, err
	}
	defer os.RemoveAll(stageDir)

	stagedConfigPath := filepath.Join(stageDir, "config.toml")
	if err := cfg.Save(stagedConfigPath); err != nil {
		return time.Time{}, err
	}

	var stagedCredentialPath string
	if credentialChange != nil {
		stagedCredentialPath = filepath.Join(stageDir, "credentials.toml")
		if err := copyFileIfExists(cfg.CredentialsPath(), stagedCredentialPath); err != nil {
			return time.Time{}, err
		}
		if credentialChange.delete {
			err = config.DeleteCredential(stageDir, credentialChange.provider)
		} else {
			err = config.SaveCredential(stageDir, credentialChange.provider, credentialChange.apiKey)
		}
		if err != nil {
			return time.Time{}, err
		}
		// DeleteCredential 在暂存目录没有原凭证时不会创建文件；仍需生成
		// 一个可 Rename 的空文件，以便提交路径保持一致。
		if _, err := os.Stat(stagedCredentialPath); os.IsNotExist(err) {
			if err := os.WriteFile(stagedCredentialPath, nil, 0600); err != nil {
				return time.Time{}, err
			}
		} else if err != nil {
			return time.Time{}, err
		}
	}

	credentialBackup, credentialExisted, err := readFileIfExists(cfg.CredentialsPath())
	if err != nil {
		return time.Time{}, err
	}
	if credentialChange != nil {
		if err := os.Rename(stagedCredentialPath, cfg.CredentialsPath()); err != nil {
			return time.Time{}, err
		}
	}
	if err := os.Rename(stagedConfigPath, cfg.ConfigPath()); err != nil {
		if credentialChange != nil {
			if restoreErr := restoreFile(cfg.CredentialsPath(), credentialBackup, credentialExisted, 0600); restoreErr != nil {
				return time.Time{}, fmt.Errorf("commit config: %w (restore credentials: %v)", err, restoreErr)
			}
		}
		return time.Time{}, err
	}
	info, err := os.Stat(cfg.ConfigPath())
	if err != nil {
		return time.Time{}, err
	}
	return info.ModTime(), nil
}

func copyFileIfExists(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func readFileIfExists(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return data, true, nil
}

func restoreFile(path string, data []byte, existed bool, mode os.FileMode) error {
	if !existed {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".restore-")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err == nil {
		_, err = tmp.Write(data)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func cloneMap(in map[string]any) map[string]any {
	return cloneConfigValueMap(in)
}

func cloneConfigValueMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = cloneConfigValue(value)
	}
	return out
}

func cloneConfigValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneConfigValueMap(typed)
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = cloneConfigValue(item)
		}
		return out
	default:
		return value
	}
}

func newRouterForConfig(cfg *config.Config) (*model.Router, error) {
	if len(cfg.Models) == 0 {
		return nil, nil
	}
	return model.NewRouter(cfg, media.NewContextResolver(cfg.AttachmentsDir()))
}

// publishConfigLocked 仅在候选 Router 和持久化配置均成功后调用。
func (a *Agent) publishConfigLocked(cfg *config.Config, router *model.Router, modTime time.Time) {
	a.cfg = cfg
	a.router = router
	a.configModTime = modTime
	a.compressor = memory.NewCompressor()
	if a.prompts != nil {
		a.compressor.SetPrompts(a.prompts)
	}
	if a.extractWorker != nil {
		if router == nil {
			a.extractWorker.SetResolver(nil)
		} else {
			a.extractWorker.SetResolver(func(ref string) (*model.ModelBinding, error) {
				return router.Bind(ref)
			})
		}
	}
	a.materializeLegacyPendingMemoryQueueModelRef(cfg, router)
}

// materializeLegacyPendingMemoryQueueModelRef 为早于 memory_queue 持久化 model_ref 的记录提供幂等兼容迁移。
// Store 不依赖配置或路由；此运行时边界在写入前校验默认模型。Worker 永不将其作为回退模型。
func (a *Agent) materializeLegacyPendingMemoryQueueModelRef(cfg *config.Config, router *model.Router) {
	if a == nil || a.store == nil || cfg == nil || router == nil {
		return
	}
	modelRef := strings.TrimSpace(cfg.ActiveModel)
	if modelRef == "" {
		return
	}
	if _, err := router.Bind(modelRef); err != nil {
		return
	}
	updated, err := a.store.MaterializePendingMemoryQueueModelRef(context.Background(), modelRef)
	if err != nil {
		logging.Error("memory", "materialize_legacy_queue_model_ref_failed", err, logging.Event{"model_ref": modelRef})
		return
	}
	if updated > 0 && a.extractQueue != nil {
		a.extractQueue.Signal()
	}
}

// reloadRuntimeConfigLocked 是发布后的无返回值同步。工具目录刷新失败不应把已经成功
// 提交的配置伪装为失败；后续刷新会重试。
func (a *Agent) reloadRuntimeConfigLocked() {
	a.reloadSkillsLocked()
	if a.mcp != nil {
		a.mcp.SetConfig(a.cfg.MCP)
	}
	if a.tools != nil {
		_ = a.tools.Reload(context.Background())
	}
}
