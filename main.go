package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/BurntSushi/toml"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/core"
	"github.com/alanchenchen/suna/internal/i18n"
	"github.com/alanchenchen/suna/internal/tui"
)

func main() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}

	configPath := filepath.Join(homeDir, ".suna", "config.toml")

	locale := i18n.EN
	type localeOnly struct {
		Locale string `toml:"locale"`
	}
	var lo localeOnly
	if _, err := toml.DecodeFile(configPath, &lo); err == nil {
		if lo.Locale == "zh" || lo.Locale == "zh-CN" {
			locale = i18n.ZH
		}
	}

	app := tui.New(configPath, locale)

	// 尝试加载已有配置，跳过 setup wizard
	if !config.NeedsSetup(configPath) {
		cfg, err := config.Load(configPath)
		if err == nil {
			cfg.DataDir = filepath.Join(homeDir, ".suna")
			config.LoadCredentials(cfg.DataDir)
			if err := cfg.ValidateAPIKeys(); err == nil {
				agent, agentErr := core.NewAgent(cfg)
				if agentErr == nil {
					app.SetAgent(agent, cfg)
				}
			}
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	if err := app.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}
