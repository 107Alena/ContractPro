package model

import (
	"net/http"
	"testing"
)

func TestMapOPMError(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		body         []byte
		resourceHint string
		want         ErrorCode
	}{
		// 5xx → OPM_UNAVAILABLE.
		{
			name:       "500 → OPM_UNAVAILABLE",
			statusCode: http.StatusInternalServerError,
			want:       ErrOPMUnavailable,
		},
		{
			name:       "502 → OPM_UNAVAILABLE",
			statusCode: http.StatusBadGateway,
			want:       ErrOPMUnavailable,
		},
		{
			name:       "503 → OPM_UNAVAILABLE",
			statusCode: http.StatusServiceUnavailable,
			want:       ErrOPMUnavailable,
		},

		// 404 → depends on resourceHint.
		{
			name:         "404 policy → POLICY_NOT_FOUND",
			statusCode:   http.StatusNotFound,
			resourceHint: "policy",
			want:         ErrPolicyNotFound,
		},
		{
			name:         "404 checklist → CHECKLIST_NOT_FOUND",
			statusCode:   http.StatusNotFound,
			resourceHint: "checklist",
			want:         ErrChecklistNotFound,
		},
		{
			name:         "404 unknown hint → INTERNAL_ERROR",
			statusCode:   http.StatusNotFound,
			resourceHint: "",
			want:         ErrInternalError,
		},

		// 400 → VALIDATION_ERROR.
		{
			name:       "400 → VALIDATION_ERROR",
			statusCode: http.StatusBadRequest,
			body:       []byte(`{"error": "invalid strictness_level"}`),
			want:       ErrValidationError,
		},

		// Other 4xx → INTERNAL_ERROR.
		{
			name:       "401 → INTERNAL_ERROR",
			statusCode: http.StatusUnauthorized,
			want:       ErrInternalError,
		},
		{
			name:       "403 → INTERNAL_ERROR",
			statusCode: http.StatusForbidden,
			want:       ErrInternalError,
		},
		{
			name:       "409 → INTERNAL_ERROR",
			statusCode: http.StatusConflict,
			want:       ErrInternalError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MapOPMError(tt.statusCode, tt.body, tt.resourceHint)
			if got != tt.want {
				t.Errorf("MapOPMError(%d, %q, %q) = %q, want %q",
					tt.statusCode, string(tt.body), tt.resourceHint, got, tt.want)
			}
		})
	}
}

func TestNewOPMErrorCodes_InCatalog(t *testing.T) {
	codes := []struct {
		code       ErrorCode
		wantStatus int
	}{
		{ErrPolicyNotFound, http.StatusNotFound},
		{ErrChecklistNotFound, http.StatusNotFound},
	}

	for _, c := range codes {
		t.Run(string(c.code), func(t *testing.T) {
			entry, ok := LookupError(c.code)
			if !ok {
				t.Fatalf("error code %q not found in catalog", c.code)
			}
			if entry.HTTPStatus != c.wantStatus {
				t.Errorf("HTTPStatus = %d, want %d", entry.HTTPStatus, c.wantStatus)
			}
			if entry.Message == "" {
				t.Error("Message is empty")
			}
			if entry.Suggestion == "" {
				t.Error("Suggestion is empty")
			}
		})
	}
}
