package memory

import (
	"strings"
	"testing"
)

func TestFormatToolSummaryCompactsManyOperations(t *testing.T) {
	items := []ToolSummaryItem{
		{Name: "readfile", Status: "success", Summary: "读取 A"},
		{Name: "search", Status: "success", Summary: "搜索 B"},
		{Name: "editfile", Status: "success", Summary: "修改 C"},
		{Name: "readfile", Status: "success", Summary: "读取 D"},
		{Name: "filesystem", Status: "success", Summary: "移动 E"},
		{Name: "exec", Status: "error", Summary: "go test ./... 超时，输出很长很长很长很长很长很长很长很长很长很长很长很长"},
	}

	got := FormatToolSummary(items)
	checks := []string{
		"上一轮工具操作摘要：",
		"6 次 · 5 成功 / 1 失败",
		"失败：exec · go test",
		"变更：editfile ×1，filesystem ×1",
		"最近：editfile → readfile → filesystem → exec",
		"已折叠 2 次较早操作",
	}
	for _, want := range checks {
		if !strings.Contains(got, want) {
			t.Fatalf("FormatToolSummary() = %q, want %q", got, want)
		}
	}
	if strings.Contains(got, "读取 A") || strings.Contains(got, "搜索 B") {
		t.Fatalf("FormatToolSummary() = %q, should not render full early tool log", got)
	}
}

func TestFormatToolSummaryShowsSmallOperationSet(t *testing.T) {
	got := FormatToolSummary([]ToolSummaryItem{
		{Name: "readfile", Status: "success", Summary: "读取输入区渲染逻辑"},
		{Name: "editfile", Status: "success", Summary: "调整高度计算"},
	})
	checks := []string{"2 次 · 全部成功", "变更：editfile ×1", "最近：readfile → editfile"}
	for _, want := range checks {
		if !strings.Contains(got, want) {
			t.Fatalf("FormatToolSummary() = %q, want %q", got, want)
		}
	}
	if strings.Contains(got, "已折叠") {
		t.Fatalf("FormatToolSummary() = %q, should not fold small operation set", got)
	}
}
