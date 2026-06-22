package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/alanchenchen/suna/internal/version"
)

const (
	githubLatestReleaseURL         = "https://api.github.com/repos/alanchenchen/suna/releases/latest"
	githubLatestReleaseRedirectURL = "https://github.com/alanchenchen/suna/releases/latest"
	githubReleaseDownloadBaseURL   = "https://github.com/alanchenchen/suna/releases/download"
	githubReleaseTagBaseURL        = "https://github.com/alanchenchen/suna/releases/tag"
	checksumsAssetName             = "checksums.txt"
)

type Options struct {
	DataDir string
	Stdout  io.Writer
}

type Latest struct {
	CurrentVersion string
	LatestVersion  string
	ReleaseURL     string
	UpdateNeeded   bool
}

type release struct {
	TagName string  `json:"tag_name"`
	HTMLURL string  `json:"html_url"`
	Assets  []asset `json:"assets"`
}

type asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

func Check(ctx context.Context, opts Options) (Latest, error) {
	rel, err := fetchLatestRelease(ctx)
	if err != nil {
		return Latest{}, err
	}
	current := version.Current()
	return Latest{
		CurrentVersion: current,
		LatestVersion:  rel.TagName,
		ReleaseURL:     rel.HTMLURL,
		UpdateNeeded:   shouldUpdate(current, rel.TagName),
	}, nil
}

func Install(ctx context.Context, opts Options) (Latest, error) {
	out := opts.Stdout
	if out == nil {
		out = io.Discard
	}
	rel, err := fetchLatestRelease(ctx)
	if err != nil {
		return Latest{}, err
	}
	current := version.Current()
	latest := Latest{
		CurrentVersion: current,
		LatestVersion:  rel.TagName,
		ReleaseURL:     rel.HTMLURL,
		UpdateNeeded:   shouldUpdate(current, rel.TagName),
	}
	if !latest.UpdateNeeded {
		return latest, nil
	}

	cacheRoot := updateCacheRoot(opts.DataDir)
	// 更新缓存由 Suna 自己管理：每次开始前清旧目录，结束后再清本次目录，避免长期占空间。
	_ = os.RemoveAll(cacheRoot)
	if err := os.MkdirAll(cacheRoot, 0o755); err != nil {
		return latest, fmt.Errorf("create update cache: %w", err)
	}
	defer os.RemoveAll(cacheRoot)

	archive, err := selectPlatformAsset(rel)
	if err != nil {
		return latest, err
	}
	checksumAsset, err := findAsset(rel, checksumsAssetName)
	if err != nil {
		return latest, err
	}

	fmt.Fprintf(out, "Downloading %s...\n", archive.Name)
	archivePath := filepath.Join(cacheRoot, archive.Name)
	if err := downloadFile(ctx, archive.URL, archivePath); err != nil {
		return latest, err
	}

	fmt.Fprintf(out, "Downloading %s...\n", checksumAsset.Name)
	checksumPath := filepath.Join(cacheRoot, checksumAsset.Name)
	if err := downloadFile(ctx, checksumAsset.URL, checksumPath); err != nil {
		return latest, err
	}
	if err := verifyChecksum(archivePath, checksumPath, archive.Name); err != nil {
		return latest, err
	}
	fmt.Fprintln(out, "SHA256 checksum verified.")

	extractedPath, err := extractSunaBinary(archivePath, cacheRoot)
	if err != nil {
		return latest, err
	}
	target, err := currentExecutablePath()
	if err != nil {
		return latest, err
	}
	if err := replaceBinary(target, extractedPath); err != nil {
		return latest, err
	}
	return latest, nil
}

func fetchLatestRelease(ctx context.Context) (release, error) {
	rel, err := fetchLatestReleaseAPI(ctx)
	if err == nil {
		return rel, nil
	}
	fallback, fallbackErr := fetchLatestReleaseRedirect(ctx)
	if fallbackErr == nil {
		return fallback, nil
	}
	return release{}, fmt.Errorf("fetch latest release: %w; fallback: %w", err, fallbackErr)
}

