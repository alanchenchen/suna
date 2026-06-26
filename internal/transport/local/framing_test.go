package local

import (
	"bufio"
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFrameRoundTripSkipsEmptyLines(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	go func() {
		_, _ = client.Write([]byte("\n{\"jsonrpc\":\"2.0\"}\n"))
	}()

	got, err := receiveFrame(bufio.NewReader(server))
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"jsonrpc":"2.0"}`; string(got) != want {
		t.Fatalf("frame = %q, want %q", got, want)
	}
}

func TestSendFrameHonorsContextDeadline(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	var mu sync.Mutex
	if err := sendFrame(ctx, &mu, client, []byte(strings.Repeat("x", 1<<20))); err == nil {
		t.Fatal("sendFrame error = nil, want deadline error")
	}
}

func BenchmarkReceiveFrameLargeLine(b *testing.B) {
	payload := []byte(strings.Repeat("x", 64*1024) + "\n")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		server, client := net.Pipe()
		done := make(chan struct{})
		go func() {
			_, _ = client.Write(payload)
			_ = client.Close()
			close(done)
		}()
		got, err := receiveFrame(bufio.NewReader(server))
		if err != nil {
			b.Fatal(err)
		}
		if len(got) != len(payload)-1 {
			b.Fatalf("len(got) = %d, want %d", len(got), len(payload)-1)
		}
		_ = server.Close()
		<-done
	}
}
