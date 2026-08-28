//go:build windows

package guard

import "regexp"

// Windows 同时接受原生 drive 路径和 shell 中常见的正斜杠相对路径。
var execPathTokenPattern = regexp.MustCompile(`(?:^|[\s=])["']?((?:~(?:[/\\]|$)|\.\.?(?:[/\\]|$)|[A-Za-z]:\\|[^"'\s;|&<>]+[/\\]\.\.?(?:[/\\]|$))[^"'\s;|&<>]*)`)
var execQuotedAbsPathPattern = regexp.MustCompile(`["']([A-Za-z]:\\[^"']*)["']`)

// execQuotedPathTokenPattern 匹配引号内（引号前可有空格）的绝对路径（POSIX 侧同构定义）。
var execQuotedPathTokenPattern = regexp.MustCompile(`["'][^"']*\s(/[^"'\s][^"'\s]*)`)

// execQuotedStandaloneSlashPattern 匹配引号内的独立斜杠（POSIX 侧同构定义）。
var execQuotedStandaloneSlashPattern = regexp.MustCompile(`["'][^"']*\s(/)(?:\s|["'])`)
