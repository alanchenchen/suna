package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/daemon"
	"github.com/alanchenchen/suna/internal/logging"
	"github.com/alanchenchen/suna/internal/protocol"
	"github.com/alanchenchen/suna/internal/transport/local"
	transporttcp "github.com/alanchenchen/suna/internal/transport/tcp"
	"github.com/alanchenchen/suna/internal/tui"
	tuitransport "github.com/alanchenchen/suna/internal/tui/transport"
	"github.com/alanchenchen/suna/internal/version"
)

func main() {
	configPath := config.DefaultConfigPath()
	cmd := parseCLI(os.Args[1:])
	// 版本查询必须无副作用；即使从 daemon 启动的工具环境继承内部标记，也不能误入 daemon 模式。
	if cmd == "version" {
		printVersion()
		return
	}
	if os.Getenv("SUNA_RUN_DAEMON") == "1" {
		runDaemon(configPath)
		return
	}

	switch cmd {
	case "tui":
		runTUI()
	case "serve":
		runServe(os.Args[2:])
	case "debug":
		runDebug(os.Args[2:])
	case "help":
		printHelp()
	case "stop":
		stopDaemonCommand()
	case "status":
		showStatus()
	case "update":
		updateCommand(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", cmd)
		printHelp()
		os.Exit(2)
	}
}

func parseCLI(args []string) string {
	fs := flag.NewFlagSet("suna", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	help := fs.Bool("help", false, "show help")
	helpShort := fs.Bool("h", false, "show help")
	showVersion := fs.Bool("version", false, "show version")
	showVersionShort := fs.Bool("V", false, "show version")
	if err := fs.Parse(args); err != nil {
		return "help"
	}
	if *showVersion || *showVersionShort {
		return "version"
	}
	if *help || *helpShort {
		return "help"
	}
	if fs.NArg() == 0 {
		return "tui"
	}
	switch fs.Arg(0) {
	case "version":
		return "version"
	case "help":
		return "help"
	case "stop":
		return "stop"
	case "status":
		return "status"
	case "update":
		return "update"
	case "serve":
		return "serve"
	case "debug":
		return "debug"
	default:
		return fs.Arg(0)
	}
}

func printVersion() {
	fmt.Printf("suna %s\n", version.Current())
}

func printHelp() {
	fmt.Print(`Suna CLI

Usage:
  suna                 Open the TUI. Starts the daemon if needed.
  suna version         Show the CLI version.
  suna --version       Show the CLI version.
  suna -V              Show the CLI version.
  suna stop            Stop the running daemon.
  suna status          Show daemon status.
  suna update          Check for updates, show release notes, and ask before installing.
  suna serve [--listen ADDRESS] [--json]
                        Ensure the headless daemon is ready for TCP clients.
  suna debug memory [--interval DURATION]
                        Monitor daemon memory and write a local diagnostic report.
  suna help            Show this help.

Notes:
  Logs:   ~/.suna/logs/
  Config: ~/.suna/config.toml
  Data:   ~/.suna/
`)
}

func runDaemon(configPath string) {
	cfg := loadOrCreateConfig(configPath)
	if err := cfg.EnsureDataDirs(); err != nil {
		fmt.Fprintf(os.Stderr, "sunad: mkdir error: %s\n", err)
		os.Exit(1)
	}

	lease, err := acquireDaemonLease(cfg.LockPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "sunad: %s\n", err)
		return
	}
	defer lease.Close()

	initLogging(cfg.DataDir)

	listen := os.Getenv(tcpListenEnv)
	var tcpTransport *transporttcp.Transport
	if listen == "" || os.Getenv(tcpDefaultListenEnv) == "1" {
		tcpTransport = transporttcp.NewDefault()
	} else {
		tcpTransport = transporttcp.New(listen)
	}
	transports := []protocol.Transport{
		local.NewPlatformTransport(local.DefaultEndpoint()),
		tcpTransport,
	}
	d, err := daemon.New(cfg, transports)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sunad: create error: %s\n", err)
		os.Exit(1)
	}

	if err := d.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "sunad: %s\n", err)
		os.Exit(1)
	}
}

func runTUI() {
	app := tui.New(tui.LocaleEN)

	ensureDaemonRunning()

	client := tuitransport.NewClient()
	if err := client.Connect(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot connect to daemon: %s\n", err)
		os.Exit(1)
	}

	app.Connect(client)

	if err := app.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
	client.Close()
}

func loadOrCreateConfig(configPath string) *config.Config {
	if !config.NeedsSetup(configPath) {
		cfg, err := config.Load(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "sunad: config error: %s\n", err)
			os.Exit(1)
		}
		return cfg
	}
	return &config.Config{
		DataDir: config.DefaultDataDir(),
		UI:      config.UIConfig{Locale: "en", Theme: "auto"},
	}
}

func initLogging(dataDir string) {
	logging.Init(dataDir)
	logging.Info("app", "daemon_start", logging.Event{"data_dir": dataDir})
}
