package builtin

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/alanchenchen/suna/internal/guard"
	"github.com/alanchenchen/suna/internal/media"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/tools"
)

// ReadImage 让多模态模型按需读取图片：本地路径、http(s) URL 或历史附件引用。
// 执行本身是纯输入输出：返回文本结果 + 图片块（Result.Images），
// 图片注入 user 消息、run 结束后摘要化均由 Runner / Agent 层处理。
type ReadImage struct{}

func (ReadImage) Spec() tools.Spec {
	return builtinSpec("read_image", "Load an image so a multimodal model can see it. Source can be a local file path, an http(s) URL, or an attachment reference from conversation history (e.g. attachment:name.png). Historical [image: ...] summaries carry a source= value that can be passed here to re-read the original image. Only use this tool if you can see images.", tools.Perceive, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"source": map[string]any{"type": "string", "description": "Image source: local file path, http(s) URL, or attachment:<name> from history"},
		},
		"required": []string{"source"},
	})
}

func (ReadImage) Execute(ctx context.Context, params map[string]any) tools.Result {
	source, _ := params["source"].(string)
	source = strings.TrimSpace(source)
	if source == "" {
		return tools.ErrorResult("source is required")
	}
	store := imageStoreFromContext(ctx)
	ref, err := parseImageSource(ctx, store, source)
	if err != nil {
		return tools.ErrorResult(err.Error())
	}
	validated, err := store.ValidateImage(ref)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("invalid image source: %v", err))
	}
	summary := fmt.Sprintf("image loaded: %s (%s, %s)", validated.Name, validated.MimeType, media.FormatSize(validated.Size))
	return tools.Result{
		Content: summary,
		Images:  []model.ContentBlock{{Type: model.ContentImage, Media: &validated}},
	}
}

// imageStoreFromContext 优先使用工具执行上下文的 per-session 附件目录，回退到默认附件目录。
func imageStoreFromContext(ctx context.Context) *media.Store {
	root := ""
	if execCtx, ok := tools.ExecutionContextFrom(ctx); ok {
		root = execCtx.AttachmentDir
	}
	if root == "" {
		root = media.DefaultRoot()
	}
	return media.NewStore(root)
}

// parseImageSource 把 source 字符串解析为 MediaRef：http(s) URL、attachment:<name>、本地路径。
func parseImageSource(ctx context.Context, store *media.Store, source string) (model.MediaRef, error) {
	lower := strings.ToLower(source)
	switch {
	case strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://"):
		return model.MediaRef{Kind: model.MediaURL, URL: source}, nil
	case strings.HasPrefix(lower, "attachment:"):
		name := strings.TrimSpace(source[len("attachment:"):])
		if name == "" || strings.ContainsAny(name, "/\\") {
			return model.MediaRef{}, fmt.Errorf("attachment source must be a bare file name, got %q", source)
		}
		path := filepath.Join(store.Root, name)
		return model.MediaRef{Kind: model.MediaAttachment, Path: path, Name: name}, nil
	default:
		path := expandPathWithContext(ctx, source)
		if sensitive, reason := guard.IsSensitivePath(path); sensitive {
			return model.MediaRef{}, fmt.Errorf("blocked: sensitive file (%s). Reading credential/secret files is not allowed.", reason)
		}
		return model.MediaRef{Kind: model.MediaPath, Path: path, Name: filepath.Base(path)}, nil
	}
}
