package config

import (
	"fmt"
	"os"
	"path/filepath"
)

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

// EnsureDataDirs 创建 Suna 在数据目录下管理的结构目录。
//
// attachments 与其余目录权限不同：它保存用户上传的图片与 MCP 回传的二进制，
// 限制为 owner-only（0700），与 TUI 侧写入附件时使用的权限保持一致。
// update 是临时缓存（用前清、用后删），不在此预建。
func (c *Config) EnsureDataDirs() error {
	dirs := []struct {
		name string
		path string
		perm os.FileMode
	}{
		{"data", c.DataDir, 0755},
		{"logs", c.LogsDir(), 0755},
		{"skills", c.SkillsDir(), 0755},
		{"themes", c.ThemesDir(), 0755},
		{"attachments", c.AttachmentsDir(), 0700},
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d.path, d.perm); err != nil {
			return fmt.Errorf("create %s dir: %w", d.name, err)
		}
	}
	return nil
}
