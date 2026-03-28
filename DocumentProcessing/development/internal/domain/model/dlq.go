package model

import "encoding/json"

// DLQMessage represents a message published to the Dead Letter Queue
// when a pipeline error exhausts retries before transitioning to FAILED.
type DLQMessage struct {
	EventMeta
	JobID           string          `json:"job_id"`
	DocumentID      string          `json:"document_id"`
	ErrorCode       string          `json:"error_code"`
	ErrorMessage    string          `json:"error_message"`
	FailedAtStage   string          `json:"failed_at_stage"`
	PipelineType    string          `json:"pipeline_type"`
	OriginalCommand json.RawMessage `json:"original_command"`
}
