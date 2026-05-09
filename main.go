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

/*
suna 入口

单二进制多模式（01-architecture.md）：

	suna              # 自动: daemon 未运行 → 后台启动 → 连接 → 进入 TUI
	suna              # 自动: daemon 已运行 → 直接连接 → 进入 TUI
	suna daemon       # 前台启动 daemon (给 systemd/launchd 用)
	suna stop         # 发送 SIGTERM 给 daemon
	suna status       # 查看 daemon 状态

实现方式：suna 启动时 exec.Command(os.Args[0], "--serve") 后台拉起自身作为 daemon。
*/
func main() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	sunaDir := filepath.Join(homeDir, ".suna")
	logFile, err := os.OpenFile(filepath.Join(sunaDir, "logs", "suna.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		log.SetOutput(logFile)
		log.SetFlags(log.Ltime | log.Lmicroseconds)
		defer logFile.Close()
	}

	configPath := filepath.Join(homeDir, ".suna", "config.toml")

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "daemon":
			runDaemon(configPath)
			return
		case "--serve":
			runDaemon(configPath)
			return
		case "stop":
			stopDaemon()
			return
		case "status":
			showStatus()
			return
		}
	}

	runTUI(configPath)
}

// runDaemon 前台启动 daemon
func runDaemon(configPath string) {
	cfg, err := loadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sunad: config error: %s\n", err)
		os.Exit(1)
	}

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

// showStatus 显示 daemon 状态
func showStatus() {
	if !daemon.IsRunning() {
		fmt.Println("sunad is not running")
		return
	}
	pid, _ := daemon.ReadPID()
	fmt.Printf("sunad is running (pid %d)\n", pid)
}

/*
runTUI 自动检测/启动 daemon → 连接 IPC → 启动 TUI

流程（01-architecture.md Daemon 生命周期）：
 1. 检查 daemon 是否运行
 2. 未运行 → 后台启动 daemon
 3. 等待 socket 就绪
 4. 连接 IPC
 5. 启动 TUI
*/
func runTUI(configPath string) {
	locale := tui.LocaleEN
	type localeProbe struct {
		Locale string `toml:"locale"`
	}
	var lp localeProbe
	if _, err := toml.DecodeFile(configPath, &lp); err == nil {
		if lp.Locale == "zh" || lp.Locale == "zh-CN" {
			locale = tui.LocaleZH
		}
	}

	app := tui.New(configPath, locale)

	if !config.NeedsSetup(configPath) {
		if _, err := loadConfig(configPath); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			os.Exit(1)
		}

		ensureDaemonRunning()

		client := tui.NewIPCClient()
		if err := client.Connect(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			os.Exit(1)
		}

		app.Connect(client)

		if err := app.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			os.Exit(1)
		}
		client.Close()
		return
	}

	// 首次使用：进入 setup wizard
	if err := app.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

// ensureDaemonRunning 确保 daemon 正在运行，未运行则后台启动
func ensureDaemonRunning() {
	if daemon.IsRunning() {
		return
	}

	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot determine executable path: %s\n", err)
		os.Exit(1)
	}

	cmd := exec.Command(exe, "--serve")
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = os.Stderr

	if err := startBackgroundDaemon(cmd); err != nil {
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

func loadConfig(configPath string) (*config.Config, error) {
	if config.NeedsSetup(configPath) {
		return nil, fmt.Errorf("config not found: %s\nPlease run 'suna' first to set up", configPath)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	if err := cfg.ValidateAPIKeys(); err != nil {
		return nil, err
	}
	return cfg, nil
}
