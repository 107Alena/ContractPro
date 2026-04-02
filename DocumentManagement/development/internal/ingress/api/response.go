package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"contractpro/document-management/internal/domain/port"
)

// ErrorResponse is the standard JSON error envelope returned by all endpoints.
type ErrorResponse struct {
	ErrorCode string `json:"error_code"`
	Message   string `json:"message"`
}

// PaginatedResponse is a generic paginated list response.
type PaginatedResponse struct {
	Items any `json:"items"`
	Total int `json:"total"`
	Page  int `json:"page"`
	Size  int `json:"size"`
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, statusCode int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(statusCode)
	// Encoding error cannot change status (headers sent). The response may be
	// partial, but that is the correct behavior for a broken connection.
	_ = json.NewEncoder(w).Encode(v)
}

// writeErrorJSON writes a JSON error response.
func writeErrorJSON(w http.ResponseWriter, statusCode int, errorCode, message string) {
	writeJSON(w, statusCode, ErrorResponse{
		ErrorCode: errorCode,
		Message:   message,
	})
}

// writeServiceError maps a DomainError to the appropriate HTTP status and writes the error response.
// Internal details are not leaked: only the error code and the domain message are returned.
func writeServiceError(w http.ResponseWriter, err error) {
	code := port.ErrorCode(err)
	var domErr *port.DomainError
	msg := "unexpected error"
	if errors.As(err, &domErr) {
		msg = domErr.Message
	}

	switch {
	case port.IsNotFound(err):
		writeErrorJSON(w, http.StatusNotFound, code, msg)
	case port.IsConflict(err):
		writeErrorJSON(w, http.StatusConflict, code, msg)
	case code == port.ErrCodeValidation || code == port.ErrCodeInvalidPayload || code == port.ErrCodeInvalidStatus:
		writeErrorJSON(w, http.StatusBadRequest, code, msg)
	case code == port.ErrCodeTenantMismatch:
		// Do not leak tenant info; return 404 to the caller.
		writeErrorJSON(w, http.StatusNotFound, port.ErrCodeDocumentNotFound, "resource not found")
	default:
		// Retryable infrastructure errors or unknown — 500 with generic message.
		writeErrorJSON(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
	}
}
