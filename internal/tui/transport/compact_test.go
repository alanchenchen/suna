//go:build !windows

package transport

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alanchenchen/suna/internal/protocol"
	transportjsonrpc "github.com/alanchenchen/suna/internal/transport/jsonrpc"
	"github.com/alanchenchen/suna/internal/transport/local"
)

func TestCompactReturnsAcceptedIDAndRPCError(t *testing.T) {
	// 客户端只公开 Dial；通过工作区内独立 Unix socket 接入本地假服务，
	// 服务端用 net.Pipe 测试既有按行分帧路径，不触碰默认 daemon 地址。
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(cwd, ".ct-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	listener, err := net.Listen("unix", filepath.Join(dir, "s"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverDone := make(chan error, 1)
	observed := make(chan protocol.CompactState, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer conn.Close()
		if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
			serverDone <- err
			return
		}
		serverDone <- serveCompactFrames(conn)
	}()

	client, err := local.Dial(filepath.Join(dir, "s"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	tuiClient := &Client{client: client}
	client.OnNotify(func(method string, params json.RawMessage) {
		if method == protocol.NotifyCompactState {
			var state protocol.CompactState
			if json.Unmarshal(params, &state) == nil {
				observed <- state
			}
		}
	})
	got, err := tuiClient.Compact()
	if err != nil || got.CompactID != "manual-123" {
		t.Fatalf("accepted = %+v, err = %v", got, err)
	}
	select {
	case state := <-observed:
		if state.CompactID != got.CompactID || state.State != "skipped" {
			t.Fatalf("notification = %+v, accepted = %+v", state, got)
		}
	case <-time.After(time.Second):
		t.Fatal("compact terminal notification not delivered before response")
	}
	got, err = tuiClient.Compact()
	if err == nil || err.Error() != "busy" || got.CompactID != "" {
		t.Fatalf("rejected = %+v, err = %v", got, err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}

func serveCompactFrames(conn net.Conn) error {
	reader := bufio.NewReader(conn)
	for i := 0; i < 2; i++ {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return err
		}
		var req transportjsonrpc.Request
		if err := json.Unmarshal(line, &req); err != nil {
			return err
		}
		if req.Method != protocol.MethodCompact || req.ID == 0 || req.Params != nil {
			return fmt.Errorf("unexpected compact request: %+v", req)
		}
		response := transportjsonrpc.Response{JSONRPC: "2.0", ID: req.ID}
		if i == 0 {
			notification := transportjsonrpc.Notification{JSONRPC: "2.0", Method: protocol.NotifyCompactState, Params: protocol.CompactState{SessionID: "session", CompactID: "manual-123", State: "skipped"}}
			frame, err := json.Marshal(notification)
			if err != nil {
				return err
			}
			if err := relayCompactFrame(conn, append(frame, '\n')); err != nil {
				return err
			}
			response.Result = protocol.CompactAccepted{CompactID: "manual-123"}
		} else {
			response.Error = &transportjsonrpc.Error{Code: -32000, Message: "busy"}
		}
		frame, err := json.Marshal(response)
		if err != nil {
			return err
		}
		if err := relayCompactFrame(conn, append(frame, '\n')); err != nil {
			return err
		}
	}
	return nil
}

func relayCompactFrame(conn net.Conn, frame []byte) error {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	writeDone := make(chan error, 1)
	go func() {
		_, err := server.Write(frame)
		writeDone <- err
	}()
	got, err := bufio.NewReader(client).ReadBytes('\n')
	if err != nil {
		return err
	}
	if err := <-writeDone; err != nil {
		return err
	}
	_, err = conn.Write(got)
	return err
}
