package model

import (
	"encoding/json"
	"time"
)

// DLQCategory determines which DLQ topic a failed message is routed to.
type DLQCategory string

const (
	DLQCategoryIngestion DLQCategory = "ingestion"
	DLQCategoryQuery     DLQCategory = "query"
	DLQCategoryInvalid   DLQCategory = "invalid"
)

// DLQRecord is a diagnostic envelope for messages that failed processing
// after retry exhaustion. It does not embed EventMeta because the DLQ
// envelope has its own schema distinct from domain events.
type DLQRecord struct {
	OriginalTopic   string          `json:"original_topic"`
	OriginalMessage json.RawMessage `json:"original_message"`
	ErrorCode       string          `json:"error_code"`
	ErrorMessage    string          `json:"error_message"`
	RetryCount      int             `json:"retry_count"`
	CorrelationID   string          `json:"correlation_id"`
	JobID           string          `json:"job_id"`
	FailedAt        time.Time       `json:"failed_at"`
	Category        DLQCategory     `json:"category"`
}

// DLQRecordWithMeta extends DLQRecord with DB-managed replay tracking fields.
type DLQRecordWithMeta struct {
	ID             string     `json:"id"`
	DLQRecord                 // embedded
	ReplayCount    int        `json:"replay_count"`
	LastReplayedAt *time.Time `json:"last_replayed_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}
