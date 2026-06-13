package builtin

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/alanchenchen/suna/internal/tools"
)

const (
	defaultSearchMaxMatches = 200
	maxSearchMaxMatches     = 1000
	maxSearchDepth          = 20
	maxSearchFileSize       = 2 * 1024 * 1024
	maxSearchOutputBytes    = 100 * 1024
	maxSearchScannedFiles   = 20000
)

var defaultSearchExclude = []string{
	".git/**",
	"node_modules/**",
	"vendor/**",
	"dist/**",
	"build/**",
	"target/**",
	".cache/**",
	"coverage/**",
	"tmp/**",
	// 默认跳过常见凭据文件，避免普通 content search 将 secret 带入模型上下文；用户可关闭默认排除并交给 Guard 审核。
	".env",
	".env.*",
	"*.pem",
	"*.key",
	"*.p12",
	"*.pfx",
	"id_rsa",
	"id_ed25519",
	"id_ecdsa",
	".ssh/**",
	".gnupg/**",
	".netrc",
	".npmrc",
	".pypirc",
	"credentials.json",
	"credentials.toml",
}

type Search struct{}

func (Search) Spec() tools.Spec {
	return builtinSpec("search", "Search file names or text contents in a directory; prefer this over shell grep/find.", tools.Perceive, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":                map[string]any{"type": "string", "description": "Directory path to search."},
			"query":               map[string]any{"type": "string", "description": "Text or regex pattern to search for."},
			"mode":                map[string]any{"type": "string", "enum": []string{"content", "name"}, "description": "Search mode: content searches file text, name searches file names. Default content."},
			"regex":               map[string]any{"type": "boolean", "description": "Treat query as a regular expression."},
			"case_sensitive":      map[string]any{"type": "boolean", "description": "Match case-sensitively."},
			"recursive":           map[string]any{"type": "boolean", "description": "Search subdirectories recursively. Default true."},
			"max_depth":           map[string]any{"type": "integer", "description": "Maximum recursion depth. Default 8, max 20."},
			"include":             map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Glob patterns to include, such as [\"*.go\", \"internal/**\"]."},
			"exclude":             map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Glob patterns to exclude."},
			"use_default_exclude": map[string]any{"type": "boolean", "description": "Use default excludes for performance and common secret files. Default true."},
			"max_matches":         map[string]any{"type": "integer", "description": "Maximum matches to return. Default 200, max 1000."},
		},
		"required": []string{"path", "query"},
	})
}

func (Search) Execute(ctx context.Context, params map[string]any) tools.Result {
	root, _ := params["path"].(string)
	query, _ := params["query"].(string)
	if root == "" {
		return tools.ErrorResult("path is required")
	}
	if query == "" {
		return tools.ErrorResult("query is required")
	}
	root = expandPath(root)
	mode, _ := params["mode"].(string)
	if mode == "" {
		mode = "content"
	}
	if mode != "content" && mode != "name" {
		return tools.ErrorResult("mode must be content or name")
	}
	info, err := os.Stat(root)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("stat search path: %s", err))
	}
	if !info.IsDir() {
		return tools.ErrorResult(fmt.Sprintf("search path is not a directory: %s", root))
	}

	opts, err := searchOptionsFromParams(params, query)
	if err != nil {
		return tools.ErrorResult(err.Error())
	}
	state := &searchState{root: root, query: query, mode: mode, opts: opts}
	if err := filepath.WalkDir(root, state.visit); err != nil {
		return tools.ErrorResult(fmt.Sprintf("search path: %s", err))
	}
	content := state.resultContent()
	return tools.Result{Content: content, Truncated: state.truncated, Metadata: map[string]any{"kind": "search_result", "mode": mode, "matches": state.matches, "files_scanned": state.filesScanned, "files_matched": state.filesMatched, "truncated": state.truncated}}
}

type searchOptions struct {
	include           []string
	exclude           []string
	recursive         bool
	maxDepth          int
	maxMatches        int
	caseSensitive     bool
	useDefaultExclude bool
	regex             *regexp.Regexp
}

