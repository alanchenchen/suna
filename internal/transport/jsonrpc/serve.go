package jsonrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/alanchenchen/suna/internal/logging"
	"github.com/alanchenchen/suna/internal/protocol"
)

const sendTimeout = 5 * time.Second

func (s connSink) Emit(ctx context.Context, event protocol.Event) error {
	notif := Notification{JSONRPC: "2.0", Method: event.Method, Params: event.Params}
	data, err := json.Marshal(notif)
	if err != nil {
		return err
	}
	sendCtx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	if err := s.conn.Send(sendCtx, data); err != nil {
		logging.Error("ipc", "send_failed", err, logging.Event{"conn_id": s.conn.ID(), "method": event.Method})
		return err
	}
	return nil
}

// ServeConn 处理单条 JSON-RPC 连接。transport 只提供 Conn；业务请求统一进入 protocol.Service。
func ServeConn(ctx context.Context, conn Conn, svc protocol.Service, opts Options, onDone func()) {
	defer onDone()
	sink := connSink{conn: conn}
	connected := !opts.RequireHello
	if connected {
		svc.OnConnect(ctx, conn.ID(), sink)
	}
	defer func() {
		if connected {
			svc.OnDisconnect(ctx, conn.ID())
		}
	}()

	// RequireHello=false 用于 local/TUI 内部连接；RequireHello=true 用于公开 transport。
	handshaked := !opts.RequireHello
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		raw, err := conn.Receive()
		if err != nil {
			return
		}
		var req Request
		if err := json.Unmarshal(raw, &req); err != nil {
			sendError(conn, 0, ErrParse, "parse error", ErrorData{Kind: "parse_error"})
			continue
		}
		if req.JSONRPC != "2.0" || req.Method == "" {
			sendError(conn, req.ID, ErrInvalidReq, "invalid request", ErrorData{Kind: "invalid_request"})
			continue
		}
		if !handshaked && req.Method != protocol.MethodRuntimeHello {
			sendError(conn, req.ID, ErrHandshake, "runtime.hello is required before other methods", ErrorData{Kind: "handshake_required"})
			continue
		}
		params := req.Params
		if req.Method == protocol.MethodRuntimeHello {
			params = mergeHelloTransport(params, opts.Transport)
		}
		result, err := svc.Handle(ctx, protocol.Request{ID: req.ID, Method: req.Method, Params: params, ConnID: conn.ID()}, sink)
		if err != nil {
			sendProtocolError(conn, req.ID, err)
			continue
		}
		if req.Method == protocol.MethodRuntimeHello {
			handshaked = true
			if !connected {
				svc.OnConnect(ctx, conn.ID(), sink)
				connected = true
			}
			if opts.OnHandshake != nil {
				opts.OnHandshake()
			}
		}
		sendResult(conn, req.ID, result)
	}
}

func mergeHelloTransport(params any, transport string) any {
	if transport == "" {
		return params
	}
	data, err := json.Marshal(params)
	if err != nil || len(data) == 0 || string(data) == "null" {
		return map[string]any{"transport": transport}
	}
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return params
	}
	// transport 是承载层事实，不能信任客户端在 runtime.hello params 中声明的值。
	obj["transport"] = transport
	return obj
}

func sendResult(conn Conn, id int, result any) {
	resp := Response{JSONRPC: "2.0", ID: id, Result: result}
	data, err := json.Marshal(resp)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	_ = conn.Send(ctx, data)
}

func sendProtocolError(conn Conn, id int, err error) {
	// Service 返回的结构化错误在这里统一映射到 JSON-RPC error，避免各 transport 重复拼响应。
	code := ErrInternal
	if coded, ok := err.(CodedError); ok {
		code = coded.Code()
	}
	var data any
	if withData, ok := err.(DataError); ok {
		data = withData.Data()
	}
	if data == nil {
		data = defaultErrorData(code)
	}
	sendError(conn, id, code, err.Error(), data)
}

func sendError(conn Conn, id int, code int, message string, data any) {
	logging.Error("ipc", "response_error", fmt.Errorf("%s", message), logging.Event{"conn_id": conn.ID(), "request_id": id, "error_code": code})
	resp := Response{JSONRPC: "2.0", ID: id, Error: &Error{Code: code, Message: message, Data: data}}
	raw, err := json.Marshal(resp)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	_ = conn.Send(ctx, raw)
}

func defaultErrorData(code int) ErrorData {
	switch code {
	case ErrNotFound:
		return ErrorData{Kind: "unsupported_method"}
	case ErrInvalidParams, ErrInvalidReq:
		return ErrorData{Kind: "invalid_request"}
	case ErrHandshake:
		return ErrorData{Kind: "handshake_required"}
	default:
		return ErrorData{Kind: "internal_error"}
	}
}
