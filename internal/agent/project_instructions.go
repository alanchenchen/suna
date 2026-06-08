package agent

import (
	"os"
	"path/filepath"
	"strings"
)

var projectInstructionFiles = []string{
	"AGENTS.md",
	"CLAUDE.md",
	"GEMINI.md",
	".cursorrules",
	".windsurfrules",
}

type projectInstructions struct {
	Content string
	Source  string
}

func loadProjectInstructions(root string) projectInstructions {
	for _, name := range projectInstructionFiles {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			continue
		}
		content := strings.TrimSpace(string(data))
		if content == "" {
			continue
		}
		return projectInstructions{Content: content, Source: name}
	}
	return projectInstructions{}
}
