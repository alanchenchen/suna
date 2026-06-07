package builtin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListDirPaginatesLargeDirectory(t *testing.T) {
	dir := t.TempDir()
	for i := 1; i <= 25; i++ {
		path := filepath.Join(dir, fmt.Sprintf("file-%02d.txt", i))
		if err := os.WriteFile(path, []byte("x"), 0644); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", path, err)
		}
	}

	res := ListDir{}.Execute(context.Background(), map[string]any{"path": dir, "offset": float64(11), "limit": float64(5)})
	if res.IsError {
		t.Fatalf("ListDir.Execute() error = %s", res.Error)
	}
	if !strings.Contains(res.Content, "file-11.txt") || !strings.Contains(res.Content, "file-15.txt") {
		t.Fatalf("ListDir.Execute().Content = %q, want paged entries file-11.txt through file-15.txt", res.Content)
	}
	if strings.Contains(res.Content, "file-10.txt") || strings.Contains(res.Content, "file-16.txt") {
		t.Fatalf("ListDir.Execute().Content = %q, should not contain entries outside requested page", res.Content)
	}
	if !res.Truncated {
		t.Fatalf("ListDir.Execute().Truncated = false, want true")
	}
	if !strings.Contains(res.Content, "Use offset=16") {
		t.Fatalf("ListDir.Execute().Content = %q, want continuation hint", res.Content)
	}
}
