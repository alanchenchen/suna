//go:build !windows

package guard

import "regexp"

var execPathTokenPattern = regexp.MustCompile(`(?:^|[\s=])["']?((?:~(?:/|$)|\.\.?(?:/|$)|/|[^"'\s;|&<>]+/\.\.?/)[^"'\s;|&<>]*)`)
var execQuotedAbsPathPattern = regexp.MustCompile(`["'](/[^"']*)["']`)
