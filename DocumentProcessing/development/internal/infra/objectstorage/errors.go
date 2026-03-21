package objectstorage

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"contractpro/document-processing/internal/domain/port"
)

// nonRetryableCodes lists S3 API error codes that are permanent failures.
// Retrying these is pointless — the condition will not resolve on its own.
var nonRetryableCodes = map[string]bool{
	"NoSuchKey":         true,
	"NoSuchBucket":      true,
	"AccessDenied":      true,
	"InvalidBucketName": true,
	"NotFound":          true,
}

// mapError translates S3 and context errors into domain errors.
// Context errors (Canceled, DeadlineExceeded) pass through raw — this is the
// established pattern across the codebase so the orchestrator can distinguish
// cancellation from infrastructure failures.
func mapError(err error, operation string) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	// Check for typed NoSuchKey first (returned by GetObject).
	var noSuchKey *types.NoSuchKey
	if errors.As(err, &noSuchKey) {
		return &port.DomainError{
			Code:      port.ErrCodeStorageFailed,
			Message:   fmt.Sprintf("objectstorage: %s: NoSuchKey", operation),
			Retryable: false,
			Cause:     err,
		}
	}

	// Check for general smithy API errors (NoSuchBucket, AccessDenied, etc.).
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		retryable := !nonRetryableCodes[apiErr.ErrorCode()]
		return &port.DomainError{
			Code:      port.ErrCodeStorageFailed,
			Message:   fmt.Sprintf("objectstorage: %s: %s: %s", operation, apiErr.ErrorCode(), apiErr.ErrorMessage()),
			Retryable: retryable,
			Cause:     err,
		}
	}

	// Unknown / network-level errors — retryable by default.
	return port.NewStorageError(
		fmt.Sprintf("objectstorage: %s", operation), err,
	)
}
