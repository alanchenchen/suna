package memory

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	_ "time/tzdata"
)

func TestDBTimePreservesInstantAndTextOrder(t *testing.T) {
	for _, tt := range []struct {
		name, raw, want string
	}{
		{"positive offset", "2026-01-01T00:30:00.123456789+08:00", "2025-12-31T16:30:00.123456789Z"},
		{"negative offset", "2026-01-01T23:30:00-05:00", "2026-01-02T04:30:00.000000000Z"},
		{"sqlite UTC", "2026-01-01 00:30:00", "2026-01-01T00:30:00.000000000Z"},
		{"sqlite offset", "2026-01-01 00:30:00.123+08:00", "2025-12-31T16:30:00.123000000Z"},
		{"date only UTC", "2026-01-01", "2026-01-01T00:00:00.000000000Z"},
		{"Go local", "2026-01-01 00:30:00.123456789 +0800 CST m=+0.123", "2025-12-31T16:30:00.123456789Z"},
		{"Go UTC", "2026-01-01 00:30:00 +0000 UTC", "2026-01-01T00:30:00.000000000Z"},
		{"Go Windows zone", "2026-01-01 00:30:00 -0500 Eastern Standard Time", "2026-01-01T05:30:00.000000000Z"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeDBTime(tt.raw)
			if err != nil || dbTime(got) != tt.want || got.Location() != time.UTC {
				t.Fatalf("decodeDBTime(%q) = %v, %v, want %s UTC", tt.raw, got, err, tt.want)
			}
		})
	}
	for _, raw := range []string{"", "not a time", "2026-01-01 12:00:00 CST", "2026-01-01T00:00:00+25:00"} {
		if _, err := decodeDBTime(raw); err == nil {
			t.Fatalf("decodeDBTime(%q) error = nil, want invalid timestamp", raw)
		}
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, delta := range []time.Duration{time.Nanosecond, time.Microsecond, time.Millisecond, time.Second} {
		if dbTime(base) >= dbTime(base.Add(delta)) {
			t.Fatalf("time text order changed for delta %s", delta)
		}
	}
}

// 只把有默认时间的表恢复到旧 DDL；保留同一份列定义，避免夹具与真实表脱节。
func legacyTimeDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "memory.db")
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, table := range timeTables {
		var ddl string
		if err := store.DB().QueryRow(`SELECT sql FROM sqlite_master WHERE name=?`, table.name).Scan(&ddl); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(ddl, dbTimeDefault) {
			continue
		}
		ddl = strings.ReplaceAll(ddl, dbTimeDefault, "datetime('now')")
		if _, err := store.DB().Exec("DROP TABLE " + table.name); err != nil {
			t.Fatal(err)
		}
		if _, err := store.DB().Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, path
}

