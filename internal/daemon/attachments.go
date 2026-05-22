package daemon

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/alanchenchen/suna/internal/agent"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/protocol"
)

const maxImageAttachmentBytes = 10 * 1024 * 1024

func agentInputFromParams(ctx context.Context, params protocol.SendMessageParams) (agent.Input, error) {
	blocks := make([]model.ContentBlock, 0, len(params.Parts))
	stored := make([]model.ContentBlock, 0, len(params.Parts))
	for _, part := range params.Parts {
		switch part.Type {
		case "text":
			blocks = append(blocks, model.ContentBlock{Type: model.ContentText, Text: part.Text})
			stored = append(stored, model.ContentBlock{Type: model.ContentText, Text: part.Text})
		case "image":
			block, summary, err := normalizeImageAttachment(ctx, part.Source)
			if err != nil {
				return agent.Input{}, err
			}
			blocks = append(blocks, block)
			stored = append(stored, model.ContentBlock{Type: model.ContentText, Text: summary})
		}
	}
	return agent.Input{Blocks: blocks, StoredBlocks: stored}, nil
}

func normalizeImageAttachment(ctx context.Context, ref protocol.AttachmentRef) (model.ContentBlock, string, error) {
	if ctx.Err() != nil {
		return model.ContentBlock{}, "", ctx.Err()
	}
	switch ref.Kind {
	case "path":
		return imageBlockFromPath(ref)
	case "url":
		return imageBlockFromURL(ref)
	default:
		return model.ContentBlock{}, "", fmt.Errorf("unsupported attachment source: %s", ref.Kind)
	}
}

func imageBlockFromPath(ref protocol.AttachmentRef) (model.ContentBlock, string, error) {
	path := expandLocalPath(ref.Path)
	if path == "" {
		return model.ContentBlock{}, "", fmt.Errorf("image path is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return model.ContentBlock{}, "", fmt.Errorf("stat image: %w", err)
	}
	if info.IsDir() {
		return model.ContentBlock{}, "", fmt.Errorf("image path is a directory: %s", path)
	}
	if info.Size() > maxImageAttachmentBytes {
		return model.ContentBlock{}, "", fmt.Errorf("image too large: %d bytes (max %d)", info.Size(), maxImageAttachmentBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return model.ContentBlock{}, "", fmt.Errorf("read image: %w", err)
	}
	mimeType := ref.MimeType
	if mimeType == "" {
		mimeType = imageMimeFromName(path)
	}
	if !strings.HasPrefix(mimeType, "image/") {
		return model.ContentBlock{}, "", fmt.Errorf("unsupported image type: %s", mimeType)
	}
	name := ref.Name
	if name == "" {
		name = filepath.Base(path)
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	cleanupPastedTempImage(path)
	summary := attachmentSummary("image", name, mimeType, info.Size(), "path")
	return model.ContentBlock{Type: model.ContentImage, MediaB64: encoded, MimeType: mimeType}, summary, nil
}

func imageBlockFromURL(ref protocol.AttachmentRef) (model.ContentBlock, string, error) {
	u, err := url.Parse(ref.URL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return model.ContentBlock{}, "", fmt.Errorf("invalid image URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return model.ContentBlock{}, "", fmt.Errorf("unsupported image URL scheme: %s", u.Scheme)
	}
	mimeType := ref.MimeType
	if mimeType == "" {
		mimeType = imageMimeFromName(u.Path)
	}
	if !strings.HasPrefix(mimeType, "image/") {
		return model.ContentBlock{}, "", fmt.Errorf("unsupported image URL type: %s", mimeType)
	}
	name := ref.Name
	if name == "" {
		name = filepath.Base(u.Path)
	}
	if name == "." || name == "/" || name == "" {
		name = "remote-image"
	}
	summary := attachmentSummary("image", name, mimeType, ref.Size, "url")
	return model.ContentBlock{Type: model.ContentImage, MediaURL: ref.URL, MimeType: mimeType}, summary, nil
}

func attachmentSummary(kind, name, mimeType string, size int64, sourceKind string) string {
	parts := []string{fmt.Sprintf("[uploaded %s: %s", kind, name)}
	if mimeType != "" {
		parts = append(parts, mimeType)
	}
	if size > 0 {
		parts = append(parts, formatBytes(size))
	}
	if sourceKind != "" {
		parts = append(parts, "source="+sourceKind)
	}
	return strings.Join(parts, ", ") + "]"
}

func expandLocalPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		if home != "" {
			return filepath.Join(home, path[2:])
		}
	}
	abs, err := filepath.Abs(path)
	if err == nil {
		return abs
	}
	return path
}

func cleanupPastedTempImage(path string) {
	home, _ := os.UserHomeDir()
	if home == "" {
		return
	}
	tmpDir := filepath.Join(home, ".suna", "tmp")
	cleanPath := filepath.Clean(path)
	cleanTmp := filepath.Clean(tmpDir)
	// 只清理 TUI 为 data:image 粘贴创建的临时文件，绝不能删除用户手动选择的普通 path。
	if filepath.Dir(cleanPath) != cleanTmp || !strings.HasPrefix(filepath.Base(cleanPath), "paste-") {
		return
	}
	_ = os.Remove(cleanPath)
}

func imageMimeFromName(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	default:
		return ""
	}
}

func formatBytes(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%dB", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1fKB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1fMB", float64(n)/(1024*1024))
}
