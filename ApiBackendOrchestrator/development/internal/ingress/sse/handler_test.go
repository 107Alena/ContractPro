package sse

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"contractpro/api-orchestrator/internal/config"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"

	"github.com/golang-jwt/jwt/v5"
)

// ---------------------------------------------------------------------------
// Test mocks
// ---------------------------------------------------------------------------

type mockTokenValidator struct {
	validateFunc func(token string) (*auth.Claims, error)
}

func (m *mockTokenValidator) ValidateToken(token string) (*auth.Claims, error) {
	return m.validateFunc(token)
}

type mockSubscription struct {
	closeFunc func() error
	closedCh  chan struct{}
}

func newMockSubscription() *mockSubscription {
	return &mockSubscription{closedCh: make(chan struct{})}
}

func (m *mockSubscription) Close() error {
	select {
	case <-m.closedCh:
	default:
		close(m.closedCh)
	}
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil
}

type mockKVStore struct {
	mu            sync.Mutex
	subscribeFunc func(ctx context.Context, channel string, handler func(msg string)) (Subscription, error)
	setFunc       func(ctx context.Context, key, value string, ttl time.Duration) error
	deleteFunc    func(ctx context.Context, key string) error
	setCalls      []setCall
	deleteCalls   []string
}

type setCall struct {
	Key   string
	Value string
	TTL   time.Duration
}

func (m *mockKVStore) Subscribe(ctx context.Context, channel string, handler func(msg string)) (Subscription, error) {
	if m.subscribeFunc != nil {
		return m.subscribeFunc(ctx, channel, handler)
	}
	return newMockSubscription(), nil
}

func (m *mockKVStore) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	m.mu.Lock()
	m.setCalls = append(m.setCalls, setCall{Key: key, Value: value, TTL: ttl})
	m.mu.Unlock()
	if m.setFunc != nil {
		return m.setFunc(ctx, key, value, ttl)
	}
	return nil
}

func (m *mockKVStore) Delete(ctx context.Context, key string) error {
	m.mu.Lock()
	m.deleteCalls = append(m.deleteCalls, key)
	m.mu.Unlock()
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, key)
	}
	return nil
}

func (m *mockKVStore) getSetCalls() []setCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]setCall, len(m.setCalls))
	copy(result, m.setCalls)
	return result
}

func (m *mockKVStore) getDeleteCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]string, len(m.deleteCalls))
	copy(result, m.deleteCalls)
	return result
}

// recordedResponse captures SSE output and implements http.Flusher.
type recordedResponse struct {
	header  http.Header
	buf     bytes.Buffer
	mu      sync.Mutex
	status  int
	flushed int32 // atomic
}

func newRecordedResponse() *recordedResponse {
	return &recordedResponse{
		header: make(http.Header),
	}
}

func (rr *recordedResponse) Header() http.Header {
	return rr.header
}

func (rr *recordedResponse) WriteHeader(code int) {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	if rr.status == 0 {
		rr.status = code
	}
}

func (rr *recordedResponse) Write(data []byte) (int, error) {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	return rr.buf.Write(data)
}

func (rr *recordedResponse) Flush() {
	atomic.AddInt32(&rr.flushed, 1)
}

func (rr *recordedResponse) body() string {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	return rr.buf.String()
}

func (rr *recordedResponse) statusCode() int {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	return rr.status
}

// noFlusherWriter is a ResponseWriter that does NOT implement http.Flusher.
type noFlusherWriter struct {
	header http.Header
	buf    bytes.Buffer
	status int
}

func newNoFlusherWriter() *noFlusherWriter {
	return &noFlusherWriter{header: make(http.Header)}
}

func (w *noFlusherWriter) Header() http.Header       { return w.header }
func (w *noFlusherWriter) WriteHeader(code int)       { w.status = code }
func (w *noFlusherWriter) Write(b []byte) (int, error) { return w.buf.Write(b) }

// errorWriter always returns an error on Write (simulates broken pipe).
type errorWriter struct {
	header http.Header
	status int
}

func newErrorWriter() *errorWriter {
	return &errorWriter{header: make(http.Header)}
}

func (w *errorWriter) Header() http.Header           { return w.header }
func (w *errorWriter) WriteHeader(code int)           { w.status = code }
func (w *errorWriter) Write(_ []byte) (int, error)    { return 0, errors.New("broken pipe") }
func (w *errorWriter) Flush()                         {}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

