package skill

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/alanchenchen/suna/internal/protocol"
)

func IsProtocolMethod(method string) bool {
	switch method {
	case protocol.MethodSkillList, protocol.MethodSkillSet:
		return true
	default:
		return false
	}
}

func (r *Runtime) HandleProtocol(ctx context.Context, req protocol.Request, sink protocol.EventSink) (any, error) {
	switch req.Method {
	case protocol.MethodSkillList:
		return r.protocolList(ctx)
	case protocol.MethodSkillSet:
		return r.protocolSet(ctx, req.Params, sink)
	default:
		return nil, codedError{code: -32601, message: fmt.Sprintf("method not found: %s", req.Method)}
	}
}

func (r *Runtime) protocolList(ctx context.Context) (protocol.SkillListResult, error) {
	infos, err := r.List(ctx)
	if err != nil {
		return protocol.SkillListResult{}, codedError{code: -32603, message: err.Error()}
	}
	return protocol.SkillListResult{Skills: toProtocolInfos(infos)}, nil
}

func (r *Runtime) protocolSet(ctx context.Context, raw any, _ protocol.EventSink) (protocol.SkillSetResult, error) {
	var params protocol.SkillSetParams
	if err := decode(raw, &params); err != nil {
		return protocol.SkillSetResult{}, codedError{code: -32602, message: err.Error()}
	}
	if !params.Enabled {
		if err := r.Disable(ctx, params.Name); err != nil {
			return protocol.SkillSetResult{}, codedError{code: -32602, message: err.Error()}
		}
		return protocol.SkillSetResult{Status: "ok"}, nil
	}
	if err := r.SetEnabled(ctx, EnableDecision{Name: params.Name, Enabled: true}); err != nil {
		return protocol.SkillSetResult{}, codedError{code: -32602, message: err.Error()}
	}
	return protocol.SkillSetResult{Status: "ok"}, nil
}

func toProtocolInfos(in []Info) []protocol.SkillInfo {
	out := make([]protocol.SkillInfo, 0, len(in))
	for _, item := range in {
		out = append(out, protocol.SkillInfo{Name: item.Name, Description: item.Description, Enabled: item.Enabled, Valid: item.Valid, Reasons: item.Reasons, Path: item.Path, Error: item.Error})
	}
	return out
}

func decode(src any, dst any) error {
	data, err := json.Marshal(src)
	if err != nil {
		return err
	}
	if len(data) == 0 || string(data) == "null" {
		return fmt.Errorf("missing params")
	}
	return json.Unmarshal(data, dst)
}

type codedError struct {
	code    int
	message string
}

func (e codedError) Error() string { return e.message }
func (e codedError) Code() int     { return e.code }
