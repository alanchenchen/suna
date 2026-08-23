package jsonrpc

import (
	"context"
	"encoding/json"
	"testing"
)

type captureConn struct{ sent []byte }

func (c *captureConn) Send(_ context.Context, msg []byte) error {
	c.sent = append([]byte(nil), msg...)
	return nil
}
func (*captureConn) Receive() ([]byte, error) { return nil, context.Canceled }
func (*captureConn) Close() error             { return nil }
func (*captureConn) ID() string               { return "test" }

type nilDataError struct{}

func (nilDataError) Error() string { return "invalid" }
func (nilDataError) Code() int     { return -32602 }
func (nilDataError) Data() any     { return nil }

func TestCodedErrorWithNilDataUsesProtocolDefault(t *testing.T) {
	conn := &captureConn{}
	sendProtocolError(conn, 1, nilDataError{})
	var response Response
	if err := json.Unmarshal(conn.sent, &response); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	data, ok := response.Error.Data.(map[string]any)
	if response.Error.Code != -32602 || !ok || data["kind"] != "invalid_request" {
		t.Fatalf("response error = %#v", response.Error)
	}
}
