package agent

import "github.com/alanchenchen/suna/internal/media"

func (a *Agent) MediaStore() *media.Store {
	return a.mediaStore
}
