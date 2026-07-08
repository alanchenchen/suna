package daemon

import (
	"context"

	"github.com/alanchenchen/suna/internal/protocol"
)

type multiSink []protocol.EventSink

func (m multiSink) Emit(ctx context.Context, event protocol.Event) error {
	var first error
	for _, sink := range m {
		if sink == nil {
			continue
		}
		if err := sink.Emit(ctx, event); err != nil && first == nil {
			first = err
		}
	}
	return first
}
