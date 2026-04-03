package circuitbreaker

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"contractpro/document-management/internal/config"
	"contractpro/document-management/internal/domain/port"

	"github.com/sony/gobreaker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

// mockStorage implements port.ObjectStoragePort with function fields.
// Each method delegates to the corresponding function field if set, otherwise
// returns a zero-value success.
type mockStorage struct {
	putObjectFn           func(ctx context.Context, key string, data io.Reader, contentType string) error
	getObjectFn           func(ctx context.Context, key string) (io.ReadCloser, error)
	deleteObjectFn        func(ctx context.Context, key string) error
	headObjectFn          func(ctx context.Context, key string) (int64, bool, error)
	generatePresignedFn   func(ctx context.Context, key string, expiry time.Duration) (string, error)
	deleteByPrefixFn      func(ctx context.Context, prefix string) error
}

func (m *mockStorage) PutObject(ctx context.Context, key string, data io.Reader, contentType string) error {
	if m.putObjectFn != nil {
		return m.putObjectFn(ctx, key, data, contentType)
	}
	return nil
}

func (m *mockStorage) GetObject(ctx context.Context, key string) (io.ReadCloser, error) {
	if m.getObjectFn != nil {
		return m.getObjectFn(ctx, key)
	}
	return io.NopCloser(strings.NewReader("")), nil
}

func (m *mockStorage) DeleteObject(ctx context.Context, key string) error {
	if m.deleteObjectFn != nil {
		return m.deleteObjectFn(ctx, key)
	}
	return nil
}

func (m *mockStorage) HeadObject(ctx context.Context, key string) (int64, bool, error) {
	if m.headObjectFn != nil {
		return m.headObjectFn(ctx, key)
	}
	return 0, false, nil
}

func (m *mockStorage) GeneratePresignedURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	if m.generatePresignedFn != nil {
		return m.generatePresignedFn(ctx, key, expiry)
	}
	return "", nil
}

func (m *mockStorage) DeleteByPrefix(ctx context.Context, prefix string) error {
	if m.deleteByPrefixFn != nil {
		return m.deleteByPrefixFn(ctx, prefix)
	}
	return nil
}

// stateCall records a single SetCircuitBreakerState invocation.
type stateCall struct {
	Component string
	State     float64
}

// mockReporter implements StateReporter and records all invocations.
type mockReporter struct {
	mu    sync.Mutex
	calls []stateCall
}

func (r *mockReporter) SetCircuitBreakerState(component string, state float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, stateCall{Component: component, State: state})
}

func (r *mockReporter) getCalls() []stateCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make([]stateCall, len(r.calls))
	copy(cp, r.calls)
	return cp
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// testCfg returns a CircuitBreakerConfig suitable for fast tests.
func testCfg() config.CircuitBreakerConfig {
	return config.CircuitBreakerConfig{
		MaxRequests:      1,
		Interval:         0, // never auto-clear failure counts in closed state
		Timeout:          100 * time.Millisecond,
		FailureThreshold: 3,
		PerEventBudget:   5 * time.Second,
	}
}

// newBreaker creates a test ObjectStorageBreaker with the given mock and config.
func newBreaker(inner port.ObjectStoragePort, cfg config.CircuitBreakerConfig) (*ObjectStorageBreaker, *mockReporter) {
	reporter := &mockReporter{}
	b := NewObjectStorageBreaker(inner, cfg, reporter)
	return b, reporter
}

// retryableErr returns a retryable DomainError that should trip the circuit.
func retryableErr() error {
	return port.NewStorageError("s3 unavailable", errors.New("connection refused"))
}

// nonRetryableErr returns a non-retryable DomainError that should NOT trip the circuit.
func nonRetryableErr() error {
	return port.NewValidationError("bad request")
}

