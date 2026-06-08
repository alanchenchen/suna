package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/alanchenchen/suna/internal/protocol"
)

func IsProtocolMethod(method string) bool {
	switch method {
	case protocol.MethodMCPList, protocol.MethodMCPToggle, protocol.MethodMCPReload:
		return true
	default:
		return false
	}
}

func (r *Runtime) HandleProtocol(ctx context.Context, req protocol.Request) (any, error) {
	switch req.Method {
	case protocol.MethodMCPList:
		return r.protocolList(ctx)
	case protocol.MethodMCPToggle:
		var params protocol.MCPSetParams
		if err := decode(req.Params, &params); err != nil {
			return protocol.MCPSetResult{}, codedError{code: -32602, message: err.Error()}
		}
		if strings.TrimSpace(params.Name) == "" {
			return protocol.MCPSetResult{}, codedError{code: -32602, message: "mcp server name is required"}
		}
		if err := r.SetActive(ctx, params.Name, params.Active); err != nil {
			return protocol.MCPSetResult{}, codedError{code: -32602, message: err.Error()}
		}
		return protocol.MCPSetResult{Status: "ok"}, nil
	case protocol.MethodMCPReload:
		var params protocol.MCPReloadParams
		if err := decode(req.Params, &params); err != nil {
			return protocol.MCPReloadResult{}, codedError{code: -32602, message: err.Error()}
		}
		if strings.TrimSpace(params.Name) == "" {
			return protocol.MCPReloadResult{}, codedError{code: -32602, message: "mcp server name is required"}
		}
		if err := r.ReloadServer(ctx, params.Name); err != nil {
			return protocol.MCPReloadResult{}, codedError{code: -32603, message: err.Error()}
		}
		return protocol.MCPReloadResult{Status: "ok"}, nil
	default:
		return nil, codedError{code: -32601, message: fmt.Sprintf("method not found: %s", req.Method)}
	}
}

func (r *Runtime) protocolList(ctx context.Context) (protocol.MCPListResult, error) {
	items := r.Status(ctx)
	out := make([]protocol.MCPServerInfo, 0, len(items))
	for _, item := range items {
		out = append(out, protocol.MCPServerInfo{ID: item.ID, Name: item.ID, Transport: item.Transport, Command: item.Command, Active: item.Active, Configured: item.Configured, ToolCount: item.ToolCount, Error: item.Error})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return protocol.MCPListResult{Servers: out}, nil
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
