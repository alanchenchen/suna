package guard

import (
	"os"
	"path/filepath"
	"strings"
)

func assessFileWriteRisk(path string) RiskLevel {
	if isHighRiskFilePath(path) {
		return RiskHigh
	}
	// 新建文件也可能引入可执行行为（hook、CI、profile、包脚本）。
	// 因此文件写入默认保持 medium，除非用户显式 allow。
	return RiskMedium
}

func isHighRiskFilePath(path string) bool {
	if path == "" {
		return false
	}
	if sensitive, _ := IsSensitivePath(path); sensitive {
		return true
	}
	expanded := expandPathForCheck(path)
	abs, err := filepath.Abs(expanded)
	if err == nil {
		expanded = abs
	}
	normalized := strings.ToLower(filepath.ToSlash(expanded))
	base := strings.ToLower(filepath.Base(normalized))
	home, _ := os.UserHomeDir()
	home = strings.ToLower(filepath.ToSlash(home))

	if isSystemWritePath(normalized) || isUserStartupOrProfilePath(normalized, home) {
		return true
	}
	if strings.Contains(normalized, "/.git/hooks/") || strings.Contains(normalized, "/.github/workflows/") || strings.Contains(normalized, "/.gitlab-ci.yml") {
		return true
	}
	return isHighImpactConfigFile(base)
}

func isSystemWritePath(normalized string) bool {
	return strings.HasPrefix(normalized, "/etc/") ||
		strings.HasPrefix(normalized, "/usr/") ||
		strings.HasPrefix(normalized, "/system/") ||
		strings.HasPrefix(normalized, "/library/launchagents/") ||
		strings.HasPrefix(normalized, "/library/launchdaemons/") ||
		strings.HasPrefix(normalized, "c:/windows/") ||
		strings.HasPrefix(normalized, "c:/program files/") ||
		strings.HasPrefix(normalized, "c:/program files (x86)/") ||
		strings.HasPrefix(normalized, "c:/programdata/microsoft/windows/start menu/programs/startup/")
}

func isUserStartupOrProfilePath(normalized string, home string) bool {
	if home == "" {
		return false
	}
	for _, suffix := range []string{"/.bashrc", "/.bash_profile", "/.zshrc", "/.zprofile", "/.profile", "/.config/powershell/", "/documents/windowspowershell/", "/library/launchagents/"} {
		if normalized == home+suffix || strings.HasPrefix(normalized, home+suffix) {
			return true
		}
	}
	return false
}

func isHighImpactConfigFile(base string) bool {
	switch base {
	case "package.json", "package-lock.json", "pnpmfile.cjs", ".npmrc", "makefile", "dockerfile", "docker-compose.yml", "docker-compose.yaml", "pyproject.toml", "setup.py", "tox.ini", "crontab":
		return true
	default:
		return false
	}
}
