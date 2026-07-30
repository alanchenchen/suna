package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EffectiveCWD 统一解析工具实际使用的工作目录，避免 Guard 与执行端采用不同基准。
func EffectiveCWD(ctx context.Context, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		if execCtx, ok := ExecutionContextFrom(ctx); ok && strings.TrimSpace(execCtx.CWD) != "" {
			return cleanAbsolutePath(execCtx.CWD)
		}
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("get working directory: %w", err)
		}
		return filepath.Clean(cwd), nil
	}
	if strings.HasPrefix(requested, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("get home directory: %w", err)
		}
		requested = filepath.Join(home, requested[2:])
	}
	if filepath.IsAbs(requested) {
		return filepath.Clean(requested), nil
	}
	base, err := EffectiveCWD(ctx, "")
	if err != nil {
		return "", err
	}
	return filepath.Clean(filepath.Join(base, requested)), nil
}

func cleanAbsolutePath(path string) (string, error) {
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	return filepath.Clean(absolute), nil
}