func TestTimeMigrationPreservesAllValuesAndDefaults(t *testing.T) {
	db, path := legacyTimeDB(t)
	// 真实旧迁移会把 model_ref 追加到末尾，不能只测试与新建表相同的列顺序。
	if _, err := db.Exec(`ALTER TABLE memory_queue DROP COLUMN model_ref; ALTER TABLE memory_queue ADD COLUMN model_ref TEXT NOT NULL DEFAULT ''`); err != nil {
		t.Fatal(err)
	}
	const local = "2026-01-01 00:30:00.123456789 +0800 CST m=+0.123"
	const utc = "2025-12-31 16:30:00.123456789"
	const want = "2025-12-31T16:30:00.123456789Z"
	const history = `[{"role":"user","content":[{"type":"text","text":"at 00:30 +0800"}]}]`
	for _, row := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO sessions(id,title,cwd,model_ref,message_count,created_at,updated_at,last_attached_at) VALUES ('s','title','cwd','custom/model',2,?,?,?)`, []any{local, local, utc}},
		{`INSERT INTO session_state(session_id,summary,messages,updated_at) VALUES ('s','summary',?,?)`, []any{history, local}},
		{`INSERT INTO user_profile_memory(id,user_id,kind,content,last_used_at,expires_at,created_at,updated_at) VALUES ('m','default','preference','keep',NULL,?,?,?)`, []any{local, local, utc}},
		{`INSERT INTO memory_queue(id,user_id,model_ref,kind,content,created_at,next_attempt_at,processed_at) VALUES ('q','default','custom/model','preference','keep',?,?,NULL)`, []any{utc, local}},
		{`INSERT INTO usage_log(id,model,input_tokens,created_at) VALUES (42,'test',7,?)`, []any{local}},
		{`INSERT INTO audit_log(id,tool,params,timestamp) VALUES ('a','exec','{}',?)`, []any{utc}},
		{`INSERT INTO failure_records(id,pattern,operation,reason,created_at) VALUES (42,'p','op','reason',?)`, []any{utc}},
		{`INSERT INTO triggers(id,type,config_json,last_fire,created_at) VALUES ('t','test','{}',?,?)`, []any{local, utc}},
		{`INSERT INTO trust_rules(pattern,tool) VALUES ('keep','exec')`, nil},
		{`INSERT INTO usage_log(id,model,created_at) VALUES (99,'deleted',?)`, []any{utc}},
		{`DELETE FROM usage_log WHERE id=99`, nil},
		{`CREATE INDEX custom_audit_tool_time ON audit_log(tool,timestamp)`, nil},
		{`CREATE TRIGGER custom_usage_delete AFTER DELETE ON usage_log BEGIN SELECT 1; END`, nil},
	} {
		if _, err := db.Exec(row.query, row.args...); err != nil {
			t.Fatal(err)
		}
	}
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, table := range timeTables {
		for _, column := range table.columns {
			var value sql.NullString
			if err := store.DB().QueryRow("SELECT CAST(" + column + " AS TEXT) FROM " + table.name).Scan(&value); err != nil {
				t.Fatal(err)
			}
			if table.name == "user_profile_memory" && column == "last_used_at" || table.name == "memory_queue" && column == "processed_at" {
				if value.Valid {
					t.Fatalf("%s.%s = %q, want NULL", table.name, column, value.String)
				}
			} else if value.String != want {
				t.Fatalf("%s.%s = %q, want %q", table.name, column, value.String, want)
			}
		}
	}
	var gotHistory, summary string
	if err := store.DB().QueryRow(`SELECT messages,summary FROM session_state`).Scan(&gotHistory, &summary); err != nil || gotHistory != history || summary != "summary" {
		t.Fatalf("history/summary = %q/%q, %v, want original content", gotHistory, summary, err)
	}
	meta, err := NewSessionStore(store.DB()).Get(context.Background(), "s")
	if err != nil || meta.Title != "title" || meta.CWD != "cwd" || meta.ModelRef != "custom/model" || meta.MessageCount != 2 || dbTime(meta.UpdatedAt) != want {
		t.Fatalf("session = %+v, %v, want preserved metadata", meta, err)
	}
	var queueRef, queueContent string
	if err := store.DB().QueryRow(`SELECT model_ref,content FROM memory_queue WHERE id='q'`).Scan(&queueRef, &queueContent); err != nil || queueRef != "custom/model" || queueContent != "keep" {
		t.Fatalf("queue ref/content = %q/%q, %v, want preserved values", queueRef, queueContent, err)
	}
	var check string
	if err := store.DB().QueryRow(`PRAGMA quick_check`).Scan(&check); err != nil || check != "ok" {
		t.Fatalf("quick_check = %q, %v", check, err)
	}
	var sequence int
	if err := store.DB().QueryRow(`SELECT seq FROM sqlite_sequence WHERE name='usage_log'`).Scan(&sequence); err != nil || sequence != 99 {
		t.Fatalf("usage sequence = %d, %v, want 99", sequence, err)
	}
	for _, table := range timeTables {
		rows, err := store.DB().Query(`SELECT name,dflt_value FROM pragma_table_info(?) WHERE dflt_value IS NOT NULL`, table.name)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var name, value string
			if err := rows.Scan(&name, &value); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(value, "datetime('now')") {
				t.Fatalf("%s.%s still has legacy default", table.name, name)
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
	}
	for _, table := range []string{"usage_log", "audit_log", "user_profile_memory", "memory_queue"} {
		var count int
		if err := store.DB().QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='index' AND tbl_name=?`, table).Scan(&count); err != nil || count == 0 {
			t.Fatalf("%s indexes = %d, %v, want indexes retained", table, count, err)
		}
	}
	var customObjects int
	if err := store.DB().QueryRow(`SELECT count(*) FROM sqlite_master WHERE name IN ('custom_audit_tool_time','custom_usage_delete')`).Scan(&customObjects); err != nil || customObjects != 2 {
		t.Fatalf("custom objects = %d, %v, want index and trigger preserved", customObjects, err)
	}
	// 再次打开只检查结构，不应触发已有记录的更新。
	if _, err := store.DB().Exec(`CREATE TRIGGER forbid_time_rescan BEFORE UPDATE ON audit_log BEGIN SELECT RAISE(ABORT,'unexpected rescan'); END`); err != nil {
		t.Fatal(err)
	}
	if err := store.migrate(); err != nil {
		t.Fatalf("second migration = %v, want no-op", err)
	}
	if _, err := store.DB().Exec(`INSERT INTO usage_log(model) VALUES ('default')`); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := store.DB().QueryRow(`SELECT CAST(created_at AS TEXT) FROM usage_log WHERE model='default'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if parsed, err := time.Parse(dbTimeLayout, raw); err != nil || time.Since(parsed).Abs() > time.Minute {
		t.Fatalf("default time = %q, %v, want current fixed UTC", raw, err)
	}
}

func TestTimeMigrationRollsBackOnUnknownTimestamp(t *testing.T) {
	db, path := legacyTimeDB(t)
	if _, err := db.Exec(`ALTER TABLE session_state RENAME COLUMN summary TO compacted_state; ALTER TABLE session_state RENAME COLUMN messages TO last_messages; ALTER TABLE session_state ADD COLUMN tool_summary TEXT NOT NULL DEFAULT '[]'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sessions(id,created_at,updated_at) VALUES ('s','2026-01-01 00:00:00 +0800 CST','2026-01-01'); INSERT INTO session_state(session_id,compacted_state,last_messages,updated_at) VALUES ('s','keep','[]','2026-01-01'); INSERT INTO audit_log(id,tool,timestamp) VALUES ('bad','exec','not a time')`); err != nil {
		t.Fatal(err)
	}
	if store, err := NewStore(path); err == nil {
		store.Close()
		t.Fatal("NewStore error = nil, want unknown timestamp rejection")
	} else if !strings.Contains(err.Error(), "audit_log.timestamp rowid=") {
		t.Fatalf("migration error = %v, want field and row identity", err)
	}
	var raw, ddl string
	var oldColumns int
	if err := db.QueryRow(`SELECT count(*) FROM pragma_table_info('session_state') WHERE name IN ('compacted_state','last_messages','tool_summary')`).Scan(&oldColumns); err != nil || oldColumns != 3 {
		t.Fatalf("legacy history columns = %d, %v, want all three after rollback", oldColumns, err)
	}
	if err := db.QueryRow(`SELECT CAST(created_at AS TEXT) FROM sessions WHERE id='s'`).Scan(&raw); err != nil || raw != "2026-01-01 00:00:00 +0800 CST" {
		t.Fatalf("original timestamp = %q, %v, want transaction rollback", raw, err)
	}
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE name='usage_log'`).Scan(&ddl); err != nil || !strings.Contains(ddl, "datetime('now')") {
		t.Fatalf("old schema = %q, %v, want transaction rollback", ddl, err)
	}
}

