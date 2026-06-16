package builtin

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

func (s *searchState) resultContent() string {
	if len(s.results) == 0 {
		content := "no matches"
		if diagnostics := s.searchDiagnostics(); diagnostics != "" {
			content += "\n\n" + diagnostics
		}
		return content
	}
	var out strings.Builder
	s.writeLine(&out, fmt.Sprintf("Search results for %s in %s (mode=%s, match=%s, scope=%s)", s.searchLabel(), displaySearchRoot(s.root), s.opts.mode, s.opts.match, s.opts.scope))
	s.writeLine(&out, fmt.Sprintf("Found %d matches in %d files. Showing %d.", s.matches, s.filesMatched, len(s.results)))
	sections := []struct {
		mode  searchMode
		title string
	}{
		{searchModePath, "Path matches"},
		{searchModeSymbol, "Symbol matches"},
		{searchModeContent, "Content matches"},
	}
	for _, section := range sections {
		items := s.resultsOfMode(section.mode)
		if len(items) == 0 {
			continue
		}
		s.writeLine(&out, "")
		s.writeLine(&out, section.title+":")
		if section.mode == searchModePath {
			for _, item := range items {
				s.writeLine(&out, "- "+item.rel+s.matchSuffix(item))
			}
			continue
		}
		for _, group := range groupSearchMatches(items) {
			s.writeLine(&out, group.rel)
			for _, item := range group.items {
				for _, line := range item.before {
					s.writeLine(&out, fmt.Sprintf("  %d | %s", line.lineNo, line.text))
				}
				s.writeLine(&out, fmt.Sprintf("> %d | %s%s", item.lineNo, item.line, s.matchSuffix(item)))
				for _, line := range item.after {
					s.writeLine(&out, fmt.Sprintf("  %d | %s", line.lineNo, line.text))
				}
			}
		}
	}
	if diagnostics := s.searchDiagnostics(); diagnostics != "" {
		s.writeLine(&out, "")
		s.writeLine(&out, diagnostics)
	}
	return strings.TrimRight(out.String(), "\n")
}

func (s *searchState) searchLabel() string {
	if len(s.opts.terms) == 1 {
		return fmt.Sprintf("%q", s.opts.terms[0].raw)
	}
	terms := make([]string, 0, len(s.opts.terms))
	for _, term := range s.opts.terms {
		terms = append(terms, term.raw)
	}
	return fmt.Sprintf("terms=%q", terms)
}

func (s *searchState) matchSuffix(item searchMatch) string {
	if len(s.opts.terms) <= 1 || item.term == "" {
		return ""
	}
	return fmt.Sprintf("  [matched: %s]", item.term)
}

func (s *searchState) writeLine(out *strings.Builder, line string) {
	if s.truncated || out.Len()+len(line)+1 > maxSearchOutputBytes {
		s.truncated = true
		return
	}
	out.WriteString(line)
	out.WriteByte('\n')
}

func (s *searchState) resultsOfMode(mode searchMode) []searchMatch {
	var out []searchMatch
	for _, item := range s.results {
		if item.mode == mode {
			out = append(out, item)
		}
	}
	return out
}

type searchMatchGroup struct {
	rel   string
	items []searchMatch
}

func groupSearchMatches(items []searchMatch) []searchMatchGroup {
	groups := map[string][]searchMatch{}
	var order []string
	for _, item := range items {
		if _, ok := groups[item.rel]; !ok {
			order = append(order, item.rel)
		}
		groups[item.rel] = append(groups[item.rel], item)
	}
	sort.Strings(order)
	out := make([]searchMatchGroup, 0, len(order))
	for _, rel := range order {
		out = append(out, searchMatchGroup{rel: rel, items: groups[rel]})
	}
	return out
}

func displaySearchRoot(path string) string {
	if path == "" {
		return "."
	}
	return filepath.ToSlash(path)
}

func (s *searchState) searchDiagnostics() string {
	var lines []string
	if s.truncated {
		lines = append(lines, "Search stopped early because the result limit, output limit, or scanned file limit was reached. Narrow the pattern/terms or increase limit when appropriate.")
	}
	if s.matches == 0 {
		lines = append(lines, fmt.Sprintf("No matches found after scanning %d files and %d paths.", s.filesScanned, s.pathsScanned))
		if len(s.opts.include) > 0 && s.skippedInclude > 0 {
			lines = append(lines, fmt.Sprintf("%d files were skipped by include filters; broaden include if the target may be outside those globs.", s.skippedInclude))
		}
		if s.skippedExcluded > 0 {
			lines = append(lines, fmt.Sprintf("%d paths were skipped by scope/user excludes. Use scope=deps to inspect dependency source, or scope=all for generated/cache/dependency files while still protecting common secret files.", s.skippedExcluded))
		}
		if s.skippedDepth > 0 {
			lines = append(lines, fmt.Sprintf("%d directories were skipped by depth limits; increase depth if the target is deeper.", s.skippedDepth))
		}
		if s.skippedLarge > 0 || s.skippedBinary > 0 {
			lines = append(lines, fmt.Sprintf("Skipped %d large files and %d binary files for performance.", s.skippedLarge, s.skippedBinary))
		}
		if s.opts.match == searchMatchLiteral && len(s.opts.terms) == 1 && strings.Contains(s.opts.terms[0].raw, "|") {
			lines = append(lines, "The pattern contains '|', but literal mode searches it as text. Use terms for alternatives, or match=regex with a valid regex.")
		}
		if s.opts.match == searchMatchRegex {
			lines = append(lines, "Regex mode was enabled; use match=literal for literal punctuation, paths, markup, or ordinary text.")
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "Search diagnostics:\n- " + strings.Join(lines, "\n- ")
}

func (s *searchState) metadata() map[string]any {
	return map[string]any{
		"kind":              "search_result",
		"search_mode":       string(s.opts.mode),
		"match_mode":        string(s.opts.match),
		"scope":             string(s.opts.scope),
		"matches":           s.matches,
		"files_scanned":     s.filesScanned,
		"files_matched":     s.filesMatched,
		"paths_scanned":     s.pathsScanned,
		"path_matches":      s.pathMatches,
		"symbol_matches":    s.symbolMatches,
		"content_matches":   s.contentMatches,
		"skipped_excluded":  s.skippedExcluded,
		"skipped_depth":     s.skippedDepth,
		"skipped_include":   s.skippedInclude,
		"skipped_large":     s.skippedLarge,
		"skipped_binary":    s.skippedBinary,
		"skipped_max_files": s.skippedMaxFiles,
		"truncated":         s.truncated,
	}
}
