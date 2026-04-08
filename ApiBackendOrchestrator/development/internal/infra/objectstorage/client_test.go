package objectstorage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"

	"contractpro/api-orchestrator/internal/config"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// mockS3API implements S3API for testing via configurable function fields.
type mockS3API struct {
	putObjectFn    func(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	deleteObjectFn func(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	headObjectFn   func(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
}

var _ S3API = (*mockS3API)(nil)

func (m *mockS3API) PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	if m.putObjectFn != nil {
		return m.putObjectFn(ctx, params, optFns...)
	}
	return &s3.PutObjectOutput{}, nil
}

func (m *mockS3API) DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	if m.deleteObjectFn != nil {
		return m.deleteObjectFn(ctx, params, optFns...)
	}
	return &s3.DeleteObjectOutput{}, nil
}

func (m *mockS3API) HeadObject(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	if m.headObjectFn != nil {
		return m.headObjectFn(ctx, params, optFns...)
	}
	return &s3.HeadObjectOutput{}, nil
}

// apiError implements smithy.APIError for test classification.
type apiError struct {
	code    string
	message string
}

func (e *apiError) Error() string              { return fmt.Sprintf("%s: %s", e.code, e.message) }
func (e *apiError) ErrorCode() string          { return e.code }
func (e *apiError) ErrorMessage() string       { return e.message }
func (e *apiError) ErrorFault() smithy.ErrorFault { return smithy.FaultServer }

// testCBConfig returns a low-threshold config for fast test CB tripping.
func testCBConfig(threshold int) config.CircuitBreakerConfig {
	return config.CircuitBreakerConfig{
		FailureThreshold: threshold,
		Timeout:          200 * time.Millisecond,
		MaxRequests:      1,
	}
}

func testClient(api S3API) *Client {
	return newClientWithS3(api, "test-bucket", testCBConfig(2), 5*time.Second, 5*time.Second, logger.NewLogger("debug"))
}

// ---------------------------------------------------------------------------
// PutObject tests
// ---------------------------------------------------------------------------

func TestPutObject_Success(t *testing.T) {
	var captured *s3.PutObjectInput
	mock := &mockS3API{
		putObjectFn: func(_ context.Context, params *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			captured = params
			return &s3.PutObjectOutput{}, nil
		},
	}
	c := testClient(mock)
	data := bytes.NewReader([]byte("pdf-content"))

	err := c.PutObject(context.Background(), "uploads/org/doc/uuid", data, "application/pdf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *captured.Bucket != "test-bucket" {
		t.Errorf("bucket = %q, want %q", *captured.Bucket, "test-bucket")
	}
	if *captured.Key != "uploads/org/doc/uuid" {
		t.Errorf("key = %q, want %q", *captured.Key, "uploads/org/doc/uuid")
	}
	if *captured.ContentType != "application/pdf" {
		t.Errorf("content-type = %q, want %q", *captured.ContentType, "application/pdf")
	}
	body, _ := io.ReadAll(captured.Body)
	if string(body) != "pdf-content" {
		t.Errorf("body = %q, want %q", body, "pdf-content")
	}
}

func TestPutObject_Retry5xxThenSuccess(t *testing.T) {
	var calls atomic.Int32
	mock := &mockS3API{
		putObjectFn: func(_ context.Context, _ *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			if calls.Add(1) == 1 {
				return nil, &apiError{code: "InternalError", message: "internal"}
			}
			return &s3.PutObjectOutput{}, nil
		},
	}
	c := newClientWithS3(mock, "b", testCBConfig(5), 5*time.Second, 5*time.Second, logger.NewLogger("debug"))
	c.maxRetries = 2

	err := c.PutObject(context.Background(), "k", bytes.NewReader([]byte("x")), "application/pdf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("calls = %d, want 2", got)
	}
}

func TestPutObject_RetryTimeoutThenSuccess(t *testing.T) {
	var calls atomic.Int32
	mock := &mockS3API{
		putObjectFn: func(ctx context.Context, _ *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			if calls.Add(1) == 1 {
				return nil, context.DeadlineExceeded
			}
			return &s3.PutObjectOutput{}, nil
		},
	}
	c := newClientWithS3(mock, "b", testCBConfig(5), 5*time.Second, 5*time.Second, logger.NewLogger("debug"))

	err := c.PutObject(context.Background(), "k", bytes.NewReader([]byte("x")), "application/pdf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("calls = %d, want 2", got)
	}
}

func TestPutObject_NoRetry403(t *testing.T) {
	var calls atomic.Int32
	mock := &mockS3API{
		putObjectFn: func(_ context.Context, _ *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			calls.Add(1)
			return nil, &apiError{code: "AccessDenied", message: "forbidden"}
		},
	}
	c := testClient(mock)

	err := c.PutObject(context.Background(), "k", bytes.NewReader([]byte("x")), "application/pdf")
	if err == nil {
		t.Fatal("expected error")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1 (no retry for 403)", got)
	}
	var se *StorageError
	if !errors.As(err, &se) {
		t.Fatalf("expected StorageError, got %T", err)
	}
	if se.Retryable {
		t.Error("expected Retryable=false for AccessDenied")
	}
}

func TestPutObject_AllRetriesExhausted(t *testing.T) {
	var calls atomic.Int32
	mock := &mockS3API{
		putObjectFn: func(_ context.Context, _ *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			calls.Add(1)
			return nil, &apiError{code: "InternalError", message: "oops"}
		},
	}
	c := newClientWithS3(mock, "b", testCBConfig(10), 5*time.Second, 5*time.Second, logger.NewLogger("debug"))

	err := c.PutObject(context.Background(), "k", bytes.NewReader([]byte("x")), "application/pdf")
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("calls = %d, want 3 (1 + 2 retries)", got)
	}
	var se *StorageError
	if !errors.As(err, &se) {
		t.Fatalf("expected StorageError, got %T", err)
	}
	if !se.Retryable {
		t.Error("expected Retryable=true for InternalError")
	}
}

