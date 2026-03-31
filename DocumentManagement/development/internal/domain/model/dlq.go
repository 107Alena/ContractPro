package model

import (
	"encoding/json"
	"time"
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
}
