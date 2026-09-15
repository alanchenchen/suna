package config

import (
	"fmt"
	"os"
	"path/filepath"
)

func (c *Config) EnsureDataDir() error {
	dirs := []string{c.DataDir, c.SkillsDir(), c.LogsDir(), c.ThemesDir()}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("create dir %s: %w", d, err)
		}
	}
	return nil
}

// WriteThemesReadme 在主题目录写入模板说明（仅当文件不存在时）。
// 用 README 而不是示例 .toml：示例会被当成有效主题加载并出现在选择列表里。
func (c *Config) WriteThemesReadme(content string) error {
	if err := os.MkdirAll(c.ThemesDir(), 0755); err != nil {
		return err
	}
	path := filepath.Join(c.ThemesDir(), "README.md")
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	return os.WriteFile(path, []byte(content), 0644)
}

func (c *Config) EnsureDataDirs() error {
	for _, d := range []string{c.DataDir, c.LogsDir(), c.SkillsDir(), c.ThemesDir()} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return err
		}
	}
	return nil
}