func TestPutObject_ParentContextCanceled(t *testing.T) {
	var calls atomic.Int32
	mock := &mockS3API{
		putObjectFn: func(_ context.Context, _ *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			calls.Add(1)
			return &s3.PutObjectOutput{}, nil
		},
	}
	c := testClient(mock)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := c.PutObject(ctx, "k", bytes.NewReader([]byte("x")), "application/pdf")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("calls = %d, want 0 (should bail before S3 call)", got)
	}
}

func TestPutObject_SeeksBeforeRetry(t *testing.T) {
	var bodies []string
	var calls atomic.Int32
	mock := &mockS3API{
		putObjectFn: func(_ context.Context, params *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			b, _ := io.ReadAll(params.Body)
			bodies = append(bodies, string(b))
			if calls.Add(1) == 1 {
				return nil, &apiError{code: "InternalError", message: "fail"}
			}
			return &s3.PutObjectOutput{}, nil
		},
	}
	c := newClientWithS3(mock, "b", testCBConfig(5), 5*time.Second, 5*time.Second, logger.NewLogger("debug"))

	err := c.PutObject(context.Background(), "k", bytes.NewReader([]byte("hello")), "text/plain")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(bodies) != 2 {
		t.Fatalf("expected 2 bodies, got %d", len(bodies))
	}
	if bodies[0] != "hello" || bodies[1] != "hello" {
		t.Errorf("bodies = %v, want both 'hello' (seek must reset)", bodies)
	}
}

// ---------------------------------------------------------------------------
// DeleteObject tests
// ---------------------------------------------------------------------------

func TestDeleteObject_Success(t *testing.T) {
	var captured *s3.DeleteObjectInput
	mock := &mockS3API{
		deleteObjectFn: func(_ context.Context, params *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
			captured = params
			return &s3.DeleteObjectOutput{}, nil
		},
	}
	c := testClient(mock)

	err := c.DeleteObject(context.Background(), "uploads/org/doc/uuid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *captured.Bucket != "test-bucket" {
		t.Errorf("bucket = %q, want %q", *captured.Bucket, "test-bucket")
	}
	if *captured.Key != "uploads/org/doc/uuid" {
		t.Errorf("key = %q, want %q", *captured.Key, "uploads/org/doc/uuid")
	}
}

