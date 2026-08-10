package daemon

import (
	"os"
	"strconv"
	"testing"

	"github.com/alanchenchen/suna/internal/config"
)

func TestRemovePIDOnlyRemovesCurrentProcessFile(t *testing.T) {
	cfg := &config.Config{DataDir: t.TempDir()}
	d := &Daemon{cfg: cfg}
	if err := os.WriteFile(cfg.PIDPath(), []byte("999999"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	d.removePID()
	if _, err := os.Stat(cfg.PIDPath()); err != nil {
		t.Fatalf("foreign PID file was removed: %v", err)
	}

	if err := os.WriteFile(cfg.PIDPath(), []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		t.Fatalf("WriteFile() current PID error = %v", err)
	}
	d.removePID()
	if _, err := os.Stat(cfg.PIDPath()); !os.IsNotExist(err) {
		t.Fatalf("current PID file still exists, error = %v", err)
	}
}

func TestWritePIDPublishesCurrentProcess(t *testing.T) {
	cfg := &config.Config{DataDir: t.TempDir()}
	d := &Daemon{cfg: cfg}
	if err := d.writePID(); err != nil {
		t.Fatalf("writePID() error = %v", err)
	}
	data, err := os.ReadFile(cfg.PIDPath())
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got, want := string(data), strconv.Itoa(os.Getpid()); got != want {
		t.Fatalf("PID file = %q, want %q", got, want)
	}
}
