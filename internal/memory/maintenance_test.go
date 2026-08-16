package memory

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestPruneOperationalLogsKeepsRecentRowsAndSessions(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	now := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.FixedZone("test", 8*60*60))
	oldLocal := now.Add(-OperationalLogRetention - time.Second)
	boundaryLocal := now.Add(-OperationalLogRetention)
	oldUTC := oldLocal.UTC().Format("2006-01-02 15:04:05")
	boundaryUTC := boundaryLocal.UTC().Format("2006-01-02 15:04:05")

	for _, row := range []struct {
		id        string
		timestamp string
	}{
		{id: "audit-old", timestamp: oldUTC},
		{id: "audit-boundary", timestamp: boundaryUTC},
		{id: "audit-recent", timestamp: now.UTC().Format("2006-01-02 15:04:05")},
	} {
		if _, err := store.DB().ExecContext(ctx, `INSERT INTO audit_log (id, timestamp, tool) VALUES (?, ?, 'exec')`, row.id, row.timestamp); err != nil {
			t.Fatalf("insert audit row %q error = %v", row.id, err)
		}
	}
	for _, createdAt := range []time.Time{oldLocal, boundaryLocal, now} {
		if _, err := store.DB().ExecContext(ctx, `INSERT INTO usage_log (model, created_at) VALUES ('test-model', ?)`, createdAt); err != nil {
			t.Fatalf("insert usage row error = %v", err)
		}
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO sessions (id, title, cwd, message_count, created_at, updated_at) VALUES ('session-old', 'keep', '.', 1, ?, ?)`, oldLocal, oldLocal); err != nil {
		t.Fatalf("insert session error = %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `INSERT INTO session_state (session_id, compacted_state, updated_at) VALUES ('session-old', 'keep state', ?)`, oldLocal); err != nil {
		t.Fatalf("insert session state error = %v", err)
	}

	result, err := store.PruneOperationalLogs(ctx, now)
	if err != nil {
		t.Fatalf("PruneOperationalLogs() error = %v", err)
	}
	if result.AuditRows != 1 || result.UsageRows != 1 {
		t.Fatalf("PruneOperationalLogs() = %+v, want one audit and one usage row", result)
	}
	assertTableCount(t, store, "audit_log", 2)
	assertTableCount(t, store, "usage_log", 2)
	assertTableCount(t, store, "sessions", 1)
	assertTableCount(t, store, "session_state", 1)
}

func TestOperationalLogTimeIndexesExist(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	for _, name := range []string{"idx_audit_log_timestamp", "idx_usage_log_created_at"} {
		var count int
		if err := store.DB().QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, name).Scan(&count); err != nil {
			t.Fatalf("query index %q error = %v", name, err)
		}
		if count != 1 {
			t.Fatalf("index %q count = %d, want 1", name, count)
		}
	}
}

func assertTableCount(t *testing.T, store *Store, table string, want int) {
	t.Helper()
	var got int
	if err := store.DB().QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&got); err != nil {
		t.Fatalf("count %s error = %v", table, err)
	}
	if got != want {
		t.Fatalf("count %s = %d, want %d", table, got, want)
	}
}