var fixedTime = time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)

func validClaims() *auth.Claims {
	return &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: "user-123",
			ID:      "token-456",
		},
		Org:  "org-789",
		Role: "LAWYER",
	}
}

func testConfig() config.SSEConfig {
	return config.SSEConfig{
		HeartbeatInterval: 50 * time.Millisecond,
		MaxConnectionAge:  1 * time.Hour,
	}
}

func testLogger() *logger.Logger {
	return logger.NewLogger("debug")
}

func testHandler(validator TokenValidator, kv KVStore) *Handler {
	h := NewHandler(validator, kv, testConfig(), testLogger())
	h.uuidGen = func() string { return "test-conn-id" }
	h.now = func() time.Time { return fixedTime }
	return h
}

func validValidator() *mockTokenValidator {
	return &mockTokenValidator{
		validateFunc: func(_ string) (*auth.Claims, error) {
			return validClaims(), nil
		},
	}
}

// waitForContent polls the response body until it contains the expected string.
func waitForContent(t *testing.T, rr *recordedResponse, expected string, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		if strings.Contains(rr.body(), expected) {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for %q in response body; got: %q", expected, rr.body())
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
}

// ---------------------------------------------------------------------------
// Authentication tests
// ---------------------------------------------------------------------------

func TestHandle_MissingToken(t *testing.T) {
	h := testHandler(validValidator(), &mockKVStore{})
	handler := h.Handle()

	req := httptest.NewRequest(http.MethodGet, "/events/stream", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	assertBodyContains(t, w.Body.String(), "AUTH_TOKEN_MISSING")
}

func TestHandle_InvalidToken(t *testing.T) {
	validator := &mockTokenValidator{
		validateFunc: func(_ string) (*auth.Claims, error) {
			return nil, errors.New("invalid token")
		},
	}
	h := testHandler(validator, &mockKVStore{})
	handler := h.Handle()

	req := httptest.NewRequest(http.MethodGet, "/events/stream?token=bad", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	assertBodyContains(t, w.Body.String(), "AUTH_TOKEN_INVALID")
}

func TestHandle_ExpiredToken(t *testing.T) {
	validator := &mockTokenValidator{
		validateFunc: func(_ string) (*auth.Claims, error) {
			return nil, fmt.Errorf("token expired: %w", jwt.ErrTokenExpired)
		},
	}
	h := testHandler(validator, &mockKVStore{})
	handler := h.Handle()

	req := httptest.NewRequest(http.MethodGet, "/events/stream?token=expired", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	assertBodyContains(t, w.Body.String(), "AUTH_TOKEN_EXPIRED")
}

// ---------------------------------------------------------------------------
// Flusher support test
// ---------------------------------------------------------------------------

func TestHandle_FlusherNotSupported(t *testing.T) {
	h := testHandler(validValidator(), &mockKVStore{})
	handler := h.Handle()

	req := httptest.NewRequest(http.MethodGet, "/events/stream?token=valid", nil)
	w := newNoFlusherWriter()

	handler.ServeHTTP(w, req)

	if w.status != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.status, http.StatusInternalServerError)
	}
	assertBodyContains(t, w.buf.String(), "INTERNAL_ERROR")
}

// ---------------------------------------------------------------------------
// SSE headers tests
// ---------------------------------------------------------------------------

func TestHandle_SSEHeaders(t *testing.T) {
	kv := &mockKVStore{}
	h := testHandler(validValidator(), kv)
	handler := h.Handle()

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/events/stream?token=valid", nil)
	req = req.WithContext(ctx)
	rr := newRecordedResponse()

	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(rr, req)
		close(done)
	}()

	waitForContent(t, rr, ": connected", 2*time.Second)
	cancel()
	<-done

	if ct := rr.header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want %q", ct, "text/event-stream")
	}
	if cc := rr.header.Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want %q", cc, "no-cache")
	}
	if conn := rr.header.Get("Connection"); conn != "keep-alive" {
		t.Errorf("Connection = %q, want %q", conn, "keep-alive")
	}
	if xab := rr.header.Get("X-Accel-Buffering"); xab != "no" {
		t.Errorf("X-Accel-Buffering = %q, want %q", xab, "no")
	}
	if rr.statusCode() != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.statusCode(), http.StatusOK)
	}
}

