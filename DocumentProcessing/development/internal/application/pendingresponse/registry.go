// Package pendingresponse provides the Pending Response Registry — an
// application-layer component for tracking and correlating asynchronous
// responses from Document Management during the comparison pipeline.
//
// The comparison orchestrator sends GetSemanticTreeRequest messages to DM
// (each with a unique correlation_id), registers the expected IDs here,
// and blocks on AwaitAll. When the DM Inbound Adapter receives responses,
// it calls Receive or ReceiveError. Once every expected response arrives,
// AwaitAll unblocks and returns the collected results.
package pendingresponse

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
)

// Compile-time interface compliance check.
var _ port.PendingResponseRegistryPort = (*Registry)(nil)

// Registry tracks expected asynchronous responses from Document Management.
// It is safe for concurrent use by multiple goroutines.
type Registry struct {
	mu             sync.Mutex
	entries        map[string]*entry  // jobID → entry
	correlationIdx map[string]string  // correlationID → jobID (reverse index)
}

// entry holds the state for one pending job.
type entry struct {
	expected  map[string]struct{}          // set of expected correlationIDs
	responses map[string]port.PendingResponse // received responses
	done      chan struct{}                // closed when all responses received or canceled
	closeOnce sync.Once                   // ensures done is closed at most once
}

// tryClose closes the done channel exactly once.
func (e *entry) tryClose() {
	e.closeOnce.Do(func() { close(e.done) })
}

// New creates a Registry.
func New() *Registry {
	return &Registry{
		entries:        make(map[string]*entry),
		correlationIdx: make(map[string]string),
	}
}

// Register registers expected correlation IDs for a job.
// Returns a validation error if jobID is empty, correlationIDs is empty,
// any correlation ID is empty or already in use, or the job is already registered.
func (r *Registry) Register(jobID string, correlationIDs []string) error {
	if jobID == "" {
		return port.NewValidationError("pendingresponse: empty job ID")
	}
	if len(correlationIDs) == 0 {
		return port.NewValidationError("pendingresponse: empty correlation IDs")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.entries[jobID]; exists {
		return port.NewValidationError(fmt.Sprintf("pendingresponse: job %s already registered", jobID))
	}

	// Validate all correlation IDs before mutating state.
	seen := make(map[string]struct{}, len(correlationIDs))
	for _, cid := range correlationIDs {
		if cid == "" {
			return port.NewValidationError("pendingresponse: empty correlation ID in list")
		}
		if _, dup := seen[cid]; dup {
			return port.NewValidationError(fmt.Sprintf("pendingresponse: duplicate correlation ID %s", cid))
		}
		if _, inUse := r.correlationIdx[cid]; inUse {
			return port.NewValidationError(fmt.Sprintf("pendingresponse: correlation ID %s already in use", cid))
		}
		seen[cid] = struct{}{}
	}

	// All validations passed — commit.
	expected := make(map[string]struct{}, len(correlationIDs))
	for _, cid := range correlationIDs {
		expected[cid] = struct{}{}
		r.correlationIdx[cid] = jobID
	}

	r.entries[jobID] = &entry{
		expected:  expected,
		responses: make(map[string]port.PendingResponse, len(correlationIDs)),
		done:      make(chan struct{}),
	}

	return nil
}

// AwaitAll blocks until all expected responses for the job are received
// or the context is canceled/expired. Returns all received responses sorted
// by CorrelationID for deterministic output. Cleans up the entry on return.
//
// Returns a validation error if the job is not registered.
// Returns ctx.Err() (context.Canceled or context.DeadlineExceeded) on timeout.
func (r *Registry) AwaitAll(ctx context.Context, jobID string) ([]port.PendingResponse, error) {
	r.mu.Lock()
	e, exists := r.entries[jobID]
	r.mu.Unlock()

	if !exists {
		return nil, port.NewValidationError(fmt.Sprintf("pendingresponse: job %s not registered", jobID))
	}

	select {
	case <-e.done:
		// All responses received (or entry was canceled).
	case <-ctx.Done():
		r.cleanup(jobID)
		return nil, ctx.Err()
	}

	// Collect responses before cleanup removes the entry.
	r.mu.Lock()
	responses := r.collectResponses(e)
	r.mu.Unlock()

	r.cleanup(jobID)

	return responses, nil
}

// Receive records a successful semantic tree response for the given correlation ID.
// Returns a validation error if the correlation ID is unknown.
// Idempotent: silently ignores duplicate or post-cancel receives.
func (r *Registry) Receive(correlationID string, tree model.SemanticTree) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	jobID, exists := r.correlationIdx[correlationID]
	if !exists {
		return port.NewValidationError(fmt.Sprintf("pendingresponse: unknown correlation ID %s", correlationID))
	}

	e := r.entries[jobID]

	// Idempotent: skip if already received.
	if _, already := e.responses[correlationID]; already {
		return nil
	}

	cp := deepCopyTree(tree)
	e.responses[correlationID] = port.PendingResponse{
		CorrelationID: correlationID,
		Tree:          &cp,
	}

	if len(e.responses) == len(e.expected) {
		e.tryClose()
	}

	return nil
}