func TestDeleteObject_Retry5xxThenSuccess(t *testing.T) {
	var calls atomic.Int32
	mock := &mockS3API{
		deleteObjectFn: func(_ context.Context, _ *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
			if calls.Add(1) == 1 {
				return nil, &apiError{code: "ServiceUnavailable", message: "busy"}
			}
			return &s3.DeleteObjectOutput{}, nil
		},
	}
	c := newClientWithS3(mock, "b", testCBConfig(5), 5*time.Second, 5*time.Second, logger.NewLogger("debug"))

	err := c.DeleteObject(context.Background(), "k")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("calls = %d, want 2", got)
	}
}

func TestDeleteObject_AllRetriesExhausted(t *testing.T) {
	var calls atomic.Int32
	mock := &mockS3API{
		deleteObjectFn: func(_ context.Context, _ *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
			calls.Add(1)
			return nil, &apiError{code: "InternalError", message: "oops"}
		},
	}
	c := newClientWithS3(mock, "b", testCBConfig(10), 5*time.Second, 5*time.Second, logger.NewLogger("debug"))

	err := c.DeleteObject(context.Background(), "k")
	if err == nil {
		t.Fatal("expected error")
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("calls = %d, want 3", got)
	}
}

func TestDeleteObject_NoRetry403(t *testing.T) {
	var calls atomic.Int32
	mock := &mockS3API{
		deleteObjectFn: func(_ context.Context, _ *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
			calls.Add(1)
			return nil, &apiError{code: "AccessDenied", message: "no"}
		},
	}
	c := testClient(mock)

	err := c.DeleteObject(context.Background(), "k")
	if err == nil {
		t.Fatal("expected error")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1", got)
	}
}

func TestDeleteObject_ParentContextCanceled(t *testing.T) {
	c := testClient(&mockS3API{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := c.DeleteObject(ctx, "k")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// HeadObject tests
// ---------------------------------------------------------------------------

func TestHeadObject_Success(t *testing.T) {
	mock := &mockS3API{
		headObjectFn: func(_ context.Context, _ *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
			return &s3.HeadObjectOutput{}, nil
		},
	}
	c := testClient(mock)

	err := c.HeadObject(context.Background(), "uploads/org/doc/uuid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHeadObject_NotFound(t *testing.T) {
	var calls atomic.Int32
	mock := &mockS3API{
		headObjectFn: func(_ context.Context, _ *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
			calls.Add(1)
			return nil, &apiError{code: "NotFound", message: "not found"}
		},
	}
	c := testClient(mock)

	err := c.HeadObject(context.Background(), "no-such-key")
	if !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("expected ErrObjectNotFound, got %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1 (404 is not retried)", got)
	}
}

func TestHeadObject_NoSuchKey(t *testing.T) {
	mock := &mockS3API{
		headObjectFn: func(_ context.Context, _ *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
			return nil, &apiError{code: "NoSuchKey", message: "no such key"}
		},
	}
	c := testClient(mock)

	err := c.HeadObject(context.Background(), "no-such-key")
	if !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("expected ErrObjectNotFound, got %v", err)
	}
}

func TestHeadObject_Retry5xxThenSuccess(t *testing.T) {
	var calls atomic.Int32
	mock := &mockS3API{
		headObjectFn: func(_ context.Context, _ *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
			if calls.Add(1) == 1 {
				return nil, &apiError{code: "InternalError", message: "fail"}
			}
			return &s3.HeadObjectOutput{}, nil
		},
	}
	c := newClientWithS3(mock, "b", testCBConfig(5), 5*time.Second, 5*time.Second, logger.NewLogger("debug"))

	err := c.HeadObject(context.Background(), "k")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("calls = %d, want 2", got)
	}
}

func TestHeadObject_NoRetry403(t *testing.T) {
	var calls atomic.Int32
	mock := &mockS3API{
		headObjectFn: func(_ context.Context, _ *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
			calls.Add(1)
			return nil, &apiError{code: "AccessDenied", message: "forbidden"}
		},
	}
	c := testClient(mock)

	err := c.HeadObject(context.Background(), "k")
	if err == nil {
		t.Fatal("expected error")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1", got)
	}
}

// ---------------------------------------------------------------------------
// Circuit breaker tests
// ---------------------------------------------------------------------------

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	var calls atomic.Int32
	mock := &mockS3API{
		headObjectFn: func(_ context.Context, _ *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
			calls.Add(1)
			return nil, &apiError{code: "InternalError", message: "fail"}
		},
	}
	// CB threshold = 2, high maxRetries to not exhaust retries before CB trips.
	c := newClientWithS3(mock, "b", testCBConfig(2), 5*time.Second, 5*time.Second, logger.NewLogger("debug"))
	c.maxRetries = 0 // No retry so each call is a single attempt.

	// Trip the CB: 2 failures.
	_ = c.HeadObject(context.Background(), "k")
	_ = c.HeadObject(context.Background(), "k")
	if got := calls.Load(); got != 2 {
		t.Fatalf("pre-trip calls = %d, want 2", got)
	}

	// Third call should be rejected by CB without hitting S3.
	err := c.HeadObject(context.Background(), "k")
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("post-trip calls = %d, want 2 (CB should block)", got)
	}
}

