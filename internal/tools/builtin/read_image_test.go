package builtin

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/tools"
)

func TestReadImageLoadsLocalPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(path, []byte("fake-png-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := tools.WithExecutionContext(context.Background(), tools.ExecutionContext{CWD: dir})
	result := (ReadImage{}).Execute(ctx, map[string]any{"source": "shot.png"})
	if result.IsError {
		t.Fatalf("Execute() error = %s", result.Error)
	}
	if len(result.Images) != 1 {
		t.Fatalf("Images len = %d, want 1", len(result.Images))
	}
	img := result.Images[0]
	if img.Type != model.ContentImage || img.Media == nil {
		t.Fatalf("Images[0] = %+v, want image block with media", img)
	}
	if img.Media.Kind != model.MediaPath || img.Media.Path == "" {
		t.Fatalf("Media = %+v, want resolved local path", img.Media)
	}
}

func TestReadImageLoadsAttachmentRef(t *testing.T) {
	// t.TempDir 在 macOS 上返回 /var/folders（/private/var 的符号链接），
	// ValidateImage 的 EvalSymlinks 会把附件路径解析为真实路径，与 store.Root 不一致；
	// 生产环境附件目录（~/.suna/attachments）无符号链接，这里规范化后对齐。
	dir := t.TempDir()
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	if err := os.WriteFile(filepath.Join(dir, "sha256-abc.png"), []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := tools.WithExecutionContext(context.Background(), tools.ExecutionContext{AttachmentDir: dir})
	result := (ReadImage{}).Execute(ctx, map[string]any{"source": "attachment:sha256-abc.png"})
	if result.IsError {
		t.Fatalf("Execute() error = %s", result.Error)
	}
	if len(result.Images) != 1 {
		t.Fatalf("Images len = %d, want 1", len(result.Images))
	}
	if got, want := result.Images[0].Media.Kind, model.MediaAttachment; got != want {
		t.Fatalf("Media.Kind = %s, want %s", got, want)
	}
}

func TestReadImageRejectsSensitivePath(t *testing.T) {
	result := (ReadImage{}).Execute(context.Background(), map[string]any{"source": "~/.ssh/id_ed25519.png"})
	if !result.IsError {
		t.Fatal("Execute() on sensitive path should error")
	}
}

func TestReadImageRejectsAttachmentPathTraversal(t *testing.T) {
	dir := t.TempDir()
	ctx := tools.WithExecutionContext(context.Background(), tools.ExecutionContext{AttachmentDir: dir})
	result := (ReadImage{}).Execute(ctx, map[string]any{"source": "attachment:../outside.png"})
	if !result.IsError {
		t.Fatal("Execute() with path traversal should error")
	}
}

func TestReadImageRequiresSource(t *testing.T) {
	result := (ReadImage{}).Execute(context.Background(), nil)
	if !result.IsError {
		t.Fatal("Execute() without source should error")
	}
}
