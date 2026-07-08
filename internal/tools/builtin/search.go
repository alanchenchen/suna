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
	"unicode"

	"github.com/alanchenchen/suna/internal/tools"
)

const (
	defaultSearchLimit        = 100
	maxSearchLimit            = 1000
	maxSearchDepth            = 20
	maxSearchFileSize         = 2 * 1024 * 1024
	maxSearchOutputBytes      = 100 * 1024
	maxSearchScannedFiles     = 20000
	maxSearchContextLines     = 5
	defaultSearchContextLines = 1
)

var (
	searchWorkspaceExclude = []string{
		".git/**",
		"node_modules/**",
		"vendor/**",
		"dist/**",
		"build/**",
		"target/**",
		".cache/**",
		"coverage/**",
		"tmp/**",
	}
	searchDependencyExclude = []string{
		".git/**",
		"dist/**",
		"build/**",
		"target/**",
		".cache/**",
		"coverage/**",
		"tmp/**",
	}
	searchSensitiveExclude = []string{
		// 默认跳过常见凭据文件，避免普通搜索将 secret 带入模型上下文。
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
)

type Search struct{}

func (Search) Spec() tools.Spec {
	return builtinSpec("search", "Search local files, paths, symbols, or text. Prefer over shell find/grep.", tools.Perceive, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":    map[string]any{"type": "string", "description": "File or directory to search"},
			"pattern": map[string]any{"type": "string", "description": "Search pattern. Literal by default, safe for punctuation. Use terms for multiple alternatives."},
			"terms":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Multiple literal alternatives; matches if any appears"},
			"mode":    map[string]any{"type": "string", "enum": []string{"auto", "content", "path", "symbol"}, "description": "content=grep, path=find/glob, symbol=headings/keys/declarations, auto=all. Default auto"},
			"match":   map[string]any{"type": "string", "enum": []string{"literal", "regex", "glob"}, "description": "Match mode. Default literal. regex=Go regexp, glob=best with mode=path"},
			"case":    map[string]any{"type": "string", "enum": []string{"smart", "insensitive", "sensitive"}, "description": "Default smart: insensitive unless pattern has uppercase"},
			"scope":   map[string]any{"type": "string", "enum": []string{"workspace", "deps", "all"}, "description": "workspace skips deps/build/cache/VCS/secrets, deps includes node_modules/vendor, all includes everything except secrets. Default workspace"},
			"word":    map[string]any{"type": "boolean", "description": "Match whole identifier words; omit unless substring too noisy"},
			"include": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Glob include filters, e.g. [\"**/*.go\"]"},
			"exclude": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Glob exclude filters, in addition to scope excludes"},
			"context": map[string]any{"type": "integer", "description": "Context lines around matches. Default 1, max 5"},
			"limit":   map[string]any{"type": "integer", "description": "Max matches. Default 100, max 1000"},
			"depth":   map[string]any{"type": "integer", "description": "Max directory depth. Default 8, max 20. 0=current dir only"},
		},
		"required": []string{"path"},
	})
}

func (Search) Execute(ctx context.Context, params map[string]any) tools.Result {
	root, _ := params["path"].(string)
	if root == "" {
		return tools.ErrorResult("path is required")
	}
	root = expandPathWithContext(ctx, root)
	info, err := os.Stat(root)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("stat search path: %s", err))
	}
	opts, err := searchOptionsFromParams(params)
	if err != nil {
		return tools.ErrorResult(err.Error())
	}
	state := &searchState{root: root, rootIsFile: !info.IsDir(), opts: opts, matchedFiles: map[string]bool{}}
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

type searchMode string

const (
	searchModeAuto    searchMode = "auto"
	searchModeContent searchMode = "content"
	searchModePath    searchMode = "path"
	searchModeSymbol  searchMode = "symbol"
)

type searchMatchMode string

const (
	searchMatchLiteral searchMatchMode = "literal"
	searchMatchRegex   searchMatchMode = "regex"
	searchMatchGlob    searchMatchMode = "glob"
)

type searchCaseMode string

const (
	searchCaseSmart       searchCaseMode = "smart"
	searchCaseInsensitive searchCaseMode = "insensitive"
	searchCaseSensitive   searchCaseMode = "sensitive"
)

type searchScope string

const (
	searchScopeWorkspace searchScope = "workspace"
	searchScopeDeps      searchScope = "deps"
	searchScopeAll       searchScope = "all"
)

type searchOptions struct {
	mode         searchMode
	match        searchMatchMode
	caseMode     searchCaseMode
	scope        searchScope
	pattern      string
	terms        []searchTerm
	include      []string
	exclude      []string
	depth        int
	limit        int
	contextLines int
	word         bool
}

type searchTerm struct {
	raw             string
	cmp             string
	regex           *regexp.Regexp
	caseInsensitive bool
}

