package port

import (
	"errors"
	"fmt"
	"testing"
)

func TestConstructors(t *testing.T) {
	cause := fmt.Errorf("connection refused")

	tests := []struct {
		name          string
		err           *DomainError
		wantCode      string
		wantRetryable bool
		wantHasCause  bool
	}{
		{
			name:          "NewValidationError",
			err:           NewValidationError("invalid mime-type"),
			wantCode:      ErrCodeValidation,
			wantRetryable: false,
			wantHasCause:  false,
		},
		{
			name:          "NewFileTooLargeError",
			err:           NewFileTooLargeError("file exceeds 20 MB"),
			wantCode:      ErrCodeFileTooLarge,
			wantRetryable: false,
			wantHasCause:  false,
		},
		{
			name:          "NewTooManyPagesError",
			err:           NewTooManyPagesError("document has 150 pages"),
			wantCode:      ErrCodeTooManyPages,
			wantRetryable: false,
			wantHasCause:  false,
		},
		{
			name:          "NewInvalidFormatError",
			err:           NewInvalidFormatError("not a PDF file"),
			wantCode:      ErrCodeInvalidFormat,
			wantRetryable: false,
			wantHasCause:  false,
		},
		{
			name:          "NewFileNotFoundError",
			err:           NewFileNotFoundError("HTTP 404", cause),
			wantCode:      ErrCodeFileNotFound,
			wantRetryable: false,
			wantHasCause:  true,
		},
		{
			name:          "NewOCRError_retryable",
			err:           NewOCRError("rate limit exceeded", true, cause),
			wantCode:      ErrCodeOCRFailed,
			wantRetryable: true,
			wantHasCause:  true,
		},
		{
			name:          "NewOCRError_non_retryable",
			err:           NewOCRError("invalid input", false, cause),
			wantCode:      ErrCodeOCRFailed,
			wantRetryable: false,
			wantHasCause:  true,
		},
		{
			name:          "NewExtractionError",
			err:           NewExtractionError("text extraction failed", cause),
			wantCode:      ErrCodeExtractionFailed,
			wantRetryable: false,
			wantHasCause:  true,
		},
		{
			name:          "NewStorageError",
			err:           NewStorageError("upload failed", cause),
			wantCode:      ErrCodeStorageFailed,
			wantRetryable: true,
			wantHasCause:  true,
		},
		{
			name:          "NewBrokerError",
			err:           NewBrokerError("publish failed", cause),
			wantCode:      ErrCodeBrokerFailed,
			wantRetryable: true,
			wantHasCause:  true,
		},
		{
			name:          "NewTimeoutError",
			err:           NewTimeoutError("operation timed out", cause),
			wantCode:      ErrCodeTimeout,
			wantRetryable: true,
			wantHasCause:  true,
		},
		{
			name:          "NewServiceUnavailableError",
			err:           NewServiceUnavailableError("OCR service down", cause),
			wantCode:      ErrCodeServiceUnavailable,
			wantRetryable: true,
			wantHasCause:  true,
		},
		{
			name:          "NewDuplicateJobError",
			err:           NewDuplicateJobError("job-123"),
			wantCode:      ErrCodeDuplicateJob,
			wantRetryable: false,
			wantHasCause:  false,
		},
		{
			name:          "NewConcurrencyLimitError",
			err:           NewConcurrencyLimitError(),
			wantCode:      ErrCodeConcurrencyLimit,
			wantRetryable: true,
			wantHasCause:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Code != tt.wantCode {
				t.Errorf("Code = %q, want %q", tt.err.Code, tt.wantCode)
			}
			if tt.err.Retryable != tt.wantRetryable {
				t.Errorf("Retryable = %v, want %v", tt.err.Retryable, tt.wantRetryable)
			}
			if (tt.err.Cause != nil) != tt.wantHasCause {
				t.Errorf("Cause present = %v, want %v", tt.err.Cause != nil, tt.wantHasCause)
			}
			if tt.err.Message == "" {
				t.Error("Message should not be empty")
			}
		})
	}
}

func TestDuplicateJobError_ContainsJobID(t *testing.T) {
	err := NewDuplicateJobError("job-abc-456")
	if err.Message == "" {
		t.Fatal("Message should not be empty")
	}
	// Message should contain the job ID for debugging
	want := "job-abc-456"
	if got := err.Message; got != "" && !containsSubstring(got, want) {
		t.Errorf("Message = %q, should contain %q", got, want)
	}
}

