package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/protocol"
	"github.com/alanchenchen/suna/internal/transport/local"
)

const (
	defaultDebugMemoryInterval = 5 * time.Second
	minDebugMemoryInterval     = time.Second
)

type debugMemoryDeps struct {
	connect func() (debugMemoryClient, error)
	now     func() time.Time
	mkdir   func(string, os.FileMode) error
	open    func(string, int, os.FileMode) (*os.File, error)
	out     io.Writer
}

type debugMemoryClient interface {
	Snapshot(context.Context, bool) (protocol.DebugMemoryResult, error)
	Close() error
}

type localDebugMemoryClient struct {
	client *local.Client
}

func (c localDebugMemoryClient) Snapshot(ctx context.Context, collectGC bool) (protocol.DebugMemoryResult, error) {
	var result protocol.DebugMemoryResult
	raw, err := c.client.InvokeRaw(ctx, protocol.MethodDebugMemory, protocol.DebugMemoryParams{GC: collectGC})
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return result, err
	}
	return result, nil
}

func (c localDebugMemoryClient) Close() error { return c.client.Close() }

func runDebug(args []string) {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		printDebugHelp()
		return
	}
	switch args[0] {
	case "memory":
		if err := debugMemory(args[1:], defaultDebugMemoryDeps()); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown debug command: %s\n\n", args[0])
		printDebugHelp()
		os.Exit(2)
	}
}

func printDebugHelp() {
	fmt.Print(`Suna debug commands

Usage:
  suna debug memory [--interval DURATION]

Commands:
  memory    Monitor a running daemon's Go memory and write a diagnostic report.

Notes:
  The daemon must already be running. The default interval is 5s; the minimum is 1s.
`)
}

func defaultDebugMemoryDeps() debugMemoryDeps {
	return debugMemoryDeps{
		connect: func() (debugMemoryClient, error) {
			client, err := local.DialDefault(time.Second)
			if err != nil {
				return nil, err
			}
			return localDebugMemoryClient{client: client}, nil
		},
		now:   time.Now,
		mkdir: os.MkdirAll,
		open:  os.OpenFile,
		out:   os.Stdout,
	}
}

func debugMemory(args []string, deps debugMemoryDeps) error {
	fs := flag.NewFlagSet("suna debug memory", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	intervalText := fs.String("interval", defaultDebugMemoryInterval.String(), "sampling interval")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected argument: %s", fs.Arg(0))
	}
	interval, err := time.ParseDuration(*intervalText)
	if err != nil {
		return fmt.Errorf("invalid --interval %q: %w", *intervalText, err)
	}
	if interval < minDebugMemoryInterval {
		return fmt.Errorf("--interval must be at least %s", minDebugMemoryInterval)
	}

	client, err := deps.connect()
	if err != nil {
		return errors.New("Suna daemon is not running; start Suna first with `suna` or `suna serve`")
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	baseline, err := client.Snapshot(ctx, false)
	cancel()
	if err != nil {
		return errors.New("Suna daemon is not running; start Suna first with `suna` or `suna serve`")
	}

	// 纳秒时间戳保证并发或连续启动的诊断会话不会写入同一份报告。
	dir := filepath.Join(config.DefaultDataDir(), "debug", "memory", deps.now().Format("20060102-150405.000000000"))
	if err := deps.mkdir(dir, 0755); err != nil {
		return fmt.Errorf("create report directory: %w", err)
	}
	samples, err := deps.open(filepath.Join(dir, "samples.ndjson"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open samples report: %w", err)
	}
	defer samples.Close()

	metadata := map[string]any{
		"started_at": deps.now(),
		"pid":        baseline.PID,
		"interval":   interval.String(),
	}
	if err := writeJSONFile(filepath.Join(dir, "metadata.json"), metadata); err != nil {
		return err
	}

	fmt.Fprintf(deps.out, "Suna daemon memory monitor\nPID: %d\nInterval: %s\nReport: %s\n\n", baseline.PID, interval, dir)
	if err := writeMemorySample(deps.out, samples, "baseline", baseline, 0); err != nil {
		return err
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	lastHeap := baseline.HeapAlloc
	for {
		select {
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			sample, err := client.Snapshot(ctx, false)
			cancel()
			if err != nil {
				return fmt.Errorf("read daemon memory: %w", err)
			}
			if err := writeMemorySample(deps.out, samples, "interval", sample, int64(sample.HeapAlloc)-int64(lastHeap)); err != nil {
				return err
			}
			lastHeap = sample.HeapAlloc
		case <-signals:
			fmt.Fprintln(deps.out, "\nStopping diagnostics: collecting final GC snapshot...")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			before, err := client.Snapshot(ctx, false)
			cancel()
			if err != nil {
				return fmt.Errorf("read final daemon memory: %w", err)
			}
			if err := writeMemorySample(deps.out, samples, "final_before_gc", before, int64(before.HeapAlloc)-int64(lastHeap)); err != nil {
				return err
			}
			ctx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
			after, err := client.Snapshot(ctx, true)
			cancel()
			if err != nil {
				return fmt.Errorf("collect daemon GC snapshot: %w", err)
			}
			if err := writeMemorySample(deps.out, samples, "final_after_gc", after, int64(after.HeapAlloc)-int64(before.HeapAlloc)); err != nil {
				return err
			}
			if err := writeMemoryReport(dir, baseline, before, after); err != nil {
				return err
			}
			fmt.Fprintf(deps.out, "Report saved: %s\n", dir)
			return nil
		}
	}
}

func writeMemorySample(out io.Writer, file io.Writer, reason string, sample protocol.DebugMemoryResult, delta int64) error {
	record := struct {
		Reason string `json:"reason"`
		protocol.DebugMemoryResult
	}{Reason: reason, DebugMemoryResult: sample}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		return err
	}
	deltaText := ""
	if delta != 0 {
		deltaText = fmt.Sprintf(" (%s)", formatBytesSigned(delta))
	}
	_, err = fmt.Fprintf(out, "%s %-16s heap=%-10s%s inuse=%-10s sys=%-10s gc=%-5d goroutines=%d\n", sample.Timestamp.Format("15:04:05"), reason, formatBytes(sample.HeapAlloc), deltaText, formatBytes(sample.HeapInuse), formatBytes(sample.Sys), sample.NumGC, sample.Goroutines)
	return err
}

func writeMemoryReport(dir string, baseline, before, after protocol.DebugMemoryResult) error {
	text := fmt.Sprintf("Suna daemon memory diagnostics\n\nBaseline HeapAlloc: %s\nBefore GC HeapAlloc: %s\nAfter GC HeapAlloc: %s\nHeapAlloc change after GC: %s\n\nSamples: samples.ndjson\n", formatBytes(baseline.HeapAlloc), formatBytes(before.HeapAlloc), formatBytes(after.HeapAlloc), formatBytesSigned(int64(after.HeapAlloc)-int64(before.HeapAlloc)))
	if err := os.WriteFile(filepath.Join(dir, "report.txt"), []byte(text), 0644); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}

func writeJSONFile(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}
	return nil
}

func formatBytes(value uint64) string {
	return fmt.Sprintf("%.1fMiB", float64(value)/(1024*1024))
}

func formatBytesSigned(value int64) string {
	sign := "+"
	if value < 0 {
		sign = "-"
		value = -value
	}
	return fmt.Sprintf("%s%.1fMiB", sign, float64(value)/(1024*1024))
}
