package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// SaveCredential 按 provider 保存密钥；同一 provider 下的多模型共享同一个 API key。
func SaveCredential(dataDir, provider, apiKey string) error {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return err
	}
	creds, err := readCredentials(dataDir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if creds == nil {
		creds = credentialsFile{}
	}
	creds[provider] = struct {
		APIKey string `toml:"api_key"`
	}{APIKey: apiKey}
	return writeCredentials(dataDir, creds)
}

// DeleteCredential 删除指定 provider 的凭证；文件不存在或 provider 不存在时直接视为成功。
func DeleteCredential(dataDir, provider string) error {
	creds, err := readCredentials(dataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if _, ok := creds[provider]; !ok {
		return nil
	}
	delete(creds, provider)
	return writeCredentials(dataDir, creds)
}

// LoadCredentials 将 credentials.toml 中的密钥注入到对应 ModelConfig；解析错误会直接返回。
func LoadCredentials(cfg *Config) error {
	creds, err := readCredentials(cfg.DataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for i := range cfg.Models {
		if c, ok := creds[cfg.Models[i].Provider]; ok {
			cfg.Models[i].APIKey = c.APIKey
		}
	}
	return nil
}

func readCredentials(dataDir string) (credentialsFile, error) {
	credPath := filepath.Join(dataDir, "credentials.toml")
	var creds credentialsFile
	if _, err := toml.DecodeFile(credPath, &creds); err != nil {
		return nil, err
	}
	return creds, nil
}

func writeCredentials(dataDir string, creds credentialsFile) error {
	var buf strings.Builder
	keys := make([]string, 0, len(creds))
	for k := range creds {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, ref := range keys {
		buf.WriteString(fmt.Sprintf("[%q]\n", ref))
		buf.WriteString(fmt.Sprintf("api_key = %q\n\n", creds[ref].APIKey))
	}
	return os.WriteFile(filepath.Join(dataDir, "credentials.toml"), []byte(buf.String()), 0600)
}

func (c *Config) ValidateAPIKeys() error {
	seen := map[string]bool{}
	for _, mc := range c.Models {
		if seen[mc.Provider] {
			continue
		}
		seen[mc.Provider] = true
		if mc.APIKey == "" {
			return fmt.Errorf("provider %q: missing api_key in credentials.toml", mc.Provider)
		}
	}
	return nil
}
