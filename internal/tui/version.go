package tui

import "github.com/alanchenchen/suna/internal/version"

func appVersion() string {
	return version.Current()
}
