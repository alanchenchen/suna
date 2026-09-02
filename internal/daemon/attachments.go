package daemon

import (
	"context"
	"fmt"
	"strings"

	"github.com/alanchenchen/suna/internal/agent"
	"github.com/alanchenchen/suna/internal/media"
	"github.com/alanchenchen/suna/internal/model"
	"github.com/alanchenchen/suna/internal/protocol"
)

func (s *service) agentInputFromParams(ctx context.Context, ag *agent.Agent, params protocol.SendMessageParams) (agent.Input, error) {
	if ag == nil {
		return agent.Input{}, fmt.Errorf("session agent is not loaded")
	}
	store := ag.MediaStore()
	blocks := make([]model.ContentBlock, 0, len(params.Parts))
	stored := make([]model.ContentBlock, 0, len(params.Parts))
	for _, part := range params.Parts {
		if ctx.Err() != nil {
			return agent.Input{}, ctx.Err()
		}
		switch part.Type {
		case "text":
			blocks = append(blocks, model.ContentBlock{Type: model.ContentText, Text: part.Text})
			stored = append(stored, model.ContentBlock{Type: model.ContentText, Text: part.Text})
		case "image":
			block, summary, err := normalizeImageAttachment(store, part.Source)
			if err != nil {
				return agent.Input{}, err
			}
			blocks = append(blocks, block)
			stored = append(stored, model.ContentBlock{Type: model.ContentText, Text: summary})
		}
	}
	return agent.Input{Blocks: blocks, StoredBlocks: stored}, nil
}

func normalizeImageAttachment(store *media.Store, ref protocol.AttachmentRef) (model.ContentBlock, string, error) {
	mediaRef := model.MediaRef{Kind: model.MediaKind(ref.Kind), Path: ref.Path, URL: ref.URL, MimeType: ref.MimeType, Name: ref.Name, Size: ref.Size}
	validated, err := store.ValidateImage(mediaRef)
	if err != nil {
		return model.ContentBlock{}, "", err
	}
	summary := attachmentSummary("image", validated.Name, validated.MimeType, validated.Size, string(validated.Kind), validated.Path, validated.URL)
	return model.ContentBlock{Type: model.ContentImage, Media: &validated}, summary, nil
}

// attachmentSummary 生成历史对话中的媒体引用摘要。source 内联在摘要里，
// 模型可提取 source 后通过 read_image 读回原图；格式未来可扩展视频等媒体类型。
func attachmentSummary(kind, name, mimeType string, size int64, sourceKind, path, url string) string {
	parts := []string{fmt.Sprintf("[%s: %s", kind, name)}
	if mimeType != "" {
		parts = append(parts, mimeType)
	}
	if size > 0 {
		parts = append(parts, media.FormatSize(size))
	}
	// source 是 read_image 的可读回引用：attachment 类型用文件名，path/url 类型用原始位置。
	switch sourceKind {
	case "attachment":
		if name != "" {
			parts = append(parts, "source=attachment:"+name)
		}
	case "path":
		if path != "" {
			parts = append(parts, "source="+path)
		}
	case "url":
		if url != "" {
			parts = append(parts, "source="+url)
		}
	}
	return strings.Join(parts, ", ") + "]"
}
