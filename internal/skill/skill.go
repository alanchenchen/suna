package skill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// Record 是 config.toml 中 [skills.<name>] 保存的轻量管理信息。
// enabled 是唯一生命周期状态；reasons 只记录最近一次 workflow check 结果，供用户了解。
type Record struct {
	Enabled bool     `toml:"enabled" json:"enabled"`
	Reasons []string `toml:"reasons,omitempty" json:"reasons,omitempty"`
}

type Info struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Enabled     bool     `json:"enabled"`
	Valid       bool     `json:"valid"`
	Reasons     []string `json:"reasons,omitempty"`
	Path        string   `json:"path,omitempty"`
	Error       string   `json:"error,omitempty"`
}

type CheckResult struct {
	Name        string   `json:"name"`
	Valid       bool     `json:"valid"`
	Reasons     []string `json:"reasons,omitempty"`
	Description string   `json:"description,omitempty"`
	Error       string   `json:"error,omitempty"`
}

type Skill struct {
	Name        string
	Description string
	Dir         string
	Path        string
	Content     string
	Valid       bool
	Error       string
	Reasons     []string
}

type Manager struct {
	root    string
	records map[string]Record
	skills  map[string]*Skill
	infos   []Info
}

func NewManager(root string, records map[string]Record) *Manager {
	return &Manager{root: root, records: cloneRecords(records), skills: map[string]*Skill{}}
}

func (m *Manager) Root() string { return m.root }

func (m *Manager) SetRecords(records map[string]Record) { m.records = cloneRecords(records) }
func (m *Manager) Records() map[string]Record           { return cloneRecords(m.records) }

func (m *Manager) Reload(ctx context.Context) error {
	_ = ctx
	m.skills = map[string]*Skill{}
	m.infos = nil
	if strings.TrimSpace(m.root) == "" {
		return fmt.Errorf("skill root is empty")
	}
	if err := os.MkdirAll(m.root, 0755); err != nil {
		return err
	}
	entries, err := os.ReadDir(m.root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		s := readSkillDir(filepath.Join(m.root, entry.Name()), entry.Name())
		if existing, ok := m.skills[s.Name]; ok {
			msg := fmt.Sprintf("duplicate skill name %q", s.Name)
			existing.Valid = false
			existing.Error = msg
			s.Valid = false
			s.Error = msg
		}
		m.skills[s.Name] = s
	}
	m.rebuildInfos()
	return nil
}

