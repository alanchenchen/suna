package local

import (
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

func TestClientCloseDoesNotClearReceiveLoopConnection(t *testing.T) {
	conn := newCloseRaceConn()
	client := &Client{conn: conn, pending: make(map[int]chan clientResult)}
	done := make(chan struct{})
	go func() {
		client.receiveLoop(conn)
		close(done)
	}()

	select {
	case <-conn.firstRead:
	case <-time.After(time.Second):
		t.Fatal("receiveLoop did not enter first Read")
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	close(conn.releaseFirst)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("receiveLoop did not exit after Close")
	}
}

type closeRaceConn struct {
	firstRead    chan struct{}
	releaseFirst chan struct{}
	firstOnce    sync.Once
	mu           sync.Mutex
	reads        int
}

func newCloseRaceConn() *closeRaceConn {
	return &closeRaceConn{firstRead: make(chan struct{}), releaseFirst: make(chan struct{})}
}

func (c *closeRaceConn) Read([]byte) (int, error) {
	c.mu.Lock()
	c.reads++
	read := c.reads
	c.mu.Unlock()
	if read == 1 {
		c.firstOnce.Do(func() { close(c.firstRead) })
		<-c.releaseFirst
		return 0, nil
	}
	return 0, io.EOF
}

func (*closeRaceConn) Write(p []byte) (int, error)      { return len(p), nil }
func (*closeRaceConn) Close() error                     { return nil }
func (*closeRaceConn) LocalAddr() net.Addr              { return testAddr("local") }
func (*closeRaceConn) RemoteAddr() net.Addr             { return testAddr("remote") }
func (*closeRaceConn) SetDeadline(time.Time) error      { return nil }
func (*closeRaceConn) SetReadDeadline(time.Time) error  { return nil }
func (*closeRaceConn) SetWriteDeadline(time.Time) error { return nil }

type testAddr string

func (a testAddr) Network() string { return string(a) }
func (a testAddr) String() string  { return string(a) }
