package mcptools

import "testing"

func TestPublicNameSanitizesBothParts(t *testing.T) {
	got := PublicName("my server", "read file")
	want := "mcp__my_server__read_file"
	if got != want {
		t.Fatalf("PublicName() = %q, want %q", got, want)
	}
}

func TestParsePublicNameValid(t *testing.T) {
	server, tool, ok := ParsePublicName("mcp__server-a__tool_b")
	if !ok {
		t.Fatal("ParsePublicName() ok = false, want true")
	}
	if server != "server-a" || tool != "tool_b" {
		t.Fatalf("ParsePublicName() = (%q, %q), want (server-a, tool_b)", server, tool)
	}
}

func TestParsePublicNameRejectsInvalid(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"missing prefix", "server__tool"},
		{"empty server", "mcp____tool"},
		{"empty tool", "mcp__server__"},
		{"no separator", "mcp__onlyone"},
		{"empty string", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, ok := ParsePublicName(tt.in); ok {
				t.Fatalf("ParsePublicName(%q) ok = true, want false", tt.in)
			}
		})
	}
}

func TestParsePublicNameKeepsInnerSeparators(t *testing.T) {
	// SplitN 只切第一个 "__"，tool 内部可含 "__"。
	server, tool, ok := ParsePublicName("mcp__server__a__b")
	if !ok {
		t.Fatal("ParsePublicName() ok = false, want true")
	}
	if server != "server" || tool != "a__b" {
		t.Fatalf("ParsePublicName() = (%q, %q), want (server, a__b)", server, tool)
	}
}

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"already safe", "my-tool_1", "my-tool_1"},
		{"spaces become underscore", "my tool", "my_tool"},
		{"unsafe chars replaced and trimmed", "read file!", "read_file"},
		{"trimmed", "  spaced  ", "spaced"},
		{"empty becomes unnamed", "", "unnamed"},
		{"unicode replaced and trimmed", "中文工具", "unnamed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeName(tt.in)
			if got != tt.want {
				t.Fatalf("sanitizeName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestAttachmentLabelBoundsAndSanitizes(t *testing.T) {
	short := attachmentLabel("server name")
	if short != "server_name" {
		t.Fatalf("attachmentLabel(short) = %q, want server_name", short)
	}
	long := attachmentLabel("a-very-long-server-name-that-exceeds-forty-eight-bytes-for-sure")
	if len(long) > 48 {
		t.Fatalf("attachmentLabel(long) length = %d, want <= 48", len(long))
	}
}

func TestExtFromMime(t *testing.T) {
	tests := []struct {
		mime string
		want string
	}{
		{"image/jpeg", ".jpg"},
		{"image/png", ".png"},
		{"image/webp", ".webp"},
		{"image/gif", ".gif"},
		{"application/pdf", ".pdf"},
		{"application/json", ".json"},
		{"text/css", ".css"},
		{"text/plain", ".txt"},
		{"application/octet-stream", ".bin"},
		{"", ".bin"},
	}
	for _, tt := range tests {
		t.Run(tt.mime, func(t *testing.T) {
			got := extFromMime(tt.mime)
			if got != tt.want {
				t.Fatalf("extFromMime(%q) = %q, want %q", tt.mime, got, tt.want)
			}
		})
	}
}
