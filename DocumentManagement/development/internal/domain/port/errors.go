package port

import (
	"errors"
	"fmt"
)

// Error code constants for typed domain errors.
const (
	// --- Validation errors (non-retryable) ---

	ErrCodeValidation     = "VALIDATION_ERROR"
	ErrCodeInvalidStatus  = "INVALID_STATUS"
	ErrCodeInvalidPayload = "INVALID_PAYLOAD"

	// --- Not-found errors (non-retryable) ---

	ErrCodeDocumentNotFound = "DOCUMENT_NOT_FOUND"
	ErrCodeVersionNotFound  = "VERSION_NOT_FOUND"
	ErrCodeArtifactNotFound = "ARTIFACT_NOT_FOUND"
	ErrCodeDiffNotFound     = "DIFF_NOT_FOUND"

	// --- Conflict errors (non-retryable) ---

	ErrCodeDocumentAlreadyExists = "DOCUMENT_ALREADY_EXISTS"
	ErrCodeVersionAlreadyExists  = "VERSION_ALREADY_EXISTS"
	ErrCodeArtifactAlreadyExists = "ARTIFACT_ALREADY_EXISTS"
	ErrCodeDiffAlreadyExists     = "DIFF_ALREADY_EXISTS"
	ErrCodeStatusTransition      = "INVALID_STATUS_TRANSITION"
	ErrCodeDuplicateEvent        = "DUPLICATE_EVENT"

	// --- Content validation errors (non-retryable) ---

	ErrCodeInvalidContent        = "INVALID_CONTENT"
	ErrCodeIntegrityCheckFailed  = "INTEGRITY_CHECK_FAILED"

	// --- Authorization errors (non-retryable) ---

	ErrCodeTenantMismatch = "TENANT_MISMATCH"

	// --- Infrastructure errors (retryable) ---

	ErrCodeStorageFailed  = "STORAGE_FAILED"
	ErrCodeDatabaseFailed = "DATABASE_FAILED"
	ErrCodeBrokerFailed   = "BROKER_FAILED"
	ErrCodeTimeout        = "TIMEOUT"
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

// NewValidationError creates a non-retryable validation error.
func NewValidationError(msg string) *DomainError {
	return &DomainError{Code: ErrCodeValidation, Message: msg, Retryable: false}
}

// NewInvalidStatusError creates a non-retryable error for operations on documents
// or versions in an incompatible status.
func NewInvalidStatusError(msg string) *DomainError {
	return &DomainError{Code: ErrCodeInvalidStatus, Message: msg, Retryable: false}
}

// NewInvalidPayloadError creates a non-retryable error for malformed event payloads.
func NewInvalidPayloadError(msg string, cause error) *DomainError {
	return &DomainError{Code: ErrCodeInvalidPayload, Message: msg, Retryable: false, Cause: cause}
}

// NewDocumentNotFoundError creates a non-retryable error when the requested
// document does not exist within the given organization.
func NewDocumentNotFoundError(organizationID, documentID string) *DomainError {
	return &DomainError{
		Code:      ErrCodeDocumentNotFound,
		Message:   fmt.Sprintf("document %s not found in organization %s", documentID, organizationID),
		Retryable: false,
	}
}

// NewVersionNotFoundError creates a non-retryable error when the requested
// document version does not exist.
func NewVersionNotFoundError(versionID string) *DomainError {
	return &DomainError{
		Code:      ErrCodeVersionNotFound,
		Message:   fmt.Sprintf("version %s not found", versionID),
		Retryable: false,
	}
}

// NewArtifactNotFoundError creates a non-retryable error when the requested
// artifact does not exist for the given version.
func NewArtifactNotFoundError(versionID, artifactType string) *DomainError {
	return &DomainError{
		Code:      ErrCodeArtifactNotFound,
		Message:   fmt.Sprintf("artifact %s not found for version %s", artifactType, versionID),
		Retryable: false,
	}
}

// NewDiffNotFoundError creates a non-retryable error when no diff exists
// between the requested version pair.
func NewDiffNotFoundError(baseVersionID, targetVersionID string) *DomainError {
	return &DomainError{
		Code:      ErrCodeDiffNotFound,
		Message:   fmt.Sprintf("diff not found between versions %s and %s", baseVersionID, targetVersionID),
		Retryable: false,
	}
}

// NewDocumentAlreadyExistsError creates a non-retryable conflict error.
func NewDocumentAlreadyExistsError(documentID string) *DomainError {
	return &DomainError{
		Code:      ErrCodeDocumentAlreadyExists,
		Message:   fmt.Sprintf("document %s already exists", documentID),
		Retryable: false,
	}
}

// NewVersionAlreadyExistsError creates a non-retryable conflict error.
func NewVersionAlreadyExistsError(versionID string) *DomainError {
	return &DomainError{
		Code:      ErrCodeVersionAlreadyExists,
		Message:   fmt.Sprintf("version %s already exists", versionID),
		Retryable: false,
	}
}

// NewArtifactAlreadyExistsError creates a non-retryable conflict error
// when an artifact of the same type already exists for a version.
func NewArtifactAlreadyExistsError(versionID, artifactType string) *DomainError {
	return &DomainError{
		Code:      ErrCodeArtifactAlreadyExists,
		Message:   fmt.Sprintf("artifact %s already exists for version %s", artifactType, versionID),
		Retryable: false,
	}
}

// NewDiffAlreadyExistsError creates a non-retryable conflict error
// when a diff already exists for the given version pair.
func NewDiffAlreadyExistsError(baseVersionID, targetVersionID string) *DomainError {
	return &DomainError{
		Code:      ErrCodeDiffAlreadyExists,
		Message:   fmt.Sprintf("diff already exists between versions %s and %s", baseVersionID, targetVersionID),
		Retryable: false,
	}
}

// NewStatusTransitionError creates a non-retryable error for invalid
// document or artifact status transitions.
func NewStatusTransitionError(from, to string) *DomainError {
	return &DomainError{
		Code:      ErrCodeStatusTransition,
		Message:   fmt.Sprintf("invalid status transition from %s to %s", from, to),
		Retryable: false,
	}
}

// NewDuplicateEventError creates a non-retryable error when an event
// has already been processed (idempotency guard).
func NewDuplicateEventError(key string) *DomainError {
	return &DomainError{
		Code:      ErrCodeDuplicateEvent,
		Message:   fmt.Sprintf("event %s already processed", key),
		Retryable: false,
	}
}

// NewInvalidContentError creates a non-retryable error for artifact content
// that fails validation (size limit exceeded, malformed JSON, invalid blob
// reference). Causes the event to be routed to DLQ (BRE-029).
func NewInvalidContentError(msg string) *DomainError {
	return &DomainError{Code: ErrCodeInvalidContent, Message: msg, Retryable: false}
}

// NewIntegrityCheckError creates a non-retryable error when the content
// hash of data read from Object Storage does not match the expected hash
// stored in the ArtifactDescriptor (BRE-027).
func NewIntegrityCheckError(storageKey, expected, actual string) *DomainError {
	return &DomainError{
		Code:      ErrCodeIntegrityCheckFailed,
		Message:   fmt.Sprintf("content hash mismatch for %s: expected %s, got %s", storageKey, expected, actual),
		Retryable: false,
	}
}

// NewTenantMismatchError creates a non-retryable error when the
// organization_id in the request does not match the entity's organization.
func NewTenantMismatchError(entityID, expectedOrg, actualOrg string) *DomainError {
	return &DomainError{
		Code:      ErrCodeTenantMismatch,
		Message:   fmt.Sprintf("entity %s belongs to organization %s, not %s", entityID, actualOrg, expectedOrg),
		Retryable: false,
	}
}

// NewStorageError creates a retryable object storage error.
func NewStorageError(msg string, cause error) *DomainError {
	return &DomainError{Code: ErrCodeStorageFailed, Message: msg, Retryable: true, Cause: cause}
}

// NewDatabaseError creates a retryable database error.
func NewDatabaseError(msg string, cause error) *DomainError {
	return &DomainError{Code: ErrCodeDatabaseFailed, Message: msg, Retryable: true, Cause: cause}
}

// NewBrokerError creates a retryable message broker error.
func NewBrokerError(msg string, cause error) *DomainError {
	return &DomainError{Code: ErrCodeBrokerFailed, Message: msg, Retryable: true, Cause: cause}
}

// NewTimeoutError creates a retryable timeout error.
func NewTimeoutError(msg string, cause error) *DomainError {
	return &DomainError{Code: ErrCodeTimeout, Message: msg, Retryable: true, Cause: cause}
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

// IsNotFound returns true if err is a not-found DomainError
// (document, version, artifact, or diff).
func IsNotFound(err error) bool {
	code := ErrorCode(err)
	return code == ErrCodeDocumentNotFound ||
		code == ErrCodeVersionNotFound ||
		code == ErrCodeArtifactNotFound ||
		code == ErrCodeDiffNotFound
}

// IsConflict returns true if err is a conflict DomainError
// (already-exists or invalid status transition).
func IsConflict(err error) bool {
	code := ErrorCode(err)
	return code == ErrCodeDocumentAlreadyExists ||
		code == ErrCodeVersionAlreadyExists ||
		code == ErrCodeArtifactAlreadyExists ||
		code == ErrCodeDiffAlreadyExists ||
		code == ErrCodeStatusTransition
}

// IsDuplicateEvent returns true if err is a duplicate-event DomainError
// (idempotency guard hit — event already processed).
func IsDuplicateEvent(err error) bool {
	return ErrorCode(err) == ErrCodeDuplicateEvent
}
