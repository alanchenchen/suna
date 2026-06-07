package tools

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/alanchenchen/suna/internal/model"
)

// Manager 是统一工具目录和执行路由；它不做 Guard 决策，Guard 仍由 Agent 持有上下文处理。
type Manager struct {
	mu        sync.RWMutex
	providers []Provider
	specs     map[string]Spec
	owners    map[string]Provider
}

func NewManager() *Manager {
	return &Manager{specs: map[string]Spec{}, owners: map[string]Provider{}}
}

func (m *Manager) RegisterProvider(provider Provider) {
	if provider == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.providers = append(m.providers, provider)
}

func (m *Manager) Reload(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	specs := map[string]Spec{}
	owners := map[string]Provider{}
	for _, provider := range m.providers {
		catalog := sortedSpecs(specs)
		var items []Spec
		var err error
		if catalogProvider, ok := provider.(CatalogProvider); ok {
			items, err = catalogProvider.SpecsWithCatalog(ctx, catalog)
		} else {
			items, err = provider.Specs(ctx)
		}
		if err != nil {
			return err
		}
		for _, spec := range items {
			if spec.Name == "" {
				continue
			}
			if _, exists := specs[spec.Name]; exists {
				return fmt.Errorf("duplicate tool %q", spec.Name)
			}
			specs[spec.Name] = spec
			owners[spec.Name] = provider
		}
	}
	m.specs = specs
	m.owners = owners
	return nil
}

func sortedSpecs(specs map[string]Spec) []Spec {
	out := make([]Spec, 0, len(specs))
	for _, spec := range specs {
		out = append(out, spec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (m *Manager) Get(name string) (Spec, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	spec, ok := m.specs[name]
	return spec, ok
}

func (m *Manager) Specs() []Spec {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return sortedSpecs(m.specs)
}

func (m *Manager) Names() []string {
	specs := m.Specs()
	names := make([]string, 0, len(specs))
	for _, spec := range specs {
		names = append(names, spec.Name)
	}
	return names
}

func (m *Manager) ToolDefs(withParams func(map[string]any) map[string]any) []model.ToolDef {
	specs := m.Specs()
	defs := make([]model.ToolDef, 0, len(specs))
	for _, spec := range specs {
		params := spec.Parameters
		if withParams != nil {
			params = withParams(params)
		}
		defs = append(defs, model.ToolDef{Name: spec.Name, Description: spec.Description, Parameters: params})
	}
	return defs
}

func (m *Manager) Execute(ctx context.Context, call Call) Result {
	m.mu.RLock()
	spec, ok := m.specs[call.Name]
	owner := m.owners[call.Name]
	m.mu.RUnlock()
	if !ok || owner == nil {
		return ErrorResult(fmt.Sprintf("tool %q not found", call.Name))
	}
	call.Spec = &spec
	result, handled := owner.Execute(ctx, call)
	if !handled {
		return ErrorResult(fmt.Sprintf("tool %q not handled by owner", call.Name))
	}
	return result
}

func (m *Manager) Close(ctx context.Context) error {
	m.mu.RLock()
	providers := append([]Provider(nil), m.providers...)
	m.mu.RUnlock()
	for _, provider := range providers {
		if err := provider.Close(ctx); err != nil {
			return err
		}
	}
	return nil
}
