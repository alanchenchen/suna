package memory

import "testing"

func TestToolSummaryKeepsBoundedRecentAndFailures(t *testing.T) {
	summary := BuildToolSummary([]ToolSummaryItem{
		{Name: "readfile", Status: "success", Summary: "读取 A"},
		{Name: "search", Status: "success", Summary: "搜索 B"},
		{Name: "editfile", Status: "success", Summary: "修改 C"},
		{Name: "readfile", Status: "success", Summary: "读取 D"},
		{Name: "filesystem", Status: "success", Summary: "移动 E"},
		{Name: "exec", Status: "error", Summary: "go test ./... 超时，输出很长很长很长很长很长很长很长很长很长很长很长很长"},
	})

	if got, want := summary.Total, 6; got != want {
		t.Fatalf("total = %d, want %d", got, want)
	}
	if got, want := summary.Success, 5; got != want {
		t.Fatalf("success = %d, want %d", got, want)
	}
	if got, want := summary.Failed, 1; got != want {
		t.Fatalf("failed = %d, want %d", got, want)
	}
	if got, want := len(summary.Recent), 4; got != want {
		t.Fatalf("recent len = %d, want %d", got, want)
	}
	if got, want := summary.Recent[0].Name, "editfile"; got != want {
		t.Fatalf("first recent = %q, want %q", got, want)
	}
	if got, want := len(summary.Failures), 1; got != want {
		t.Fatalf("failures len = %d, want %d", got, want)
	}
	if got, want := summary.Omitted, 2; got != want {
		t.Fatalf("omitted = %d, want %d", got, want)
	}
	if got, want := len(summary.Changes), 2; got != want {
		t.Fatalf("changes len = %d, want %d", got, want)
	}
}

func TestDecodeToolSummaryAcceptsOldArray(t *testing.T) {
	raw := `[{"name":"readfile","status":"success","summary":"a"},{"name":"exec","status":"error","summary":"b"}]`
	summary := decodeToolSummary(raw)
	if got, want := summary.Total, 2; got != want {
		t.Fatalf("total = %d, want %d", got, want)
	}
	if got, want := summary.Failed, 1; got != want {
		t.Fatalf("failed = %d, want %d", got, want)
	}
}

func TestDecodeToolSummaryIgnoresInvalidData(t *testing.T) {
	summary := decodeToolSummary(`not-json`)
	if !summary.Empty() {
		t.Fatalf("summary = %+v, want empty", summary)
	}
}
