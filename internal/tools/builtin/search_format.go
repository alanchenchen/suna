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
	s.writeLine(&out, fmt.Sprintf("Search results for %q in %s (kind=%s)", s.query, displaySearchRoot(s.root), s.opts.kind))
	s.writeLine(&out, fmt.Sprintf("Found %d matches in %d files. Showing %d.", s.matches, s.filesMatched, len(s.results)))
	sections := []struct {
		kind  searchKind
		title string
	}{
		{searchKindPath, "Path matches"},
		{searchKindSymbol, "Symbol matches"},
		{searchKindContent, "Content matches"},
	}
	for _, section := range sections {
		items := s.resultsOfKind(section.kind)
		if len(items) == 0 {
			continue
		}
		s.writeLine(&out, "")
		s.writeLine(&out, section.title+":")
		if section.kind == searchKindPath {
			for _, item := range items {
				s.writeLine(&out, "- "+item.rel)
			}
			continue
		}
		for _, group := range groupSearchMatches(items) {
			s.writeLine(&out, group.rel)
			for _, item := range group.items {
				for _, line := range item.before {
					s.writeLine(&out, fmt.Sprintf("  %d | %s", line.lineNo, line.text))
				}
				s.writeLine(&out, fmt.Sprintf("> %d | %s", item.lineNo, item.line))
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

func (s *searchState) writeLine(out *strings.Builder, line string) {
	if s.truncated || out.Len()+len(line)+1 > maxSearchOutputBytes {
		s.truncated = true
		return
	}
	out.WriteString(line)
	out.WriteByte('\n')
}

func (s *searchState) resultsOfKind(kind searchKind) []searchMatch {
	var out []searchMatch
	for _, item := range s.results {
		if item.kind == kind {
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
		lines = append(lines, "Search stopped early because the result limit, output limit, or scanned file limit was reached. Narrow the query or increase max_results when appropriate.")
	}
	if s.matches == 0 {
		lines = append(lines, fmt.Sprintf("No matches found after scanning %d files and %d paths.", s.filesScanned, s.pathsScanned))
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

func (s *searchState) metadata() map[string]any {
	return map[string]any{
		"kind":              "search_result",
		"search_kind":       string(s.opts.kind),
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