// tripCircuit calls PutObject with retryable errors until the circuit opens.
func tripCircuit(t *testing.T, b *ObjectStorageBreaker, mock *mockStorage, threshold int) {
	t.Helper()
	mock.putObjectFn = func(ctx context.Context, key string, data io.Reader, contentType string) error {
		return retryableErr()
	}
	for i := 0; i < threshold; i++ {
		_ = b.PutObject(context.Background(), "key", nil, "")
	}
	require.Equal(t, gobreaker.StateOpen, b.State(), "circuit should be open after %d failures", threshold)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// 1. Constructor panics on nil arguments.
func TestNewObjectStorageBreaker_PanicsOnNilInner(t *testing.T) {
	reporter := &mockReporter{}
	assert.PanicsWithValue(t,
		"circuitbreaker: inner ObjectStoragePort must not be nil",
		func() { NewObjectStorageBreaker(nil, testCfg(), reporter) },
	)
}

func TestNewObjectStorageBreaker_PanicsOnNilReporter(t *testing.T) {
	mock := &mockStorage{}
	assert.PanicsWithValue(t,
		"circuitbreaker: StateReporter must not be nil",
		func() { NewObjectStorageBreaker(mock, testCfg(), nil) },
	)
}

// 2. Constructor sets initial metric value.
func TestNewObjectStorageBreaker_SetsInitialMetric(t *testing.T) {
	_, reporter := newBreaker(&mockStorage{}, testCfg())

	calls := reporter.getCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, "object_storage", calls[0].Component)
	assert.Equal(t, float64(0), calls[0].State)
}

// 3. Compile-time interface compliance (redundant with source, but explicit).
func TestObjectStorageBreaker_ImplementsPort(t *testing.T) {
	var _ port.ObjectStoragePort = (*ObjectStorageBreaker)(nil)
}

// 4. Closed state: calls pass through to inner, results returned correctly.
func TestClosedState_Passthrough(t *testing.T) {
	t.Run("PutObject", func(t *testing.T) {
		var called bool
		mock := &mockStorage{
			putObjectFn: func(ctx context.Context, key string, data io.Reader, contentType string) error {
				called = true
				assert.Equal(t, "k1", key)
				assert.Equal(t, "application/pdf", contentType)
				return nil
			},
		}
		b, _ := newBreaker(mock, testCfg())

		err := b.PutObject(context.Background(), "k1", nil, "application/pdf")
		require.NoError(t, err)
		assert.True(t, called, "inner.PutObject must be called")
	})

	t.Run("GetObject", func(t *testing.T) {
		wantBody := "hello world"
		mock := &mockStorage{
			getObjectFn: func(ctx context.Context, key string) (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader(wantBody)), nil
			},
		}
		b, _ := newBreaker(mock, testCfg())

		body, err := b.GetObject(context.Background(), "k2")
		require.NoError(t, err)
		defer body.Close()

		data, readErr := io.ReadAll(body)
		require.NoError(t, readErr)
		assert.Equal(t, wantBody, string(data))
	})

	t.Run("DeleteObject", func(t *testing.T) {
		var called bool
		mock := &mockStorage{
			deleteObjectFn: func(ctx context.Context, key string) error {
				called = true
				return nil
			},
		}
		b, _ := newBreaker(mock, testCfg())

		err := b.DeleteObject(context.Background(), "k3")
		require.NoError(t, err)
		assert.True(t, called)
	})

	t.Run("HeadObject", func(t *testing.T) {
		mock := &mockStorage{
			headObjectFn: func(ctx context.Context, key string) (int64, bool, error) {
				return 1024, true, nil
			},
		}
		b, _ := newBreaker(mock, testCfg())

		size, exists, err := b.HeadObject(context.Background(), "k4")
		require.NoError(t, err)
		assert.Equal(t, int64(1024), size)
		assert.True(t, exists)
	})

	t.Run("GeneratePresignedURL", func(t *testing.T) {
		mock := &mockStorage{
			generatePresignedFn: func(ctx context.Context, key string, expiry time.Duration) (string, error) {
				assert.Equal(t, 10*time.Minute, expiry)
				return "https://presigned.example.com/k5", nil
			},
		}
		b, _ := newBreaker(mock, testCfg())

		url, err := b.GeneratePresignedURL(context.Background(), "k5", 10*time.Minute)
		require.NoError(t, err)
		assert.Equal(t, "https://presigned.example.com/k5", url)
	})

	t.Run("DeleteByPrefix", func(t *testing.T) {
		var called bool
		mock := &mockStorage{
			deleteByPrefixFn: func(ctx context.Context, prefix string) error {
				called = true
				assert.Equal(t, "org/doc/", prefix)
				return nil
			},
		}
		b, _ := newBreaker(mock, testCfg())

		err := b.DeleteByPrefix(context.Background(), "org/doc/")
		require.NoError(t, err)
		assert.True(t, called)
	})

	t.Run("PutObject_InnerError", func(t *testing.T) {
		wantErr := retryableErr()
		mock := &mockStorage{
			putObjectFn: func(ctx context.Context, key string, data io.Reader, contentType string) error {
				return wantErr
			},
		}
		b, _ := newBreaker(mock, testCfg())

		err := b.PutObject(context.Background(), "k6", nil, "")
		require.Error(t, err)
		// Error should pass through (not wrapped in circuit error since circuit is closed).
		assert.Equal(t, port.ErrCodeStorageFailed, port.ErrorCode(err))
	})
}