// ---------------------------------------------------------------------------
// Connected comment test
// ---------------------------------------------------------------------------

func TestHandle_ConnectedComment(t *testing.T) {
	h := testHandler(validValidator(), &mockKVStore{})
	handler := h.Handle()

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/events/stream?token=valid", nil)
	req = req.WithContext(ctx)
	rr := newRecordedResponse()

	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(rr, req)
		close(done)
	}()

	waitForContent(t, rr, ": connected\n\n", 2*time.Second)
	cancel()
	<-done

	body := rr.body()
	if !strings.HasPrefix(body, ": connected\n\n") {
		t.Errorf("first output should be connected comment, got: %q", body[:min(len(body), 50)])
	}
}

// ---------------------------------------------------------------------------
// Connection lifecycle tests
// ---------------------------------------------------------------------------

func TestHandle_ConnectionRegistered(t *testing.T) {
	kv := &mockKVStore{}
	h := testHandler(validValidator(), kv)
	handler := h.Handle()

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/events/stream?token=valid", nil)
	req = req.WithContext(ctx)
	rr := newRecordedResponse()

	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(rr, req)
		close(done)
	}()

	waitForContent(t, rr, ": connected", 2*time.Second)

	// Verify registration happened.
	calls := kv.getSetCalls()
	if len(calls) == 0 {
		t.Fatal("expected Set call for connection registration")
	}

	expectedKey := "sse:conn:org-789:user-123:test-conn-id"
	if calls[0].Key != expectedKey {
		t.Errorf("registration key = %q, want %q", calls[0].Key, expectedKey)
	}

	expectedTTL := testConfig().HeartbeatInterval * 3
	if calls[0].TTL != expectedTTL {
		t.Errorf("TTL = %v, want %v", calls[0].TTL, expectedTTL)
	}

	// Verify JSON value contains user_id and org_id.
	var record connRecord
	if err := json.Unmarshal([]byte(calls[0].Value), &record); err != nil {
		t.Fatalf("failed to unmarshal registration value: %v", err)
	}
	if record.UserID != "user-123" {
		t.Errorf("user_id = %q, want %q", record.UserID, "user-123")
	}
	if record.OrgID != "org-789" {
		t.Errorf("org_id = %q, want %q", record.OrgID, "org-789")
	}

	cancel()
	<-done
}

func TestHandle_ConnectionUnregistered(t *testing.T) {
	kv := &mockKVStore{}
	h := testHandler(validValidator(), kv)
	handler := h.Handle()

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/events/stream?token=valid", nil)
	req = req.WithContext(ctx)
	rr := newRecordedResponse()

	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(rr, req)
		close(done)
	}()

	waitForContent(t, rr, ": connected", 2*time.Second)
	cancel()
	<-done

	expectedKey := "sse:conn:org-789:user-123:test-conn-id"
	delCalls := kv.getDeleteCalls()
	if len(delCalls) == 0 {
		t.Fatal("expected Delete call for connection unregistration")
	}
	if delCalls[0] != expectedKey {
		t.Errorf("delete key = %q, want %q", delCalls[0], expectedKey)
	}
}

// ---------------------------------------------------------------------------
// Event delivery tests
// ---------------------------------------------------------------------------

func TestEventLoop_EventDelivery(t *testing.T) {
	var publishHandler func(msg string)

	kv := &mockKVStore{
		subscribeFunc: func(_ context.Context, _ string, handler func(msg string)) (Subscription, error) {
			publishHandler = handler
			return newMockSubscription(), nil
		},
	}

	h := testHandler(validValidator(), kv)
	handler := h.Handle()

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/events/stream?token=valid", nil)
	req = req.WithContext(ctx)
	rr := newRecordedResponse()

	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(rr, req)
		close(done)
	}()

	waitForContent(t, rr, ": connected", 2*time.Second)

	// Publish an event.
	event := `{"event_type":"status_update","document_id":"doc-1","status":"PROCESSING","message":"Извлечение текста"}`
	publishHandler(event)

	waitForContent(t, rr, "event: status_update", 2*time.Second)

	cancel()
	<-done

	body := rr.body()
	if !strings.Contains(body, "event: status_update\ndata: "+event+"\n\n") {
		t.Errorf("expected SSE event in output, got: %q", body[:min(len(body), 300)])
	}
}

