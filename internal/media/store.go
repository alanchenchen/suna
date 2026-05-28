package media

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/alanchenchen/suna/internal/config"
	"github.com/alanchenchen/suna/internal/model"
)

const MaxImageBytes int64 = 10 * 1024 * 1024

type Store struct {
	Root string
}

func NewStore(root string) *Store { return &Store{Root: root} }

func DefaultRoot() string {
	return config.DefaultAttachmentsDir()
}

func (s *Store) ValidateImage(ref model.MediaRef) (model.MediaRef, error) {
	switch ref.Kind {
	case model.MediaPath, model.MediaAttachment:
		return s.validateLocalImage(ref)
	case model.MediaURL:
		return validateURLImage(ref)
	default:
		return model.MediaRef{}, fmt.Errorf("unsupported image source: %s", ref.Kind)
	}
}

func (s *Store) Resolve(ctx context.Context, ref model.MediaRef, mode model.ResolveMode) (model.ResolvedMedia, error) {
	if ctx.Err() != nil {
		return model.ResolvedMedia{}, ctx.Err()
	}
	ref, err := s.ValidateImage(ref)
	if err != nil {
		return model.ResolvedMedia{}, err
	}
	if mode == model.ResolveAsURL {
		if ref.Kind != model.MediaURL {
			return model.ResolvedMedia{}, fmt.Errorf("image source %s cannot resolve as URL", ref.Kind)
		}
		return model.ResolvedMedia{URL: ref.URL, MimeType: ref.MimeType, Name: ref.Name, Size: ref.Size}, nil
	}
	if mode != model.ResolveAsBase64 {
		return model.ResolvedMedia{}, fmt.Errorf("unsupported resolve mode: %s", mode)
	}
	if ref.Kind == model.MediaURL {
		return model.ResolvedMedia{URL: ref.URL, MimeType: ref.MimeType, Name: ref.Name, Size: ref.Size}, nil
	}
	data, err := os.ReadFile(ref.Path)
	if err != nil {
		return model.ResolvedMedia{}, fmt.Errorf("read image: %w", err)
	}
	if int64(len(data)) > MaxImageBytes {
		return model.ResolvedMedia{}, fmt.Errorf("image too large: %d bytes (max %d)", len(data), MaxImageBytes)
	}
	return model.ResolvedMedia{Base64: base64.StdEncoding.EncodeToString(data), MimeType: ref.MimeType, Name: ref.Name, Size: int64(len(data))}, nil
}

func (s *Store) Usage() (int64, int, error) {
	if s.Root == "" {
		return 0, 0, nil
	}
	entries, err := os.ReadDir(s.Root)
	if os.IsNotExist(err) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	var total int64
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err == nil {
			total += info.Size()
			count++
		}
	}
	return total, count, nil
}

func (s *Store) Clear() (int64, int, error) {
	if s.Root == "" {
		return 0, 0, nil
	}
	entries, err := os.ReadDir(s.Root)
	if os.IsNotExist(err) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	var removedBytes int64
	removedCount := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(s.Root, entry.Name())
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if err := os.Remove(path); err == nil {
			removedBytes += info.Size()
			removedCount++
		}
	}
	return removedBytes, removedCount, nil
}

func (s *Store) validateLocalImage(ref model.MediaRef) (model.MediaRef, error) {
	path := expandPath(ref.Path)
	if path == "" {
		return model.MediaRef{}, fmt.Errorf("image path is required")
	}
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}
	if ref.Kind == model.MediaAttachment {
		if err := s.ensureAttachmentPath(path); err != nil {
			return model.MediaRef{}, err
		}
	}
	lstat, err := os.Lstat(path)
	if err != nil {
		return model.MediaRef{}, fmt.Errorf("stat image: %w", err)
	}
	if lstat.Mode()&os.ModeSymlink != 0 {
		return model.MediaRef{}, fmt.Errorf("image symlinks are not supported")
	}
	if !lstat.Mode().IsRegular() {
		return model.MediaRef{}, fmt.Errorf("image path is not a regular file: %s", path)
	}
	if ref.Kind == model.MediaAttachment {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return model.MediaRef{}, fmt.Errorf("resolve attachment path: %w", err)
		}
		if err := s.ensureAttachmentPath(resolved); err != nil {
			return model.MediaRef{}, err
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		return model.MediaRef{}, fmt.Errorf("stat image: %w", err)
	}
	if info.IsDir() {
		return model.MediaRef{}, fmt.Errorf("image path is a directory: %s", path)
	}
	if info.Size() > MaxImageBytes {
		return model.MediaRef{}, fmt.Errorf("image too large: %d bytes (max %d)", info.Size(), MaxImageBytes)
	}
	mimeType := mediaRefMime(ref.MimeType, path)
	if mimeType == "" {
		return model.MediaRef{}, fmt.Errorf("unsupported image type: %s", ref.MimeType)
	}
	ref.Path = path
	ref.MimeType = mimeType
	ref.Size = info.Size()
	if ref.Name == "" {
		ref.Name = filepath.Base(path)
	}
	return ref, nil
}

func (s *Store) ensureAttachmentPath(path string) error {
	root := s.Root
	if root == "" {
		return fmt.Errorf("attachments directory is unavailable")
	}
	rootAbs, err := filepath.Abs(root)
	if err == nil {
		root = rootAbs
	}
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return fmt.Errorf("attachment path is outside attachments directory")
	}
	return nil
}

func validateURLImage(ref model.MediaRef) (model.MediaRef, error) {
	u, err := url.Parse(ref.URL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return model.MediaRef{}, fmt.Errorf("invalid image URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return model.MediaRef{}, fmt.Errorf("unsupported image URL scheme: %s", u.Scheme)
	}
	mimeType := mediaRefMime(ref.MimeType, u.Path)
	if mimeType == "" {
		return model.MediaRef{}, fmt.Errorf("unsupported image URL type: %s", ref.MimeType)
	}
	if ref.Name == "" {
		ref.Name = filepath.Base(u.Path)
		if ref.Name == "." || ref.Name == "/" || ref.Name == "" {
			ref.Name = "remote-image"
		}
	}
	ref.MimeType = mimeType
	return ref, nil
}

func ImageMimeFromName(name string) string {
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

func ExtFromMime(mimeType string) string {
	switch mimeType {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".png"
	}
}

func mediaRefMime(mimeType, name string) string {
	// 对外部 path/url 优先信任文件名后缀；客户端传入的 MIME 只作为无后缀时的兜底，且仍需白名单。
	if fromName := ImageMimeFromName(name); fromName != "" {
		return fromName
	}
	if isAllowedImageMime(mimeType) {
		return mimeType
	}
	return ""
}

func isAllowedImageMime(mimeType string) bool {
	switch mimeType {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
		return true
	default:
		return false
	}
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		if home != "" {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