// 5. Circuit opens after N consecutive retryable failures.
func TestCircuitOpens_AfterThresholdRetryableFailures(t *testing.T) {
	cfg := testCfg()
	cfg.FailureThreshold = 3
	mock := &mockStorage{}
	b, _ := newBreaker(mock, cfg)

	mock.putObjectFn = func(ctx context.Context, key string, data io.Reader, contentType string) error {
		return retryableErr()
	}

	// First 3 failures: circuit should stay closed, inner is called.
	for i := 0; i < 3; i++ {
		err := b.PutObject(context.Background(), "key", nil, "")
		require.Error(t, err)
	}
	// After 3 consecutive failures, circuit should be open.
	require.Equal(t, gobreaker.StateOpen, b.State())

	// 4th call: circuit is open, inner must NOT be called.
	innerCalled := false
	mock.putObjectFn = func(ctx context.Context, key string, data io.Reader, contentType string) error {
		innerCalled = true
		return nil
	}

	err := b.PutObject(context.Background(), "key", nil, "")
	require.Error(t, err)
	assert.False(t, innerCalled, "inner must not be called when circuit is open")
	assert.True(t, errors.Is(err, gobreaker.ErrOpenState), "error should wrap gobreaker.ErrOpenState")
	assert.Equal(t, port.ErrCodeStorageFailed, port.ErrorCode(err))
	assert.Contains(t, err.Error(), ErrCircuitOpen.Error())
}

// 6. Non-retryable errors do not trip the circuit.
func TestNonRetryableErrors_DoNotTripCircuit(t *testing.T) {
	cfg := testCfg()
	cfg.FailureThreshold = 3
	mock := &mockStorage{}
	b, _ := newBreaker(mock, cfg)

	callCount := 0
	mock.deleteObjectFn = func(ctx context.Context, key string) error {
		callCount++
		return nonRetryableErr()
	}

	// Even after threshold+1 non-retryable errors, circuit stays closed.
	for i := 0; i < int(cfg.FailureThreshold)+2; i++ {
		err := b.DeleteObject(context.Background(), "key")
		require.Error(t, err)
	}

	assert.Equal(t, gobreaker.StateClosed, b.State(), "circuit must remain closed on non-retryable errors")
	assert.Equal(t, int(cfg.FailureThreshold)+2, callCount, "inner must be called every time")
}

// 7. Context cancellation does not trip the circuit.
func TestContextCancellation_DoesNotTripCircuit(t *testing.T) {
	cfg := testCfg()
	cfg.FailureThreshold = 3
	mock := &mockStorage{}
	b, _ := newBreaker(mock, cfg)

	callCount := 0
	mock.putObjectFn = func(ctx context.Context, key string, data io.Reader, contentType string) error {
		callCount++
		return context.Canceled
	}

	for i := 0; i < int(cfg.FailureThreshold)+2; i++ {
		err := b.PutObject(context.Background(), "key", nil, "")
		require.Error(t, err)
	}

	assert.Equal(t, gobreaker.StateClosed, b.State(), "context.Canceled must not trip circuit")
	assert.Equal(t, int(cfg.FailureThreshold)+2, callCount)
}

