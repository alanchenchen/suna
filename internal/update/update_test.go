package update

import (
	"strings"
	"testing"
)

func TestParseChecksumFindsNamedAsset(t *testing.T) {
	got, err := parseChecksum("abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd  suna-darwin-arm64.zip\n", "suna-darwin-arm64.zip")
	if err != nil {
		t.Fatalf("parseChecksum error: %v", err)
	}
	want := "abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestShouldUpdateUsesSemverAndDevBuilds(t *testing.T) {
	tests := []struct {
		name    string
		current string
		latest  string
		want    bool
	}{
		{name: "older", current: "v0.3.0", latest: "v0.3.1", want: true},
		{name: "same", current: "v0.3.1", latest: "v0.3.1", want: false},
		{name: "newer", current: "v0.4.0", latest: "v0.3.1", want: false},
		{name: "dev", current: "dev+abc123", latest: "v0.3.1", want: true},
		{name: "legacy date version", current: "2026-06-15", latest: "v0.3.1", want: true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldUpdate(tt.current, tt.latest); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFindAssetReportsAvailableNames(t *testing.T) {
	_, err := findAsset(release{TagName: "v0.3.0", Assets: []asset{{Name: "checksums.txt"}, {Name: "suna-linux-amd64.tar.gz"}}}, "suna-darwin-arm64.zip")
	if err == nil {
		t.Fatal("expected error")
	}
	for _, part := range []string{"suna-darwin-arm64.zip", "checksums.txt", "suna-linux-amd64.tar.gz"} {
		if !strings.Contains(err.Error(), part) {
			t.Fatalf("error %q missing %q", err.Error(), part)
		}
	}
}

func TestReleaseTagFromURL(t *testing.T) {
	got := releaseTagFromURL("https://github.com/alanchenchen/suna/releases/tag/v0.3.0?foo=bar")
	if got != "v0.3.0" {
		t.Fatalf("got %q, want v0.3.0", got)
	}
}

func TestReleaseAssetsForTagUsesFixedReleaseNaming(t *testing.T) {
	rel := release{TagName: "v0.3.0", Assets: releaseAssetsForTag("v0.3.0")}
	asset, err := findAsset(rel, "suna-darwin-arm64.zip")
	if err != nil {
		t.Fatalf("findAsset error: %v", err)
	}
	want := "https://github.com/alanchenchen/suna/releases/download/v0.3.0/suna-darwin-arm64.zip"
	if asset.URL != want {
		t.Fatalf("got %q, want %q", asset.URL, want)
	}
}
