package daemon

import (
	"strings"
	"unicode/utf8"

	"github.com/alanchenchen/suna/internal/protocol"
)

const autoTitleMaxRunes = 80

func autoTitleFromParts(parts []protocol.MessagePart) string {
	for _, part := range parts {
		if part.Type != "text" {
			continue
		}
		text := strings.TrimSpace(part.Text)
		if text == "" {
			continue
		}
		if index := strings.IndexAny(text, "\r\n"); index >= 0 {
			text = text[:index]
		}
		return truncateTitle(strings.TrimSpace(text))
	}
	for _, part := range parts {
		if part.Type != "image" {
			continue
		}
		name := strings.TrimSpace(part.Source.Name)
		if name != "" {
			return truncateTitle("Inspect image: " + name)
		}
		if url := strings.TrimSpace(part.Source.URL); url != "" {
			return truncateTitle("Inspect image: " + url)
		}
		return "Inspect image"
	}
	return ""
}

func truncateTitle(title string) string {
	if utf8.RuneCountInString(title) <= autoTitleMaxRunes {
		return title
	}
	runes := []rune(title)
	return string(runes[:autoTitleMaxRunes])
}