type searchState struct {
	root            string
	rootIsFile      bool
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
	mode   searchMode
	rel    string
	lineNo int
	line   string
	term   string
	before []contextLine
	after  []contextLine
}

type contextLine struct {
	lineNo int
	text   string
}

func searchOptionsFromParams(params map[string]any) (searchOptions, error) {
	opts := searchOptions{
		mode:         searchModeAuto,
		match:        searchMatchLiteral,
		caseMode:     searchCaseSmart,
		scope:        searchScopeWorkspace,
		depth:        8,
		limit:        defaultSearchLimit,
		contextLines: defaultSearchContextLines,
	}
	if m, _ := params["mode"].(string); m != "" {
		opts.mode = searchMode(m)
	}
	if opts.mode != searchModeAuto && opts.mode != searchModeContent && opts.mode != searchModePath && opts.mode != searchModeSymbol {
		return opts, fmt.Errorf("mode must be auto, content, path, or symbol")
	}
	if m, _ := params["match"].(string); m != "" {
		opts.match = searchMatchMode(m)
	}
	if opts.match != searchMatchLiteral && opts.match != searchMatchRegex && opts.match != searchMatchGlob {
		return opts, fmt.Errorf("match must be literal, regex, or glob")
	}
	if c, _ := params["case"].(string); c != "" {
		opts.caseMode = searchCaseMode(c)
	}
	if opts.caseMode != searchCaseSmart && opts.caseMode != searchCaseInsensitive && opts.caseMode != searchCaseSensitive {
		return opts, fmt.Errorf("case must be smart, insensitive, or sensitive")
	}
	if s, _ := params["scope"].(string); s != "" {
		opts.scope = searchScope(s)
	}
	if opts.scope != searchScopeWorkspace && opts.scope != searchScopeDeps && opts.scope != searchScopeAll {
		return opts, fmt.Errorf("scope must be workspace, deps, or all")
	}
	if d, ok := numberParam(params["depth"]); ok && d >= 0 {
		opts.depth = d
		if opts.depth > maxSearchDepth {
			opts.depth = maxSearchDepth
		}
	}
	if n, ok := numberParam(params["limit"]); ok && n > 0 {
		opts.limit = n
	}
	if opts.limit > maxSearchLimit {
		opts.limit = maxSearchLimit
	}
	if n, ok := numberParam(params["context"]); ok && n >= 0 {
		opts.contextLines = n
		if opts.contextLines > maxSearchContextLines {
			opts.contextLines = maxSearchContextLines
		}
	}
	if v, ok := params["word"].(bool); ok {
		opts.word = v
	}
	opts.include = stringListParam(params["include"])
	opts.exclude = append(searchScopeExcludes(opts.scope), stringListParam(params["exclude"])...)
	opts.pattern, _ = params["pattern"].(string)
	rawTerms := stringListParam(params["terms"])
	if len(rawTerms) == 0 && strings.TrimSpace(opts.pattern) != "" {
		rawTerms = []string{opts.pattern}
	}
	if len(rawTerms) == 0 {
		return opts, fmt.Errorf("pattern or terms is required")
	}
	if len(rawTerms) > 1 && opts.match != searchMatchLiteral {
		return opts, fmt.Errorf("terms are literal alternatives; omit match or use match=literal when terms is provided")
	}
	for _, raw := range rawTerms {
		term, err := buildSearchTerm(raw, opts.match, opts.caseMode)
		if err != nil {
			return opts, err
		}
		opts.terms = append(opts.terms, term)
	}
	return opts, nil
}

func buildSearchTerm(raw string, match searchMatchMode, caseMode searchCaseMode) (searchTerm, error) {
	raw = strings.TrimSpace(raw)
	term := searchTerm{raw: raw, caseInsensitive: searchTermCaseInsensitive(raw, caseMode)}
	if raw == "" {
		return term, fmt.Errorf("empty search term")
	}
	if term.caseInsensitive {
		term.cmp = strings.ToLower(raw)
	} else {
		term.cmp = raw
	}
	if match == searchMatchRegex {
		pattern := raw
		if term.caseInsensitive {
			pattern = "(?i:" + pattern + ")"
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return term, fmt.Errorf("compile regex: %s\nTip: use match=literal for literal text with punctuation, or use terms for multiple alternatives", err)
		}
		term.regex = re
	}
	return term, nil
}

func searchTermCaseInsensitive(value string, mode searchCaseMode) bool {
	switch mode {
	case searchCaseSensitive:
		return false
	case searchCaseInsensitive:
		return true
	default:
		return !containsUpper(value)
	}
}

