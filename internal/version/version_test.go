package version

import "testing"

func TestCurrentUsesInjectedBuildVersion(t *testing.T) {
	old := BuildVersion
	t.Cleanup(func() { BuildVersion = old })
	for _, tt := range []struct {
		name    string
		version string
	}{
		{name: "release", version: "v9.9.9"},
		{name: "development", version: "dev+1234567"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			BuildVersion = tt.version
			if got := Current(); got != tt.version {
				t.Fatalf("Current() = %q, want %q", got, tt.version)
			}
			if got, want := UserAgent(), "Suna/"+tt.version; got != want {
				t.Fatalf("UserAgent() = %q, want %q", got, want)
			}
		})
	}
}
