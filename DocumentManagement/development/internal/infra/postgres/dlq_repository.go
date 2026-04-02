package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
)

// Compile-time interface check.
var _ port.DLQRepository = (*DLQRepository)(nil)

// DLQRepository implements port.DLQRepository using PostgreSQL.
type DLQRepository struct{}

// NewDLQRepository creates a new DLQRepository.
func NewDLQRepository() *DLQRepository {
	return &DLQRepository{}
}

// Insert persists a DLQ record to the dm_dlq_records table.
func (r *DLQRepository) Insert(ctx context.Context, record *model.DLQRecord) error {
	conn := ConnFromCtx(ctx)

	originalMsg, err := json.Marshal(record.OriginalMessage)
	if err != nil {
		return port.NewDatabaseError("dlq: marshal original_message", err)
	}

	const query = `
		INSERT INTO dm_dlq_records
			(original_topic, original_message, error_code, error_message,
			 correlation_id, job_id, category, failed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	_, execErr := conn.Exec(ctx, query,
		record.OriginalTopic,
		originalMsg,
		record.ErrorCode,
		record.ErrorMessage,
		record.CorrelationID,
		record.JobID,
		string(record.Category),
		record.FailedAt,
	)
	if execErr != nil {
		return port.NewDatabaseError("dlq: insert record", execErr)
	}
	return nil
}

// FindByFilter returns DLQ records matching the given criteria.
func (r *DLQRepository) FindByFilter(ctx context.Context, params port.DLQFilterParams) ([]*model.DLQRecordWithMeta, error) {
	conn := ConnFromCtx(ctx)

	query := `SELECT id, original_topic, original_message, error_code, error_message,
		correlation_id, job_id, category, failed_at, replay_count, last_replayed_at, created_at
		FROM dm_dlq_records WHERE 1=1`
	args := []any{}
	argIdx := 1

	if params.Category != "" {
		query += fmt.Sprintf(" AND category = $%d", argIdx)
		args = append(args, string(params.Category))
		argIdx++
	}
	if params.CorrelationID != "" {
		query += fmt.Sprintf(" AND correlation_id = $%d", argIdx)
		args = append(args, params.CorrelationID)
		argIdx++
	}
	if params.MaxReplay > 0 {
		query += fmt.Sprintf(" AND replay_count < $%d", argIdx)
		args = append(args, params.MaxReplay)
		argIdx++
	}

	limit := params.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", argIdx)
	args = append(args, limit)

	rows, err := conn.Query(ctx, query, args...)
	if err != nil {
		return nil, port.NewDatabaseError("dlq: find by filter", err)
	}
	defer rows.Close()

	var result []*model.DLQRecordWithMeta
	for rows.Next() {
		rec := &model.DLQRecordWithMeta{}
		var originalMsg json.RawMessage
		err := rows.Scan(
			&rec.ID,
			&rec.OriginalTopic,
			&originalMsg,
			&rec.ErrorCode,
			&rec.ErrorMessage,
			&rec.CorrelationID,
			&rec.JobID,
			&rec.Category,
			&rec.FailedAt,
			&rec.ReplayCount,
			&rec.LastReplayedAt,
			&rec.CreatedAt,
		)
		if err != nil {
			return nil, port.NewDatabaseError("dlq: scan record", err)
		}
		rec.OriginalMessage = originalMsg
		result = append(result, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, port.NewDatabaseError("dlq: rows iteration", err)
	}

	if result == nil {
		result = []*model.DLQRecordWithMeta{}
	}
	return result, nil
}

// IncrementReplayCount atomically increments the replay count for a record.
func (r *DLQRepository) IncrementReplayCount(ctx context.Context, id string) error {
	conn := ConnFromCtx(ctx)

	const query = `UPDATE dm_dlq_records SET replay_count = replay_count + 1, last_replayed_at = now() WHERE id = $1`
	tag, err := conn.Exec(ctx, query, id)
	if err != nil {
		return port.NewDatabaseError("dlq: increment replay count", err)
	}
	if tag.RowsAffected() == 0 {
		return port.NewDatabaseError("dlq: record not found for replay", fmt.Errorf("id=%s", id))
	}
	return nil
}
