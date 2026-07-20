package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/alanchenchen/suna/internal/protocol"
	transporttcp "github.com/alanchenchen/suna/internal/transport/tcp"
)

const (
	tcpListenEnv        = "SUNA_TCP_LISTEN"
	tcpDefaultListenEnv = "SUNA_TCP_DEFAULT_LISTEN"
)

type serveResult struct {
	Status      string `json:"status"`
	PID         int    `json:"pid"`
	TCPEndpoint string `json:"tcp_endpoint"`
}

type serveDeps struct {
	queryStatus func(context.Context) (protocol.DaemonStatusParams, error)
	start       func(listen string, defaultListen bool) error
}

func defaultServeDeps() serveDeps {
	return serveDeps{queryStatus: queryDaemonStatus, start: startDaemonWithTCP}
}

func runServe(args []string) {
	result, err := serve(args, defaultServeDeps())
	if err != nil {
		fmt.Fprintf(os.Stderr, "serve: %s\n", err)
		os.Exit(1)
	}
	if hasServeJSONFlag(args) {
		data, err := json.Marshal(result)
		if err != nil {
			fmt.Fprintf(os.Stderr, "serve: encode result: %s\n", err)
			os.Exit(1)
		}
		fmt.Println(string(data))
		return
	}
	fmt.Printf("Suna daemon is ready (pid %d, TCP %s)\n", result.PID, result.TCPEndpoint)
}

func serve(args []string, deps serveDeps) (serveResult, error) {
	fs := flag.NewFlagSet("suna serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	listen := fs.String("listen", transporttcp.DefaultEndpoint, "TCP listen address")
	_ = fs.Bool("json", false, "output machine-readable JSON")
	if err := fs.Parse(args); err != nil {
		return serveResult{}, err
	}
	if fs.NArg() != 0 {
		return serveResult{}, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	endpoint := strings.TrimSpace(*listen)
	if err := transporttcp.ValidateEndpoint(endpoint); err != nil {
		return serveResult{}, err
	}
	if deps.queryStatus == nil || deps.start == nil {
		return serveResult{}, fmt.Errorf("serve dependencies are not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	status, err := deps.queryStatus(ctx)
	cancel()
	if err != nil {
		if err := deps.start(endpoint, endpoint == transporttcp.DefaultEndpoint); err != nil {
			return serveResult{}, err
		}
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		status, err = deps.queryStatus(ctx)
		cancel()
		if err != nil {
			return serveResult{}, fmt.Errorf("daemon started but is not reachable: %w", err)
		}
	}
	if status.TCPEndpoint == "" {
		return serveResult{}, fmt.Errorf("the running daemon has no TCP endpoint; stop it before starting the current server")
	}
	if endpoint != transporttcp.DefaultEndpoint && !sameTCPEndpoint(status.TCPEndpoint, endpoint) {
		return serveResult{}, fmt.Errorf("Suna daemon is already listening on %s; stop it before using --listen %s", status.TCPEndpoint, endpoint)
	}
	return serveResult{Status: "ready", PID: status.PID, TCPEndpoint: status.TCPEndpoint}, nil
}

func hasServeJSONFlag(args []string) bool {
	for _, arg := range args {
		if arg == "--json" {
			return true
		}
	}
	return false
}

func sameTCPEndpoint(left, right string) bool {
	leftAddr, leftErr := net.ResolveTCPAddr("tcp", left)
	rightAddr, rightErr := net.ResolveTCPAddr("tcp", right)
	if leftErr != nil || rightErr != nil {
		return left == right
	}
	return leftAddr.Port == rightAddr.Port && leftAddr.IP.Equal(rightAddr.IP)
}
