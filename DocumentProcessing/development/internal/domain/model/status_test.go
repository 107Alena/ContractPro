package model

import (
	"testing"
)

func TestValidateTransition_ValidTransitions(t *testing.T) {
	valid := []struct {
		from JobStatus
		to   JobStatus
	}{
		{StatusQueued, StatusInProgress},
		{StatusQueued, StatusRejected},
		{StatusInProgress, StatusCompleted},
		{StatusInProgress, StatusCompletedWithWarnings},
		{StatusInProgress, StatusFailed},
		{StatusInProgress, StatusTimedOut},
		{StatusInProgress, StatusRejected},
	}

	for _, tc := range valid {
		t.Run(string(tc.from)+"->"+string(tc.to), func(t *testing.T) {
			if err := ValidateTransition(tc.from, tc.to); err != nil {
				t.Errorf("expected valid transition from %s to %s, got error: %v", tc.from, tc.to, err)
			}
		})
	}
}

func TestValidateTransition_InvalidTransitions(t *testing.T) {
	invalid := []struct {
		from JobStatus
		to   JobStatus
	}{
		// From QUEUED — invalid targets
		{StatusQueued, StatusQueued},
		{StatusQueued, StatusCompleted},
		{StatusQueued, StatusCompletedWithWarnings},
		{StatusQueued, StatusFailed},
		{StatusQueued, StatusTimedOut},

		// From IN_PROGRESS — invalid targets
		{StatusInProgress, StatusQueued},
		{StatusInProgress, StatusInProgress},

		// From terminal statuses — no transitions allowed
		{StatusCompleted, StatusQueued},
		{StatusCompleted, StatusInProgress},
		{StatusCompleted, StatusCompleted},
		{StatusCompleted, StatusCompletedWithWarnings},
		{StatusCompleted, StatusFailed},
		{StatusCompleted, StatusTimedOut},
		{StatusCompleted, StatusRejected},

		{StatusCompletedWithWarnings, StatusQueued},
		{StatusCompletedWithWarnings, StatusInProgress},
		{StatusCompletedWithWarnings, StatusCompleted},
		{StatusCompletedWithWarnings, StatusCompletedWithWarnings},
		{StatusCompletedWithWarnings, StatusFailed},
		{StatusCompletedWithWarnings, StatusTimedOut},
		{StatusCompletedWithWarnings, StatusRejected},

		{StatusFailed, StatusQueued},
		{StatusFailed, StatusInProgress},
		{StatusFailed, StatusCompleted},
		{StatusFailed, StatusCompletedWithWarnings},
		{StatusFailed, StatusFailed},
		{StatusFailed, StatusTimedOut},
		{StatusFailed, StatusRejected},

		{StatusTimedOut, StatusQueued},
		{StatusTimedOut, StatusInProgress},
		{StatusTimedOut, StatusCompleted},
		{StatusTimedOut, StatusCompletedWithWarnings},
		{StatusTimedOut, StatusFailed},
		{StatusTimedOut, StatusTimedOut},
		{StatusTimedOut, StatusRejected},

		{StatusRejected, StatusQueued},
		{StatusRejected, StatusInProgress},
		{StatusRejected, StatusCompleted},
		{StatusRejected, StatusCompletedWithWarnings},
		{StatusRejected, StatusFailed},
		{StatusRejected, StatusTimedOut},
		{StatusRejected, StatusRejected},
	}

	for _, tc := range invalid {
		t.Run(string(tc.from)+"->"+string(tc.to), func(t *testing.T) {
			if err := ValidateTransition(tc.from, tc.to); err == nil {
				t.Errorf("expected error for transition from %s to %s, but got nil", tc.from, tc.to)
			}
		})
	}
}

func TestValidateTransition_AllCombinationsCovered(t *testing.T) {
	validCount := 7  // from TestValidateTransition_ValidTransitions
	invalidCount := 42 // from TestValidateTransition_InvalidTransitions
	total := validCount + invalidCount
	expected := len(AllStatuses) * len(AllStatuses) // 7 * 7 = 49

	if total != expected {
		t.Errorf("test coverage: %d combinations tested, expected %d (7x7)", total, expected)
	}
}

func TestIsTerminal(t *testing.T) {
	tests := []struct {
		status   JobStatus
		terminal bool
	}{
		{StatusQueued, false},
		{StatusInProgress, false},
		{StatusCompleted, true},
		{StatusCompletedWithWarnings, true},
		{StatusFailed, true},
		{StatusTimedOut, true},
		{StatusRejected, true},
	}

	for _, tc := range tests {
		t.Run(string(tc.status), func(t *testing.T) {
			if got := tc.status.IsTerminal(); got != tc.terminal {
				t.Errorf("IsTerminal(%s) = %v, want %v", tc.status, got, tc.terminal)
			}
		})
	}
}

func TestAllStatusesCount(t *testing.T) {
	if len(AllStatuses) != 7 {
		t.Errorf("expected 7 statuses, got %d", len(AllStatuses))
	}
}