func TestError_WithoutCause(t *testing.T) {
	err := NewValidationError("invalid mime-type")
	want := "VALIDATION_ERROR: invalid mime-type"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestError_WithCause(t *testing.T) {
	cause := fmt.Errorf("connection refused")
	err := NewStorageError("upload failed", cause)
	want := "STORAGE_FAILED: upload failed: connection refused"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestUnwrap_ReturnsCause(t *testing.T) {
	cause := fmt.Errorf("original error")
	err := NewStorageError("upload failed", cause)
	if got := err.Unwrap(); got != cause {
		t.Errorf("Unwrap() = %v, want %v", got, cause)
	}
}

func TestUnwrap_NilCause(t *testing.T) {
	err := NewValidationError("bad input")
	if got := err.Unwrap(); got != nil {
		t.Errorf("Unwrap() = %v, want nil", got)
	}
}

func TestErrorsAs_Direct(t *testing.T) {
	err := NewFileTooLargeError("too big")
	var domErr *DomainError
	if !errors.As(err, &domErr) {
		t.Fatal("errors.As should find *DomainError")
	}
	if domErr.Code != ErrCodeFileTooLarge {
		t.Errorf("Code = %q, want %q", domErr.Code, ErrCodeFileTooLarge)
	}
}

func TestErrorsAs_Wrapped(t *testing.T) {
	original := NewTimeoutError("timed out", nil)
	wrapped := fmt.Errorf("handler failed: %w", original)

	var domErr *DomainError
	if !errors.As(wrapped, &domErr) {
		t.Fatal("errors.As should find *DomainError through wrapping")
	}
	if domErr.Code != ErrCodeTimeout {
		t.Errorf("Code = %q, want %q", domErr.Code, ErrCodeTimeout)
	}
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"retryable_storage", NewStorageError("fail", nil), true},
		{"retryable_broker", NewBrokerError("fail", nil), true},
		{"retryable_timeout", NewTimeoutError("fail", nil), true},
		{"retryable_service_unavailable", NewServiceUnavailableError("fail", nil), true},
		{"retryable_concurrency_limit", NewConcurrencyLimitError(), true},
		{"retryable_ocr", NewOCRError("rate limit", true, nil), true},
		{"non_retryable_validation", NewValidationError("bad"), false},
		{"non_retryable_file_too_large", NewFileTooLargeError("big"), false},
		{"non_retryable_too_many_pages", NewTooManyPagesError("150"), false},
		{"non_retryable_invalid_format", NewInvalidFormatError("not pdf"), false},
		{"non_retryable_file_not_found", NewFileNotFoundError("404", nil), false},
		{"non_retryable_ocr", NewOCRError("bad input", false, nil), false},
		{"non_retryable_extraction", NewExtractionError("fail", nil), false},
		{"non_retryable_duplicate", NewDuplicateJobError("j1"), false},
		{"non_domain_error", fmt.Errorf("plain error"), false},
		{"wrapped_retryable", fmt.Errorf("wrap: %w", NewStorageError("fail", nil)), true},
		{"wrapped_non_retryable", fmt.Errorf("wrap: %w", NewValidationError("bad")), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRetryable(tt.err); got != tt.want {
				t.Errorf("IsRetryable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestErrorCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"validation", NewValidationError("bad"), ErrCodeValidation},
		{"file_too_large", NewFileTooLargeError("big"), ErrCodeFileTooLarge},
		{"too_many_pages", NewTooManyPagesError("150"), ErrCodeTooManyPages},
		{"invalid_format", NewInvalidFormatError("not pdf"), ErrCodeInvalidFormat},
		{"file_not_found", NewFileNotFoundError("404", nil), ErrCodeFileNotFound},
		{"ocr_failed", NewOCRError("fail", true, nil), ErrCodeOCRFailed},
		{"extraction_failed", NewExtractionError("fail", nil), ErrCodeExtractionFailed},
		{"storage_failed", NewStorageError("fail", nil), ErrCodeStorageFailed},
		{"broker_failed", NewBrokerError("fail", nil), ErrCodeBrokerFailed},
		{"timeout", NewTimeoutError("fail", nil), ErrCodeTimeout},
		{"service_unavailable", NewServiceUnavailableError("fail", nil), ErrCodeServiceUnavailable},
		{"duplicate_job", NewDuplicateJobError("j1"), ErrCodeDuplicateJob},
		{"concurrency_limit", NewConcurrencyLimitError(), ErrCodeConcurrencyLimit},
		{"non_domain_error", fmt.Errorf("plain"), ""},
		{"wrapped", fmt.Errorf("wrap: %w", NewTimeoutError("t", nil)), ErrCodeTimeout},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ErrorCode(tt.err); got != tt.want {
				t.Errorf("ErrorCode() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsDomainError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"domain_error", NewValidationError("bad"), true},
		{"wrapped_domain_error", fmt.Errorf("wrap: %w", NewTimeoutError("t", nil)), true},
		{"plain_error", fmt.Errorf("plain"), false},
		{"nil_error_is_not_checked", NewConcurrencyLimitError(), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsDomainError(tt.err); got != tt.want {
				t.Errorf("IsDomainError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && searchSubstring(s, substr))
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
