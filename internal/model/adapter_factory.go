package model

import (
	"fmt"
	"sync"

	"github.com/alanchenchen/suna/internal/config"
)

// AdapterSpec 是将模型配置编译后的静态协议适配信息。
type AdapterSpec struct {
	Protocol        config.ModelProtocol
	ModelID         string
	BaseURL         string
	APIKey          string
	ContextWindow   int
	MaxOutputTokens int
}

// AdapterDependencies 是 Adapter 创建时注入的共享依赖。
type AdapterDependencies struct {
	MediaResolver MediaResolver
}

// AdapterFactory 只负责创建一个明确协议契约的 Adapter。
type AdapterFactory interface {
	Protocol() config.ModelProtocol
	Create(spec AdapterSpec, deps AdapterDependencies) (Adapter, error)
}

// AdapterRegistry 按协议保存内建 AdapterFactory。它不负责模型路由或当前模型选择。
type AdapterRegistry struct {
	factories map[config.ModelProtocol]AdapterFactory
}

func NewAdapterRegistry(factories ...AdapterFactory) (*AdapterRegistry, error) {
	r := &AdapterRegistry{factories: make(map[config.ModelProtocol]AdapterFactory, len(factories))}
	for _, factory := range factories {
		if factory == nil {
			return nil, fmt.Errorf("adapter factory is nil")
		}
		protocol := factory.Protocol()
		if protocol == "" {
			return nil, fmt.Errorf("adapter factory protocol is empty")
		}
		if _, exists := r.factories[protocol]; exists {
			return nil, fmt.Errorf("duplicate adapter factory for protocol %q", protocol)
		}
		r.factories[protocol] = factory
	}
	return r, nil
}

func (r *AdapterRegistry) Create(spec AdapterSpec, deps AdapterDependencies) (Adapter, error) {
	if r == nil {
		return nil, fmt.Errorf("adapter registry is unavailable")
	}
	factory, ok := r.factories[spec.Protocol]
	if !ok {
		return nil, fmt.Errorf("protocol %q is not supported", spec.Protocol)
	}
	return factory.Create(spec, deps)
}

func newBuiltinAdapterRegistry() *AdapterRegistry {
	registry, err := NewAdapterRegistry(
		openAIResponsesAdapterFactory{},
		openAIChatAdapterFactory{},
		anthropicAdapterFactory{},
	)
	if err != nil {
		panic(err)
	}
	return registry
}

type openAIResponsesAdapterFactory struct{}

func (openAIResponsesAdapterFactory) Protocol() config.ModelProtocol {
	return config.ModelProtocolOpenAIResponses
}

func (openAIResponsesAdapterFactory) Create(spec AdapterSpec, deps AdapterDependencies) (Adapter, error) {
	return NewOpenAIResponsesAdapter(spec, deps), nil
}

type openAIChatAdapterFactory struct{}

func (openAIChatAdapterFactory) Protocol() config.ModelProtocol {
	return config.ModelProtocolOpenAIChat
}

func (openAIChatAdapterFactory) Create(spec AdapterSpec, deps AdapterDependencies) (Adapter, error) {
	return NewOpenAIChatAdapter(spec, deps), nil
}

type anthropicAdapterFactory struct{}

func (anthropicAdapterFactory) Protocol() config.ModelProtocol {
	return config.ModelProtocolAnthropic
}

func (anthropicAdapterFactory) Create(spec AdapterSpec, deps AdapterDependencies) (Adapter, error) {
	return NewAnthropicAdapter(spec, deps), nil
}

var builtinAdapterRegistry = sync.OnceValue(newBuiltinAdapterRegistry)
