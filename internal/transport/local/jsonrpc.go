package local

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/alanchenchen/suna/internal/logging"
	"github.com/alanchenchen/suna/internal/protocol"
)

const (
	errParse         = -32700
	errNotFound      = -32601
	errInvalidParams = -32602
	errInternal      = -32603
)

type Request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id,omitempty"`
	Result  any    `json:"result,omitempty"`
	Error   *Error `json:"error,omitempty"`
}

type Notification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type localConn interface {
	Send(ctx context.Context, msg []byte) error
	Receive() ([]byte, error)
	Close() error
	ID() string
}

type connSink struct{ conn localConn }

func (s connSink) Emit(ctx context.Context, event protocol.Event) error {
	// local transport 当前使用 JSON-RPC notification 承载 protocol.Event；这个 framing 不泄漏到 protocol 层。
	notif := Notification{JSONRPC: "2.0", Method: event.Method, Params: event.Params}
	data, err := json.Marshal(notif)
	if err != nil {
		return err
	}
	sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.conn.Send(sendCtx, data); err != nil {
		logging.Error("transport", "send_failed", err, logging.Event{"conn_id": s.conn.ID(), "method": event.Method})
		return err
	}
	return nil
}

func serveConn(ctx context.Context, conn localConn, svc protocol.Service, onDone func()) {
	defer onDone()
	sink := connSink{conn: conn}
	// 连接生命周期也交给 Service，使 daemon 能维护当前连接的 EventSink，用于后台任务继续推送事件。
	svc.OnConnect(ctx, conn.ID(), sink)
	defer svc.OnDisconnect(ctx, conn.ID())
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
			sendError(conn, 0, errParse, "parse error")
			continue
		}
		// JSON-RPC 只在 local transport 内部解包；Service 只接收 transport-neutral 的 protocol.Request。
		result, err := svc.Handle(ctx, protocol.Request{ID: req.ID, Method: req.Method, Params: req.Params, ConnID: conn.ID()}, sink)
		if err != nil {
			code := errInternal
			if coded, ok := err.(interface{ Code() int }); ok {
				code = coded.Code()
			}
			sendError(conn, req.ID, code, err.Error())
			continue
		}
		sendResult(conn, req.ID, result)
	}
}

func sendResult(conn localConn, id int, result any) {
	resp := Response{JSONRPC: "2.0", ID: id, Result: result}
	data, err := json.Marshal(resp)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = conn.Send(ctx, data)
}

func sendError(conn localConn, id int, code int, message string) {
	logging.Error("transport", "response_error", fmt.Errorf("%s", message), logging.Event{"conn_id": conn.ID(), "request_id": id, "error_code": code})
	resp := Response{JSONRPC: "2.0", ID: id, Error: &Error{Code: code, Message: message}}
	data, err := json.Marshal(resp)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = conn.Send(ctx, data)
}
