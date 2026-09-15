package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// EnsureDataDirs 必须建齐结构目录，且附件目录为 owner-only：
// 它保存用户上传的图片与 MCP 回传的二进制，不应让同机其他用户读取。
func TestEnsureDataDirsCreatesAttachmentsOwnerOnly(t *testing.T) {
	dataDir := t.TempDir()
	cfg := &Config{DataDir: dataDir}

	if err := cfg.EnsureDataDirs(); err != nil {
		t.Fatalf("EnsureDataDirs() error = %v", err)
	}

	for _, dir := range []string{
		dataDir,
		cfg.LogsDir(),
		cfg.SkillsDir(),
		cfg.ThemesDir(),
		cfg.AttachmentsDir(),
	} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("stat %s: %v", dir, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s is not a directory", dir)
		}
	}

	// Windows 的 FileMode.Perm 不表示 ACL，不能用 POSIX mode 数值判断目录私有性。
	if runtime.GOOS != "windows" {
		info, err := os.Stat(cfg.AttachmentsDir())
		if err != nil {
			t.Fatalf("stat attachments dir: %v", err)
		}
		if got := info.Mode().Perm(); got != 0o700 {
			t.Fatalf("attachments dir perm = %o, want 700", got)
		}
	}

	// 幂等：重复调用不应报错（daemon 重启、多次初始化都会走到这里）。
	if err := cfg.EnsureDataDirs(); err != nil {
		t.Fatalf("EnsureDataDirs() must be idempotent, got %v", err)
	}
}

// 已存在的目录权限必须被修正：MkdirAll 对已存在目录不生效，
// 老用户升级后 attachments 会保留旧权限，因此需要显式 Chmod。
func TestEnsureDataDirsCorrectsExistingPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 的 FileMode.Perm 不表示 ACL")
	}
	dataDir := t.TempDir()
	cfg := &Config{DataDir: dataDir}

	// 模拟老用户：attachments 已存在且权限过宽。
	if err := os.MkdirAll(cfg.AttachmentsDir(), 0o755); err != nil {
		t.Fatalf("prepare attachments dir: %v", err)
	}
	if err := os.Chmod(cfg.AttachmentsDir(), 0o755); err != nil {
		t.Fatalf("prepare attachments perm: %v", err)
	}

	if err := cfg.EnsureDataDirs(); err != nil {
		t.Fatalf("EnsureDataDirs() error = %v", err)
	}

	info, err := os.Stat(cfg.AttachmentsDir())
	if err != nil {
		t.Fatalf("stat attachments dir: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("attachments dir perm = %o, want 700 (existing dir must be corrected)", got)
	}
}

// update 目录是临时缓存（用前清、用后删），不应在启动时预建。
func TestEnsureDataDirsDoesNotCreateUpdateCache(t *testing.T) {
	dataDir := t.TempDir()
	cfg := &Config{DataDir: dataDir}

	if err := cfg.EnsureDataDirs(); err != nil {
		t.Fatalf("EnsureDataDirs() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(dataDir, "update")); !os.IsNotExist(err) {
		t.Fatalf("update cache must not be pre-created, stat err = %v", err)
	}
}
