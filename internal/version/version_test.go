package version

import "testing"

func TestCurrentUsesInjectedBuildVersion(t *testing.T) {
	old := BuildVersion
	BuildVersion = "v9.9.9"
	t.Cleanup(func() { BuildVersion = old })

	if got := Current(); got != "v9.9.9" {
		t.Fatalf("got %q, want %q", got, "v9.9.9")
	}
}