func TestContextDeadlineExceeded_DoesNotTripCircuit(t *testing.T) {
	cfg := testCfg()
	cfg.FailureThreshold = 3
	mock := &mockStorage{}
	b, _ := newBreaker(mock, cfg)

	callCount := 0
	mock.putObjectFn = func(ctx context.Context, key string, data io.Reader, contentType string) error {
		callCount++
		return context.DeadlineExceeded
	}

	for i := 0; i < int(cfg.FailureThreshold)+2; i++ {
		err := b.PutObject(context.Background(), "key", nil, "")
		require.Error(t, err)
	}

	assert.Equal(t, gobreaker.StateClosed, b.State(), "context.DeadlineExceeded must not trip circuit")
	assert.Equal(t, int(cfg.FailureThreshold)+2, callCount)
}

// 8. Half-open recovery: trip -> wait past timeout -> probe succeeds -> closed.
func TestHalfOpenRecovery(t *testing.T) {
	cfg := testCfg()
	cfg.FailureThreshold = 3
	cfg.Timeout = 100 * time.Millisecond
	cfg.MaxRequests = 1

	mock := &mockStorage{}
	b, _ := newBreaker(mock, cfg)

	// Trip the circuit.
	tripCircuit(t, b, mock, int(cfg.FailureThreshold))

	// Wait for Timeout to expire -> circuit transitions to half-open on next call.
	time.Sleep(cfg.Timeout + 50*time.Millisecond)

	// Next call is the probe: make it succeed.
	mock.putObjectFn = func(ctx context.Context, key string, data io.Reader, contentType string) error {
		return nil
	}

	err := b.PutObject(context.Background(), "probe", nil, "")
	require.NoError(t, err)
	assert.Equal(t, gobreaker.StateClosed, b.State(), "circuit should close after successful probe")
}

// 9. Half-open re-open: trip -> wait past timeout -> probe fails -> re-open.
func TestHalfOpenReopen(t *testing.T) {
	cfg := testCfg()
	cfg.FailureThreshold = 3
	cfg.Timeout = 100 * time.Millisecond
	cfg.MaxRequests = 1

	mock := &mockStorage{}
	b, _ := newBreaker(mock, cfg)

	// Trip the circuit.
	tripCircuit(t, b, mock, int(cfg.FailureThreshold))

	// Wait for Timeout -> half-open.
	time.Sleep(cfg.Timeout + 50*time.Millisecond)

	// Probe fails.
	mock.putObjectFn = func(ctx context.Context, key string, data io.Reader, contentType string) error {
		return retryableErr()
	}

	err := b.PutObject(context.Background(), "probe", nil, "")
	require.Error(t, err)
	assert.Equal(t, gobreaker.StateOpen, b.State(), "circuit should re-open after failed probe")
}

// 10. Metric callback fires on state transitions.
func TestMetricCallback_FiresOnStateTransitions(t *testing.T) {
	cfg := testCfg()
	cfg.FailureThreshold = 3
	cfg.Timeout = 100 * time.Millisecond
	cfg.MaxRequests = 1

	mock := &mockStorage{}
	b, reporter := newBreaker(mock, cfg)

	// Initial metric: closed = 0.
	calls := reporter.getCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, float64(0), calls[0].State)

	// Trip circuit -> closed to open.
	tripCircuit(t, b, mock, int(cfg.FailureThreshold))

	calls = reporter.getCalls()
	// Should now have: initial(0) + transition to open(2).
	require.GreaterOrEqual(t, len(calls), 2)
	lastCall := calls[len(calls)-1]
	assert.Equal(t, "object_storage", lastCall.Component)
	assert.Equal(t, float64(2), lastCall.State, "open state should be reported as 2")

	// Wait for half-open.
	time.Sleep(cfg.Timeout + 50*time.Millisecond)

	// Successful probe -> half-open (1) then closed (0).
	mock.putObjectFn = func(ctx context.Context, key string, data io.Reader, contentType string) error {
		return nil
	}
	err := b.PutObject(context.Background(), "probe", nil, "")
	require.NoError(t, err)

	calls = reporter.getCalls()
	// Find the half-open (1) and closed (0) transitions.
	var foundHalfOpen, foundClosed bool
	for _, c := range calls[1:] { // skip the initial metric
		if c.State == 1 {
			foundHalfOpen = true
		}
		if c.State == 0 && foundHalfOpen {
			foundClosed = true
		}
	}
	assert.True(t, foundHalfOpen, "should have reported half-open state (1)")
	assert.True(t, foundClosed, "should have reported closed state (0) after recovery")
}

