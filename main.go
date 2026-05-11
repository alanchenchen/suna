package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/daemon"
	"github.com/alanchenchen/suna/internal/tui"
)

func main() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	configPath := filepath.Join(homeDir, ".suna", "config.toml")

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "daemon", "--serve":
			runDaemon(configPath)
			return
		case "stop":
			stopDaemonCommand()
			return
		case "status":
			showStatus()
			return
		}
	}

	runTUI(configPath)
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

func runTUI(configPath string) {
	locale := tui.LocaleEN
	var lp struct {
		UI struct {
			Locale string `toml:"locale"`
		} `toml:"ui"`
	}
	if _, err := toml.DecodeFile(configPath, &lp); err == nil {
		if lp.UI.Locale == "zh" || lp.UI.Locale == "zh-CN" {
			locale = tui.LocaleZH
		}
	}

	app := tui.New(configPath, locale)

	ensureDaemonRunning()

	client := tui.NewIPCClient()
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

	cmd := exec.Command(exe, "--serve")
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

	fmt.Fprintf(os.Stderr, "Error: daemon failed to start within 10 seconds (check logs at ~/.suna/logs/suna.log)\n")
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
	logPath := filepath.Join(dataDir, "logs", "suna.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	log.SetOutput(logFile)
	log.SetFlags(log.Ltime | log.Lmicroseconds)
}