func TestEventLoop_ComparisonEventType(t *testing.T) {
	var publishHandler func(msg string)

	kv := &mockKVStore{
		subscribeFunc: func(_ context.Context, _ string, handler func(msg string)) (Subscription, error) {
			publishHandler = handler
			return newMockSubscription(), nil
		},
	}

	h := testHandler(validValidator(), kv)
	handler := h.Handle()

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/events/stream?token=valid", nil)
	req = req.WithContext(ctx)
	rr := newRecordedResponse()

	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(rr, req)
		close(done)
	}()

	waitForContent(t, rr, ": connected", 2*time.Second)

	publishHandler(`{"event_type":"comparison_update","document_id":"doc-1"}`)

	waitForContent(t, rr, "event: comparison_update", 2*time.Second)
	cancel()
	<-done
}

func TestEventLoop_MissingEventType(t *testing.T) {
	var publishHandler func(msg string)

	kv := &mockKVStore{
		subscribeFunc: func(_ context.Context, _ string, handler func(msg string)) (Subscription, error) {
			publishHandler = handler
			return newMockSubscription(), nil
		},
	}

	h := testHandler(validValidator(), kv)
	handler := h.Handle()

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/events/stream?token=valid", nil)
	req = req.WithContext(ctx)
	rr := newRecordedResponse()

	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(rr, req)
		close(done)
	}()

	waitForContent(t, rr, ": connected", 2*time.Second)

	publishHandler(`{"status":"READY"}`)

	waitForContent(t, rr, "event: message", 2*time.Second)
	cancel()
	<-done
}

func TestEventLoop_InvalidJSON(t *testing.T) {
	var publishHandler func(msg string)

	kv := &mockKVStore{
		subscribeFunc: func(_ context.Context, _ string, handler func(msg string)) (Subscription, error) {
			publishHandler = handler
			return newMockSubscription(), nil
		},
	}

	h := testHandler(validValidator(), kv)
	handler := h.Handle()

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/events/stream?token=valid", nil)
	req = req.WithContext(ctx)
	rr := newRecordedResponse()

	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(rr, req)
		close(done)
	}()

	waitForContent(t, rr, ": connected", 2*time.Second)

	publishHandler("not json at all")

	waitForContent(t, rr, "event: message\ndata: not json at all", 2*time.Second)
	cancel()
	<-done
}

// ---------------------------------------------------------------------------
// Heartbeat tests
// ---------------------------------------------------------------------------

func TestEventLoop_Heartbeat(t *testing.T) {
	kv := &mockKVStore{}
	cfg := config.SSEConfig{
		HeartbeatInterval: 30 * time.Millisecond,
		MaxConnectionAge:  1 * time.Hour,
	}
	h := NewHandler(validValidator(), kv, cfg, testLogger())
	h.uuidGen = func() string { return "test-conn-id" }
	h.now = func() time.Time { return fixedTime }
	handler := h.Handle()

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/events/stream?token=valid", nil)
	req = req.WithContext(ctx)
	rr := newRecordedResponse()

	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(rr, req)
		close(done)
	}()

	// Wait for at least one heartbeat.
	waitForContent(t, rr, ":ping\n\n", 2*time.Second)
	cancel()
	<-done

	body := rr.body()
	if !strings.Contains(body, ":ping\n\n") {
		t.Errorf("expected heartbeat ping in output, got: %q", body)
	}
}

func TestEventLoop_HeartbeatRefreshesConnection(t *testing.T) {
	kv := &mockKVStore{}
	cfg := config.SSEConfig{
		HeartbeatInterval: 30 * time.Millisecond,
		MaxConnectionAge:  1 * time.Hour,
	}
	h := NewHandler(validValidator(), kv, cfg, testLogger())
	h.uuidGen = func() string { return "test-conn-id" }
	h.now = func() time.Time { return fixedTime }
	handler := h.Handle()

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/events/stream?token=valid", nil)
	req = req.WithContext(ctx)
	rr := newRecordedResponse()

	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(rr, req)
		close(done)
	}()

	waitForContent(t, rr, ":ping\n\n", 2*time.Second)
	cancel()
	<-done

	// Should have at least 2 Set calls: registration + refresh.
	calls := kv.getSetCalls()
	if len(calls) < 2 {
		t.Fatalf("expected at least 2 Set calls (register + refresh), got %d", len(calls))
	}

	expectedKey := "sse:conn:org-789:user-123:test-conn-id"
	for i, c := range calls {
		if c.Key != expectedKey {
			t.Errorf("Set call %d: key = %q, want %q", i, c.Key, expectedKey)
		}
	}
}