// 11. Budget enforcement: context without deadline gets budget applied.
func TestBudgetEnforcement_NoExistingDeadline(t *testing.T) {
	cfg := testCfg()
	cfg.PerEventBudget = 2 * time.Second

	mock := &mockStorage{
		putObjectFn: func(ctx context.Context, key string, data io.Reader, contentType string) error {
			deadline, ok := ctx.Deadline()
			assert.True(t, ok, "context must have a deadline when no caller deadline is set")

			remaining := time.Until(deadline)
			// Budget is 2s; allow generous tolerance for test execution jitter.
			assert.InDelta(t, cfg.PerEventBudget.Seconds(), remaining.Seconds(), 0.5,
				"deadline should be approximately budget duration from now")
			return nil
		},
	}
	b, _ := newBreaker(mock, cfg)

	err := b.PutObject(context.Background(), "key", nil, "")
	require.NoError(t, err)
}

// 12. Budget respects existing tighter deadline.
func TestBudgetRespectsExistingDeadline(t *testing.T) {
	cfg := testCfg()
	cfg.PerEventBudget = 10 * time.Second

	callerDeadline := time.Now().Add(500 * time.Millisecond)
	ctx, cancel := context.WithDeadline(context.Background(), callerDeadline)
	defer cancel()

	mock := &mockStorage{
		putObjectFn: func(ctx context.Context, key string, data io.Reader, contentType string) error {
			deadline, ok := ctx.Deadline()
			require.True(t, ok)
			// The original deadline should be preserved (not extended to budget).
			assert.WithinDuration(t, callerDeadline, deadline, 10*time.Millisecond,
				"original caller deadline must be preserved when tighter than budget")
			return nil
		},
	}
	b, _ := newBreaker(mock, cfg)

	err := b.PutObject(ctx, "key", nil, "")
	require.NoError(t, err)
}

// 13. GetObject body passthrough: io.ReadCloser from inner is returned.
func TestGetObject_BodyPassthrough(t *testing.T) {
	wantContent := "binary content here"
	mock := &mockStorage{
		getObjectFn: func(ctx context.Context, key string) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader([]byte(wantContent))), nil
		},
	}
	b, _ := newBreaker(mock, testCfg())

	body, err := b.GetObject(context.Background(), "key")
	require.NoError(t, err)
	require.NotNil(t, body)
	defer body.Close()

	got, readErr := io.ReadAll(body)
	require.NoError(t, readErr)
	assert.Equal(t, wantContent, string(got))
}

func TestGetObject_ErrorReturnsNilBody(t *testing.T) {
	mock := &mockStorage{
		getObjectFn: func(ctx context.Context, key string) (io.ReadCloser, error) {
			return nil, retryableErr()
		},
	}
	b, _ := newBreaker(mock, testCfg())

	body, err := b.GetObject(context.Background(), "key")
	require.Error(t, err)
	assert.Nil(t, body)
}

