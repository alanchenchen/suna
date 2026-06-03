package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	maxReviewFiles     = 12
	maxReviewFileChars = 6000
	maxReviewTotal     = 24000
)

func (r *Runtime) reviewRequestLocked(check CheckResult) (LLMReviewRequest, error) {
	s, ok := r.manager.skills[check.Name]
	if !ok || s == nil || !s.Valid {
		return LLMReviewRequest{}, fmt.Errorf("skill %q is missing or invalid", check.Name)
	}
	files, err := collectReviewFiles(s.Dir)
	if err != nil {
		return LLMReviewRequest{}, err
	}
	return LLMReviewRequest{Name: check.Name, Description: check.Description, Reasons: append([]string(nil), check.Reasons...), Files: files}, nil
}

func collectReviewFiles(dir string) ([]ReviewFile, error) {
	var paths []string
	visited := 0
	limited := false
	if err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != dir && skippedSkillDirs[strings.ToLower(d.Name())] {
				return filepath.SkipDir
			}
			return nil
		}
		if visited >= maxCheckFiles {
			limited = true
			return nil
		}
		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() {
			return err
		}
		visited++
		if info.Size() > maxCheckFileBytes {
			return nil
		}
		paths = append(paths, path)
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Slice(paths, func(i, j int) bool {
		return reviewFileRank(paths[i]) < reviewFileRank(paths[j]) || reviewFileRank(paths[i]) == reviewFileRank(paths[j]) && paths[i] < paths[j]
	})
	files := make([]ReviewFile, 0, len(paths))
	total := 0
	for _, path := range paths {
		if len(files) >= maxReviewFiles || total >= maxReviewTotal {
			limited = true
			break
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if looksBinary(data) {
			continue
		}
		rel, _ := filepath.Rel(dir, path)
		text := string(data)
		truncated := false
		if len(text) > maxReviewFileChars {
			text = text[:maxReviewFileChars]
			truncated = true
		}
		remaining := maxReviewTotal - total
		if len(text) > remaining {
			text = text[:remaining]
			truncated = true
		}
		total += len(text)
		files = append(files, ReviewFile{Path: filepath.ToSlash(rel), Content: text, Truncated: truncated})
	}
	if limited {
		files = append(files, ReviewFile{Path: "[scan-limit]", Content: "Some Skill files were skipped because the Skill is large.", Truncated: true})
	}
	return files, nil
}

func reviewFileRank(path string) int {
	rel := strings.ToLower(filepath.ToSlash(path))
	if strings.HasSuffix(rel, "/skill.md") || rel == "skill.md" {
		return 0
	}
	if strings.Contains(rel, "/scripts/") || strings.HasPrefix(rel, "scripts/") {
		return 1
	}
	if strings.Contains(rel, "/references/") || strings.HasPrefix(rel, "references/") {
		return 2
	}
	return 3
}