func fetchLatestReleaseAPI(ctx context.Context) (release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubLatestReleaseURL, nil)
	if err != nil {
		return release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "suna-update")
	client := http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return release{}, fmt.Errorf("GitHub API HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var rel release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return release{}, fmt.Errorf("decode latest release: %w", err)
	}
	if rel.TagName == "" {
		return release{}, errors.New("latest release has empty tag")
	}
	return rel, nil
}

func fetchLatestReleaseRedirect(ctx context.Context) (release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, githubLatestReleaseRedirectURL, nil)
	if err != nil {
		return release{}, err
	}
	req.Header.Set("User-Agent", "suna-update")
	client := http.Client{
		Timeout: 60 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return release{}, err
	}
	defer resp.Body.Close()
	location := resp.Header.Get("Location")
	if location == "" && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		location = resp.Request.URL.String()
	}
	tag := releaseTagFromURL(location)
	if tag == "" {
		return release{}, fmt.Errorf("latest release redirect did not include a tag: HTTP %d", resp.StatusCode)
	}
	assets := releaseAssetsForTag(tag)
	return release{
		TagName: tag,
		HTMLURL: githubReleaseTagBaseURL + "/" + tag,
		Assets:  assets,
	}, nil
}

func releaseTagFromURL(raw string) string {
	idx := strings.LastIndex(raw, "/releases/tag/")
	if idx < 0 {
		return ""
	}
	tag := raw[idx+len("/releases/tag/"):]
	if cut := strings.IndexAny(tag, "?#"); cut >= 0 {
		tag = tag[:cut]
	}
	return strings.TrimSpace(tag)
}

func releaseAssetsForTag(tag string) []asset {
	names := []string{
		"suna-darwin-amd64.zip",
		"suna-darwin-arm64.zip",
		"suna-linux-amd64.tar.gz",
		"suna-linux-arm64.tar.gz",
		"suna-windows-amd64.zip",
		"suna-windows-arm64.zip",
		checksumsAssetName,
	}
	assets := make([]asset, 0, len(names))
	for _, name := range names {
		assets = append(assets, asset{
			Name: name,
			URL:  githubReleaseDownloadBaseURL + "/" + tag + "/" + name,
		})
	}
	return assets
}

func selectPlatformAsset(rel release) (asset, error) {
	name := fmt.Sprintf("suna-%s-%s", runtime.GOOS, runtime.GOARCH)
	switch runtime.GOOS {
	case "darwin", "windows":
		name += ".zip"
	case "linux":
		name += ".tar.gz"
	default:
		return asset{}, fmt.Errorf("unsupported OS for update: %s", runtime.GOOS)
	}
	return findAsset(rel, name)
}

func findAsset(rel release, name string) (asset, error) {
	for _, a := range rel.Assets {
		if a.Name == name {
			return a, nil
		}
	}
	available := make([]string, 0, len(rel.Assets))
	for _, a := range rel.Assets {
		available = append(available, a.Name)
	}
	sort.Strings(available)
	return asset{}, fmt.Errorf("release %s does not contain %s; available assets: %s", rel.TagName, name, strings.Join(available, ", "))
}

func downloadFile(ctx context.Context, url, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "suna-update")
	client := http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("download %s: HTTP %d: %s", url, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(file, resp.Body)
	return err
}

func verifyChecksum(path, checksumPath, assetName string) error {
	data, err := os.ReadFile(checksumPath)
	if err != nil {
		return err
	}
	expected, err := parseChecksum(string(data), assetName)
	if err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return err
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", assetName, expected, actual)
	}
	return nil
}

