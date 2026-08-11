package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alanchenchen/suna/internal/tools"
)

type catalogCountProvider struct {
	mu    sync.Mutex
	calls int
}

func (p *catalogCountProvider) Specs(context.Context) ([]tools.Spec, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	return nil, nil
}
func (*catalogCountProvider) Execute(context.Context, tools.Call) (tools.Result, bool) {
	return tools.Result{}, false
}
func (*catalogCountProvider) Close(context.Context) error { return nil }
func (p *catalogCountProvider) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func TestMCPToolCatalogRefreshDebouncesConcurrentChanges(t *testing.T) {
	provider := &catalogCountProvider{}
	manager := tools.NewManager()
	manager.RegisterProvider(provider)
	if err := manager.Reload(context.Background()); err != nil {
		t.Fatalf("initial Reload() error = %v", err)
	}
	agent := &Agent{tools: manager}
	var wg sync.WaitGroup
	errs := make(chan error, 3)
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- agent.syncMCPToolCatalog(context.Background())
		}()
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("catalog refresh did not complete")
	}
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("syncMCPToolCatalog() error = %v", err)
		}
	}
	if got, want := provider.count(), 2; got != want {
		t.Fatalf("Specs() calls = %d, want %d (initial + one debounced refresh)", got, want)
	}
}
