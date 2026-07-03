package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/daemon"
	"github.com/alanchenchen/suna/internal/protocol"
	"github.com/alanchenchen/suna/internal/transport/stdio"
)

func runRuntime(args []string) {
	fs := flag.NewFlagSet("suna runtime", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	transport := fs.String("transport", "", "runtime transport: stdio")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, "Usage: suna runtime --transport stdio\n")
	}
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if *transport == "" {
		fs.Usage()
		os.Exit(2)
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "runtime: unexpected argument %q\n", fs.Arg(0))
		fs.Usage()
		os.Exit(2)
	}
	if *transport != "stdio" {
		fmt.Fprintf(os.Stderr, "runtime: unsupported transport %q\n", *transport)
		os.Exit(2)
	}

	cfg := loadOrCreateConfig(config.DefaultConfigPath())
	if err := cfg.EnsureDataDirs(); err != nil {
		fmt.Fprintf(os.Stderr, "runtime: mkdir error: %s\n", err)
		os.Exit(1)
	}
	initLogging(cfg.DataDir)

	transports := []protocol.Transport{stdio.New(os.Stdin, os.Stdout)}
	// runtime 是前台 headless 进程，不写 sunad.pid；PID 文件只属于后台 local daemon 管理语义。
	d, err := daemon.New(cfg, transports, daemon.Options{RegisterPID: false})
	if err != nil {
		fmt.Fprintf(os.Stderr, "runtime: create error: %s\n", err)
		os.Exit(1)
	}
	if err := d.RunAs("runtime"); err != nil {
		fmt.Fprintf(os.Stderr, "runtime: %s\n", err)
		os.Exit(1)
	}
}
