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
	"github.com/alanchenchen/suna/internal/tui"
)

func main() {
	configPath := config.DefaultConfigPath()
	if os.Getenv("SUNA_RUN_DAEMON") == "1" {
		runDaemon(configPath)
		return
	}

	cmd := parseCLI(os.Args[1:])
	switch cmd {
	case "tui":
		runTUI()
	case "help":
		printHelp()
	case "start":
		startDaemonCommand()
	case "stop":
		stopDaemonCommand()
	case "status":
		showStatus()
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
	if err := fs.Parse(args); err != nil {
		return "help"
	}
	if *help || *helpShort {
		return "help"
	}
	if fs.NArg() == 0 {
		return "tui"
	}
	switch fs.Arg(0) {
	case "help":
		return "help"
	case "start":
		return "start"
	case "stop":
		return "stop"
	case "status":
		return "status"
	default:
		return fs.Arg(0)
	}
}

func printHelp() {
	fmt.Print(`Suna CLI

Usage:
  suna                 Open the TUI. Starts the daemon if needed.
  suna start           Start the daemon in the background.
  suna stop            Stop the running daemon.
  suna status          Show daemon status.
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

	initLogging(cfg.DataDir)

	transports := []protocol.Transport{local.NewPlatformTransport(local.DefaultEndpoint())}
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

	client := tui.NewLocalClient()
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
