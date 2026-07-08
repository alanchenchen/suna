package agent

import "github.com/alanchenchen/suna/internal/memory"

func (a *Agent) SessionStore() *memory.SessionStore {
	return a.root().sessionStore
}

func (a *Agent) SessionStateStore() *memory.SessionStateStore {
	return a.root().stateStore
}