func TestGetObject_CircuitOpenReturnsNilBody(t *testing.T) {
	cfg := testCfg()
	cfg.FailureThreshold = 3
	mock := &mockStorage{}
	b, _ := newBreaker(mock, cfg)

	tripCircuit(t, b, mock, int(cfg.FailureThreshold))

	body, err := b.GetObject(context.Background(), "key")
	require.Error(t, err)
	assert.Nil(t, body)
	assert.True(t, errors.Is(err, gobreaker.ErrOpenState))
	assert.Contains(t, err.Error(), ErrCircuitOpen.Error())
}

// 14. mapCircuitError: gobreaker sentinels -> StorageError, other -> passthrough.
func TestMapCircuitError(t *testing.T) {
	mock := &mockStorage{}
	b, _ := newBreaker(mock, testCfg())

	t.Run("nil_error", func(t *testing.T) {
		result := b.mapCircuitError(nil)
		assert.NoError(t, result)
	})

	t.Run("ErrOpenState", func(t *testing.T) {
		result := b.mapCircuitError(gobreaker.ErrOpenState)
		require.Error(t, result)
		assert.Equal(t, port.ErrCodeStorageFailed, port.ErrorCode(result))
		assert.True(t, port.IsRetryable(result))
		// ErrCircuitOpen is used as the message string, not as a wrapped cause.
		assert.Contains(t, result.Error(), ErrCircuitOpen.Error())
		// The gobreaker sentinel IS the wrapped cause, so errors.Is works.
		assert.True(t, errors.Is(result, gobreaker.ErrOpenState))
	})

	t.Run("ErrTooManyRequests", func(t *testing.T) {
		result := b.mapCircuitError(gobreaker.ErrTooManyRequests)
		require.Error(t, result)
		assert.Equal(t, port.ErrCodeStorageFailed, port.ErrorCode(result))
		assert.True(t, port.IsRetryable(result))
		assert.Contains(t, result.Error(), ErrCircuitOpen.Error())
		assert.True(t, errors.Is(result, gobreaker.ErrTooManyRequests))
	})

	t.Run("other_error_passthrough", func(t *testing.T) {
		original := errors.New("random infra error")
		result := b.mapCircuitError(original)
		assert.Equal(t, original, result)
	})

	t.Run("domain_error_passthrough", func(t *testing.T) {
		original := port.NewStorageError("s3 timeout", errors.New("timeout"))
		result := b.mapCircuitError(original)
		assert.Equal(t, original, result)
	})
}

// 15. stateToFloat: Closed=0, HalfOpen=1, Open=2.
func TestStateToFloat(t *testing.T) {
	tests := []struct {
		name  string
		state gobreaker.State
		want  float64
	}{
		{"Closed", gobreaker.StateClosed, 0},
		{"HalfOpen", gobreaker.StateHalfOpen, 1},
		{"Open", gobreaker.StateOpen, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, stateToFloat(tt.state))
		})
	}
}

func TestStateToFloat_Unknown(t *testing.T) {
	// Unknown state should return -1 (defensive default).
	result := stateToFloat(gobreaker.State(99))
	assert.Equal(t, float64(-1), result)
}

// ---------------------------------------------------------------------------
// Additional edge-case tests
// ---------------------------------------------------------------------------

// Mixed errors: a success resets consecutive failure count.
func TestMixedErrors_SuccessResetsConsecutiveCount(t *testing.T) {
	cfg := testCfg()
	cfg.FailureThreshold = 3
	mock := &mockStorage{}
	b, _ := newBreaker(mock, cfg)

	callIdx := 0
	mock.putObjectFn = func(ctx context.Context, key string, data io.Reader, contentType string) error {
		callIdx++
		// Fail twice, succeed once, fail twice again.
		switch callIdx {
		case 1, 2, 4, 5:
			return retryableErr()
		default:
			return nil
		}
	}

	// 2 failures.
	_ = b.PutObject(context.Background(), "key", nil, "")
	_ = b.PutObject(context.Background(), "key", nil, "")
	// 1 success resets consecutive count.
	_ = b.PutObject(context.Background(), "key", nil, "")
	// 2 more failures (not 3 consecutive, so circuit stays closed).
	_ = b.PutObject(context.Background(), "key", nil, "")
	_ = b.PutObject(context.Background(), "key", nil, "")

	assert.Equal(t, gobreaker.StateClosed, b.State(),
		"circuit should stay closed because success reset the consecutive count")
}

