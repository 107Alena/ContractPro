package postgres

import (
	"context"
	"fmt"
	"time"

	"contractpro/document-management/internal/domain/port"
)

// Compile-time proof that AuditPartitionManager satisfies port.AuditPartitionManager.
var _ port.AuditPartitionManager = (*AuditPartitionManager)(nil)

// AuditPartitionManager manages monthly RANGE partitions on
// audit_records.created_at (REV-027).
type AuditPartitionManager struct{}

// NewAuditPartitionManager creates a new AuditPartitionManager.
func NewAuditPartitionManager() *AuditPartitionManager {
	return &AuditPartitionManager{}
}

// EnsurePartitions creates monthly partitions covering the next
// monthsAhead months from the current month. Already-existing partitions
// are silently skipped (IF NOT EXISTS).
func (m *AuditPartitionManager) EnsurePartitions(ctx context.Context, monthsAhead int) error {
	conn := ConnFromCtx(ctx)

	now := time.Now().UTC()
	for i := 0; i <= monthsAhead; i++ {
		start := time.Date(now.Year(), now.Month()+time.Month(i), 1, 0, 0, 0, 0, time.UTC)
		end := start.AddDate(0, 1, 0)

		partitionName := fmt.Sprintf("audit_records_%04d_%02d", start.Year(), start.Month())

		sql := fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s PARTITION OF audit_records
			 FOR VALUES FROM ('%s') TO ('%s')`,
			partitionName,
			start.Format("2006-01-02"),
			end.Format("2006-01-02"),
		)

		if _, err := conn.Exec(ctx, sql); err != nil {
			return port.NewDatabaseError(
				fmt.Sprintf("create audit partition %s", partitionName), err,
			)
		}
	}
	return nil
}

// DropPartitionsOlderThan drops partitions whose upper bound is before cutoff.
// Inspects pg_catalog for child tables of audit_records with names matching
// audit_records_YYYY_MM, parses the month, and drops if expired.
// Returns the number of partitions dropped.
func (m *AuditPartitionManager) DropPartitionsOlderThan(ctx context.Context, cutoff time.Time) (int, error) {
	conn := ConnFromCtx(ctx)

	// List all child partitions of audit_records (excluding the default partition).
	rows, err := conn.Query(ctx,
		`SELECT c.relname
		 FROM pg_inherits i
		 JOIN pg_class c ON c.oid = i.inhrelid
		 JOIN pg_class p ON p.oid = i.inhparent
		 WHERE p.relname = 'audit_records'
		   AND c.relname LIKE 'audit_records____\___'
		 ORDER BY c.relname`,
	)
	if err != nil {
		return 0, port.NewDatabaseError("list audit partitions", err)
	}
	defer rows.Close()

	var partitionNames []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return 0, port.NewDatabaseError("scan partition name", err)
		}
		partitionNames = append(partitionNames, name)
	}
	if err := rows.Err(); err != nil {
		return 0, port.NewDatabaseError("iterate partition names", err)
	}

	dropped := 0
	for _, name := range partitionNames {
		// Parse "audit_records_YYYY_MM" → upper bound is first day of NEXT month.
		var year, month int
		if _, err := fmt.Sscanf(name, "audit_records_%04d_%02d", &year, &month); err != nil {
			continue // skip unparseable names
		}
		// Upper bound of this partition is the first day of the next month.
		upperBound := time.Date(year, time.Month(month)+1, 1, 0, 0, 0, 0, time.UTC)

		if upperBound.Before(cutoff) {
			sql := fmt.Sprintf("DROP TABLE IF EXISTS %s", name)
			if _, err := conn.Exec(ctx, sql); err != nil {
				return dropped, port.NewDatabaseError(
					fmt.Sprintf("drop audit partition %s", name), err,
				)
			}
			dropped++
		}
	}
	return dropped, nil
}
