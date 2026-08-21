package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeModelsValidatesAuthModeByProtocol(t *testing.T) {
	tests := []struct {
		name     string
		protocol ModelProtocol
		authMode AuthMode
		wantErr  bool
	}{
		{name: "anthropic default", protocol: ModelProtocolAnthropic},
		{name: "anthropic bearer", protocol: ModelProtocolAnthropic, authMode: AuthModeBearer},
		{name: "anthropic both", protocol: ModelProtocolAnthropic, authMode: AuthModeBoth},
		{name: "unknown mode", protocol: ModelProtocolAnthropic, authMode: "unknown", wantErr: true},
		{name: "openai bearer", protocol: ModelProtocolOpenAIChat, authMode: AuthModeBearer, wantErr: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{Models: []ModelConfig{{Provider: "provider-a", Protocol: tt.protocol, AuthMode: tt.authMode, Model: "model"}}}
			err := cfg.NormalizeModels()
			if (err != nil) != tt.wantErr {
				t.Fatalf("NormalizeModels() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSaveOmitsDefaultAuthModeAndPreservesExplicitMode(t *testing.T) {
	tests := []struct {
		name     string
		authMode AuthMode
		want     string
		unwant   string
	}{
		{name: "default omitted", unwant: "auth_mode"},
		{name: "bearer preserved", authMode: AuthModeBearer, want: `auth_mode = "bearer"`},
		{name: "both preserved", authMode: AuthModeBoth, want: `auth_mode = "both"`},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.toml")
			cfg := Config{
				ActiveModel: "provider-a/model",
				DataDir:     dir,
				Models: []ModelConfig{{
					Provider:        "provider-a",
					Protocol:        ModelProtocolAnthropic,
					AuthMode:        tt.authMode,
					Model:           "model",
					BaseURL:         "https://api.example.com",
					ContextWindow:   128000,
					MaxOutputTokens: 8192,
				}},
			}
			if err := cfg.Save(path); err != nil {
				t.Fatalf("Save() error = %v", err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile() error = %v", err)
			}
			text := string(data)
			if tt.want != "" && !strings.Contains(text, tt.want) {
				t.Fatalf("config = %q, want %q", text, tt.want)
			}
			if tt.unwant != "" && strings.Contains(text, tt.unwant) {
				t.Fatalf("config = %q, should not contain %q", text, tt.unwant)
			}
		})
	}
}
