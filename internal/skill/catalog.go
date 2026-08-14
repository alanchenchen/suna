package skill

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Scope string

const (
	ScopeGlobal  Scope = "global"
	ScopeProject Scope = "project"
)

var projectSkillRoots = []string{
	filepath.Join(".agents", "skills"),
	filepath.Join(".claude", "skills"),
	filepath.Join(".github", "skills"),
	filepath.Join(".gemini", "skills"),
	filepath.Join(".cursor", "skills"),
	filepath.Join(".opencode", "skills"),
}

type Descriptor struct {
	Name        string
	Description string
	Scope       Scope
	Path        string
	Valid       bool
	Error       string
}

type Catalog struct {
	items []Descriptor
}

func NewCatalog(items []Descriptor) *Catalog {
	out := append([]Descriptor(nil), items...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Path < out[j].Path
	})
	return &Catalog{items: out}
}

func (c *Catalog) Descriptors() []Descriptor {
	if c == nil {
		return nil
	}
	return append([]Descriptor(nil), c.items...)
}

func (c *Catalog) LoadProject(name, exactPath string) (Descriptor, string, error) {
	name = strings.TrimSpace(name)
	exactPath = strings.TrimSpace(exactPath)
	if name == "" || exactPath == "" {
		return Descriptor{}, "", fmt.Errorf("project skill name and exact path are required")
	}
	for _, item := range c.items {
		if item.Scope != ScopeProject || item.Name != name || item.Path != exactPath {
			continue
		}
		if !item.Valid {
			return Descriptor{}, "", fmt.Errorf("project skill %q is invalid: %s", name, item.Error)
		}
		skillPath := filepath.Join(item.Path, "SKILL.md")
		if err := validateProjectSkillPath(item.Path, skillPath); err != nil {
			return Descriptor{}, "", fmt.Errorf("project skill %q is no longer safe to load: %w", name, err)
		}
		data, err := readProjectSkillFile(item.Path)
		if err != nil {
			return Descriptor{}, "", fmt.Errorf("read project skill %q: %w", name, err)
		}
		return item, string(data), nil
	}
	return Descriptor{}, "", fmt.Errorf("project skill %q with path %q is not in this session catalog", name, exactPath)
}

func DiscoverProject(cwd string) *Catalog {
	cwd = canonicalExistingDir(cwd)
	if cwd == "" {
		return NewCatalog(nil)
	}
	gitRoot, ok := nearestGitRoot(cwd)
	levels := []string{cwd}
	if ok {
		levels = ancestorsTo(cwd, gitRoot)
	}
	var items []Descriptor
	for _, level := range levels {
		for _, rel := range projectSkillRoots {
			root := filepath.Join(level, rel)
			if !hasDirectSkill(root) {
				continue
			}
			items = append(items, scanProjectSkillRoot(root)...)
			break
		}
	}
	return NewCatalog(items)
}

func RenderSummary(global, project []Descriptor, globalRoot string) string {
	var sections []string
	var globalLines []string
	for _, item := range global {
		if !item.Valid {
			continue
		}
		desc := strings.TrimSpace(item.Description)
		if desc == "" {
			desc = "No description provided."
		}
		globalLines = append(globalLines, fmt.Sprintf("- %s: %s", item.Name, desc))
	}
	if strings.TrimSpace(globalRoot) != "" {
		body := fmt.Sprintf("Global Skills root: `%s`", globalRoot)
		if len(globalLines) > 0 {
			body += "\n" + strings.Join(globalLines, "\n")
		} else {
			body += "\n- No enabled global Skills."
		}
		sections = append(sections, body)
	}
	var projectLines []string
	for _, item := range project {
		if !item.Valid {
			continue
		}
		desc := strings.TrimSpace(item.Description)
		if desc == "" {
			desc = "No description provided."
		}
		projectLines = append(projectLines, fmt.Sprintf("- %s (path: `%s`): %s", item.Name, item.Path, desc))
	}
	if len(projectLines) > 0 {
		sections = append(sections, "Project Skills (session catalog; use the exact path with scope=project):\n"+strings.Join(projectLines, "\n"))
	}
	if len(sections) == 0 {
		return ""
	}
	return "## Available Skills\n" + strings.Join(sections, "\n\n")
}

func readDescriptor(dir, fallback string, scope Scope) Descriptor {
	name, description, err := readSkillMetadata(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return Descriptor{Name: fallback, Scope: scope, Path: dir, Valid: false, Error: err.Error()}
	}
	if name == "" {
		name = fallback
	}
	if !validName(name) {
		return Descriptor{Name: name, Description: description, Scope: scope, Path: dir, Valid: false, Error: "invalid skill name"}
	}
	return Descriptor{Name: name, Description: description, Scope: scope, Path: dir, Valid: true}
}

func scanProjectSkillRoot(root string) []Descriptor {
	if err := validateProjectRoot(root); err != nil {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	items := make([]Descriptor, 0, len(entries))
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		skillPath := filepath.Join(dir, "SKILL.md")
		if err := validateProjectSkillPath(dir, skillPath); err != nil {
			continue
		}
		items = append(items, readDescriptor(dir, entry.Name(), ScopeProject))
	}
	return items
}

func nearestGitRoot(cwd string) (string, bool) {
	for dir := cwd; ; dir = filepath.Dir(dir) {
		if info, err := os.Lstat(filepath.Join(dir, ".git")); err == nil && info.Mode()&os.ModeSymlink == 0 {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
	}
}

func ancestorsTo(cwd, root string) []string {
	var out []string
	for dir := cwd; ; dir = filepath.Dir(dir) {
		out = append(out, dir)
		if dir == root {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	return out
}

func hasDirectSkill(root string) bool {
	if err := validateProjectRoot(root); err != nil {
		return false
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		if validateProjectSkillPath(dir, filepath.Join(dir, "SKILL.md")) == nil {
			return true
		}
	}
	return false
}

func validateProjectRoot(root string) error {
	agentDir := filepath.Dir(root)
	if err := requirePlainDir(agentDir); err != nil {
		return err
	}
	return requirePlainDir(root)
}

func validateProjectSkillPath(dir, skillPath string) error {
	if err := validateProjectRoot(filepath.Dir(dir)); err != nil {
		return err
	}
	if err := requirePlainDir(dir); err != nil {
		return err
	}
	info, err := os.Lstat(skillPath)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("SKILL.md must be a regular non-symlink file")
	}
	return nil
}

func requirePlainDir(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%s must be a non-symlink directory", path)
	}
	return nil
}

// readProjectSkillFile 使用目录句柄锚定已发现的 Skill 目录，读取当前磁盘上的完整正文。
func readProjectSkillFile(dir string) ([]byte, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	file, err := root.Open("SKILL.md")
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("SKILL.md must be a regular file")
	}
	return io.ReadAll(file)
}

func canonicalExistingDir(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return ""
	}
	info, err := os.Stat(real)
	if err != nil || !info.IsDir() {
		return ""
	}
	return filepath.Clean(real)
}