// HeadObject returns correct values when circuit is open.
func TestHeadObject_CircuitOpen(t *testing.T) {
	cfg := testCfg()
	cfg.FailureThreshold = 3
	mock := &mockStorage{}
	b, _ := newBreaker(mock, cfg)

	tripCircuit(t, b, mock, int(cfg.FailureThreshold))

	size, exists, err := b.HeadObject(context.Background(), "key")
	require.Error(t, err)
	assert.Equal(t, int64(0), size)
	assert.False(t, exists)
	assert.True(t, errors.Is(err, gobreaker.ErrOpenState))
	assert.Contains(t, err.Error(), ErrCircuitOpen.Error())
}

// GeneratePresignedURL returns empty string on circuit open.
func TestGeneratePresignedURL_CircuitOpen(t *testing.T) {
	cfg := testCfg()
	cfg.FailureThreshold = 3
	mock := &mockStorage{}
	b, _ := newBreaker(mock, cfg)

	tripCircuit(t, b, mock, int(cfg.FailureThreshold))

	url, err := b.GeneratePresignedURL(context.Background(), "key", time.Minute)
	require.Error(t, err)
	assert.Empty(t, url)
	assert.True(t, errors.Is(err, gobreaker.ErrOpenState))
	assert.Contains(t, err.Error(), ErrCircuitOpen.Error())
}

// DeleteByPrefix: circuit open.
func TestDeleteByPrefix_CircuitOpen(t *testing.T) {
	cfg := testCfg()
	cfg.FailureThreshold = 3
	mock := &mockStorage{}
	b, _ := newBreaker(mock, cfg)

	tripCircuit(t, b, mock, int(cfg.FailureThreshold))

	err := b.DeleteByPrefix(context.Background(), "prefix/")
	require.Error(t, err)
	assert.True(t, errors.Is(err, gobreaker.ErrOpenState))
	assert.Contains(t, err.Error(), ErrCircuitOpen.Error())
}

// DeleteObject: circuit open.
func TestDeleteObject_CircuitOpen(t *testing.T) {
	cfg := testCfg()
	cfg.FailureThreshold = 3
	mock := &mockStorage{}
	b, _ := newBreaker(mock, cfg)

	tripCircuit(t, b, mock, int(cfg.FailureThreshold))

	err := b.DeleteObject(context.Background(), "key")
	require.Error(t, err)
	assert.True(t, errors.Is(err, gobreaker.ErrOpenState))
	assert.Contains(t, err.Error(), ErrCircuitOpen.Error())
}

// Budget applied for each method type, not just PutObject.
func TestBudgetApplied_AllMethods(t *testing.T) {
	cfg := testCfg()
	cfg.PerEventBudget = 3 * time.Second

	assertBudget := func(ctx context.Context) {
		deadline, ok := ctx.Deadline()
		assert.True(t, ok, "budget must set deadline")
		remaining := time.Until(deadline)
		assert.InDelta(t, cfg.PerEventBudget.Seconds(), remaining.Seconds(), 0.5)
	}

	mock := &mockStorage{
		putObjectFn: func(ctx context.Context, key string, data io.Reader, contentType string) error {
			assertBudget(ctx)
			return nil
		},
		getObjectFn: func(ctx context.Context, key string) (io.ReadCloser, error) {
			assertBudget(ctx)
			return io.NopCloser(strings.NewReader("")), nil
		},
		deleteObjectFn: func(ctx context.Context, key string) error {
			assertBudget(ctx)
			return nil
		},
		headObjectFn: func(ctx context.Context, key string) (int64, bool, error) {
			assertBudget(ctx)
			return 0, false, nil
		},
		generatePresignedFn: func(ctx context.Context, key string, expiry time.Duration) (string, error) {
			assertBudget(ctx)
			return "", nil
		},
		deleteByPrefixFn: func(ctx context.Context, prefix string) error {
			assertBudget(ctx)
			return nil
		},
	}
	b, _ := newBreaker(mock, cfg)
	ctx := context.Background()

	require.NoError(t, b.PutObject(ctx, "k", nil, ""))

	body, err := b.GetObject(ctx, "k")
	require.NoError(t, err)
	body.Close()

	require.NoError(t, b.DeleteObject(ctx, "k"))

	_, _, err = b.HeadObject(ctx, "k")
	require.NoError(t, err)

	_, err = b.GeneratePresignedURL(ctx, "k", time.Minute)
	require.NoError(t, err)

	require.NoError(t, b.DeleteByPrefix(ctx, "k"))
}

