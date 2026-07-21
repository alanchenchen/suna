package guard

import (
	"strings"
	"testing"
)

func TestIsSensitivePath(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		sensitive bool
	}{
		{name: "ssh private key", path: "~/.ssh/id_rsa", sensitive: true},
		{name: "env file", path: "~/.env", sensitive: true},
		{name: "credentials file", path: ".credentials", sensitive: true},
		{name: "regular config", path: "config.json", sensitive: false},
		{name: "suna config", path: "~/.suna/config.toml", sensitive: false},
		{name: "service account key", path: "service-account-key.json", sensitive: true},
		{name: "aws credentials", path: "~/.aws/credentials", sensitive: true},
		{name: "source file", path: "/home/user/project/main.go", sensitive: false},
		{name: "pem file", path: "/tmp/.pem", sensitive: true},
		{name: "netrc", path: "~/.netrc", sensitive: true},
		{name: "kube config", path: "~/.kube/config", sensitive: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got, reason := IsSensitivePath(tt.path)
			if got != tt.sensitive {
				t.Fatalf("IsSensitivePath(%q) = %v, want %v; reason = %q", tt.path, got, tt.sensitive, reason)
			}
		})
	}
}

func TestMaskSensitiveContent(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantParts  []string
		avoidParts []string
	}{
		{
			name:       "api key",
			input:      `key=sk-abc12345678901234567890`,
			wantParts:  []string{"REDACTED"},
			avoidParts: []string{"example-api-key"},
		},
		{
			name:       "password assignment",
			input:      `password=super_secret`,
			wantParts:  []string{"REDACTED"},
			avoidParts: []string{"super_secret"},
		},
		{
			name:       "bearer token",
			input:      `Authorization: Bearer eyJhbGciOi12345678901234567890`,
			wantParts:  []string{"REDACTED"},
			avoidParts: []string{"eyJhbGciOi"},
		},
		{
			name:       "aws key",
			input:      `aws_access_key_id = AKIAIOSFODNN7EXAMPLE`,
			wantParts:  []string{"REDACTED"},
			avoidParts: []string{"AKIAIOSFODNN7"},
		},
		{
			name:       "url with credentials",
			input:      `database=https://admin:password123@api.example.com`,
			wantParts:  []string{"REDACTED", "api.example.com"},
			avoidParts: []string{"admin:password123"},
		},
		{
			name:       "private key block",
			input:      "-----BEGIN PRIVATE KEY-----\nabc123\n-----END PRIVATE KEY-----",
			wantParts:  []string{"REDACTED"},
			avoidParts: []string{"abc123"},
		},
		{
			name:       "normal text",
			input:      "Hello, this is normal text with numbers 12345 and paths /usr/local/bin",
			wantParts:  []string{"Hello, this is normal text", "/usr/local/bin"},
			avoidParts: nil,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := MaskSensitiveContent(tt.input)
			for _, want := range tt.wantParts {
				if !strings.Contains(got, want) {
					t.Fatalf("MaskSensitiveContent(%q) = %q, want substring %q", tt.input, got, want)
				}
			}
			for _, avoid := range tt.avoidParts {
				if strings.Contains(got, avoid) {
					t.Fatalf("MaskSensitiveContent(%q) = %q, should not contain %q", tt.input, got, avoid)
				}
			}
		})
	}
}