// ReceiveError records an error response for the given correlation ID.
// Returns a validation error if the correlation ID is unknown.
// Idempotent: silently ignores duplicate or post-cancel receives.
func (r *Registry) ReceiveError(correlationID string, err error) error {
	if err == nil {
		return port.NewValidationError("pendingresponse: nil error in ReceiveError")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	jobID, exists := r.correlationIdx[correlationID]
	if !exists {
		return port.NewValidationError(fmt.Sprintf("pendingresponse: unknown correlation ID %s", correlationID))
	}

	e := r.entries[jobID]

	if _, already := e.responses[correlationID]; already {
		return nil
	}

	e.responses[correlationID] = port.PendingResponse{
		CorrelationID: correlationID,
		Err:           err,
	}

	if len(e.responses) == len(e.expected) {
		e.tryClose()
	}

	return nil
}

// Cancel removes a job's entry from the registry, unblocking any AwaitAll
// call waiting on it. Safe to call multiple times or on non-existent jobs.
func (r *Registry) Cancel(jobID string) {
	r.cleanup(jobID)
}

// cleanup removes a job entry and its reverse index entries.
func (r *Registry) cleanup(jobID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	e, exists := r.entries[jobID]
	if !exists {
		return
	}

	// Unblock any goroutine waiting on done.
	e.tryClose()

	// Remove reverse index entries.
	for cid := range e.expected {
		delete(r.correlationIdx, cid)
	}
	delete(r.entries, jobID)
}

// collectResponses extracts responses from an entry sorted by CorrelationID.
// Caller must hold r.mu.
func (r *Registry) collectResponses(e *entry) []port.PendingResponse {
	responses := make([]port.PendingResponse, 0, len(e.responses))
	for _, resp := range e.responses {
		responses = append(responses, resp)
	}
	sort.Slice(responses, func(i, j int) bool {
		return responses[i].CorrelationID < responses[j].CorrelationID
	})
	return responses
}

// deepCopyTree returns a deep copy of a SemanticTree, including the full
// node tree. This ensures the registry owns its data and callers cannot
// corrupt it by mutating the original after Receive.
func deepCopyTree(t model.SemanticTree) model.SemanticTree {
	return model.SemanticTree{
		DocumentID: t.DocumentID,
		Root:       deepCopyNode(t.Root),
	}
}

func deepCopyNode(n *model.SemanticNode) *model.SemanticNode {
	if n == nil {
		return nil
	}
	cp := &model.SemanticNode{
		ID:      n.ID,
		Type:    n.Type,
		Content: n.Content,
	}
	if n.Metadata != nil {
		cp.Metadata = make(map[string]string, len(n.Metadata))
		for k, v := range n.Metadata {
			cp.Metadata[k] = v
		}
	}
	if n.Children != nil {
		cp.Children = make([]*model.SemanticNode, len(n.Children))
		for i, child := range n.Children {
			cp.Children[i] = deepCopyNode(child)
		}
	}
	return cp
}
