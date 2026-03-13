package model

import "fmt"

// JobStatus represents the external status of a processing or comparison job.
type JobStatus string

const (
	StatusQueued                JobStatus = "QUEUED"
	StatusInProgress            JobStatus = "IN_PROGRESS"
	StatusCompleted             JobStatus = "COMPLETED"
	StatusCompletedWithWarnings JobStatus = "COMPLETED_WITH_WARNINGS"
	StatusFailed                JobStatus = "FAILED"
	StatusTimedOut              JobStatus = "TIMED_OUT"
	StatusRejected              JobStatus = "REJECTED"
)

// AllStatuses contains all defined job statuses.
var AllStatuses = []JobStatus{
	StatusQueued,
	StatusInProgress,
	StatusCompleted,
	StatusCompletedWithWarnings,
	StatusFailed,
	StatusTimedOut,
	StatusRejected,
}

// validTransitions maps each status to the set of statuses it can transition to.
// Terminal statuses have no outgoing transitions.
var validTransitions = map[JobStatus][]JobStatus{
	StatusQueued:     {StatusInProgress, StatusRejected},
	StatusInProgress: {StatusCompleted, StatusCompletedWithWarnings, StatusFailed, StatusTimedOut, StatusRejected},
}

// ValidateTransition checks whether a transition from one status to another is allowed.
func ValidateTransition(from, to JobStatus) error {
	targets, ok := validTransitions[from]
	if !ok {
		return fmt.Errorf("no transitions allowed from terminal status %s", from)
	}
	for _, t := range targets {
		if t == to {
			return nil
		}
	}
	return fmt.Errorf("invalid transition from %s to %s", from, to)
}

// IsTerminal returns true if the status is a terminal state (no outgoing transitions).
func (s JobStatus) IsTerminal() bool {
	_, hasTransitions := validTransitions[s]
	return !hasTransitions
}
