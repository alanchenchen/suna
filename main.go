package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/daemon"
	"github.com/alanchenchen/suna/internal/logging"
	"github.com/alanchenchen/suna/internal/tui"
)

func main() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	configPath := filepath.Join(homeDir, ".suna", "config.toml")
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
		if daemon.IsRunning() {
			pid, _ := daemon.ReadPID()
			fmt.Printf("sunad is already running (pid %d)\n", pid)
			return
		}
		startDaemon()
		fmt.Println("sunad started")
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

	d, err := daemon.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sunad: create error: %s\n", err)
		os.Exit(1)
	}

	if err := d.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "sunad: %s\n", err)
		os.Exit(1)
	}
}

func showStatus() {
	if !daemon.IsRunning() {
		fmt.Println("sunad is not running")
		return
	}
	pid, _ := daemon.ReadPID()
	fmt.Printf("sunad is running (pid %d)\n", pid)
}

func stopDaemonCommand() {
	if !daemon.IsRunning() {
		fmt.Println("sunad is not running")
		return
	}
	if err := daemon.StopRunning(); err != nil {
		fmt.Fprintf(os.Stderr, "Error stopping daemon: %s\n", err)
		os.Exit(1)
	}
	fmt.Println("sunad stopped")
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

func ensureDaemonRunning() {
	if daemon.IsRunning() {
		return
	}
	startDaemon()
}

func startDaemon() {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot determine executable path: %s\n", err)
		os.Exit(1)
	}

	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), "SUNA_RUN_DAEMON=1")
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = os.Stderr

	if err := daemon.StartBackground(cmd); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to start daemon: %s\n", err)
		os.Exit(1)
	}

	for i := 0; i < 50; i++ {
		time.Sleep(200 * time.Millisecond)
		if daemon.IsRunning() {
			return
		}
	}

	fmt.Fprintf(os.Stderr, "Error: daemon failed to start within 10 seconds (check logs at ~/.suna/logs/app.log)\n")
	os.Exit(1)
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
	homeDir, _ := os.UserHomeDir()
	return &config.Config{
		DataDir: filepath.Join(homeDir, ".suna"),
		UI:      config.UIConfig{Locale: "en", Theme: "auto"},
	}
}

func initLogging(dataDir string) {
	logging.Init(dataDir)
	logging.Info("app", "daemon_start", logging.Event{"data_dir": dataDir})
}
