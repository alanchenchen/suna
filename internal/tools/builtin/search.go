package builtin

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/alanchenchen/suna/internal/tools"
)

const (
	defaultSearchMaxResults = 100
	maxSearchMaxResults     = 1000
	maxSearchDepth          = 20
	maxSearchFileSize       = 2 * 1024 * 1024
	maxSearchOutputBytes    = 100 * 1024
	maxSearchScannedFiles   = 20000
	maxSearchContextLines   = 5
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
	return builtinSpec("search", "Search file paths, structured entries, and text in a file or directory. Prefer this over shell grep/rg/find for local search.", tools.Perceive, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":                map[string]any{"type": "string", "description": "File or directory to search. Use project-relative paths when possible. If a file is given, only that file is searched."},
			"query":               map[string]any{"type": "string", "description": "Text, file path fragment, heading, definition name, key, or regex pattern to search for."},
			"kind":                map[string]any{"type": "string", "enum": []string{"auto", "content", "path", "symbol"}, "description": "Search kind. Default auto. auto searches path names, structured entries, and text content; content searches file text; path searches file and directory names; symbol searches lightweight structure such as headings, configuration keys/sections, and common definition or declaration lines."},
			"regex":               map[string]any{"type": "boolean", "description": "Treat query as a regular expression. Default false. Use false for literal punctuation, paths, and markup."},
			"case_sensitive":      map[string]any{"type": "boolean", "description": "Match case-sensitively. Default false."},
			"word":                map[string]any{"type": "boolean", "description": "Match query as a whole word or identifier when possible. Default false."},
			"context_lines":       map[string]any{"type": "integer", "description": "Lines of context before and after each content or symbol match. Default 1, max 5. Use 0 for single-line output."},
			"recursive":           map[string]any{"type": "boolean", "description": "Search subdirectories when path is a directory. Default true."},
			"max_depth":           map[string]any{"type": "integer", "description": "Maximum recursion depth for directory searches. Default 8, max 20."},
			"include":             map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Glob patterns to include, such as [" + `"**/*.go"` + ", " + `"internal/**"` + ", or " + `"**/*_test.go"` + ". Omit to search all supported text files."},
			"exclude":             map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Glob patterns to exclude, in addition to default excludes when use_default_exclude is true."},
			"use_default_exclude": map[string]any{"type": "boolean", "description": "Skip common dependency, build, cache, VCS, and secret files. Default true."},
			"max_results":         map[string]any{"type": "integer", "description": "Maximum ranked matches to return. Default 100, max 1000."},
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
	info, err := os.Stat(root)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("stat search path: %s", err))
	}
	opts, err := searchOptionsFromParams(params, query)
	if err != nil {
		return tools.ErrorResult(err.Error())
	}
	state := &searchState{root: root, rootIsFile: !info.IsDir(), query: query, opts: opts, matchedFiles: map[string]bool{}}
	if info.IsDir() {
		if err := filepath.WalkDir(root, state.visit(ctx)); err != nil {
			return tools.ErrorResult(fmt.Sprintf("search path: %s", err))
		}
	} else {
		state.searchPath(ctx, root, filepath.Base(root), false)
	}
	content := state.resultContent()
	return tools.Result{Content: content, Truncated: state.truncated, Metadata: state.metadata()}
}

type searchKind string

const (
	searchKindAuto    searchKind = "auto"
	searchKindContent searchKind = "content"
	searchKindPath    searchKind = "path"
	searchKindSymbol  searchKind = "symbol"
)

type searchOptions struct {
	kind              searchKind
	include           []string
	exclude           []string
	recursive         bool
	maxDepth          int
	maxResults        int
	contextLines      int
	caseSensitive     bool
	word              bool
	useDefaultExclude bool
	regex             *regexp.Regexp
}

type searchState struct {
	root            string
	rootIsFile      bool
	query           string
	opts            searchOptions
	matches         int
	pathMatches     int
	symbolMatches   int
	contentMatches  int
	pathsScanned    int
	filesScanned    int
	filesMatched    int
	matchedFiles    map[string]bool
	skippedExcluded int
	skippedDepth    int
	skippedInclude  int
	skippedLarge    int
	skippedBinary   int
	skippedMaxFiles int
	truncated       bool
	results         []searchMatch
}

type searchMatch struct {
	kind   searchKind
	rel    string
	lineNo int
	line   string
	before []contextLine
	after  []contextLine
}