type searchState struct {
	root            string
	query           string
	mode            string
	opts            searchOptions
	matches         int
	filesScanned    int
	filesMatched    int
	skippedExcluded int
	skippedDepth    int
	skippedInclude  int
	skippedLarge    int
	skippedBinary   int
	skippedMaxFiles int
	truncated       bool
	output          strings.Builder
}

func searchOptionsFromParams(params map[string]any, query string) (searchOptions, error) {
	opts := searchOptions{recursive: true, maxDepth: 8, maxMatches: defaultSearchMaxMatches}
	if r, ok := params["recursive"].(bool); ok {
		opts.recursive = r
	}
	if d, ok := params["max_depth"].(float64); ok && int(d) >= 0 {
		opts.maxDepth = int(d)
		if opts.maxDepth > maxSearchDepth {
			opts.maxDepth = maxSearchDepth
		}
	}
	if n, ok := params["max_matches"].(float64); ok && int(n) > 0 {
		opts.maxMatches = int(n)
		if opts.maxMatches > maxSearchMaxMatches {
			opts.maxMatches = maxSearchMaxMatches
		}
	}
	opts.include = stringListParam(params["include"])
	opts.exclude = stringListParam(params["exclude"])
	useDefault := true
	if v, ok := params["use_default_exclude"].(bool); ok {
		useDefault = v
	}
	opts.useDefaultExclude = useDefault
	if useDefault {
		opts.exclude = append(append([]string{}, defaultSearchExclude...), opts.exclude...)
	}
	if v, ok := params["case_sensitive"].(bool); ok {
		opts.caseSensitive = v
	}
	if v, ok := params["regex"].(bool); ok && v {
		pattern := query
		if !opts.caseSensitive {
			pattern = "(?i:" + pattern + ")"
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return opts, fmt.Errorf("compile regex: %s", err)
		}
		opts.regex = re
	}
	return opts, nil
}

func (s *searchState) visit(path string, d os.DirEntry, err error) error {
	if err != nil || s.truncated {
		return nil
	}
	if path == s.root {
		return nil
	}
	rel, _ := filepath.Rel(s.root, path)
	if d.IsDir() {
		if !s.opts.recursive || depthOf(rel) > s.opts.maxDepth {
			s.skippedDepth++
			return filepath.SkipDir
		}
		if matchAnyGlob(rel+"/", s.opts.exclude) || matchAnyGlob(rel, s.opts.exclude) {
			s.skippedExcluded++
			return filepath.SkipDir
		}
		return nil
	}
	if len(s.opts.include) > 0 && !matchAnyGlob(rel, s.opts.include) {
		s.skippedInclude++
		return nil
	}
	if matchAnyGlob(rel, s.opts.exclude) {
		s.skippedExcluded++
		return nil
	}
	if s.mode == "name" {
		if s.matchString(filepath.Base(path)) {
			s.addLine(fmt.Sprintf("%s\n", rel))
			s.matches++
			s.filesMatched++
		}
		return nil
	}
	if s.filesScanned >= maxSearchScannedFiles {
		s.skippedMaxFiles++
		s.truncated = true
		return nil
	}
	s.filesScanned++
	info, err := d.Info()
	if err != nil {
		return nil
	}
	if info.Size() > maxSearchFileSize {
		s.skippedLarge++
		return nil
	}
	matched, err := s.searchFile(path, rel)
	if err == nil && matched {
		s.filesMatched++
	}
	return nil
}

func (s *searchState) searchFile(path string, rel string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	if binary, err := looksBinary(file); err != nil || binary {
		if binary {
			s.skippedBinary++
		}
		return false, err
	}
	reader := bufio.NewReader(file)
	lineNo := 0
	matched := false
	for {
		line, readErr := readLogicalLine(reader)
		if readErr != nil && readErr != io.EOF {
			return matched, readErr
		}
		if readErr == io.EOF && line == "" {
			break
		}
		lineNo++
		if s.matchString(line) {
			matched = true
			s.matches++
			s.addLine(fmt.Sprintf("%s:%d: %s\n", rel, lineNo, strings.TrimSpace(line)))
			if s.matches >= s.opts.maxMatches {
				s.truncated = true
				break
			}
		}
		if readErr == io.EOF || s.truncated {
			break
		}
	}
	return matched, nil
}