// ---------------------------------------------------------------------------
// Max connection age tests
// ---------------------------------------------------------------------------

func TestEventLoop_MaxConnectionAge(t *testing.T) {
	kv := &mockKVStore{}
	cfg := config.SSEConfig{
		HeartbeatInterval: 1 * time.Hour, // No heartbeat during this test.
		MaxConnectionAge:  50 * time.Millisecond,
	}
	h := NewHandler(validValidator(), kv, cfg, testLogger())
	h.uuidGen = func() string { return "test-conn-id" }
	h.now = func() time.Time { return fixedTime }
	handler := h.Handle()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/events/stream?token=valid", nil)
	req = req.WithContext(ctx)
	rr := newRecordedResponse()

	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(rr, req)
		close(done)
	}()

	// Handler should exit after max age.
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not exit after max connection age")
	}

	body := rr.body()
	if !strings.Contains(body, "event: connection_expired") {
		t.Errorf("expected connection_expired event, got: %q", body)
	}
	if !strings.Contains(body, "Переподключитесь") {
		t.Errorf("expected reconnect hint in Russian, got: %q", body)
	}
}

// ---------------------------------------------------------------------------
// Client disconnect test
// ---------------------------------------------------------------------------

func TestEventLoop_ContextCancelled(t *testing.T) {
	kv := &mockKVStore{}
	h := testHandler(validValidator(), kv)
	handler := h.Handle()

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/events/stream?token=valid", nil)
	req = req.WithContext(ctx)
	rr := newRecordedResponse()

	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(rr, req)
		close(done)
	}()

	waitForContent(t, rr, ": connected", 2*time.Second)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not exit after context cancel")
	}
}

// ---------------------------------------------------------------------------
// Write error test
// ---------------------------------------------------------------------------

func TestEventLoop_WriteErrorExitsLoop(t *testing.T) {
	var publishHandler func(msg string)

	kv := &mockKVStore{
		subscribeFunc: func(_ context.Context, _ string, handler func(msg string)) (Subscription, error) {
			publishHandler = handler
			return newMockSubscription(), nil
		},
	}

	h := testHandler(validValidator(), kv)
	handler := h.Handle()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/events/stream?token=valid", nil)
	req = req.WithContext(ctx)

	ew := newErrorWriter()
	// The handler will fail on writeComment("connected") and return.
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(ew, req)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		// Send an event to trigger write if connected comment didn't fail first.
		if publishHandler != nil {
			publishHandler(`{"event_type":"status_update"}`)
		}
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("handler did not exit after write error")
		}
	}
}

// ---------------------------------------------------------------------------
// Event channel backpressure test
// ---------------------------------------------------------------------------

func TestSubscription_EventDropOnFullChannel(t *testing.T) {
	var publishHandler func(msg string)

	kv := &mockKVStore{
		subscribeFunc: func(_ context.Context, _ string, handler func(msg string)) (Subscription, error) {
			publishHandler = handler
			return newMockSubscription(), nil
		},
	}

	h := testHandler(validValidator(), kv)
	handler := h.Handle()

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/events/stream?token=valid", nil)
	req = req.WithContext(ctx)
	rr := newRecordedResponse()

	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(rr, req)
		close(done)
	}()

	waitForContent(t, rr, ": connected", 2*time.Second)

	// Fill the buffer beyond capacity. This should not block.
	for i := 0; i < eventBufferSize+10; i++ {
		publishHandler(fmt.Sprintf(`{"event_type":"status_update","seq":%d}`, i))
	}

	// Handler should still be alive.
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not exit after context cancel")
	}
}

// ---------------------------------------------------------------------------
// Redis subscribe failure test
// ---------------------------------------------------------------------------

func TestHandle_SubscribeFailure(t *testing.T) {
	kv := &mockKVStore{
		subscribeFunc: func(_ context.Context, _ string, _ func(msg string)) (Subscription, error) {
			return nil, errors.New("redis connection refused")
		},
	}

	h := testHandler(validValidator(), kv)
	handler := h.Handle()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/events/stream?token=valid", nil)
	req = req.WithContext(ctx)
	rr := newRecordedResponse()

	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(rr, req)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not exit after subscribe failure")
	}

	body := rr.body()
	if !strings.Contains(body, "event: error") {
		t.Errorf("expected SSE error event, got: %q", body)
	}
	if !strings.Contains(body, "BROKER_UNAVAILABLE") {
		t.Errorf("expected BROKER_UNAVAILABLE in error event, got: %q", body)
	}
}