type contextLine struct {
	lineNo int
	text   string
}

func searchOptionsFromParams(params map[string]any, query string) (searchOptions, error) {
	opts := searchOptions{kind: searchKindAuto, recursive: true, maxDepth: 8, maxResults: defaultSearchMaxResults, contextLines: 1}
	if k, _ := params["kind"].(string); k != "" {
		opts.kind = searchKind(k)
	}
	if opts.kind != searchKindAuto && opts.kind != searchKindContent && opts.kind != searchKindPath && opts.kind != searchKindSymbol {
		return opts, fmt.Errorf("kind must be auto, content, path, or symbol")
	}
	if r, ok := params["recursive"].(bool); ok {
		opts.recursive = r
	}
	if d, ok := numberParam(params["max_depth"]); ok && d >= 0 {
		opts.maxDepth = d
		if opts.maxDepth > maxSearchDepth {
			opts.maxDepth = maxSearchDepth
		}
	}
	if n, ok := numberParam(params["max_results"]); ok && n > 0 {
		opts.maxResults = n
	}
	if opts.maxResults > maxSearchMaxResults {
		opts.maxResults = maxSearchMaxResults
	}
	if n, ok := numberParam(params["context_lines"]); ok && n >= 0 {
		opts.contextLines = n
		if opts.contextLines > maxSearchContextLines {
			opts.contextLines = maxSearchContextLines
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
	if v, ok := params["word"].(bool); ok {
		opts.word = v
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

func (s *searchState) visit(ctx context.Context) fs.WalkDirFunc {
	return func(path string, d os.DirEntry, err error) error {
		if err != nil || s.truncated || ctx.Err() != nil {
			return nil
		}
		if path == s.root {
			return nil
		}
		rel, _ := filepath.Rel(s.root, path)
		rel = filepath.ToSlash(rel)
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
		s.searchPath(ctx, path, rel, true)
		return nil
	}
}

func (s *searchState) searchPath(ctx context.Context, path string, rel string, applyInclude bool) {
	if s.truncated || ctx.Err() != nil {
		return
	}
	rel = filepath.ToSlash(rel)
	s.pathsScanned++
	if applyInclude && len(s.opts.include) > 0 && !matchAnyGlob(rel, s.opts.include) {
		s.skippedInclude++
		return
	}
	if matchAnyGlob(rel, s.opts.exclude) {
		s.skippedExcluded++
		return
	}
	if s.wantsPath() && s.matchString(rel) {
		s.addMatch(searchMatch{kind: searchKindPath, rel: rel})
	}
	if !s.wantsFileScan() || s.truncated {
		return
	}
	if s.filesScanned >= maxSearchScannedFiles {
		s.skippedMaxFiles++
		s.truncated = true
		return
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return
	}
	if info.Size() > maxSearchFileSize {
		s.skippedLarge++
		return
	}
	s.filesScanned++
	_ = s.searchFile(path, rel)
}

func (s *searchState) wantsPath() bool {
	return s.opts.kind == searchKindAuto || s.opts.kind == searchKindPath
}

func (s *searchState) wantsFileScan() bool {
	return s.opts.kind == searchKindAuto || s.opts.kind == searchKindContent || s.opts.kind == searchKindSymbol
}

func (s *searchState) searchFile(path string, rel string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if binary, err := looksBinary(file); err != nil || binary {
		if binary {
			s.skippedBinary++
		}
		return err
	}
	lines, err := readSearchLines(file)
	if err != nil {
		return err
	}
	for i, line := range lines {
		if s.truncated {
			break
		}
		lineNo := i + 1
		symbolMatched := false
		if s.wantsSymbol() && isLikelySymbolLine(line) && s.matchString(line) {
			s.addMatch(s.lineMatch(searchKindSymbol, rel, lineNo, line, lines))
			symbolMatched = true
		}
		// auto 模式下结构入口已经作为 symbol 返回，避免同一行在 content 中重复出现。
		if s.wantsContent() && !(s.opts.kind == searchKindAuto && symbolMatched) && s.matchString(line) {
			s.addMatch(s.lineMatch(searchKindContent, rel, lineNo, line, lines))
		}
	}
	return nil
}

func (s *searchState) wantsSymbol() bool {
	return s.opts.kind == searchKindAuto || s.opts.kind == searchKindSymbol
}

func (s *searchState) wantsContent() bool {
	return s.opts.kind == searchKindAuto || s.opts.kind == searchKindContent
}

func (s *searchState) lineMatch(kind searchKind, rel string, lineNo int, line string, lines []string) searchMatch {
	ctx := s.opts.contextLines
	m := searchMatch{kind: kind, rel: rel, lineNo: lineNo, line: strings.TrimSpace(line)}
	if ctx <= 0 {
		return m
	}
	start := lineNo - ctx
	if start < 1 {
		start = 1
	}
	for n := start; n < lineNo; n++ {
		m.before = append(m.before, contextLine{lineNo: n, text: strings.TrimSpace(lines[n-1])})
	}
	end := lineNo + ctx
	if end > len(lines) {
		end = len(lines)
	}
	for n := lineNo + 1; n <= end; n++ {
		m.after = append(m.after, contextLine{lineNo: n, text: strings.TrimSpace(lines[n-1])})
	}
	return m
}

func readSearchLines(reader io.Reader) ([]string, error) {
	buf := bufio.NewReader(reader)
	var lines []string
	for {
		line, readErr := readLogicalLine(buf)
		if readErr != nil && readErr != io.EOF {
			return lines, readErr
		}
		if readErr == io.EOF && line == "" {
			break
		}
		lines = append(lines, line)
		if readErr == io.EOF {
			break
		}
	}
	return lines, nil
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

func (s *searchState) addMatch(m searchMatch) {
	if s.matches >= s.opts.maxResults || len(s.results) >= s.opts.maxResults {
		s.truncated = true
		return
	}
	s.results = append(s.results, m)
	s.matches++
	s.markFileMatched(m.rel)
	switch m.kind {
	case searchKindPath:
		s.pathMatches++
	case searchKindSymbol:
		s.symbolMatches++
	case searchKindContent:
		s.contentMatches++
	}
}

func (s *searchState) markFileMatched(rel string) {
	if rel == "" {
		return
	}
	if !s.matchedFiles[rel] {
		s.matchedFiles[rel] = true
		s.filesMatched++
	}
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
	if s.opts.word {
		return identifierWordMatch(value, query)
	}
	return strings.Contains(value, query)
}

func identifierWordMatch(value, query string) bool {
	if query == "" {
		return false
	}
	start := 0
	for {
		idx := strings.Index(value[start:], query)
		if idx < 0 {
			return false
		}
		idx += start
		beforeOK := idx == 0 || !isIdentRune(rune(value[idx-1]))
		after := idx + len(query)
		afterOK := after >= len(value) || !isIdentRune(rune(value[after]))
		if beforeOK && afterOK {
			return true
		}
		start = idx + len(query)
	}
}

func isIdentRune(r rune) bool {
	return r == '_' || r == '-' || r == '.' || r == '/' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
}

func isLikelySymbolLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "//") {
		return false
	}
	// symbol 是轻量结构入口，不限于代码：文档标题、配置段、键值项和常见声明都可帮助模型快速定位。
	if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
		return true
	}
	if looksLikeKeyValueLine(trimmed) {
		return true
	}
	prefixes := []string{"func ", "type ", "const ", "var ", "class ", "interface ", "enum ", "struct ", "def ", "async def ", "function ", "export function ", "export class ", "export interface ", "public ", "private ", "protected ", "service ", "message ", "rpc ", "table ", "CREATE TABLE ", "create table "}
	for _, prefix := range prefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}

func looksLikeKeyValueLine(line string) bool {
	idx := strings.IndexAny(line, "=:")
	if idx <= 0 || idx > 80 {
		return false
	}
	key := strings.TrimSpace(line[:idx])
	if key == "" || strings.ContainsAny(key, " \t{}()") {
		return false
	}
	for _, r := range key {
		if !isIdentRune(r) {
			return false
		}
	}
	return true
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

func numberParam(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case float32:
		return int(v), true
	case string:
		if v == "" {
			return 0, false
		}
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n, true
		}
	}
	return 0, false
}

func matchAnyGlob(path string, patterns []string) bool {
	path = filepath.ToSlash(path)
	for _, pattern := range patterns {
		pattern = filepath.ToSlash(strings.TrimSpace(pattern))
		if pattern == "" {
			continue
		}
		if strings.HasPrefix(pattern, "**/") {
			suffix := strings.TrimPrefix(pattern, "**/")
			if ok, _ := filepath.Match(suffix, filepath.Base(path)); ok {
				return true
			}
			if ok, _ := filepath.Match(suffix, path); ok {
				return true
			}
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
