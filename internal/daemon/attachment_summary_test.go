package daemon

import (
	"strings"
	"testing"
)

func TestAttachmentSummaryIncludesReadableSource(t *testing.T) {
	tests := []struct {
		name       string
		kind       string
		sourceKind string
		path       string
		url        string
		want       string
	}{
		// attachment 的 source 使用文件名（落盘名，如 sha256-<hash>.png），由调用方传入 name。
		{name: "attachment", kind: "image", sourceKind: "attachment", path: "/att/sha256-abc.png", want: "source=attachment:sha256-abc.png"},
		{name: "path", kind: "image", sourceKind: "path", path: "/abs/photo.png", want: "source=/abs/photo.png"},
		{name: "url", kind: "image", sourceKind: "url", url: "https://example.com/a.png", want: "source=https://example.com/a.png"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// attachment 的 name 即落盘文件名；path/url 场景 name 只是展示名。
			name := "shot.png"
			if tt.sourceKind == "attachment" {
				name = "sha256-abc.png"
			}
			got := attachmentSummary(tt.kind, name, "image/png", 2048, tt.sourceKind, tt.path, tt.url)
			if !strings.Contains(got, "[image: "+name) {
				t.Fatalf("summary = %q, want [image: %s prefix", got, name)
			}
			if !strings.Contains(got, tt.want) {
				t.Fatalf("summary = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAttachmentSummaryOmitsEmptySource(t *testing.T) {
	got := attachmentSummary("image", "shot.png", "image/png", 10, "", "", "")
	if strings.Contains(got, "source=") {
		t.Fatalf("summary = %q, want no source for unknown kind", got)
	}
}
