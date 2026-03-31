package model

import "time"

// IdempotencyStatus represents the processing state of an idempotency record.
type IdempotencyStatus string

const (
	IdempotencyStatusProcessing IdempotencyStatus = "PROCESSING"
	IdempotencyStatusCompleted  IdempotencyStatus = "COMPLETED"
)

// IdempotencyRecord tracks the processing state of an incoming event for deduplication.
type IdempotencyRecord struct {
	Key            string            `json:"key"`
	Status         IdempotencyStatus `json:"status"`
	ResultSnapshot string            `json:"result_snapshot,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

// NewIdempotencyRecord creates a new IdempotencyRecord in PROCESSING status.
func NewIdempotencyRecord(key string) *IdempotencyRecord {
	now := time.Now().UTC()
	return &IdempotencyRecord{
		Key:       key,
		Status:    IdempotencyStatusProcessing,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// MarkCompleted transitions the record to COMPLETED status with an optional result snapshot.
func (r *IdempotencyRecord) MarkCompleted(resultSnapshot string) {
	r.Status = IdempotencyStatusCompleted
	r.ResultSnapshot = resultSnapshot
	r.UpdatedAt = time.Now().UTC()
}

// Age returns the duration since the record was created.
func (r *IdempotencyRecord) Age() time.Duration {
	return time.Since(r.CreatedAt)
}

// IsStuck returns true if the record is in PROCESSING state and older than the given threshold.
func (r *IdempotencyRecord) IsStuck(threshold time.Duration) bool {
	return r.Status == IdempotencyStatusProcessing && r.Age() >= threshold
}