func TestCircuitBreaker_4xxDoesNotTrip(t *testing.T) {
	var calls atomic.Int32
	mock := &mockS3API{
		headObjectFn: func(_ context.Context, _ *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
			calls.Add(1)
			return nil, &apiError{code: "AccessDenied", message: "no"}
		},
	}
	c := newClientWithS3(mock, "b", testCBConfig(2), 5*time.Second, 5*time.Second, logger.NewLogger("debug"))
	c.maxRetries = 0

	// Make 5 calls with 403 — all should reach S3 (CB stays closed).
	for i := 0; i < 5; i++ {
		_ = c.HeadObject(context.Background(), "k")
	}
	if got := calls.Load(); got != 5 {
		t.Errorf("calls = %d, want 5 (4xx should not trip CB)", got)
	}
}

func TestCircuitBreaker_NotFoundDoesNotTrip(t *testing.T) {
	var calls atomic.Int32
	mock := &mockS3API{
		headObjectFn: func(_ context.Context, _ *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
			calls.Add(1)
			return nil, &apiError{code: "NotFound", message: "not found"}
		},
	}
	c := newClientWithS3(mock, "b", testCBConfig(2), 5*time.Second, 5*time.Second, logger.NewLogger("debug"))
	c.maxRetries = 0

	for i := 0; i < 5; i++ {
		_ = c.HeadObject(context.Background(), "k")
	}
	if got := calls.Load(); got != 5 {
		t.Errorf("calls = %d, want 5 (NotFound should not trip CB)", got)
	}
}

func TestCircuitBreaker_HalfOpenRecovery(t *testing.T) {
	var shouldFail atomic.Bool
	shouldFail.Store(true)
	var calls atomic.Int32

	mock := &mockS3API{
		headObjectFn: func(_ context.Context, _ *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
			calls.Add(1)
			if shouldFail.Load() {
				return nil, &apiError{code: "InternalError", message: "fail"}
			}
			return &s3.HeadObjectOutput{}, nil
		},
	}
	cbCfg := config.CircuitBreakerConfig{
		FailureThreshold: 2,
		Timeout:          100 * time.Millisecond, // Fast transition to half-open.
		MaxRequests:      1,
	}
	c := newClientWithS3(mock, "b", cbCfg, 5*time.Second, 5*time.Second, logger.NewLogger("debug"))
	c.maxRetries = 0

	// Trip to OPEN.
	_ = c.HeadObject(context.Background(), "k")
	_ = c.HeadObject(context.Background(), "k")

	// Wait for half-open.
	time.Sleep(150 * time.Millisecond)

	// Now fix S3 and call — should succeed and close CB.
	shouldFail.Store(false)
	err := c.HeadObject(context.Background(), "k")
	if err != nil {
		t.Fatalf("expected success in half-open, got %v", err)
	}

	// CB should be closed now — more calls succeed.
	err = c.HeadObject(context.Background(), "k")
	if err != nil {
		t.Fatalf("expected success after CB closed, got %v", err)
	}
}

