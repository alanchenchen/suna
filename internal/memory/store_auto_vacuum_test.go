package memory

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestNewStoreEnablesFullAutoVacuum(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	var mode int
	if err := store.DB().QueryRow(`PRAGMA auto_vacuum`).Scan(&mode); err != nil {
		t.Fatalf("read auto_vacuum error = %v", err)
	}
	if got, want := mode, autoVacuumFull; got != want {
		t.Fatalf("auto_vacuum = %d, want %d", got, want)
	}
}

func TestNewStoreConvertsLegacyDatabaseAndKeepsRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")

	// 先用当前 schema 建库，再降级成 auto_vacuum=NONE，模拟升级前的老库文件。
	seed, err := NewStore(path)
	if err != nil {
		t.Fatalf("seed NewStore() error = %v", err)
	}
	if _, err := seed.DB().Exec(`INSERT INTO sessions (id, title, cwd, message_count, created_at, updated_at) VALUES ('legacy-session', 'legacy', '/tmp', 0, ?, ?)`, dbTime(time.Now()), dbTime(time.Now())); err != nil {
		t.Fatalf("insert legacy session error = %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("close seed store error = %v", err)
	}
	downgradeToNoAutoVacuum(t, path)

	// 重新打开时应当转换到 FULL，并保留既有数据。
	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	var mode int
	if err := store.DB().QueryRow(`PRAGMA auto_vacuum`).Scan(&mode); err != nil {
		t.Fatalf("read auto_vacuum error = %v", err)
	}
	if got, want := mode, autoVacuumFull; got != want {
		t.Fatalf("auto_vacuum = %d, want %d", got, want)
	}
	var title string
	if err := store.DB().QueryRow(`SELECT title FROM sessions WHERE id = 'legacy-session'`).Scan(&title); err != nil {
		t.Fatalf("read migrated session error = %v", err)
	}
	if got, want := title, "legacy"; got != want {
		t.Fatalf("title = %q, want %q", got, want)
	}
}

// downgradeToNoAutoVacuum 用 VACUUM 把库文件切换回 auto_vacuum=NONE。
func downgradeToNoAutoVacuum(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=auto_vacuum(0)")
	if err != nil {
		t.Fatalf("open downgrade database error = %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`VACUUM`); err != nil {
		t.Fatalf("downgrade vacuum error = %v", err)
	}
	var mode int
	if err := db.QueryRow(`PRAGMA auto_vacuum`).Scan(&mode); err != nil {
		t.Fatalf("read downgraded auto_vacuum error = %v", err)
	}
	if mode == autoVacuumFull {
		t.Fatalf("downgraded auto_vacuum = %d, want non-full", mode)
	}
}
