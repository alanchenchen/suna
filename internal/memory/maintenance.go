package memory

import (
	"context"
	"fmt"
	"time"
)

const OperationalLogRetention = 30 * 24 * time.Hour

// OperationalPruneResult 记录本次后台维护释放的运维明细行数。
type OperationalPruneResult struct {
	AuditRows int64
	UsageRows int64
}

// PruneOperationalLogs 删除过期的可丢弃运维明细；Session、Memory 和用户配置不在此维护范围内。
func (s *Store) PruneOperationalLogs(ctx context.Context, now time.Time) (OperationalPruneResult, error) {
	if s == nil || s.db == nil {
		return OperationalPruneResult{}, fmt.Errorf("prune operational logs: nil store")
	}
	if now.IsZero() {
		now = time.Now()
	}
	localCutoff := now.Add(-OperationalLogRetention)
	utcCutoff := localCutoff.UTC()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return OperationalPruneResult{}, fmt.Errorf("prune operational logs: begin transaction: %w", err)
	}
	defer tx.Rollback()

	auditResult, err := tx.ExecContext(ctx, `DELETE FROM audit_log WHERE timestamp < ?`, utcCutoff.Format("2006-01-02 15:04:05"))
	if err != nil {
		return OperationalPruneResult{}, fmt.Errorf("prune operational logs: delete audit log: %w", err)
	}
	usageResult, err := tx.ExecContext(ctx, `DELETE FROM usage_log WHERE created_at < ?`, localCutoff)
	if err != nil {
		return OperationalPruneResult{}, fmt.Errorf("prune operational logs: delete usage log: %w", err)
	}
	auditRows, err := auditResult.RowsAffected()
	if err != nil {
		return OperationalPruneResult{}, fmt.Errorf("prune operational logs: audit rows affected: %w", err)
	}
	usageRows, err := usageResult.RowsAffected()
	if err != nil {
		return OperationalPruneResult{}, fmt.Errorf("prune operational logs: usage rows affected: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return OperationalPruneResult{}, fmt.Errorf("prune operational logs: commit: %w", err)
	}
	return OperationalPruneResult{AuditRows: auditRows, UsageRows: usageRows}, nil
}
