package memory

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/alanchenchen/suna/internal/model"
)

// 旧DDL转换已退出；在初始化前识别旧结构，避免遗漏历史后创建空库冒充升级。
func (s *Store) checkUpgradeBaseline() error {
	exists, err := s.tableExists("conversation_state")
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("database requires initialization with Suna v0.31.0 before upgrading")
	}
	for _, column := range []struct{ table, name string }{{"sessions", "model_ref"}, {"memory_queue", "model_ref"}} {
		exists, err = s.tableExists(column.table)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		ok, err := s.columnExists(column.table, column.name)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("database requires initialization with Suna v0.31.0 before upgrading")
		}
	}
	for _, column := range []struct{ table, name string }{{"audit_log", "risk_level"}, {"trust_rules", "risk_adjustment"}} {
		ok, err := s.columnExists(column.table, column.name)
		if err != nil {
			return err
		}
		if ok {
			return fmt.Errorf("database requires initialization with Suna v0.31.0 before upgrading")
		}
	}
	return nil
}

func (s *Store) migrateSessionMessages(tx *sql.Tx) error {
	var legacy int
	err := tx.QueryRow(`SELECT count(*) FROM pragma_table_info('session_state') WHERE name='compacted_state'`).Scan(&legacy)
	if err != nil {
		return err
	}
	if legacy == 0 {
		return nil
	}
	// 改列、正文规范化和删除旧聚合共享一次事务，坏数据使整个升级回滚。
	for _, query := range []string{`ALTER TABLE session_state RENAME COLUMN compacted_state TO summary`, `ALTER TABLE session_state RENAME COLUMN last_messages TO messages`, `ALTER TABLE session_state DROP COLUMN tool_summary`} {
		if _, err = tx.Exec(query); err != nil {
			return err
		}
	}
	rows, err := tx.Query(`SELECT session_id FROM session_state`)
	if err != nil {
		return err
	}
	// 只缓存主键；一次只规范化一个会话，升级内存不随所有历史总量膨胀。
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		var raw string
		if err = tx.QueryRow(`SELECT messages FROM session_state WHERE session_id=?`, id).Scan(&raw); err != nil {
			return err
		}
		var messages []Message
		if err = json.Unmarshal([]byte(raw), &messages); err != nil {
			return fmt.Errorf("migrate session messages: %w", err)
		}
		// 旧库没有消费标记；仅后续完整回复已证明读取过的前缀可选。
		consumedEnd := 0
		for i, m := range messages {
			if m.Role == model.RoleAssistant && len(m.ToolCalls) == 0 {
				consumedEnd = i
			}
		}
		for i := range messages {
			messages[i].Consumed = i < consumedEnd
			if messages[i].Role == model.RoleUser {
				messages[i].Origin = OriginLegacy
			}
		}
		data, err := json.Marshal(ProjectMessages(messages))
		if err != nil {
			return err
		}
		if _, err = tx.Exec(`UPDATE session_state SET messages=? WHERE session_id=?`, string(data), id); err != nil {
			return err
		}
	}
	return nil
}
