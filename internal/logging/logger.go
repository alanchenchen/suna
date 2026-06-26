package logging

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	maxLogSizeBytes    = 10 * 1024 * 1024
	retainLogSizeBytes = 5 * 1024 * 1024
)

type Event map[string]any

var defaultLogger = &Logger{}

type Logger struct {
	mu  sync.Mutex
	dir string
}

func Init(dataDir string) {
	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()
	defaultLogger.dir = filepath.Join(dataDir, "logs")
	_ = os.MkdirAll(defaultLogger.dir, 0755)
	file, err := os.OpenFile(filepath.Join(defaultLogger.dir, "app.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		log.SetOutput(file)
		log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	}
}

func Info(category, event string, fields Event) {
	write("INFO", category, event, fields)
}

func Error(category, event string, err error, fields Event) {
	if fields == nil {
		fields = Event{}
	}
	if err != nil {
		fields["err"] = err.Error()
	}
	write("ERROR", category, event, fields)
}

func write(level, category, event string, fields Event) {
	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()
	if defaultLogger.dir == "" {
		return
	}
	category = normalizeCategory(category)
	path := filepath.Join(defaultLogger.dir, category+".log")
	defaultLogger.truncateIfNeeded(path)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	line := formatLine(level, category, event, fields)
	_, _ = f.WriteString(line + "\n")
}

func formatLine(level, category, event string, fields Event) string {
	parts := []string{
		time.Now().Format("2006-01-02 15:04:05.000"),
		level,
		event,
	}
	for _, key := range sortedKeys(fields) {
		parts = append(parts, key+"="+quoteValue(fields[key]))
	}
	return strings.Join(parts, " ")
}

func sortedKeys(fields Event) []string {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		if strings.TrimSpace(key) != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func quoteValue(v any) string {
	s := strings.TrimSpace(fmt.Sprintf("%v", v))
	if s == "" {
		return `""`
	}
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	if strings.ContainsAny(s, " \t=\"") {
		s = strings.ReplaceAll(s, `"`, `\"`)
		return `"` + s + `"`
	}
	return s
}

func (l *Logger) truncateIfNeeded(path string) {
	compactLogFileIfNeeded(path, maxLogSizeBytes, retainLogSizeBytes)
}

func compactLogFileIfNeeded(path string, maxSize, retainSize int64) {
	if maxSize <= 0 || retainSize <= 0 {
		return
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() < maxSize {
		return
	}
	if retainSize > maxSize {
		retainSize = maxSize
	}
	start := info.Size() - retainSize
	if start < 0 {
		start = 0
	}
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return
	}
	// 从换行后开始保留，避免日志文件开头出现半行；如果只有一条超长行，则保留尾部内容。
	if start > 0 {
		if idx := bytes.IndexByte(data, '\n'); idx >= 0 && idx+1 < len(data) {
			data = data[idx+1:]
		}
	}
	_ = os.WriteFile(path, data, 0644)
}

func normalizeCategory(category string) string {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "llm", "ipc", "memory", "agent", "config", "app", "perf":
		return strings.ToLower(strings.TrimSpace(category))
	default:
		return "app"
	}
}