func parseChecksum(text, assetName string) (string, error) {
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		if name == assetName {
			if len(fields[0]) != 64 {
				return "", fmt.Errorf("invalid checksum for %s", assetName)
			}
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("checksums.txt does not contain %s", assetName)
}

func extractSunaBinary(archivePath, destDir string) (string, error) {
	exeName := "suna"
	if runtime.GOOS == "windows" {
		exeName = "suna.exe"
	}
	switch {
	case strings.HasSuffix(archivePath, ".zip"):
		return extractZipBinary(archivePath, destDir, exeName)
	case strings.HasSuffix(archivePath, ".tar.gz"):
		return extractTarGzBinary(archivePath, destDir, exeName)
	default:
		return "", fmt.Errorf("unsupported archive format: %s", filepath.Base(archivePath))
	}
}

func extractZipBinary(archivePath, destDir, exeName string) (string, error) {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return "", err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if filepath.Base(f.Name) != exeName {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		defer rc.Close()
		return writeExtractedBinary(rc, destDir, exeName)
	}
	return "", fmt.Errorf("archive %s does not contain %s", filepath.Base(archivePath), exeName)
}

func extractTarGzBinary(archivePath, destDir, exeName string) (string, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return "", err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		if filepath.Base(hdr.Name) != exeName || hdr.Typeflag != tar.TypeReg {
			continue
		}
		return writeExtractedBinary(tr, destDir, exeName)
	}
	return "", fmt.Errorf("archive %s does not contain %s", filepath.Base(archivePath), exeName)
}

func writeExtractedBinary(r io.Reader, destDir, exeName string) (string, error) {
	path := filepath.Join(destDir, exeName)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(file, r); err != nil {
		file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	return path, nil
}

func currentExecutablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		return real, nil
	}
	return exe, nil
}

func replaceBinary(target, source string) error {
	parent := filepath.Dir(target)
	tmp, err := os.CreateTemp(parent, ".suna-update-*")
	if err != nil {
		return fmt.Errorf("create install temp file in %s: %w", parent, err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	src, err := os.Open(source)
	if err != nil {
		tmp.Close()
		return err
	}
	if _, err := io.Copy(tmp, src); err != nil {
		src.Close()
		tmp.Close()
		return err
	}
	src.Close()
	if err := tmp.Close(); err != nil {
		return err
	}
	if info, err := os.Stat(target); err == nil {
		_ = os.Chmod(tmpPath, info.Mode().Perm())
	} else if runtime.GOOS != "windows" {
		_ = os.Chmod(tmpPath, 0o755)
	}

	// Windows 通常不能覆盖正在运行的 exe；第一版保持简单，失败时给用户明确提示而不做 helper。
	if err := os.Rename(tmpPath, target); err != nil {
		return fmt.Errorf("replace %s: %w", target, err)
	}
	return nil
}

func updateCacheRoot(dataDir string) string {
	if strings.TrimSpace(dataDir) == "" {
		dataDir = filepath.Join(homeDir(), ".suna")
	}
	return filepath.Join(dataDir, "update")
}

func homeDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return "."
}

func shouldUpdate(current, latest string) bool {
	current = normalizeVersion(current)
	latest = normalizeVersion(latest)
	if latest == "" || current == latest {
		return false
	}
	if current == "" || strings.HasPrefix(current, "dev") {
		return true
	}
	// 迁移到 SemVer 后，旧的日期版本无法用 SemVer 比较；只要 latest 是正式 SemVer，就允许旧格式升级。
	if isSemverTag(latest) && !isSemverTag(current) {
		return true
	}
	return compareSemver(current, latest) < 0
}

func isSemverTag(v string) bool {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	v = strings.SplitN(v, "-", 2)[0]
	v = strings.SplitN(v, "+", 2)[0]
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "refs/tags/")
	return v
}

func compareSemver(a, b string) int {
	ap := semverParts(a)
	bp := semverParts(b)
	for i := 0; i < 3; i++ {
		if ap[i] < bp[i] {
			return -1
		}
		if ap[i] > bp[i] {
			return 1
		}
	}
	return strings.Compare(a, b)
}

func semverParts(v string) [3]int {
	v = strings.TrimPrefix(v, "v")
	v = strings.SplitN(v, "-", 2)[0]
	v = strings.SplitN(v, "+", 2)[0]
	parts := strings.Split(v, ".")
	var out [3]int
	for i := 0; i < len(parts) && i < 3; i++ {
		var n int
		for _, r := range parts[i] {
			if r < '0' || r > '9' {
				break
			}
			n = n*10 + int(r-'0')
		}
		out[i] = n
	}
	return out
}