func TestLegacyHistoryAndTimeUpgradeCommitTogether(t *testing.T) {
	db, path := legacyTimeDB(t)
	if _, err := db.Exec(`ALTER TABLE session_state RENAME COLUMN summary TO compacted_state; ALTER TABLE session_state RENAME COLUMN messages TO last_messages; ALTER TABLE session_state ADD COLUMN tool_summary TEXT NOT NULL DEFAULT '[]'; INSERT INTO sessions(id,title,created_at,updated_at) VALUES ('s','keep','2026-01-01 08:00:00 +0800 CST','2026-01-01 08:00:00 +0800 CST'); INSERT INTO session_state(session_id,compacted_state,last_messages,updated_at) VALUES ('s','keep facts','[{"role":"user","content":[{"type":"text","text":"original"}]}]','2026-01-01 08:00:00 +0800 CST')`); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	state, err := NewSessionStateStore(store.DB()).Load(context.Background(), "s")
	if err != nil || state.Summary != "keep facts" || len(state.Messages) != 1 || state.Messages[0].Text() != "original" || dbTime(state.UpdatedAt) != "2026-01-01T00:00:00.000000000Z" {
		t.Fatalf("upgraded history = %+v, %v, want original history at UTC instant", state, err)
	}
}

func TestSessionTimeOrderAndUsageLocalDayAcrossDST(t *testing.T) {
	for _, zone := range []string{"Asia/Shanghai", "America/New_York"} {
		t.Run(zone, func(t *testing.T) {
			loc, err := time.LoadLocation(zone)
			if err != nil {
				t.Fatal(err)
			}
			store, err := NewStore(filepath.Join(t.TempDir(), "memory.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			ctx := context.Background()
			start := time.Date(2026, 11, 1, 0, 0, 0, 0, loc)
			ss := NewSessionStore(store.DB())
			for i, timestamp := range []time.Time{start, start.Add(time.Nanosecond), start.Add(3 * time.Hour)} {
				if err := ss.Create(ctx, SessionMeta{ID: fmt.Sprint(i), CreatedAt: timestamp}); err != nil {
					t.Fatal(err)
				}
			}
			items, err := ss.List(ctx)
			var ids []string
			for _, item := range items {
				ids = append(ids, item.ID)
			}
			if err != nil || !reflect.DeepEqual(ids, []string{"2", "1", "0"}) {
				t.Fatalf("session order = %v, %v", ids, err)
			}
			for _, timestamp := range []time.Time{start.Add(-time.Nanosecond), start, start.Add(3 * time.Hour)} {
				if _, err := store.DB().Exec(`INSERT INTO usage_log(model,input_tokens,created_at) VALUES ('test',1,?)`, dbTime(timestamp)); err != nil {
					t.Fatal(err)
				}
			}
			sum, err := NewUsageStore(store.DB()).UsageSummary(ctx, start)
			if err != nil || sum.Requests != 2 || sum.InputTokens != 2 {
				t.Fatalf("local-day usage = %+v, %v, want two requests", sum, err)
			}
		})
	}
}

func TestUTCWritesAndQueueExpiryQueries(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	ss := NewSessionStore(store.DB())
	if err := ss.Create(ctx, SessionMeta{ID: "s"}); err != nil {
		t.Fatal(err)
	}
	for _, run := range []func() error{
		func() error { return ss.UpdateMetadata(ctx, "s", "title", true, "custom/model", true) },
		func() error { return ss.TouchAttached(ctx, "s") },
		func() error { return ss.SetMessageCount(ctx, "s", 2) },
		func() error { return NewSessionStateStore(store.DB()).Save(ctx, "s", "summary", nil) },
		func() error { return NewUsageStore(store.DB()).SaveUsage(ctx, "s", "test", 1, 1) },
	} {
		if err := run(); err != nil {
			t.Fatal(err)
		}
	}
	memories := NewMemoryStore(store.DB())
	if err := memories.ReplaceAll(ctx, DefaultUserID, []UserMemory{
		{ID: "active", Kind: MemoryKindPreference, Content: "active", ExpiresAt: time.Now().Add(time.Hour).In(time.FixedZone("test", -5*60*60))},
		{ID: "expired", Kind: MemoryKindPreference, Content: "expired", ExpiresAt: time.Now().Add(-time.Hour).In(time.FixedZone("test", 8*60*60))},
	}); err != nil {
		t.Fatal(err)
	}
	items, err := memories.List(ctx, DefaultUserID, 10)
	if err != nil || len(items) != 1 || items[0].Content != "active" {
		t.Fatalf("active memories = %+v, %v, want unexpired only", items, err)
	}
	if active, _ := memories.Count(ctx, DefaultUserID); active != 1 {
		t.Fatalf("active memory count = %d, want 1", active)
	}
	if err := memories.MarkUsed(ctx, []string{items[0].ID}); err != nil {
		t.Fatal(err)
	}
	queue := NewExtractQueue(store.DB())
	if !queue.Push(ctx, DefaultUserID, "custom/model", Candidate{Kind: MemoryKindPreference, Content: "candidate", Confidence: .9, Significance: SignificanceHigh}) {
		t.Fatal("queue Push = false")
	}
	due, err := LoadDueQueue(ctx, store.DB(), DefaultUserID, 10)
	if err != nil || len(due) != 1 || QueueDueCount(ctx, store.DB(), DefaultUserID) != 1 {
		t.Fatalf("due queue = %+v, %v, want one due item", due, err)
	}
	worker := NewWorker(queue, memories, store.DB(), nil)
	if !worker.hasHighPending() {
		t.Fatal("high pending = false, want current high-significance item")
	}
	if err := RetryQueueItems(ctx, store.DB(), []string{due[0].ID}, fmt.Errorf("test failure")); err != nil {
		t.Fatal(err)
	}
	due, err = LoadDueQueue(ctx, store.DB(), DefaultUserID, 10)
	if err != nil || len(due) != 0 || QueueDueCount(ctx, store.DB(), DefaultUserID) != 0 || worker.hasHighPending() {
		t.Fatalf("backoff queue = %+v, %v, want none due", due, err)
	}
	for _, query := range []string{
		`INSERT INTO audit_log(id,tool) VALUES ('a','readfile')`,
		`INSERT INTO failure_records(pattern,operation,reason) VALUES ('p','op','test')`,
		`INSERT INTO triggers(id,type,last_fire) VALUES ('t','test',NULL)`,
	} {
		if _, err := store.DB().Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	for _, table := range timeTables {
		for _, column := range table.columns {
			rows, err := store.DB().Query("SELECT CAST(" + column + " AS TEXT) FROM " + table.name + " WHERE " + column + " IS NOT NULL")
			if err != nil {
				t.Fatal(err)
			}
			for rows.Next() {
				var raw string
				if err := rows.Scan(&raw); err != nil {
					t.Fatal(err)
				}
				parsed, err := time.Parse(dbTimeLayout, raw)
				if err != nil || raw != dbTime(parsed) {
					t.Fatalf("%s.%s = %q, %v, want canonical fixed UTC", table.name, column, raw, err)
				}
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			rows.Close()
		}
	}
}

func TestTimeMigrationHandlesMultiplePages(t *testing.T) {
	db, path := legacyTimeDB(t)
	if _, err := db.Exec(`WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<600) INSERT INTO audit_log(id,tool,timestamp) SELECT CAST(i AS TEXT),'readfile','2026-01-01 08:00:00 +0800 CST' FROM n`); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var count int
	if err := store.DB().QueryRow(`SELECT count(*) FROM audit_log WHERE CAST(timestamp AS TEXT)='2026-01-01T00:00:00.000000000Z'`).Scan(&count); err != nil || count != 600 {
		t.Fatalf("normalized audit rows = %d, %v, want 600", count, err)
	}
}