func looksBinary(file *os.File) (bool, error) {
	buf := make([]byte, 4096)
	n, err := file.Read(buf)
	if err != nil && err != io.EOF {
		return false, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return false, err
	}
	for _, b := range buf[:n] {
		if b == 0 {
			return true, nil
		}
	}
	return false, nil
}

func (s *searchState) resultContent() string {
	content := strings.TrimRight(s.output.String(), "\n")
	if content == "" {
		content = "no matches"
	}

	diagnostics := s.searchDiagnostics()
	if diagnostics != "" {
		content += "\n\n" + diagnostics
	}
	return content
}

func (s *searchState) searchDiagnostics() string {
	var lines []string
	if s.truncated {
		lines = append(lines, "Search stopped early because the result limit, output limit, or scanned file limit was reached. Narrow the query or increase max_matches when appropriate.")
	}
	if s.matches == 0 {
		lines = append(lines, fmt.Sprintf("No matches found after scanning %d files.", s.filesScanned))
		if len(s.opts.include) > 0 && s.skippedInclude > 0 {
			lines = append(lines, fmt.Sprintf("%d files were skipped by include filters; broaden include if the target may be outside those globs.", s.skippedInclude))
		}
		if s.opts.useDefaultExclude && s.skippedExcluded > 0 {
			lines = append(lines, fmt.Sprintf("%d paths were skipped by default/user excludes; retry with use_default_exclude=false only when you intentionally need generated, dependency, hidden, or sensitive paths.", s.skippedExcluded))
		} else if s.skippedExcluded > 0 {
			lines = append(lines, fmt.Sprintf("%d paths were skipped by exclude filters; relax exclude if needed.", s.skippedExcluded))
		}
		if s.skippedDepth > 0 {
			lines = append(lines, fmt.Sprintf("%d directories were skipped by recursive/max_depth limits; increase max_depth if the target is deeper.", s.skippedDepth))
		}
		if s.skippedLarge > 0 || s.skippedBinary > 0 {
			lines = append(lines, fmt.Sprintf("Skipped %d large files and %d binary files for performance.", s.skippedLarge, s.skippedBinary))
		}
		if s.opts.regex != nil {
			lines = append(lines, "Regex mode was enabled; retry with regex=false when searching for literal punctuation or markup.")
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "Search diagnostics:\n- " + strings.Join(lines, "\n- ")
}

func (s *searchState) matchString(value string) bool {
	if s.opts.regex != nil {
		return s.opts.regex.MatchString(value)
	}
	query := s.query
	if !s.opts.caseSensitive {
		value = strings.ToLower(value)
		query = strings.ToLower(query)
	}
	return strings.Contains(value, query)
}

func (s *searchState) addLine(line string) {
	if s.output.Len()+len(line) > maxSearchOutputBytes {
		s.truncated = true
		return
	}
	s.output.WriteString(line)
}

func depthOf(rel string) int {
	if rel == "." || rel == "" {
		return 0
	}
	return strings.Count(filepath.ToSlash(rel), "/") + 1
}

func stringListParam(value any) []string {
	switch v := value.(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	case []string:
		out := make([]string, 0, len(v))
		for _, s := range v {
			if strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	default:
		return nil
	}
}

func matchAnyGlob(path string, patterns []string) bool {
	path = filepath.ToSlash(path)
	for _, pattern := range patterns {
		pattern = filepath.ToSlash(strings.TrimSpace(pattern))
		if pattern == "" {
			continue
		}
		if strings.HasSuffix(pattern, "/**") {
			prefix := strings.TrimSuffix(pattern, "/**")
			if path == prefix || strings.HasPrefix(path, prefix+"/") || strings.Contains(path, "/"+prefix+"/") {
				return true
			}
		}
		if ok, _ := filepath.Match(pattern, path); ok {
			return true
		}
		if ok, _ := filepath.Match(pattern, filepath.Base(path)); ok {
			return true
		}
	}
	return false
}
