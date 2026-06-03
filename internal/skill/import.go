package skill

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	maxImportZipFiles      = 256
	maxImportZipTotalBytes = 32 * 1024 * 1024
	maxImportZipFileBytes  = 8 * 1024 * 1024
)

type ImportResult struct {
	Name  string      `json:"name"`
	Path  string      `json:"path"`
	Check CheckResult `json:"check"`
}

func (r *Runtime) Import(ctx context.Context, source string, name string) (ImportResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	source = strings.TrimSpace(source)
	name = strings.TrimSpace(name)
	if source == "" {
		return ImportResult{}, fmt.Errorf("source is required")
	}
	if isRemoteSource(source) {
		return r.importGit(ctx, source, name)
	}
	if strings.EqualFold(filepath.Ext(source), ".zip") {
		return r.importZip(ctx, source, name)
	}
	return r.importLocal(ctx, source, name)
}

func (r *Runtime) importGit(ctx context.Context, source string, name string) (ImportResult, error) {
	// 远程导入只做浅 clone；是否启用仍必须经过 check + 用户确认。
	tmp, err := os.MkdirTemp("", "suna-skill-*")
	if err != nil {
		return ImportResult{}, err
	}
	defer os.RemoveAll(tmp)
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", source, tmp)
	if out, err := cmd.CombinedOutput(); err != nil {
		return ImportResult{}, fmt.Errorf("git clone failed: %s", strings.TrimSpace(string(out)))
	}
	return r.importLocal(ctx, tmp, name)
}

func (r *Runtime) importZip(ctx context.Context, source string, name string) (ImportResult, error) {
	tmp, err := os.MkdirTemp("", "suna-skill-zip-*")
	if err != nil {
		return ImportResult{}, err
	}
	defer os.RemoveAll(tmp)
	if err := unzip(source, tmp); err != nil {
		return ImportResult{}, err
	}
	root := tmp
	if _, err := os.Stat(filepath.Join(root, "SKILL.md")); err != nil {
		entries, _ := os.ReadDir(tmp)
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			candidate := filepath.Join(tmp, entry.Name())
			if _, err := os.Stat(filepath.Join(candidate, "SKILL.md")); err == nil {
				root = candidate
				break
			}
		}
	}
	return r.importLocal(ctx, root, name)
}

func (r *Runtime) importLocal(ctx context.Context, source string, name string) (ImportResult, error) {
	_ = ctx
	absSource, err := filepath.Abs(source)
	if err != nil {
		return ImportResult{}, err
	}
	info, err := os.Stat(absSource)
	if err != nil {
		return ImportResult{}, err
	}
	if !info.IsDir() {
		return ImportResult{}, fmt.Errorf("source must be a skill directory")
	}
	content, err := os.ReadFile(filepath.Join(absSource, "SKILL.md"))
	if err != nil {
		return ImportResult{}, fmt.Errorf("source missing SKILL.md")
	}
	parsedName, _ := parseSkillHeader(string(content))
	if name == "" {
		name = parsedName
	}
	if name == "" {
		name = filepath.Base(absSource)
	}
	if !validName(name) {
		return ImportResult{}, fmt.Errorf("invalid skill name")
	}
	if parsedName != "" && parsedName != name {
		return ImportResult{}, fmt.Errorf("SKILL.md name %q does not match target name %q", parsedName, name)
	}
	dest := filepath.Join(r.root, name)
	if err := ensureSafeImportPaths(absSource, dest); err != nil {
		return ImportResult{}, err
	}
	if err := replaceDir(absSource, dest); err != nil {
		return ImportResult{}, err
	}
	if err := r.reloadLocked(ctx); err != nil {
		return ImportResult{}, err
	}
	check := r.manager.Check(name)
	if err := r.saveWorkflowCheckLocked(ctx, name, false, check); err != nil {
		return ImportResult{}, err
	}
	return ImportResult{Name: name, Path: dest, Check: check}, nil
}

func isRemoteSource(source string) bool {
	if strings.HasPrefix(source, "git@") {
		return true
	}
	u, err := url.Parse(source)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https" || u.Scheme == "ssh")
}

func ensureSafeImportPaths(src, dst string) error {
	absSrc, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	absDst, err := filepath.Abs(dst)
	if err != nil {
		return err
	}
	absSrc = filepath.Clean(absSrc)
	absDst = filepath.Clean(absDst)
	if absSrc == absDst {
		return fmt.Errorf("source is already installed at destination")
	}
	if pathContains(absSrc, absDst) || pathContains(absDst, absSrc) {
		return fmt.Errorf("source and destination directories must not contain each other")
	}
	return nil
}

func pathContains(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil || rel == "." {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func replaceDir(src, dst string) error {
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil || rel == "." {
			return err
		}
		// Git 元数据不属于 Skill 内容，避免导入后污染上下文和静态检查结果。
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
}

func unzip(src, dst string) error {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer zr.Close()
	files := 0
	total := uint64(0)
	for _, f := range zr.File {
		cleanName := filepath.Clean(f.Name)
		if strings.HasPrefix(cleanName, "..") || filepath.IsAbs(cleanName) {
			return fmt.Errorf("zip contains unsafe path: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			target := filepath.Join(dst, cleanName)
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
			continue
		}
		files++
		if files > maxImportZipFiles {
			return fmt.Errorf("zip contains too many files (max %d)", maxImportZipFiles)
		}
		if f.UncompressedSize64 > maxImportZipFileBytes {
			return fmt.Errorf("zip file %s is too large (max %d bytes)", f.Name, maxImportZipFileBytes)
		}
		total += f.UncompressedSize64
		if total > maxImportZipTotalBytes {
			return fmt.Errorf("zip content is too large (max %d bytes)", maxImportZipTotalBytes)
		}
		target := filepath.Join(dst, cleanName)
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(io.LimitReader(rc, int64(maxImportZipFileBytes)+1))
		closeErr := rc.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		if len(data) > maxImportZipFileBytes {
			return fmt.Errorf("zip file %s exceeds read limit", f.Name)
		}
		if err := os.WriteFile(target, data, f.FileInfo().Mode().Perm()); err != nil {
			return err
		}
	}
	return nil
}
