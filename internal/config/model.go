package config

import (
	"fmt"
	"regexp"
	"strings"
)

type ModelProtocol string

const (
	ModelProtocolOpenAIChat      ModelProtocol = "openai_chat"
	ModelProtocolOpenAIResponses ModelProtocol = "openai_responses"
	ModelProtocolAnthropic       ModelProtocol = "anthropic"
)

type AuthMode string

const (
	AuthModeBearer AuthMode = "bearer"
	AuthModeBoth   AuthMode = "both"
)

func SupportedModelProtocols() []ModelProtocol {
	return []ModelProtocol{ModelProtocolOpenAIChat, ModelProtocolOpenAIResponses, ModelProtocolAnthropic}
}

func IsSupportedModelProtocol(protocol ModelProtocol) bool {
	switch protocol {
	case ModelProtocolOpenAIChat, ModelProtocolOpenAIResponses, ModelProtocolAnthropic:
		return true
	default:
		return false
	}
}

// NormalizeModelProtocol 是旧配置兼容边界：只有读取/归一化配置时允许空值默认成 openai_chat。
func NormalizeModelProtocol(protocol ModelProtocol) ModelProtocol {
	protocol = ModelProtocol(strings.ToLower(strings.TrimSpace(string(protocol))))
	if protocol == "" {
		return ModelProtocolOpenAIChat
	}
	return protocol
}

func (c *Config) NormalizeModels() error {
	for i := range c.Models {
		if strings.Contains(c.Models[i].Provider, "/") {
			return fmt.Errorf("model provider %q must not contain '/'", c.Models[i].Provider)
		}
		protocol := NormalizeModelProtocol(c.Models[i].Protocol)
		if !IsSupportedModelProtocol(protocol) {
			return fmt.Errorf("model %q protocol %q is not supported", c.Models[i].Ref(), c.Models[i].Protocol)
		}
		authMode := AuthMode(strings.ToLower(strings.TrimSpace(string(c.Models[i].AuthMode))))
		switch authMode {
		case "", AuthModeBearer, AuthModeBoth:
		default:
			return fmt.Errorf("model %q auth_mode %q is not supported", c.Models[i].Ref(), c.Models[i].AuthMode)
		}
		if authMode != "" && protocol != ModelProtocolAnthropic {
			return fmt.Errorf("model %q auth_mode is only supported by the anthropic protocol", c.Models[i].Ref())
		}
		c.Models[i].Protocol = protocol
		c.Models[i].AuthMode = authMode
	}
	return nil
}

func (c *Config) ModelByRef(ref string) (ModelConfig, bool) {
	for _, mc := range c.Models {
		if mc.Ref() == ref {
			return mc, true
		}
	}
	return ModelConfig{}, false
}

func (c *Config) ActiveModelConfig() (ModelConfig, bool) { return c.ModelByRef(c.ActiveModel) }

func (mc ModelConfig) Ref() string { return mc.Provider + "/" + mc.Model }

func (mc ModelConfig) ProtocolOrDefault() ModelProtocol { return NormalizeModelProtocol(mc.Protocol) }

// AvailableAsSubtaskFor 判断当前模型是否应作为 activeRef 的子任务候选展示；
// subtask_for 仅是可见性过滤器，不改变 strengths 或工具授权语义。
func (mc ModelConfig) AvailableAsSubtaskFor(activeRef string) bool {
	ref := mc.Ref()
	if strings.TrimSpace(activeRef) == "" || activeRef == ref {
		return true
	}
	if len(mc.SubtaskFor) == 0 {
		return true
	}
	for _, pattern := range mc.SubtaskFor {
		if matchModelRefPattern(strings.TrimSpace(pattern), activeRef) {
			return true
		}
	}
	return false
}

func matchModelRefPattern(pattern, ref string) bool {
	if pattern == "" {
		return false
	}
	if pattern == "*" || pattern == "**" {
		return true
	}
	re, err := modelRefGlobRegexp(pattern)
	return err == nil && re.MatchString(ref)
}

func modelRefGlobRegexp(pattern string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				b.WriteString(".*")
				i += 2
				continue
			}
			b.WriteString("[^/]*")
			i++
		case '?':
			b.WriteString("[^/]")
			i++
		default:
			b.WriteString(regexp.QuoteMeta(string(pattern[i])))
			i++
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

func (mc ModelConfig) ResolveAPIKey() (string, error) {
	if mc.APIKey == "" {
		return "", fmt.Errorf("provider %q missing api_key in credentials.toml", mc.Provider)
	}
	return mc.APIKey, nil
}

// APIKeyHint 返回适合展示的脱敏 API Key：保留前缀标识（如 sk-）和末尾 4 位，
// 中间用固定长度的 • 打码。key 过短时只保留末尾 4 位，避免前缀+尾段
// 覆盖整个 key 造成信息泄漏。空 key 返回空串。
// 只用于 UI 展示；提示本身绝不能当作凭据回传。
func (mc ModelConfig) APIKeyHint() string {
	key := strings.TrimSpace(mc.APIKey)
	if key == "" {
		return ""
	}
	const tail = 4
	const maskMinLen = tail + 4 // 少于 8 位时前缀+尾段会覆盖大半 key，只显示尾段
	if len(key) < maskMinLen {
		return "••••" + key[len(key)-tail:]
	}
	prefixLen := 0
	if strings.HasPrefix(key, "sk-") {
		prefixLen = 3
	}
	masked := strings.Repeat("•", min(8, len(key)-prefixLen-tail))
	return key[:prefixLen] + masked + key[len(key)-tail:]
}

func (mc ModelConfig) IsAnthropic() bool { return mc.ProtocolOrDefault() == ModelProtocolAnthropic }
func (mc ModelConfig) IsOpenAI() bool    { return mc.ProtocolOrDefault() == ModelProtocolOpenAIResponses }