// ---------------------------------------------------------------------------
// Redis registration failure tests
// ---------------------------------------------------------------------------

func TestRegisterConnection_RedisFailure(t *testing.T) {
	kv := &mockKVStore{
		setFunc: func(_ context.Context, _ string, _ string, _ time.Duration) error {
			return errors.New("redis timeout")
		},
	}

	h := testHandler(validValidator(), kv)
	handler := h.Handle()

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/events/stream?token=valid", nil)
	req = req.WithContext(ctx)
	rr := newRecordedResponse()

	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(rr, req)
		close(done)
	}()

	// Connection should proceed despite registration failure.
	waitForContent(t, rr, ": connected", 2*time.Second)
	cancel()
	<-done
}

func TestUnregisterConnection_RedisFailure(t *testing.T) {
	kv := &mockKVStore{
		deleteFunc: func(_ context.Context, _ string) error {
			return errors.New("redis timeout")
		},
	}

	h := testHandler(validValidator(), kv)
	handler := h.Handle()

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/events/stream?token=valid", nil)
	req = req.WithContext(ctx)
	rr := newRecordedResponse()

	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(rr, req)
		close(done)
	}()

	waitForContent(t, rr, ": connected", 2*time.Second)
	cancel()

	// Handler should exit cleanly even with delete failure.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not exit after context cancel with delete failure")
	}
}

// ---------------------------------------------------------------------------
// Subscribe channel test
// ---------------------------------------------------------------------------

func TestHandle_SubscribesCorrectChannel(t *testing.T) {
	var subscribedChannel string

	kv := &mockKVStore{
		subscribeFunc: func(_ context.Context, channel string, _ func(msg string)) (Subscription, error) {
			subscribedChannel = channel
			return newMockSubscription(), nil
		},
	}

	h := testHandler(validValidator(), kv)
	handler := h.Handle()

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/events/stream?token=valid", nil)
	req = req.WithContext(ctx)
	rr := newRecordedResponse()

	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(rr, req)
		close(done)
	}()

	waitForContent(t, rr, ": connected", 2*time.Second)
	cancel()
	<-done

	expected := "sse:broadcast:org-789"
	if subscribedChannel != expected {
		t.Errorf("subscribed channel = %q, want %q", subscribedChannel, expected)
	}
}

// ---------------------------------------------------------------------------
// Subscription close test
// ---------------------------------------------------------------------------

func TestHandle_SubscriptionClosed(t *testing.T) {
	var closeCalled atomic.Int32
	sub := &mockSubscription{
		closedCh: make(chan struct{}),
		closeFunc: func() error {
			closeCalled.Add(1)
			return nil
		},
	}

	kv := &mockKVStore{
		subscribeFunc: func(_ context.Context, _ string, _ func(msg string)) (Subscription, error) {
			return sub, nil
		},
	}

	h := testHandler(validValidator(), kv)
	handler := h.Handle()

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/events/stream?token=valid", nil)
	req = req.WithContext(ctx)
	rr := newRecordedResponse()

	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(rr, req)
		close(done)
	}()

	waitForContent(t, rr, ": connected", 2*time.Second)
	cancel()
	<-done

	if closeCalled.Load() == 0 {
		t.Error("expected Subscription.Close() to be called")
	}
}

// ---------------------------------------------------------------------------
// extractEventType unit tests
// ---------------------------------------------------------------------------

