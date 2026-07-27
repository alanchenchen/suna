package chat

import "testing"

func TestRegisteredSlashCommandRecognizesWhitespaceSeparatedArguments(t *testing.T) {
	for _, input := range []string{"/mcp", "/mcp\t", "/mcp\nextra"} {
		if !IsRegisteredSlashCommand(input) {
			t.Fatalf("IsRegisteredSlashCommand(%q) = false, want true", input)
		}
	}
}

func TestRegisteredSlashCommandRejectsUnknownCommand(t *testing.T) {
	if IsRegisteredSlashCommand("/unknown") {
		t.Fatal("IsRegisteredSlashCommand(/unknown) = true, want false")
	}
}
