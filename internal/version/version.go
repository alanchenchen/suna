package version

import "runtime/debug"

// BuildVersion 由 release 构建脚本通过 ldflags 注入；go install 场景无法注入时，Current 会读取 Go build info 的 module version。
var BuildVersion = "dev"

func Current() string {
	if BuildVersion != "" && BuildVersion != "dev" {
		return BuildVersion
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return BuildVersion
}
