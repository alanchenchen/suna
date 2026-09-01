package config

import "testing"

func TestModelConfigAPIKeyHint(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want string
	}{
		{"empty key returns empty", "", ""},
		{"whitespace key returns empty", "   ", ""},
		{"short key hides prefix", "abc12345", "••••2345"},
		{"sk prefix is kept", "sk-abcd1234efgh5678", "sk-••••••••5678"},
		{"plain long key keeps only tail", "abcd1234efgh5678", "••••••••5678"},
		{"long key masks at most 8 dots", "sk-verylongkeyvalue0123456789", "sk-••••••••6789"},
		{"exactly 8 chars keeps tail only", "12345678", "••••5678"},
		{"7 chars is short key", "1234567", "••••4567"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mc := ModelConfig{APIKey: tt.key}
			if got := mc.APIKeyHint(); got != tt.want {
				t.Fatalf("APIKeyHint() = %q, want %q", got, tt.want)
			}
		})
	}
}

// 提示长度必须远小于原 key，保证无法从提示还原凭据。
func TestModelConfigAPIKeyHintNeverRevealsFullKey(t *testing.T) {
	key := "sk-abcdefghijklmnopqrstuvwxyz0123456789"
	hint := ModelConfig{APIKey: key}.APIKeyHint()
	if hint == key {
		t.Fatal("hint equals full key")
	}
	// 提示最多含前缀(3) + 打码(8) + 尾段(4) = 15 字符（点号是多字节字符，按 rune 计）。
	if maxHintLen := 3 + 8 + 4; len([]rune(hint)) > maxHintLen {
		t.Fatalf("hint length = %d, want <= %d", len([]rune(hint)), maxHintLen)
	}
}