func TestCircuitBreaker_ErrCircuitOpenNotRetried(t *testing.T) {
	var calls atomic.Int32
	mock := &mockS3API{
		deleteObjectFn: func(_ context.Context, _ *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
			calls.Add(1)
			return nil, &apiError{code: "InternalError", message: "fail"}
		},
	}
	c := newClientWithS3(mock, "b", testCBConfig(2), 5*time.Second, 5*time.Second, logger.NewLogger("debug"))
	c.maxRetries = 0

	// Trip CB.
	_ = c.DeleteObject(context.Background(), "k")
	_ = c.DeleteObject(context.Background(), "k")

	calls.Store(0)
	// Now with retries enabled — should still return immediately (no retry on CB open).
	c.maxRetries = 2
	err := c.DeleteObject(context.Background(), "k")
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("calls = %d, want 0 (CB open, no retry)", got)
	}
}

func TestCircuitBreaker_TripsDuringRetry(t *testing.T) {
	var calls atomic.Int32
	mock := &mockS3API{
		putObjectFn: func(_ context.Context, _ *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			calls.Add(1)
			return nil, &apiError{code: "InternalError", message: "fail"}
		},
	}
	// CB threshold = 2, long CB timeout so it stays OPEN during retries.
	cbCfg := config.CircuitBreakerConfig{
		FailureThreshold: 2,
		Timeout:          60 * time.Second, // Much longer than any backoff.
		MaxRequests:      1,
	}
	c := newClientWithS3(mock, "b", cbCfg, 5*time.Second, 5*time.Second, logger.NewLogger("debug"))
	c.maxRetries = 5

	err := c.PutObject(context.Background(), "k", bytes.NewReader([]byte("x")), "application/pdf")
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen after CB trips mid-retry, got %v", err)
	}
	// Should have made exactly 2 S3 calls (threshold) then CB blocks.
	if got := calls.Load(); got != 2 {
		t.Errorf("calls = %d, want 2 (CB trips after threshold)", got)
	}
}

func TestPutObject_ContextCancelledDuringBackoff(t *testing.T) {
	var calls atomic.Int32
	mock := &mockS3API{
		putObjectFn: func(_ context.Context, _ *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			calls.Add(1)
			return nil, &apiError{code: "InternalError", message: "fail"}
		},
	}
	c := newClientWithS3(mock, "b", testCBConfig(10), 5*time.Second, 5*time.Second, logger.NewLogger("debug"))

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel shortly after first attempt (during backoff wait).
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	err := c.PutObject(ctx, "k", bytes.NewReader([]byte("x")), "application/pdf")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1 (cancelled during backoff after 1st attempt)", got)
	}
}

// ---------------------------------------------------------------------------
// mapError tests
// ---------------------------------------------------------------------------

func TestMapError_Code404_ReturnsNotFound(t *testing.T) {
	err := mapError(&apiError{code: "404", message: "not found"}, "HeadObject")
	if !errors.Is(err, ErrObjectNotFound) {
		t.Errorf("expected ErrObjectNotFound for code '404', got %v", err)
	}
}

func TestMapError_ContextCanceled(t *testing.T) {
	err := mapError(context.Canceled, "PutObject")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestMapError_ContextDeadlineExceeded(t *testing.T) {
	err := mapError(context.DeadlineExceeded, "PutObject")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}
}

