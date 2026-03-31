package model

import "time"

// EventMeta contains fields shared by all events: correlation ID for tracing
// and timestamp for ordering/observability. Compatible with DP EventMeta.
type EventMeta struct {
	CorrelationID string    `json:"correlation_id"`
	Timestamp     time.Time `json:"timestamp"`
}

// BlobReference is a claim-check reference to a binary file in Object Storage.
// The producer uploads the blob before publishing the event; the consumer
// retrieves it using StorageKey.
type BlobReference struct {
	StorageKey  string `json:"storage_key"`
	FileName    string `json:"file_name"`
	SizeBytes   int64  `json:"size_bytes"`
	ContentHash string `json:"content_hash"`
}
