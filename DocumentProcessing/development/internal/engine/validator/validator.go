package validator

import (
	"context"
	"fmt"

	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
)

// Validator implements InputValidatorPort — validates command metadata
// before file download (FR-1.1.1).
type Validator struct {
	maxFileSize     int64    // maximum allowed file size in bytes
	allowedMimeType string   // expected mime type (e.g. "application/pdf")
	resolver        Resolver // DNS resolver for SSRF checks; nil uses net.DefaultResolver
}

// NewValidator creates a Validator with the given max file size limit (bytes) and
// allowed mime type. Typically called with config.Limits.MaxFileSize
// (default 20 MB) and "application/pdf".
// The resolver parameter controls DNS lookups for SSRF validation;
// pass nil to use net.DefaultResolver.
func NewValidator(maxFileSize int64, allowedMimeType string, resolver Resolver) *Validator {
	return &Validator{
		maxFileSize:     maxFileSize,
		allowedMimeType: allowedMimeType,
		resolver:        resolver,
	}
}

// Validate checks ProcessDocumentCommand metadata and returns a typed
// DomainError on the first violated rule. Returns nil if all checks pass.
//
// Rules (checked in order):
//  1. document_id must not be empty → VALIDATION_ERROR
//  2. file_url must not be empty → VALIDATION_ERROR
//  3. file_url must use http/https scheme and not target blocked IPs → SSRF_BLOCKED
//  4. If file_size is declared (>0), it must be ≤ maxFileSize → FILE_TOO_LARGE
//  5. If mime_type is declared (non-empty), it must match allowedMimeType → INVALID_FORMAT
func (v *Validator) Validate(ctx context.Context, cmd model.ProcessDocumentCommand) error {
	if cmd.DocumentID == "" {
		return port.NewValidationError("document_id is required")
	}
	if cmd.FileURL == "" {
		return port.NewValidationError("file_url is required")
	}
	if err := ValidateURLSecurity(ctx, cmd.FileURL, v.resolver); err != nil {
		return err
	}
	if cmd.FileSize > 0 && cmd.FileSize > v.maxFileSize {
		return port.NewFileTooLargeError(
			fmt.Sprintf("declared file size %d bytes exceeds limit %d bytes", cmd.FileSize, v.maxFileSize),
		)
	}
	if cmd.MimeType != "" && cmd.MimeType != v.allowedMimeType {
		return port.NewInvalidFormatError(
			fmt.Sprintf("unsupported mime type %q, expected %s", cmd.MimeType, v.allowedMimeType),
		)
	}
	return nil
}

// compile-time check: Validator implements InputValidatorPort.
var _ port.InputValidatorPort = (*Validator)(nil)
