package memory

import (
	"database/sql"
	"fmt"
	"strings"
)

type timeTable struct {
	name    string
	columns []string
}

var timeTables = []timeTable{
	{"user_profile_memory", []string{"last_used_at", "expires_at", "created_at", "updated_at"}},
	{"sessions", []string{"created_at", "updated_at", "last_attached_at"}},
	{"session_state", []string{"updated_at"}},
	{"memory_queue", []string{"created_at", "next_attempt_at", "processed_at"}},
	{"usage_log", []string{"created_at"}},
	{"audit_log", []string{"timestamp"}},
	{"failure_records", []string{"created_at"}},
	{"triggers", []string{"last_fire", "created_at"}},
}

// 旧默认值是本次升级的结构标识；升级完成后不再扫描历史，也不增加版本表或双写。
func (s *Store) migrateState(schemas []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// 从 v0.31.0 直接升级时，历史改列和时间升级也必须一并提交或回滚。
	if err := s.migrateSessionMessages(tx); err != nil {
		return err
	}
	var rebuild []timeTable
	for _, table := range timeTables {
		var ddl string
		if err := tx.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name=?`, table.name).Scan(&ddl); err != nil {
			return err
		}
		if strings.Contains(ddl, "datetime('now')") {
			rebuild = append(rebuild, table)
		}
	}
	if len(rebuild) == 0 {
		return tx.Commit()
	}
	// 时间值与默认值、索引在同一事务中升级；任意坏值都保留原库，不猜测时区。
	for _, table := range timeTables {
		if err := normalizeTableTimes(tx, table); err != nil {
			return err
		}
	}
	var objects []string
	for _, table := range rebuild {
		rows, err := tx.Query(`SELECT sql FROM sqlite_master WHERE tbl_name=? AND type IN ('index','trigger') AND sql IS NOT NULL`, table.name)
		if err != nil {
			return err
		}
		for rows.Next() {
			var ddl string
			if err := rows.Scan(&ddl); err != nil {
				rows.Close()
				return err
			}
			objects = append(objects, ddl)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	for _, table := range rebuild {
		prefix := "CREATE TABLE IF NOT EXISTS " + table.name + " ("
		temp := table.name + "_time_upgrade"
		var ddl string
		for _, schema := range schemas {
			if strings.HasPrefix(schema, prefix) {
				ddl = strings.Replace(schema, prefix, "CREATE TABLE "+temp+" (", 1)
				break
			}
		}
		if ddl == "" {
			return fmt.Errorf("missing schema for timestamp migration: %s", table.name)
		}
		// 这些表没有外键引用；保留全部业务列与自增高水位，避免删除记录后复用旧 ID。
		var sequence sql.NullInt64
		if err := tx.QueryRow(`SELECT seq FROM sqlite_sequence WHERE name=?`, table.name).Scan(&sequence); err != nil && err != sql.ErrNoRows {
			return err
		}
		columns, err := tx.Query(`SELECT name FROM pragma_table_info(?) ORDER BY cid`, table.name)
		if err != nil {
			return err
		}
		var names []string
		for columns.Next() {
			var name string
			if err := columns.Scan(&name); err != nil {
				columns.Close()
				return err
			}
			names = append(names, `"`+strings.ReplaceAll(name, `"`, `""`)+`"`)
		}
		err = columns.Err()
		columns.Close()
		if err != nil {
			return err
		}
		// 旧迁移可能把 model_ref 加在末尾，复制时按列名对应，不能依赖列位置。
		list := strings.Join(names, ",")
		for _, query := range []string{ddl, "INSERT INTO " + temp + " (" + list + ") SELECT " + list + " FROM " + table.name, "DROP TABLE " + table.name, "ALTER TABLE " + temp + " RENAME TO " + table.name} {
			if _, err := tx.Exec(query); err != nil {
				return fmt.Errorf("migrate timestamps in %s: %w", table.name, err)
			}
		}
		if sequence.Valid {
			if _, err := tx.Exec(`UPDATE sqlite_sequence SET seq=max(seq, ?) WHERE name=?`, sequence.Int64, table.name); err != nil {
				return err
			}
		}
	}
	// 重建会移除表上的索引和触发器，必须原样恢复，不能只恢复源码中的默认索引。
	for _, ddl := range objects {
		if _, err := tx.Exec(ddl); err != nil {
			return err
		}
	}
	for _, schema := range schemas {
		if strings.HasPrefix(schema, "CREATE INDEX") {
			if _, err := tx.Exec(schema); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func normalizeTableTimes(tx *sql.Tx, table timeTable) error {
	casts := make([]string, len(table.columns))
	sets := make([]string, len(table.columns))
	for i, column := range table.columns {
		casts[i] = "CAST(" + column + " AS TEXT)"
		sets[i] = column + "=?"
	}
	var last int64
	first := true
	for {
		query := "SELECT rowid," + strings.Join(casts, ",") + " FROM " + table.name
		var args []any
		if !first {
			query += " WHERE rowid > ?"
			args = append(args, last)
		}
		query += " ORDER BY rowid LIMIT 256"
		rows, err := tx.Query(query, args...)
		if err != nil {
			return err
		}
		var batch [][]any
		for rows.Next() {
			var id int64
			values := make([]sql.NullString, len(table.columns))
			dest := []any{&id}
			for i := range values {
				dest = append(dest, &values[i])
			}
			if err := rows.Scan(dest...); err != nil {
				rows.Close()
				return err
			}
			update := make([]any, len(values)+1)
			for i, value := range values {
				if !value.Valid {
					continue
				}
				parsed, err := decodeDBTime(value.String)
				if err != nil {
					rows.Close()
					return fmt.Errorf("migrate timestamp %s.%s rowid=%d: %w", table.name, table.columns[i], id, err)
				}
				update[i] = dbTime(parsed)
			}
			update[len(values)] = id
			batch = append(batch, update)
			last = id
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}
		for _, values := range batch {
			if _, err := tx.Exec("UPDATE "+table.name+" SET "+strings.Join(sets, ",")+" WHERE rowid=?", values...); err != nil {
				return err
			}
		}
		first = false
	}
}
