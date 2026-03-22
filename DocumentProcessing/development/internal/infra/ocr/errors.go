package ocr

import (
	"context"
	"errors"
	"fmt"

	"contractpro/document-processing/internal/domain/port"
)

// mapError translates transport/context errors into domain errors.
// Context errors (Canceled, DeadlineExceeded) pass through raw — this is the
// established pattern across the codebase so the orchestrator can distinguish
// cancellation from infrastructure failures.
// Network errors are wrapped as retryable OCR_FAILED.
func mapError(err error, operation string) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	return port.NewOCRError(
		fmt.Sprintf("ocr: %s: %v", operation, err), true, err,
	)
}

// mapHTTPStatus converts non-200 HTTP status codes into OCR domain errors.
// 429 (rate limit) and >=500 (server errors) are retryable.
// All other status codes are non-retryable.
func mapHTTPStatus(status int, body string) error {
	retryable := status == 429 || status >= 500

	return port.NewOCRError(
		fmt.Sprintf("ocr: HTTP %d: %s", status, body), retryable, nil,
	)
}
