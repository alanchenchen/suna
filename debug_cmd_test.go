package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alanchenchen/suna/internal/protocol"
)

func TestDebugMemoryRejectsTooSmallInterval(t *testing.T) {
	err := debugMemory([]string{"--interval", "500ms"}, debugMemoryDeps{})
	if err == nil {
		t.Fatal("debugMemory error = nil, want interval validation error")
	}
}

type unavailableDebugMemoryClient struct{}

func (unavailableDebugMemoryClient) Snapshot(context.Context, bool) (protocol.DebugMemoryResult, error) {
	return protocol.DebugMemoryResult{}, os.ErrNotExist
}

func (unavailableDebugMemoryClient) Close() error { return nil }

func TestDebugMemoryReportsDaemonUnavailable(t *testing.T) {
	err := debugMemory(nil, debugMemoryDeps{
		connect: func() (debugMemoryClient, error) { return unavailableDebugMemoryClient{}, nil },
		now:     time.Now,
	})
	if err == nil {
		t.Fatal("debugMemory error = nil, want daemon unavailable error")
	}
}

func TestWriteMemorySampleIncludesReasonAndMetrics(t *testing.T) {
	var terminal bytes.Buffer
	var samples bytes.Buffer
	sample := protocol.DebugMemoryResult{
		Timestamp:  time.Date(2026, 3, 12, 15, 30, 45, 0, time.UTC),
		HeapAlloc:  64 * 1024 * 1024,
		HeapInuse:  72 * 1024 * 1024,
		Sys:        128 * 1024 * 1024,
		NumGC:      12,
		Goroutines: 31,
	}
	if err := writeMemorySample(&terminal, &samples, "baseline", sample, 0); err != nil {
		t.Fatalf("writeMemorySample error = %v", err)
	}
	if got := terminal.String(); got == "" || !bytes.Contains([]byte(got), []byte("heap=64.0MiB")) {
		t.Fatalf("terminal output = %q, want readable heap stats", got)
	}
	if got := samples.String(); !bytes.Contains([]byte(got), []byte(`"reason":"baseline"`)) {
		t.Fatalf("sample output = %q, want baseline reason", got)
	}
}

func TestWriteMemoryReportWritesSummary(t *testing.T) {
	dir := t.TempDir()
	before := protocol.DebugMemoryResult{HeapAlloc: 96 * 1024 * 1024}
	after := protocol.DebugMemoryResult{HeapAlloc: 64 * 1024 * 1024}
	if err := writeMemoryReport(dir, protocol.DebugMemoryResult{}, before, after); err != nil {
		t.Fatalf("writeMemoryReport error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "report.txt"))
	if err != nil {
		t.Fatalf("ReadFile report error = %v", err)
	}
	if !bytes.Contains(data, []byte("After GC HeapAlloc: 64.0MiB")) {
		t.Fatalf("report = %q, want after GC summary", data)
	}
}
