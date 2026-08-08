//go:build windows

package guard

import "regexp"

// Windows 同时接受原生 drive 路径和 shell 中常见的正斜杠相对路径。
var execPathTokenPattern = regexp.MustCompile(`(?:^|[\s=])["']?((?:~(?:[/\\]|$)|\.\.?(?:[/\\]|$)|[A-Za-z]:\\|[^"'\s;|&<>]+[/\\]\.\.?(?:[/\\]|$))[^"'\s;|&<>]*)`)
var execQuotedAbsPathPattern = regexp.MustCompile(`["']([A-Za-z]:\\[^"']*)["']`)
