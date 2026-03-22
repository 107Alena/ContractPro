package idempotency

import (
	"context"
	"errors"
	"fmt"
	"time"

	"contractpro/document-processing/internal/domain/port"
	"contractpro/document-processing/internal/infra/kvstore"
)

const keyPrefix = "idempotency:"

// KVStoreAPI is the consumer-side interface covering the KV-store operations
// needed by the idempotency Store. Defined here (consumer-side) to keep the
// dependency inverted and enable unit testing with a mock.
//
// Satisfied by: *kvstore.Client
type KVStoreAPI interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
	SetNX(ctx context.Context, key string, value string, ttl time.Duration) (bool, error)
}

// Store implements port.IdempotencyStorePort backed by a key-value store.
// It translates between domain-level idempotency semantics (Check/Register/
// MarkCompleted) and low-level KV operations (Get/Set/SetNX) with a key prefix
// and configurable TTL.
type Store struct {
	kv  KVStoreAPI
	ttl time.Duration
}

// Compile-time check that Store implements IdempotencyStorePort.
var _ port.IdempotencyStorePort = (*Store)(nil)

// NewStore creates an idempotency Store.
// Panics if kv is nil or ttl is zero (programmer error at startup).
func NewStore(kv KVStoreAPI, ttl time.Duration) *Store {
	if kv == nil {
		panic("idempotency: kv store must not be nil")
	}
	if ttl <= 0 {
		panic("idempotency: TTL must be positive")
	}
	return &Store{
		kv:  kv,
		ttl: ttl,
	}
}

// validStatuses maps stored string values to their typed status constants.
// IdempotencyStatusNew is deliberately excluded: "new" is never stored in the
// KV store; it is synthesized by Check when the key does not exist.
var validStatuses = map[string]port.IdempotencyStatus{
	string(port.IdempotencyStatusInProgress): port.IdempotencyStatusInProgress,
	string(port.IdempotencyStatusCompleted):  port.IdempotencyStatusCompleted,
}

// Check looks up the job_id in the KV-store and returns its idempotency status.
// Returns IdempotencyStatusNew if the key does not exist.
func (s *Store) Check(ctx context.Context, jobID string) (port.IdempotencyStatus, error) {
	if err := validateJobID(jobID); err != nil {
		return "", err
	}
	val, err := s.kv.Get(ctx, keyFor(jobID))
	if err != nil {
		if errors.Is(err, kvstore.ErrKeyNotFound) {
			return port.IdempotencyStatusNew, nil
		}
		return "", err
	}

	status, ok := validStatuses[val]
	if !ok {
		return "", &port.DomainError{
			Code:      port.ErrCodeStorageFailed,
			Message:   fmt.Sprintf("idempotency: invalid stored value %q for job %s", val, jobID),
			Retryable: false,
		}
	}
	return status, nil
}

// Register atomically registers a job_id with status "in_progress" using SetNX.
// Returns nil if the key was set (first writer wins).
// Returns NewDuplicateJobError if the key already existed (another instance won the race).
func (s *Store) Register(ctx context.Context, jobID string) error {
	if err := validateJobID(jobID); err != nil {
		return err
	}
	acquired, err := s.kv.SetNX(ctx, keyFor(jobID), string(port.IdempotencyStatusInProgress), s.ttl)
	if err != nil {
		return err
	}
	if !acquired {
		return port.NewDuplicateJobError(jobID)
	}
	return nil
}

// MarkCompleted updates the job_id status to "completed" and refreshes the TTL.
// Uses unconditional Set (not SetNX) by design: only the instance that won the
// Register race calls MarkCompleted, and overwriting "in_progress" with
// "completed" is the intended behavior. The TTL is refreshed so the
// deduplication window covers 24h from completion, not from registration.
func (s *Store) MarkCompleted(ctx context.Context, jobID string) error {
	if err := validateJobID(jobID); err != nil {
		return err
	}
	return s.kv.Set(ctx, keyFor(jobID), string(port.IdempotencyStatusCompleted), s.ttl)
}

// validateJobID returns a non-retryable validation error if jobID is empty.
func validateJobID(jobID string) error {
	if jobID == "" {
		return &port.DomainError{
			Code:      port.ErrCodeValidation,
			Message:   "idempotency: job ID must not be empty",
			Retryable: false,
		}
	}
	return nil
}

// keyFor builds the KV-store key for a job_id with the standard prefix.
func keyFor(jobID string) string {
	return keyPrefix + jobID
}
