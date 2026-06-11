package builtin

import "time"

const (
	maxHTTPBodySize = 100 * 1024
	httpTimeout     = 30 * time.Second
	maxRedirects    = 5
)
