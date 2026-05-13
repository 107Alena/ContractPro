// Package jobid provides a single helper for generating processing-flow
// correlation identifiers shared across DM, DP, LIC and downstream publications.
package jobid

import "github.com/google/uuid"

// NewJobID returns a fresh UUID v4 for a new processing flow. The returned
// value is used as a stable correlation key across DM (CreateVersionRequest.job_id),
// DP (ProcessDocumentRequested.job_id), LIC и downstream-публикаций.
func NewJobID() string {
	return uuid.NewString()
}
