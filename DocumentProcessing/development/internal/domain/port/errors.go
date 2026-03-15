package port

import (
	"errors"
	"fmt"
)

// Error code constants for typed domain errors.
const (
	ErrCodeValidation          = "VALIDATION_ERROR"
	ErrCodeFileTooLarge        = "FILE_TOO_LARGE"
	ErrCodeTooManyPages        = "TOO_MANY_PAGES"
	ErrCodeInvalidFormat       = "INVALID_FORMAT"
	ErrCodeFileNotFound        = "FILE_NOT_FOUND"
	ErrCodeOCRFailed           = "OCR_FAILED"
	ErrCodeExtractionFailed    = "EXTRACTION_FAILED"
	ErrCodeStorageFailed       = "STORAGE_FAILED"
	ErrCodeBrokerFailed        = "BROKER_FAILED"
	ErrCodeTimeout             = "TIMEOUT"
	ErrCodeServiceUnavailable  = "SERVICE_UNAVAILABLE"
	ErrCodeDuplicateJob        = "DUPLICATE_JOB"
	ErrCodeConcurrencyLimit    = "CONCURRENCY_LIMIT"
)

// DomainError represents a typed domain error with machine-readable code
// and retryable flag for orchestrator decision-making.
type DomainError struct {
	Code      string // machine-readable error code (ErrCode* constants)
	Message   string // human-readable description
	Retryable bool   // whether the operation can be retried
	Cause     error  // original error (for errors.Is/As unwrapping)
}

func (e *DomainError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *DomainError) Unwrap() error {
	return e.Cause
}

// --- Constructors (one per error code) ---

// NewValidationError creates a non-retryable validation error (→ REJECTED).
func NewValidationError(msg string) *DomainError {
	return &DomainError{Code: ErrCodeValidation, Message: msg, Retryable: false}
}

// NewFileTooLargeError creates a non-retryable error for files exceeding 20 MB.
func NewFileTooLargeError(msg string) *DomainError {
	return &DomainError{Code: ErrCodeFileTooLarge, Message: msg, Retryable: false}
}

// NewTooManyPagesError creates a non-retryable error for documents exceeding 100 pages.
func NewTooManyPagesError(msg string) *DomainError {
	return &DomainError{Code: ErrCodeTooManyPages, Message: msg, Retryable: false}
}

// NewInvalidFormatError creates a non-retryable error for non-PDF files.
func NewInvalidFormatError(msg string) *DomainError {
	return &DomainError{Code: ErrCodeInvalidFormat, Message: msg, Retryable: false}
}

// NewFileNotFoundError creates a non-retryable error when file URL returns 404.
func NewFileNotFoundError(msg string, cause error) *DomainError {
	return &DomainError{Code: ErrCodeFileNotFound, Message: msg, Retryable: false, Cause: cause}
}

// NewOCRError creates an OCR error. Retryable depends on the OCR service error type
// (rate limit → retryable, invalid input → non-retryable).
func NewOCRError(msg string, retryable bool, cause error) *DomainError {
	return &DomainError{Code: ErrCodeOCRFailed, Message: msg, Retryable: retryable, Cause: cause}
}

// NewExtractionError creates a non-retryable text/structure extraction error.
func NewExtractionError(msg string, cause error) *DomainError {
	return &DomainError{Code: ErrCodeExtractionFailed, Message: msg, Retryable: false, Cause: cause}
}

// NewStorageError creates a retryable temporary storage error.
func NewStorageError(msg string, cause error) *DomainError {
	return &DomainError{Code: ErrCodeStorageFailed, Message: msg, Retryable: true, Cause: cause}
}

// NewBrokerError creates a retryable message broker error.
func NewBrokerError(msg string, cause error) *DomainError {
	return &DomainError{Code: ErrCodeBrokerFailed, Message: msg, Retryable: true, Cause: cause}
}

// NewTimeoutError creates a retryable timeout error (→ TIMED_OUT).
func NewTimeoutError(msg string, cause error) *DomainError {
	return &DomainError{Code: ErrCodeTimeout, Message: msg, Retryable: true, Cause: cause}
}

// NewServiceUnavailableError creates a retryable error for unavailable external services.
func NewServiceUnavailableError(msg string, cause error) *DomainError {
	return &DomainError{Code: ErrCodeServiceUnavailable, Message: msg, Retryable: true, Cause: cause}
}

// NewDuplicateJobError creates a non-retryable error for already processed/in-progress jobs.
func NewDuplicateJobError(jobID string) *DomainError {
	return &DomainError{
		Code:      ErrCodeDuplicateJob,
		Message:   fmt.Sprintf("job %s already exists", jobID),
		Retryable: false,
	}
}

// NewConcurrencyLimitError creates a retryable error when no processing slots are available.
func NewConcurrencyLimitError() *DomainError {
	return &DomainError{
		Code:      ErrCodeConcurrencyLimit,
		Message:   "no available processing slots",
		Retryable: true,
	}
}

// --- Helpers ---

// IsDomainError checks whether err is or wraps a *DomainError.
func IsDomainError(err error) bool {
	var domErr *DomainError
	return errors.As(err, &domErr)
}

// IsRetryable returns true if err is a retryable DomainError.
// Returns false for non-DomainError errors.
func IsRetryable(err error) bool {
	var domErr *DomainError
	if errors.As(err, &domErr) {
		return domErr.Retryable
	}
	return false
}

// ErrorCode extracts the error code from a DomainError.
// Returns empty string for non-DomainError errors.
func ErrorCode(err error) string {
	var domErr *DomainError
	if errors.As(err, &domErr) {
		return domErr.Code
	}
	return ""
}