func containsUpper(value string) bool {
	for _, r := range value {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

func searchScopeExcludes(scope searchScope) []string {
	var out []string
	switch scope {
	case searchScopeDeps:
		out = append(out, searchDependencyExclude...)
	case searchScopeAll:
		// all 仍保留敏感文件保护，避免无意中把凭据带入上下文。
	default:
		out = append(out, searchWorkspaceExclude...)
	}
	out = append(out, searchSensitiveExclude...)
	return out
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
			if depthOf(rel) > s.opts.depth {
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
	if s.wantsPath() {
		if term, ok := s.matchString(rel); ok {
			s.addMatch(searchMatch{mode: searchModePath, rel: rel, term: term})
		}
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
	return s.opts.mode == searchModeAuto || s.opts.mode == searchModePath
}

func (s *searchState) wantsFileScan() bool {
	return s.opts.mode == searchModeAuto || s.opts.mode == searchModeContent || s.opts.mode == searchModeSymbol
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
		if s.wantsSymbol() && isLikelySymbolLine(line) {
			if term, ok := s.matchString(line); ok {
				s.addMatch(s.lineMatch(searchModeSymbol, rel, lineNo, line, term, lines))
				symbolMatched = true
			}
		}
		// auto 模式下结构入口已经作为 symbol 返回，避免同一行在 content 中重复出现。
		if s.wantsContent() && !(s.opts.mode == searchModeAuto && symbolMatched) {
			if term, ok := s.matchString(line); ok {
				s.addMatch(s.lineMatch(searchModeContent, rel, lineNo, line, term, lines))
			}
		}
	}
	return nil
}

func (s *searchState) wantsSymbol() bool {
	return s.opts.mode == searchModeAuto || s.opts.mode == searchModeSymbol
}

func (s *searchState) wantsContent() bool {
	return s.opts.mode == searchModeAuto || s.opts.mode == searchModeContent
}

func (s *searchState) lineMatch(mode searchMode, rel string, lineNo int, line string, term string, lines []string) searchMatch {
	ctx := s.opts.contextLines
	m := searchMatch{mode: mode, rel: rel, lineNo: lineNo, line: strings.TrimSpace(line), term: term}
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
	if s.matches >= s.opts.limit || len(s.results) >= s.opts.limit {
		s.truncated = true
		return
	}
	s.results = append(s.results, m)
	s.matches++
	s.markFileMatched(m.rel)
	switch m.mode {
	case searchModePath:
		s.pathMatches++
	case searchModeSymbol:
		s.symbolMatches++
	case searchModeContent:
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

func (s *searchState) matchString(value string) (string, bool) {
	for _, term := range s.opts.terms {
		if s.matchTerm(value, term) {
			return term.raw, true
		}
	}
	return "", false
}

func (s *searchState) matchTerm(value string, term searchTerm) bool {
	if term.regex != nil {
		return term.regex.MatchString(value)
	}
	needle := term.cmp
	candidate := value
	if term.caseInsensitive {
		candidate = strings.ToLower(candidate)
	}
	if s.opts.match == searchMatchGlob {
		return globMatch(candidate, needle)
	}
	if s.opts.word {
		return identifierWordMatch(candidate, needle)
	}
	return strings.Contains(candidate, needle)
}

func globMatch(value string, pattern string) bool {
	value = cleanGlobPath(value)
	pattern = cleanGlobPath(pattern)
	if doublestarGlobMatch(pattern, value) {
		return true
	}
	// 没有路径分隔符的 glob 按文件名匹配，贴近常见 find/fd 使用习惯。
	if !strings.Contains(pattern, "/") {
		return doublestarGlobMatch(pattern, filepath.Base(value))
	}
	return false
}

func cleanGlobPath(path string) string {
	path = filepath.ToSlash(strings.TrimSpace(path))
	path = strings.TrimPrefix(path, "./")
	path = strings.Trim(path, "/")
	return path
}

func doublestarGlobMatch(pattern string, value string) bool {
	if pattern == "" {
		return value == ""
	}
	return matchGlobParts(strings.Split(pattern, "/"), strings.Split(value, "/"))
}

func matchGlobParts(patternParts []string, valueParts []string) bool {
	if len(patternParts) == 0 {
		return len(valueParts) == 0
	}
	part := patternParts[0]
	if part == "**" {
		// ** 匹配零个或多个路径段，支持 **/*.go、dir/** 和 dir/**/file 等常见 glob。
		if matchGlobParts(patternParts[1:], valueParts) {
			return true
		}
		for i := range valueParts {
			if matchGlobParts(patternParts[1:], valueParts[i+1:]) {
				return true
			}
		}
		return false
	}
	if len(valueParts) == 0 {
		return false
	}
	ok, err := filepath.Match(part, valueParts[0])
	if err != nil || !ok {
		return false
	}
	return matchGlobParts(patternParts[1:], valueParts[1:])
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