func TestMapError_NoSuchKey(t *testing.T) {
	err := mapError(&apiError{code: "NoSuchKey", message: "no such key"}, "HeadObject")
	if !errors.Is(err, ErrObjectNotFound) {
		t.Errorf("expected ErrObjectNotFound, got %v", err)
	}
}

func TestMapError_NotFound(t *testing.T) {
	err := mapError(&apiError{code: "NotFound", message: "not found"}, "HeadObject")
	if !errors.Is(err, ErrObjectNotFound) {
		t.Errorf("expected ErrObjectNotFound, got %v", err)
	}
}

func TestMapError_AccessDenied_NonRetryable(t *testing.T) {
	err := mapError(&apiError{code: "AccessDenied", message: "denied"}, "PutObject")
	var se *StorageError
	if !errors.As(err, &se) {
		t.Fatalf("expected StorageError, got %T", err)
	}
	if se.Retryable {
		t.Error("expected Retryable=false for AccessDenied")
	}
	if se.Operation != "PutObject" {
		t.Errorf("operation = %q, want PutObject", se.Operation)
	}
}

func TestMapError_InternalError_Retryable(t *testing.T) {
	err := mapError(&apiError{code: "InternalError", message: "oops"}, "PutObject")
	var se *StorageError
	if !errors.As(err, &se) {
		t.Fatalf("expected StorageError, got %T", err)
	}
	if !se.Retryable {
		t.Error("expected Retryable=true for InternalError")
	}
}

func TestMapError_NetError_Retryable(t *testing.T) {
	err := mapError(&net.OpError{Op: "dial", Err: errors.New("connection refused")}, "PutObject")
	var se *StorageError
	if !errors.As(err, &se) {
		t.Fatalf("expected StorageError, got %T", err)
	}
	if !se.Retryable {
		t.Error("expected Retryable=true for network error")
	}
}

func TestMapError_UnknownError_Retryable(t *testing.T) {
	err := mapError(errors.New("something unexpected"), "DeleteObject")
	var se *StorageError
	if !errors.As(err, &se) {
		t.Fatalf("expected StorageError, got %T", err)
	}
	if !se.Retryable {
		t.Error("expected Retryable=true for unknown error (safe default)")
	}
}

// ---------------------------------------------------------------------------
// isRetryable tests
// ---------------------------------------------------------------------------

func TestIsRetryable_RetryableStorageError(t *testing.T) {
	if !isRetryable(&StorageError{Retryable: true}) {
		t.Error("expected true")
	}
}

func TestIsRetryable_NonRetryableStorageError(t *testing.T) {
	if isRetryable(&StorageError{Retryable: false}) {
		t.Error("expected false")
	}
}

func TestIsRetryable_ErrCircuitOpen(t *testing.T) {
	if isRetryable(fmt.Errorf("wrap: %w", ErrCircuitOpen)) {
		t.Error("expected false for ErrCircuitOpen")
	}
}

func TestIsRetryable_ErrObjectNotFound(t *testing.T) {
	if isRetryable(fmt.Errorf("wrap: %w", ErrObjectNotFound)) {
		t.Error("expected false for ErrObjectNotFound")
	}
}

func TestIsRetryable_ContextCanceled(t *testing.T) {
	if isRetryable(context.Canceled) {
		t.Error("expected false for context.Canceled")
	}
}

func TestIsRetryable_ContextDeadlineExceeded(t *testing.T) {
	if !isRetryable(context.DeadlineExceeded) {
		t.Error("expected true for per-attempt DeadlineExceeded")
	}
}

// ---------------------------------------------------------------------------
// isCBFailure tests
// ---------------------------------------------------------------------------

func TestIsCBFailure_RetryableStorageError(t *testing.T) {
	if !isCBFailure(&StorageError{Retryable: true}) {
		t.Error("expected true")
	}
}

func TestIsCBFailure_NonRetryableStorageError(t *testing.T) {
	if isCBFailure(&StorageError{Retryable: false}) {
		t.Error("expected false for non-retryable")
	}
}