func TestExtractEventType(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"status_update", `{"event_type":"status_update","status":"READY"}`, "status_update"},
		{"comparison_update", `{"event_type":"comparison_update"}`, "comparison_update"},
		{"version_created", `{"event_type":"version_created"}`, "version_created"},
		{"empty_event_type", `{"event_type":""}`, "message"},
		{"missing_event_type", `{"status":"READY"}`, "message"},
		{"invalid_json", `not json at all`, "message"},
		{"empty_string", ``, "message"},
		{"number_event_type", `{"event_type":123}`, "message"},
		{"newline_injection", `{"event_type":"evil\ndata: injected"}`, "evildata: injected"},
		{"cr_injection", `{"event_type":"evil\rdata: injected"}`, "evildata: injected"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractEventType(tt.input)
			if result != tt.expected {
				t.Errorf("extractEventType(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// sanitizeEventType unit tests
// ---------------------------------------------------------------------------

func TestSanitizeEventType(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"clean", "status_update", "status_update"},
		{"newline", "evil\ninjection", "evilinjection"},
		{"carriage_return", "evil\rinjection", "evilinjection"},
		{"both", "evil\r\ninjection", "evilinjection"},
		{"empty_after_sanitize", "\n\r", "message"},
		{"empty_input", "", "message"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeEventType(tt.input)
			if result != tt.expected {
				t.Errorf("sanitizeEventType(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Multiline data test
// ---------------------------------------------------------------------------

func TestWriteSSEEvent_MultilineData(t *testing.T) {
	rr := newRecordedResponse()
	err := writeSSEEvent(rr, rr, "test", "line1\nline2\nline3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "event: test\ndata: line1\ndata: line2\ndata: line3\n\n"
	if rr.body() != expected {
		t.Errorf("got %q, want %q", rr.body(), expected)
	}
}

// ---------------------------------------------------------------------------
// isExpiredError unit tests
// ---------------------------------------------------------------------------

func TestIsExpiredError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"expired", jwt.ErrTokenExpired, true},
		{"wrapped_expired", fmt.Errorf("wrapped: %w", jwt.ErrTokenExpired), true},
		{"invalid_signature", jwt.ErrSignatureInvalid, false},
		{"generic_error", errors.New("something else"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isExpiredError(tt.err)
			if result != tt.expected {
				t.Errorf("isExpiredError(%v) = %v, want %v", tt.err, result, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Key/channel format unit tests
// ---------------------------------------------------------------------------

func TestSSEChannel(t *testing.T) {
	result := sseChannel("org-123")
	expected := "sse:broadcast:org-123"
	if result != expected {
		t.Errorf("sseChannel(\"org-123\") = %q, want %q", result, expected)
	}
}

func TestConnKey(t *testing.T) {
	result := connKey("org-123", "user-456", "conn-789")
	expected := "sse:conn:org-123:user-456:conn-789"
	if result != expected {
		t.Errorf("connKey(\"org-123\", \"user-456\", \"conn-789\") = %q, want %q", result, expected)
	}
}

// ---------------------------------------------------------------------------
// Constructor test
// ---------------------------------------------------------------------------

func TestNewHandler(t *testing.T) {
	validator := validValidator()
	kv := &mockKVStore{}
	cfg := testConfig()
	log := testLogger()

	h := NewHandler(validator, kv, cfg, log)

	if h.validator == nil {
		t.Error("validator is nil")
	}
	if h.kv == nil {
		t.Error("kv is nil")
	}
	if h.cfg.HeartbeatInterval != cfg.HeartbeatInterval {
		t.Errorf("HeartbeatInterval = %v, want %v", h.cfg.HeartbeatInterval, cfg.HeartbeatInterval)
	}
	if h.cfg.MaxConnectionAge != cfg.MaxConnectionAge {
		t.Errorf("MaxConnectionAge = %v, want %v", h.cfg.MaxConnectionAge, cfg.MaxConnectionAge)
	}
	if h.uuidGen == nil {
		t.Error("uuidGen is nil")
	}
	if h.now == nil {
		t.Error("now is nil")
	}

	// UUID generator should produce valid UUIDs.
	id := h.uuidGen()
	if len(id) != 36 { // UUID v4 format: 8-4-4-4-12
		t.Errorf("generated ID length = %d, want 36", len(id))
	}
}

// ---------------------------------------------------------------------------
// Compile-time interface checks
// ---------------------------------------------------------------------------

// Verify that mockKVStore satisfies the KVStore interface.
var _ KVStore = (*mockKVStore)(nil)

// Verify that mockSubscription satisfies the Subscription interface.
var _ Subscription = (*mockSubscription)(nil)

// Verify that mockTokenValidator satisfies the TokenValidator interface.
var _ TokenValidator = (*mockTokenValidator)(nil)

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

func assertBodyContains(t *testing.T, body, expected string) {
	t.Helper()
	if !strings.Contains(body, expected) {
		t.Errorf("body does not contain %q; got: %q", expected, body)
	}
}
