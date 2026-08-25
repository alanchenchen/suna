package config

import "testing"

func TestValidateProviderFormAllowsReadableIDsButRejectsSlash(t *testing.T) {
	labels := ProviderValidationLabels{Required: "required", InvalidProvider: "invalid provider", InvalidProtocol: "invalid protocol", EndpointRequired: "endpoint required", InvalidEndpoint: "invalid endpoint", InvalidContextWindow: "invalid context", InvalidMaxOutputTokens: "invalid output"}
	for _, tt := range []struct {
		provider string
		wantErr  bool
	}{
		{provider: "provider-name"},
		{provider: "Provider Name"},
		{provider: "Provider–Name"},
		{provider: "Provider—Name"},
		{provider: "provider/name", wantErr: true},
	} {
		tt := tt
		t.Run(tt.provider, func(t *testing.T) {
			err := ValidateProviderForm(ProviderFormValues{Provider: tt.provider, Protocol: "openai_chat", Model: "model", Endpoint: "https://api.example.com/v1", ContextWindow: "128000", MaxOutputTokens: "8192"}, false, labels)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateProviderForm() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
