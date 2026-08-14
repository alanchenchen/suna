package skill

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const maxSkillFrontmatterBytes = 64 * 1024

type skillFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// readSkillMetadata 只读取受限的 frontmatter；Skill 正文始终留到 skill_load 时按需读取。
func readSkillMetadata(path string) (name, description string, err error) {
	file, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", "", err
	}
	if !info.Mode().IsRegular() {
		return "", "", fmt.Errorf("SKILL.md must be a regular file")
	}

	limited := io.LimitReader(file, int64(maxSkillFrontmatterBytes)+1)
	reader := bufio.NewReader(limited)
	first, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", "", err
	}
	if trimLineEnding(first) != "---" {
		// 兼容早期 Suna Skill：只在受限索引窗口内提取完整的 H1 和首段说明。
		rest, readErr := io.ReadAll(reader)
		if readErr != nil {
			return "", "", readErr
		}
		content := first + string(rest)
		if len(content) > maxSkillFrontmatterBytes {
			content = content[:maxSkillFrontmatterBytes]
			if idx := strings.LastIndexByte(content, '\n'); idx >= 0 {
				content = content[:idx+1]
			} else {
				content = ""
			}
		}
		return extractH1(content), extractDescription(content), nil
	}

	var frontmatter strings.Builder
	readBytes := len(first)
	for {
		line, readErr := reader.ReadString('\n')
		readBytes += len(line)
		if readBytes > maxSkillFrontmatterBytes {
			return "", "", fmt.Errorf("SKILL.md frontmatter exceeds %d bytes", maxSkillFrontmatterBytes)
		}
		if trimLineEnding(line) == "---" {
			break
		}
		frontmatter.WriteString(line)
		if readErr == io.EOF {
			return "", "", fmt.Errorf("SKILL.md frontmatter is not terminated")
		}
		if readErr != nil {
			return "", "", readErr
		}
	}

	var meta skillFrontmatter
	if err := yaml.Unmarshal([]byte(frontmatter.String()), &meta); err != nil {
		return "", "", fmt.Errorf("parse SKILL.md frontmatter: %w", err)
	}
	return strings.TrimSpace(meta.Name), strings.TrimSpace(meta.Description), nil
}

func trimLineEnding(line string) string {
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
}

func extractH1(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(line[2:])
		}
	}
	return ""
}

func extractDescription(body string) string {
	seenH1 := false
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			seenH1 = true
			continue
		}
		if seenH1 && line != "" && !strings.HasPrefix(line, "#") {
			if len(line) > 240 {
				return line[:240]
			}
			return line
		}
	}
	return ""
}
