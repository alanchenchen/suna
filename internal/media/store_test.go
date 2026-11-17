package media

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/alanchenchen/suna/internal/model"
)

func TestResolveReportsMissingLocalSourcesWithoutWeakeningValidation(t *testing.T) {
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	store := NewStore(root)
	for _, kind := range []model.MediaKind{model.MediaPath, model.MediaAttachment} {
		t.Run(string(kind), func(t *testing.T) {
			ref := model.MediaRef{Kind: kind, Path: filepath.Join(root, "missing.png"), Name: "missing.png"}
			resolved, err := store.Resolve(context.Background(), ref, model.ResolveAsBase64)
			if err != nil || resolved != (model.ResolvedMedia{Missing: true}) {
				t.Fatalf("resolved=%+v error=%v, want only Missing and no error", resolved, err)
			}
			// 工具入口仍拒绝不存在的来源；Missing 只描述发送时的实际解析结果。
			if _, err := store.ValidateImage(ref); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("ValidateImage error=%v, want original missing file error", err)
			}
		})
	}
	for _, tt := range []struct {
		name string
		ref  model.MediaRef
	}{
		{name: "attachment outside root", ref: model.MediaRef{Kind: model.MediaAttachment, Path: filepath.Join(filepath.Dir(root), "outside.png")}},
		{name: "attachment without root", ref: model.MediaRef{Kind: model.MediaAttachment, Path: filepath.Join(root, "missing.png")}},
		{name: "empty local source", ref: model.MediaRef{Kind: model.MediaPath}},
		{name: "directory", ref: model.MediaRef{Kind: model.MediaPath, Path: root}},
		{name: "invalid remote URL", ref: model.MediaRef{Kind: model.MediaURL, URL: "invalid"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			selected := store
			if tt.name == "attachment without root" {
				selected = NewStore("")
			}
			resolved, err := selected.Resolve(context.Background(), tt.ref, model.ResolveAsBase64)
			if err == nil || resolved.Missing {
				t.Fatalf("resolved=%+v error=%v, want unmodified validation failure", resolved, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	resolved, err := store.Resolve(ctx, model.MediaRef{Kind: model.MediaPath, Path: filepath.Join(root, "missing.png")}, model.ResolveAsBase64)
	if !errors.Is(err, context.Canceled) || resolved.Missing {
		t.Fatalf("resolved=%+v error=%v, want cancellation before missing source handling", resolved, err)
	}
}

func TestImageReadFailurePreservesOtherCausesAndCancellation(t *testing.T) {
	// 覆盖校验之后 ReadFile 才发现文件被删除的分类路径，不依赖不确定的文件竞争。
	cause := fmt.Errorf("read image: %w", &fs.PathError{Op: "open", Path: "resolved.png", Err: fs.ErrNotExist})
	for _, tt := range []struct {
		name    string
		kind    model.MediaKind
		err     error
		cancel  bool
		missing bool
	}{
		{name: "missing path", kind: model.MediaPath, err: cause, missing: true},
		{name: "missing attachment", kind: model.MediaAttachment, err: cause, missing: true},
		{name: "remote failure", kind: model.MediaURL, err: cause},
		{name: "permission", kind: model.MediaPath, err: fs.ErrPermission},
		{name: "cancelled error", kind: model.MediaPath, err: context.Canceled},
		{name: "deadline", kind: model.MediaPath, err: context.DeadlineExceeded},
		{name: "unknown", kind: model.MediaPath, err: fmt.Errorf("unknown read failure")},
		{name: "cancel takes precedence over deletion", kind: model.MediaPath, err: cause, cancel: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.cancel {
				cancel()
			}
			resolved, err := imageResolveFailure(ctx, tt.kind, tt.err)
			if tt.missing {
				if err != nil || resolved != (model.ResolvedMedia{Missing: true}) {
					t.Fatalf("resolved=%+v error=%v, want missing", resolved, err)
				}
				return
			}
			wantErr := tt.err
			if tt.cancel {
				wantErr = context.Canceled
			}
			if resolved.Missing || !errors.Is(err, wantErr) {
				t.Fatalf("resolved=%+v error=%v, want original failure %v", resolved, err, wantErr)
			}
		})
	}
}

func TestResolveKeepsAvailableSourcesAndRemoteURLs(t *testing.T) {
	file := filepath.Join(t.TempDir(), "image.png")
	if err := os.WriteFile(file, []byte("image"), 0600); err != nil {
		t.Fatal(err)
	}
	store := NewStore("")
	resolved, err := store.Resolve(context.Background(), model.MediaRef{Kind: model.MediaPath, Path: file}, model.ResolveAsBase64)
	if err != nil || resolved.Missing || resolved.Base64 == "" {
		t.Fatalf("resolved=%+v error=%v, want available local image", resolved, err)
	}
	for _, mode := range []model.ResolveMode{model.ResolveAsURL, model.ResolveAsBase64} {
		resolved, err := store.Resolve(context.Background(), model.MediaRef{Kind: model.MediaURL, URL: "https://example.com/image.png"}, mode)
		if err != nil || resolved.Missing || resolved.URL != "https://example.com/image.png" || resolved.Base64 != "" {
			t.Fatalf("mode=%s resolved=%+v error=%v, want unchanged URL without network probing", mode, resolved, err)
		}
	}
}

func TestResolveRejectsUnsupportedModeEvenWhenSourceMissing(t *testing.T) {
	ref := model.MediaRef{Kind: model.MediaPath, Path: filepath.Join(t.TempDir(), "missing.png")}
	for _, mode := range []model.ResolveMode{"unsupported", model.ResolveAsURL} {
		resolved, err := NewStore("").Resolve(context.Background(), ref, mode)
		if err == nil || resolved.Missing {
			t.Fatalf("mode=%s resolved=%+v error=%v, want mode failure, not Missing", mode, resolved, err)
		}
	}
}

func TestResolveMissingParentDirectoryDoesNotAcceptDanglingSymlinks(t *testing.T) {
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	store := NewStore(root)
	ref := model.MediaRef{Kind: model.MediaAttachment, Path: filepath.Join(root, "removed", "image.png")}
	resolved, err := store.Resolve(context.Background(), ref, model.ResolveAsBase64)
	if err != nil || !resolved.Missing {
		t.Fatalf("resolved=%+v error=%v, want Missing when attachment directory was cleaned", resolved, err)
	}
	link := filepath.Join(root, "link.png")
	if err := os.Symlink(filepath.Join(root, "absent.png"), link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	resolved, err = store.Resolve(context.Background(), model.MediaRef{Kind: model.MediaPath, Path: link}, model.ResolveAsBase64)
	if err == nil || resolved.Missing {
		t.Fatalf("resolved=%+v error=%v, want symlink rejection even if target is missing", resolved, err)
	}
}
