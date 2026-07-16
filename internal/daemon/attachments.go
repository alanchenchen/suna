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
	summary := attachmentSummary("image", validated.Name, validated.MimeType, validated.Size, string(validated.Kind))
	return model.ContentBlock{Type: model.ContentImage, Media: &validated}, summary, nil
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

func formatBytes(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%dB", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1fKB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1fMB", float64(n)/(1024*1024))
}
