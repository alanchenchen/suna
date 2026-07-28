package model

func testAdapterSpec(modelID string) AdapterSpec {
	return AdapterSpec{
		ModelID:         modelID,
		BaseURL:         "https://api.example.com/v1",
		APIKey:          "test-api-key",
		ContextWindow:   128000,
		MaxOutputTokens: 8192,
	}
}