func TestIsCBFailure_ContextCanceled(t *testing.T) {
	if isCBFailure(context.Canceled) {
		t.Error("expected false for context.Canceled")
	}
}

func TestIsCBFailure_ContextDeadlineExceeded(t *testing.T) {
	if !isCBFailure(context.DeadlineExceeded) {
		t.Error("expected true for DeadlineExceeded")
	}
}

func TestIsCBFailure_ErrObjectNotFound(t *testing.T) {
	if isCBFailure(fmt.Errorf("wrap: %w", ErrObjectNotFound)) {
		t.Error("expected false for ErrObjectNotFound")
	}
}

// ---------------------------------------------------------------------------
// backoffDelay tests
// ---------------------------------------------------------------------------

func TestBackoffDelay_Values(t *testing.T) {
	c := testClient(&mockS3API{})

	for i := 0; i < 20; i++ {
		d1 := c.backoffDelay(1)
		if d1 < 500*time.Millisecond || d1 > 625*time.Millisecond {
			t.Errorf("attempt 1: delay = %v, want [500ms, 625ms]", d1)
		}
		d2 := c.backoffDelay(2)
		if d2 < 1000*time.Millisecond || d2 > 1250*time.Millisecond {
			t.Errorf("attempt 2: delay = %v, want [1000ms, 1250ms]", d2)
		}
	}
}

// ---------------------------------------------------------------------------
// StorageError tests
// ---------------------------------------------------------------------------

func TestStorageError_ErrorString(t *testing.T) {
	se := &StorageError{
		Operation: "PutObject",
		Message:   "InternalError: oops",
		Retryable: true,
		Cause:     errors.New("original"),
	}
	want := "objectstorage: PutObject: InternalError: oops: original"
	if se.Error() != want {
		t.Errorf("Error() = %q, want %q", se.Error(), want)
	}
}

func TestStorageError_ErrorStringNilCause(t *testing.T) {
	se := &StorageError{
		Operation: "DeleteObject",
		Message:   "some error",
	}
	want := "objectstorage: DeleteObject: some error"
	if se.Error() != want {
		t.Errorf("Error() = %q, want %q", se.Error(), want)
	}
}

func TestStorageError_Unwrap(t *testing.T) {
	cause := errors.New("root cause")
	se := &StorageError{Cause: cause}
	if !errors.Is(se, cause) {
		t.Error("Unwrap should expose the cause")
	}
}

// ---------------------------------------------------------------------------
// Retry with network errors
// ---------------------------------------------------------------------------

func TestPutObject_RetryNetworkErrorThenSuccess(t *testing.T) {
	var calls atomic.Int32
	mock := &mockS3API{
		putObjectFn: func(_ context.Context, _ *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
			if calls.Add(1) == 1 {
				return nil, &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
			}
			return &s3.PutObjectOutput{}, nil
		},
	}
	c := newClientWithS3(mock, "b", testCBConfig(5), 5*time.Second, 5*time.Second, logger.NewLogger("debug"))

	err := c.PutObject(context.Background(), "k", bytes.NewReader([]byte("x")), "application/pdf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("calls = %d, want 2", got)
	}
}

// ---------------------------------------------------------------------------
// Seek error
// ---------------------------------------------------------------------------

func TestPutObject_SeekError_NonRetryable(t *testing.T) {
	c := testClient(&mockS3API{})

	err := c.PutObject(context.Background(), "k", &brokenSeeker{}, "application/pdf")
	if err == nil {
		t.Fatal("expected error")
	}
	var se *StorageError
	if !errors.As(err, &se) {
		t.Fatalf("expected StorageError, got %T", err)
	}
	if se.Retryable {
		t.Error("expected Retryable=false for seek error")
	}
}

// brokenSeeker always returns an error on Seek.
type brokenSeeker struct{}

func (b *brokenSeeker) Read([]byte) (int, error)          { return 0, io.EOF }
func (b *brokenSeeker) Seek(int64, int) (int64, error) { return 0, errors.New("seek broken") }
