package objectstorage

import (
	"context"
	"errors"
	"fmt"

	"contractpro/document-management/internal/domain/port"

	"github.com/aws/smithy-go"
)

// nonRetryableCodes lists S3 API error codes that are not retryable
// (configuration or permission issues, not transient failures).
var nonRetryableCodes = map[string]bool{
	"NoSuchKey":         true,
	"NoSuchBucket":      true,
	"AccessDenied":      true,
	"InvalidBucketName": true,
	"NotFound":          true,
}

// mapError converts S3/context errors to DomainError.
// Context errors (Canceled, DeadlineExceeded) pass through raw so the
// orchestrator can distinguish cancellation from infrastructure failures.
func mapError(err error, operation string) error {
	if err == nil {
		return nil
	}

	// Context errors pass through raw.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	// Smithy API error with a known code.
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		retryable := !nonRetryableCodes[apiErr.ErrorCode()]
		return &port.DomainError{
			Code:      port.ErrCodeStorageFailed,
			Message:   fmt.Sprintf("%s: %s (%s)", operation, apiErr.ErrorMessage(), apiErr.ErrorCode()),
			Retryable: retryable,
			Cause:     err,
		}
	}

	// Unknown / network error — assume retryable.
	return port.NewStorageError(fmt.Sprintf("%s failed", operation), err)
}
