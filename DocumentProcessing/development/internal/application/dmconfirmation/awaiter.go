// Package dmconfirmation provides the DM Confirmation Awaiter — an
// application-layer component for awaiting Document Management persistence
// confirmations during the processing pipeline.
//
// After the processing orchestrator sends artifacts to DM, it registers a
// pending confirmation and blocks on Await. When the DM response handler
// receives ArtifactsPersisted or ArtifactsPersistFailed, it calls Confirm
// or Reject to unblock the waiting orchestrator.
package dmconfirmation

import (
	"context"
	"fmt"
	"sync"

	"contractpro/document-processing/internal/domain/port"
)

// Compile-time interface compliance check.
var _ port.DMConfirmationAwaiterPort = (*Awaiter)(nil)

// Awaiter tracks pending DM persistence confirmations.
// It is safe for concurrent use by multiple goroutines.
type Awaiter struct {
	mu      sync.Mutex
	entries map[string]*entry // jobID → entry
}

// entry holds the state for one pending confirmation.
type entry struct {
	result    *port.DMConfirmationResult // nil until Confirm/Reject called
	done      chan struct{}
	closeOnce sync.Once
}

// tryClose closes the done channel exactly once.
func (e *entry) tryClose() {
	e.closeOnce.Do(func() { close(e.done) })
}

// NewAwaiter creates an Awaiter.
func NewAwaiter() *Awaiter {
	return &Awaiter{
		entries: make(map[string]*entry),
	}
}

// Register creates a pending confirmation slot for the given job.
// Must be called before sending artifacts to DM.
// Returns a validation error if jobID is empty or already registered.
func (a *Awaiter) Register(jobID string) error {
	if jobID == "" {
		return port.NewValidationError("dmconfirmation: empty job ID")
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if _, exists := a.entries[jobID]; exists {
		return port.NewValidationError(
			fmt.Sprintf("dmconfirmation: job %s already registered", jobID))
	}

	a.entries[jobID] = &entry{
		done: make(chan struct{}),
	}
	return nil
}

// Await blocks until the confirmation for jobID arrives or ctx expires.
// Returns DMConfirmationResult on success/failure, or ctx.Err() on timeout.
// Cleans up the entry on return.
//
// Returns a validation error if the job is not registered.
func (a *Awaiter) Await(ctx context.Context, jobID string) (port.DMConfirmationResult, error) {
	a.mu.Lock()
	e, exists := a.entries[jobID]
	a.mu.Unlock()

	if !exists {
		return port.DMConfirmationResult{}, port.NewValidationError(
			fmt.Sprintf("dmconfirmation: job %s not registered", jobID))
	}

	select {
	case <-e.done:
		// Confirmation received (or entry was canceled).
	case <-ctx.Done():
		a.cleanup(jobID)
		return port.DMConfirmationResult{}, ctx.Err()
	}

	a.mu.Lock()
	result := e.result
	a.mu.Unlock()

	a.cleanup(jobID)

	if result == nil {
		// Entry was canceled without a result.
		return port.DMConfirmationResult{JobID: jobID}, nil
	}
	return *result, nil
}

// Confirm signals that DM successfully persisted artifacts for the job.
// Idempotent: silently ignores duplicate confirms.
// Returns a validation error if the job is not registered.
func (a *Awaiter) Confirm(jobID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	e, exists := a.entries[jobID]
	if !exists {
		return port.NewValidationError(
			fmt.Sprintf("dmconfirmation: job %s not registered", jobID))
	}

	// Idempotent: skip if already received.
	if e.result != nil {
		return nil
	}

	e.result = &port.DMConfirmationResult{JobID: jobID, Err: nil}
	e.tryClose()
	return nil
}

// Reject signals that DM failed to persist artifacts for the job.
// The error should be a *DomainError with appropriate retryable flag.
// Idempotent: silently ignores duplicate rejects.
// Returns a validation error if jobID is not registered or err is nil.
func (a *Awaiter) Reject(jobID string, err error) error {
	if err == nil {
		return port.NewValidationError("dmconfirmation: nil error in Reject")
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	e, exists := a.entries[jobID]
	if !exists {
		return port.NewValidationError(
			fmt.Sprintf("dmconfirmation: job %s not registered", jobID))
	}

	// Idempotent: skip if already received.
	if e.result != nil {
		return nil
	}

	e.result = &port.DMConfirmationResult{JobID: jobID, Err: err}
	e.tryClose()
	return nil
}

// Cancel removes a pending confirmation, unblocking any Await call.
// Safe to call multiple times or on non-existent jobs.
func (a *Awaiter) Cancel(jobID string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	e, exists := a.entries[jobID]
	if !exists {
		return
	}

	e.tryClose()
	delete(a.entries, jobID)
}

// cleanup removes a job entry from the awaiter.
func (a *Awaiter) cleanup(jobID string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	e, exists := a.entries[jobID]
	if !exists {
		return
	}

	e.tryClose()
	delete(a.entries, jobID)
}
