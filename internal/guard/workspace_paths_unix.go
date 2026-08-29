//go:build !windows

package guard

import "regexp"

var execPathTokenPattern = regexp.MustCompile(`(?:^|[\s=])["']?((?:~(?:/|$)|\.\.?(?:/|$)|/|[^"'\s;|&<>]+/\.\.?/)[^"'\s;|&<>]*)`)
var execQuotedAbsPathPattern = regexp.MustCompile(`["'](/[^"']*)["']`)

// execQuotedPathTokenPattern 匹配引号内空格后的绝对路径 token，
// 覆盖 printf '%s' "mentions /tmp here" 这类引号文本里的路径；
// 不匹配变量展开（$HOME/.ssh）和普通文本（"GLOBAL / PROJECT" 的 / 后是空格）。
var execQuotedPathTokenPattern = regexp.MustCompile(`["'][^"']*\s(/[^"'\s][^"'\s]*)`)

// execQuotedStandaloneSlashPattern 匹配引号内的独立斜杠（/ 前后是空格或引号边界），
// 仅当命令含解释器时启用（解释器可能执行引号内容，独立斜杠不豁免）。
var execQuotedStandaloneSlashPattern = regexp.MustCompile(`["'][^"']*\s(/)(?:\s|["'])`)
