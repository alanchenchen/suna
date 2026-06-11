package builtin

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/alanchenchen/suna/internal/tools"
)

const tailReadBlockSize = 32 * 1024

func readTailText(path string, tail int) tools.Result {
	if tail > maxReadLineLimit {
		tail = maxReadLineLimit
	}
	if tail <= 0 {
		return tools.TextResult("")
	}
	file, err := os.Open(path)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("open file: %s", err))
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("stat file: %s", err))
	}
	data, err := readTailBytes(file, info.Size(), tail)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("read file tail: %s", err))
	}
	text := strings.TrimRight(string(data), "\n")
	if text == "" {
		return tools.TextResult("")
	}
	lines := strings.Split(text, "\n")
	if len(lines) > tail {
		lines = lines[len(lines)-tail:]
	}
	startLine := estimateTailStartLine(file, info.Size(), len(lines))
	var sb strings.Builder
	truncated := false
	for i, line := range lines {
		entry := fmt.Sprintf("%d: %s\n", startLine+i, line)
		if sb.Len()+len(entry) > maxReadResultBytes {
			truncated = true
			break
		}
		sb.WriteString(entry)
	}
	if truncated {
		sb.WriteString(fmt.Sprintf("\n... (truncated. tail_lines capped at %d lines and %d bytes per result)", maxReadLineLimit, maxReadResultBytes))
	}
	return tools.Result{Content: sb.String(), Truncated: truncated}
}

func readTailBytes(file *os.File, size int64, tail int) ([]byte, error) {
	var chunks [][]byte
	var total int
	lineBreaks := 0
	for pos := size; pos > 0 && lineBreaks <= tail; {
		readSize := tailReadBlockSize
		if pos < int64(readSize) {
			readSize = int(pos)
		}
		pos -= int64(readSize)
		buf := make([]byte, readSize)
		if _, err := file.ReadAt(buf, pos); err != nil && err != io.EOF {
			return nil, err
		}
		chunks = append(chunks, buf)
		total += len(buf)
		lineBreaks += strings.Count(string(buf), "\n")
	}
	data := make([]byte, 0, total)
	for i := len(chunks) - 1; i >= 0; i-- {
		data = append(data, chunks[i]...)
	}
	return data, nil
}

func estimateTailStartLine(file *os.File, size int64, tailLines int) int {
	if size <= 0 {
		return 1
	}
	// 为了避免 tail 读取再次全量扫描大文件，这里只在小文件上精确估算起始行号；大文件用 1 兜底展示。
	if size > maxReadResultBytes*4 {
		return 1
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return 1
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return 1
	}
	total := strings.Count(strings.TrimRight(string(data), "\n"), "\n") + 1
	start := total - tailLines + 1
	if start < 1 {
		return 1
	}
	return start
}
