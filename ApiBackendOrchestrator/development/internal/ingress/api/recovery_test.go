package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"contractpro/api-orchestrator/internal/domain/model"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
)

func newTestLogger() *logger.Logger {
	return logger.NewLogger("error")
}

func TestRecoveryMiddleware_NoPanic_PassesThrough(t *testing.T) {
	log := newTestLogger()
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/test", nil)

	RecoveryMiddleware(log)(handler).ServeHTTP(w, r)

	if w.Code != 200 {
		t.Errorf("got status %d, want 200", w.Code)
	}
	if w.Body.String() != "ok" {
		t.Errorf("got body %q, want ok", w.Body.String())
	}
}

func TestRecoveryMiddleware_StringPanic_Returns500(t *testing.T) {
	log := newTestLogger()
	handler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("something broke")
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/test", nil)

	RecoveryMiddleware(log)(handler).ServeHTTP(w, r)

	if w.Code != 500 {
		t.Errorf("got status %d, want 500", w.Code)
	}
}

func TestRecoveryMiddleware_ErrorPanic_Returns500(t *testing.T) {
	log := newTestLogger()
	handler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic(errors.New("wrapped error"))
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/test", nil)

	RecoveryMiddleware(log)(handler).ServeHTTP(w, r)

	if w.Code != 500 {
		t.Errorf("got status %d, want 500", w.Code)
	}
}

func TestRecoveryMiddleware_IntPanic_Returns500(t *testing.T) {
	log := newTestLogger()
	handler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic(42)
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/test", nil)

	RecoveryMiddleware(log)(handler).ServeHTTP(w, r)

	if w.Code != 500 {
		t.Errorf("got status %d, want 500", w.Code)
	}
}

func TestRecoveryMiddleware_ResponseBody_Format(t *testing.T) {
	log := newTestLogger()
	handler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("boom")
	})

	rc := logger.RequestContext{CorrelationID: "cid-panic"}
	ctx := logger.WithRequestContext(context.Background(), rc)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/test", nil).WithContext(ctx)

	RecoveryMiddleware(log)(handler).ServeHTTP(w, r)

	var resp model.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ErrorCode != "INTERNAL_ERROR" {
		t.Errorf("error_code = %q", resp.ErrorCode)
	}
	if resp.Message == "" {
		t.Error("message is empty")
	}
	if resp.CorrelationID != "cid-panic" {
		t.Errorf("correlation_id = %q, want cid-panic", resp.CorrelationID)
	}
	if resp.Suggestion == "" {
		t.Error("suggestion is empty")
	}
	if resp.Details != nil {
		t.Error("details should be nil for INTERNAL_ERROR")
	}
}

func TestRecoveryMiddleware_SetsCorrelationIDHeader(t *testing.T) {
	log := newTestLogger()
	handler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("boom")
	})

	rc := logger.RequestContext{CorrelationID: "hdr-cid"}
	ctx := logger.WithRequestContext(context.Background(), rc)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/test", nil).WithContext(ctx)

	RecoveryMiddleware(log)(handler).ServeHTTP(w, r)

	if got := w.Header().Get("X-Correlation-Id"); got != "hdr-cid" {
		t.Errorf("X-Correlation-Id = %q, want hdr-cid", got)
	}
}

func TestRecoveryMiddleware_SetsContentType(t *testing.T) {
	log := newTestLogger()
	handler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("boom")
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/test", nil)

	RecoveryMiddleware(log)(handler).ServeHTTP(w, r)

	if got := w.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
}

func TestRecoveryMiddleware_WithCorrelationID(t *testing.T) {
	log := newTestLogger()
	handler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("boom")
	})

	rc := logger.RequestContext{CorrelationID: "full-cid-test"}
	ctx := logger.WithRequestContext(context.Background(), rc)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/test", nil).WithContext(ctx)

	RecoveryMiddleware(log)(handler).ServeHTTP(w, r)

	// Check header
	if got := w.Header().Get("X-Correlation-Id"); got != "full-cid-test" {
		t.Errorf("header X-Correlation-Id = %q", got)
	}

	// Check body
	var resp model.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.CorrelationID != "full-cid-test" {
		t.Errorf("body correlation_id = %q", resp.CorrelationID)
	}
}

func TestRecoveryMiddleware_ErrAbortHandler_RePanics(t *testing.T) {
	log := newTestLogger()
	handler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic(http.ErrAbortHandler)
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/test", nil)

	defer func() {
		rec := recover()
		if rec == nil {
			t.Fatal("expected http.ErrAbortHandler to be re-panicked")
		}
		if rec != http.ErrAbortHandler {
			t.Errorf("expected http.ErrAbortHandler, got %v", rec)
		}
	}()

	RecoveryMiddleware(log)(handler).ServeHTTP(w, r)
	t.Fatal("should not reach here")
}

func TestRecoveryMiddleware_NoCorrelationID(t *testing.T) {
	log := newTestLogger()
	handler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("boom")
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/test", nil)

	RecoveryMiddleware(log)(handler).ServeHTTP(w, r)

	var resp model.ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.CorrelationID != "" {
		t.Errorf("expected empty correlation_id, got %q", resp.CorrelationID)
	}
}
