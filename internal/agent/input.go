package agent

import (
	"strings"

	"github.com/alanchenchen/suna/internal/model"
)

type Input struct {
	Blocks []model.ContentBlock
}

func TextInput(text string) Input {
	return Input{Blocks: []model.ContentBlock{{Type: model.ContentText, Text: text}}}
}

func (in Input) Message(role model.Role) model.Message {
	blocks := normalizeInputBlocks(in.Blocks)
	return model.Message{
		Role:        role,
		TextContent: textFromBlocks(blocks),
		Content:     blocks,
	}
}

func (in Input) Text() string {
	return textFromBlocks(normalizeInputBlocks(in.Blocks))
}

func normalizeInputBlocks(blocks []model.ContentBlock) []model.ContentBlock {
	out := make([]model.ContentBlock, 0, len(blocks))
	for _, b := range blocks {
		switch b.Type {
		case model.ContentText:
			if strings.TrimSpace(b.Text) == "" {
				continue
			}
		case model.ContentImage, model.ContentAudio:
			if b.MediaURL == "" && b.MediaB64 == "" {
				continue
			}
		default:
			continue
		}
		out = append(out, b)
	}
	return out
}

func textFromBlocks(blocks []model.ContentBlock) string {
	parts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		if b.Type == model.ContentText && strings.TrimSpace(b.Text) != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}
