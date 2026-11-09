package memory

import (
	"fmt"
	"strings"
	"time"
)

// 固定九位小数使文本顺序等于时间顺序；不能用会省略尾零的 RFC3339Nano 写入。
const dbTimeLayout = "2006-01-02T15:04:05.000000000Z"

// SQLite 的 now 精确到毫秒，补足尾零以匹配 Go 写入的纳秒格式。
const dbTimeDefault = "strftime('%Y-%m-%dT%H:%M:%f', 'now') || '000000Z'"

func dbTime(t time.Time) string { return t.UTC().Format(dbTimeLayout) }

func parseDBTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t.UTC()
}

// 只在升级时兼容旧驱动的 time.String 和 SQLite UTC 文本；数字偏移优先于时区简称。
// 无偏移的旧值来自 SQLite 日期函数，按 UTC 解释，绝不套用当前机器的本地时区。
func decodeDBTime(s string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05.999999999-07:00", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	wall, _, _ := strings.Cut(s, " m=")
	fields := strings.Fields(wall)
	if len(fields) >= 4 {
		if t, err := time.Parse("2006-01-02 15:04:05.999999999 -0700", strings.Join(fields[:3], " ")); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported database timestamp")
}

func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return dbTime(t)
}
