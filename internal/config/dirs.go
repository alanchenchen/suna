package config

import (
	"fmt"
	"os"
)

func (c *Config) EnsureDataDir() error {
	dirs := []string{c.DataDir, c.SkillsDir(), c.LogsDir()}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("create dir %s: %w", d, err)
		}
	}
	return nil
}

func (c *Config) EnsureDataDirs() error {
	for _, d := range []string{c.DataDir, c.LogsDir(), c.SkillsDir()} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return err
		}
	}
	return nil
}
