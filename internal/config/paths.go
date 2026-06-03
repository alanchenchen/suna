package config

import (
	"os"
	"path/filepath"
)

// AppDirName 是 Suna 默认数据目录名；所有默认运行态路径都从 DefaultDataDir 派生。
const AppDirName = ".suna"

func DefaultDataDir() string {
	homeDir, _ := os.UserHomeDir()
	if homeDir == "" {
		return AppDirName
	}
	return filepath.Join(homeDir, AppDirName)
}

func DataDirConfigPath(dataDir string) string      { return filepath.Join(dataDir, "config.toml") }
func DataDirCredentialsPath(dataDir string) string { return filepath.Join(dataDir, "credentials.toml") }
func DataDirLogsDir(dataDir string) string         { return filepath.Join(dataDir, "logs") }
func DataDirLogPath(dataDir string) string         { return filepath.Join(DataDirLogsDir(dataDir), "app.log") }
func DataDirSkillsDir(dataDir string) string       { return filepath.Join(dataDir, "skills") }
func DataDirDBPath(dataDir string) string          { return filepath.Join(dataDir, "memory.db") }
func DataDirPIDPath(dataDir string) string         { return filepath.Join(dataDir, "sunad.pid") }
func DataDirSocketPath(dataDir string) string      { return filepath.Join(dataDir, "sunad.sock") }
func DataDirAttachmentsDir(dataDir string) string  { return filepath.Join(dataDir, "attachments") }

func DefaultConfigPath() string      { return DataDirConfigPath(DefaultDataDir()) }
func DefaultCredentialsPath() string { return DataDirCredentialsPath(DefaultDataDir()) }
func DefaultLogsDir() string         { return DataDirLogsDir(DefaultDataDir()) }
func DefaultLogPath() string         { return DataDirLogPath(DefaultDataDir()) }
func DefaultSkillsDir() string       { return DataDirSkillsDir(DefaultDataDir()) }
func DefaultDBPath() string          { return DataDirDBPath(DefaultDataDir()) }
func DefaultPIDPath() string         { return DataDirPIDPath(DefaultDataDir()) }
func DefaultSocketPath() string      { return DataDirSocketPath(DefaultDataDir()) }
func DefaultAttachmentsDir() string  { return DataDirAttachmentsDir(DefaultDataDir()) }

func (c *Config) DBPath() string          { return DataDirDBPath(c.DataDir) }
func (c *Config) ConfigPath() string      { return DataDirConfigPath(c.DataDir) }
func (c *Config) CredentialsPath() string { return DataDirCredentialsPath(c.DataDir) }
func (c *Config) LogsDir() string         { return DataDirLogsDir(c.DataDir) }
func (c *Config) LogPath() string         { return DataDirLogPath(c.DataDir) }
func (c *Config) SkillsDir() string       { return DataDirSkillsDir(c.DataDir) }
func (c *Config) PIDPath() string         { return DataDirPIDPath(c.DataDir) }
func (c *Config) SocketPath() string      { return DataDirSocketPath(c.DataDir) }
func (c *Config) AttachmentsDir() string  { return DataDirAttachmentsDir(c.DataDir) }
