package guard_test

import (
	"fmt"
	"testing"

	"github.com/alanchenchen/suna/internal/guard"
)

func TestIsSensitivePath(t *testing.T) {
	tests := []struct {
		path      string
		sensitive bool
	}{
		{"~/.ssh/id_rsa", true},
		{"~/.env", true},
		{".credentials", true},
		{"config.json", false},
		{"~/.suna/config.toml", false},
		{"service-account-key.json", true},
		{"~/.aws/credentials", true},
		{"/home/user/project/main.go", false},
		{"/tmp/.pem", true},
		{"~/.netrc", true},
		{"~/.kube/config", true},
	}
	for _, tt := range tests {
		sensitive, reason := guard.IsSensitivePath(tt.path)
		if sensitive != tt.sensitive {
			t.Errorf("IsSensitivePath(%q) = %v, want %v (reason: %s)", tt.path, sensitive, tt.sensitive, reason)
		}
	}
}

func TestMaskSensitiveContent(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
		bad   []string
	}{
		{
			name:  "api key",
			input: `key=sk-abc123def456ghi789jkl012mno345pqr`,
			want:  []string{"REDACTED"},
			bad:   []string{"sk-abc123"},
		},
		{
			name:  "password assignment",
			input: `password = "super_secret_value_here"`,
			want:  []string{"REDACTED"},
			bad:   []string{"super_secret"},
		},
		{
			name:  "bearer token",
			input: `Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.verylongtoken1234567890`,
			want:  []string{"REDACTED"},
			bad:   []string{"eyJhbGciOi"},
		},
		{
			name:  "aws key",
			input: `aws_access_key_id = AKIAIOSFODNN7EXAMPLE`,
			want:  []string{"REDACTED"},
			bad:   []string{"AKIAIOSFODNN7"},
		},
		{
			name:  "url with credentials",
			input: `database=https://admin:password123@api.example.com`,
			want:  []string{"REDACTED"},
			bad:   []string{"admin:password123"},
		},
		{
			name:  "private key block",
			input: "-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA...",
			want:  []string{"REDACTED"},
			bad:   []string{"MIIEpAIBAAKCAQEA"},
		},
		{
			name:  "safe content preserved",
			input: "Hello, this is normal text with numbers 12345 and paths /usr/local/bin",
			want:  []string{"Hello, this is normal text", "/usr/local/bin"},
			bad:   []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := guard.MaskSensitiveContent(tt.input)
			fmt.Printf("  %s: %s\n", tt.name, result)
			for _, w := range tt.want {
				if !contains(result, w) {
					t.Errorf("expected result to contain %q, got: %s", w, result)
				}
			}
			for _, b := range tt.bad {
				if contains(result, b) {
					t.Errorf("result should NOT contain %q, got: %s", b, result)
				}
			}
		})
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