// State() method exposes current gobreaker state.
func TestState_ReflectsCircuitState(t *testing.T) {
	cfg := testCfg()
	cfg.FailureThreshold = 3
	cfg.Timeout = 100 * time.Millisecond
	mock := &mockStorage{}
	b, _ := newBreaker(mock, cfg)

	// Initially closed.
	assert.Equal(t, gobreaker.StateClosed, b.State())

	// Trip.
	tripCircuit(t, b, mock, int(cfg.FailureThreshold))
	assert.Equal(t, gobreaker.StateOpen, b.State())

	// Wait for half-open.
	time.Sleep(cfg.Timeout + 50*time.Millisecond)
	assert.Equal(t, gobreaker.StateHalfOpen, b.State())
}

// ErrCircuitOpen wraps correctly through the error chain.
func TestErrCircuitOpen_ErrorChain(t *testing.T) {
	cfg := testCfg()
	cfg.FailureThreshold = 3
	mock := &mockStorage{}
	b, _ := newBreaker(mock, cfg)

	tripCircuit(t, b, mock, int(cfg.FailureThreshold))

	err := b.PutObject(context.Background(), "key", nil, "")
	require.Error(t, err)

	// Check error chain: DomainError wraps gobreaker.ErrOpenState as Cause.
	// The Message is ErrCircuitOpen.Error() (the text, not the error itself).
	var domErr *port.DomainError
	require.True(t, errors.As(err, &domErr))
	assert.Equal(t, port.ErrCodeStorageFailed, domErr.Code)
	assert.True(t, domErr.Retryable)
	assert.Equal(t, ErrCircuitOpen.Error(), domErr.Message)

	// The Cause is gobreaker.ErrOpenState, so errors.Is unwraps to it.
	assert.True(t, errors.Is(err, gobreaker.ErrOpenState))
}

// MaxRequests in half-open: only MaxRequests probes allowed, extra rejected.
func TestHalfOpen_MaxRequestsEnforced(t *testing.T) {
	cfg := testCfg()
	cfg.FailureThreshold = 3
	cfg.Timeout = 100 * time.Millisecond
	cfg.MaxRequests = 1 // only 1 probe allowed

	mock := &mockStorage{}
	b, _ := newBreaker(mock, cfg)

	tripCircuit(t, b, mock, int(cfg.FailureThreshold))

	// Wait for half-open.
	time.Sleep(cfg.Timeout + 50*time.Millisecond)

	// The first call in half-open is the probe. Make it block so we can
	// attempt a second call while the probe is in progress.
	probeStarted := make(chan struct{})
	probeRelease := make(chan struct{})

	mock.putObjectFn = func(ctx context.Context, key string, data io.Reader, contentType string) error {
		close(probeStarted)
		<-probeRelease
		return nil
	}

	probeErrCh := make(chan error, 1)
	go func() {
		probeErrCh <- b.PutObject(context.Background(), "probe", nil, "")
	}()

	<-probeStarted

	// Second call while probe is in flight should be rejected (ErrTooManyRequests).
	err := b.PutObject(context.Background(), "second", nil, "")
	require.Error(t, err)
	assert.True(t, errors.Is(err, gobreaker.ErrTooManyRequests),
		"excess half-open requests should be rejected with ErrTooManyRequests")

	close(probeRelease)
	probeErr := <-probeErrCh
	assert.NoError(t, probeErr)
}