func (m *Manager) List() []Info {
	out := append([]Info(nil), m.infos...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (m *Manager) Summary() string {
	var lines []string
	for _, info := range m.List() {
		if !info.Enabled || !info.Valid {
			continue
		}
		desc := strings.TrimSpace(info.Description)
		if desc == "" {
			desc = "No description provided."
		}
		lines = append(lines, fmt.Sprintf("- %s: %s", info.Name, desc))
	}
	return strings.Join(lines, "\n")
}

func (m *Manager) Load(name string) (string, bool, string) {
	name = strings.TrimSpace(name)
	s, ok := m.skills[name]
	if !ok {
		return "", false, "skill not found"
	}
	if !s.Valid {
		return "", false, "skill is invalid: " + s.Error
	}
	if !m.records[name].Enabled {
		return "", false, "skill is not enabled"
	}
	return s.Content, true, ""
}

func (m *Manager) Check(name string) CheckResult {
	name = strings.TrimSpace(name)
	s, ok := m.skills[name]
	if !ok {
		return CheckResult{Name: name, Valid: false, Error: "skill not found"}
	}
	if !s.Valid {
		return CheckResult{Name: s.Name, Valid: false, Error: s.Error}
	}
	reasons := checkReasons(s.Dir)
	s.Reasons = append([]string(nil), reasons...)
	return CheckResult{Name: s.Name, Valid: true, Reasons: reasons, Description: s.Description}
}

func (m *Manager) Info(name string) (Info, bool) {
	name = strings.TrimSpace(name)
	for _, info := range m.infos {
		if info.Name == name {
			return info, true
		}
	}
	return Info{}, false
}

func (m *Manager) rebuildInfos() {
	seen := map[string]bool{}
	for name, s := range m.skills {
		seen[name] = true
		record := m.records[name]
		m.infos = append(m.infos, Info{Name: name, Description: s.Description, Enabled: record.Enabled, Valid: s.Valid, Reasons: append([]string(nil), s.Reasons...), Path: s.Dir, Error: s.Error})
	}
	for name, record := range m.records {
		if seen[name] {
			continue
		}
		m.infos = append(m.infos, Info{Name: name, Enabled: record.Enabled, Valid: false, Reasons: append([]string(nil), record.Reasons...), Error: "skill not found"})
	}
}

func readSkillDir(dir, fallback string) *Skill {
	path := filepath.Join(dir, "SKILL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return &Skill{Name: fallback, Dir: dir, Path: path, Valid: false, Error: "SKILL.md missing or unreadable"}
	}
	name, desc := parseSkillHeader(string(data))
	if name == "" {
		name = fallback
	}
	if !validName(name) {
		return &Skill{Name: fallback, Dir: dir, Path: path, Content: string(data), Valid: false, Error: "invalid skill name"}
	}
	return &Skill{Name: name, Description: desc, Dir: dir, Path: path, Content: string(data), Valid: true}
}

func parseSkillHeader(content string) (name, description string) {
	body := content
	if strings.HasPrefix(content, "---\n") || strings.HasPrefix(content, "---\r\n") {
		end := frontmatterEnd(content)
		if end > 0 {
			fm := content[4:end]
			body = strings.TrimLeft(content[end+4:], "\r\n")
			name, description = parseFrontmatter(fm)
		}
	}
	if name == "" {
		name = extractH1(body)
	}
	if description == "" {
		description = extractDescription(body)
	}
	return strings.TrimSpace(name), strings.TrimSpace(description)
}

func frontmatterEnd(content string) int {
	if idx := strings.Index(content[4:], "\n---\n"); idx >= 0 {
		return idx + 4
	}
	if idx := strings.Index(content[4:], "\n---\r\n"); idx >= 0 {
		return idx + 4
	}
	return -1
}

func parseFrontmatter(fm string) (name, description string) {
	for _, line := range strings.Split(fm, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := trimYAMLScalar(line[idx+1:])
		switch key {
		case "name":
			name = val
		case "description":
			description = val
		}
	}
	return
}

func trimYAMLScalar(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		q := s[0]
		if (q == '\'' || q == '"') && s[len(s)-1] == q {
			return s[1 : len(s)-1]
		}
	}
	return s
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

const (
	maxCheckFiles     = 256
	maxCheckFileBytes = 512 * 1024
	maxCheckTotal     = 8 * 1024 * 1024
)

var skippedSkillDirs = map[string]bool{".git": true, "node_modules": true, "vendor": true, "dist": true, "build": true, ".cache": true}

func checkReasons(dir string) []string {
	reasons := map[string]bool{}
	files := 0
	total := int64(0)
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			reasons["contains unreadable files"] = true
			return nil
		}
		if d.IsDir() {
			if path != dir && skippedSkillDirs[strings.ToLower(d.Name())] {
				reasons["skipped generated or dependency directories during check"] = true
				return filepath.SkipDir
			}
			return nil
		}
		if files >= maxCheckFiles || total >= maxCheckTotal {
			reasons["skipped some files because the Skill is large"] = true
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		lowerRel := strings.ToLower(filepath.ToSlash(rel))
		if strings.HasPrefix(lowerRel, "scripts/") {
			reasons["includes scripts/ helper files"] = true
		}
		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		files++
		if info.Size() > maxCheckFileBytes {
			reasons["contains large files skipped during check"] = true
			return nil
		}
		if total+info.Size() > maxCheckTotal {
			reasons["skipped some files because the Skill is large"] = true
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			reasons["contains unreadable files"] = true
			return nil
		}
		total += int64(len(data))
		if looksBinary(data) {
			reasons["contains binary or obfuscated content"] = true
			return nil
		}
		text := strings.ToLower(string(data))
		patterns := []struct{ needle, reason string }{{"rm -rf", "contains destructive delete commands"}, {"sudo ", "contains privilege escalation commands"}, {"curl ", "contains network access commands"}, {"wget ", "contains network access commands"}, {"github_token", "mentions sensitive environment variables or tokens"}, {"api_key", "mentions sensitive environment variables or tokens"}, {"/etc/", "mentions sensitive system paths"}, {"ignore previous", "possible prompt injection instruction"}, {"忽略之前", "possible prompt injection instruction"}}
		for _, p := range patterns {
			if strings.Contains(text, p.needle) {
				reasons[p.reason] = true
			}
		}
		return nil
	})
	out := make([]string, 0, len(reasons))
	for reason := range reasons {
		out = append(out, reason)
	}
	sort.Strings(out)
	return out
}

func looksBinary(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	limit := len(data)
	if limit > 4096 {
		limit = 4096
	}
	for _, b := range data[:limit] {
		if b == 0 {
			return true
		}
	}
	return false
}

func validName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 80 {
		return false
	}
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func cloneRecords(in map[string]Record) map[string]Record {
	out := make(map[string]Record, len(in))
	for k, v := range in {
		v.Reasons = append([]string(nil), v.Reasons...)
		out[k] = v
	}
	return out
}
