package model

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"contractpro/api-orchestrator/internal/infra/observability/logger"
)

// allErrorCodes lists every ErrorCode constant for catalog completeness tests.
var allErrorCodes = []ErrorCode{
	ErrAuthTokenMissing, ErrAuthTokenExpired, ErrAuthTokenInvalid,
	ErrPermissionDenied,
	ErrFileTooLarge, ErrUnsupportedFormat, ErrInvalidFile,
	ErrDocumentNotFound, ErrVersionNotFound, ErrArtifactNotFound, ErrDiffNotFound,
	ErrDocumentArchived, ErrDocumentDeleted, ErrVersionStillProcessing, ErrResultsNotReady,
	ErrRateLimitExceeded,
	ErrStorageUnavailable, ErrDMUnavailable, ErrOPMUnavailable, ErrBrokerUnavailable,
	ErrValidationError,
	ErrInternalError,
}

func TestErrorCatalog_AllCodesHaveEntries(t *testing.T) {
	for _, code := range allErrorCodes {
		_, ok := LookupError(code)
		if !ok {
			t.Errorf("error code %q not found in catalog", code)
		}
	}
}

func TestErrorCatalog_AllEntriesHaveNonEmptyMessage(t *testing.T) {
	for _, code := range allErrorCodes {
		entry, _ := LookupError(code)
		if entry.Message == "" {
			t.Errorf("error code %q has empty message", code)
		}
	}
}

func TestErrorCatalog_AllEntriesHaveValidHTTPStatus(t *testing.T) {
	for _, code := range allErrorCodes {
		entry, _ := LookupError(code)
		if entry.HTTPStatus < 400 || entry.HTTPStatus > 599 {
			t.Errorf("error code %q has invalid HTTP status %d", code, entry.HTTPStatus)
		}
	}
}

func TestErrorCatalog_HTTPStatusCategories(t *testing.T) {
	tests := []struct {
		code   ErrorCode
		status int
	}{
		{ErrAuthTokenMissing, 401},
		{ErrAuthTokenExpired, 401},
		{ErrAuthTokenInvalid, 401},
		{ErrPermissionDenied, 403},
		{ErrFileTooLarge, 413},
		{ErrUnsupportedFormat, 415},
		{ErrInvalidFile, 400},
		{ErrDocumentNotFound, 404},
		{ErrVersionNotFound, 404},
		{ErrArtifactNotFound, 404},
		{ErrDiffNotFound, 404},
		{ErrDocumentArchived, 409},
		{ErrDocumentDeleted, 409},
		{ErrVersionStillProcessing, 409},
		{ErrResultsNotReady, 409},
		{ErrRateLimitExceeded, 429},
		{ErrStorageUnavailable, 502},
		{ErrDMUnavailable, 502},
		{ErrOPMUnavailable, 502},
		{ErrBrokerUnavailable, 502},
		{ErrValidationError, 400},
		{ErrInternalError, 500},
	}
	for _, tt := range tests {
		t.Run(string(tt.code), func(t *testing.T) {
			entry, ok := LookupError(tt.code)
			if !ok {
				t.Fatalf("code %q not in catalog", tt.code)
			}
			if entry.HTTPStatus != tt.status {
				t.Errorf("got status %d, want %d", entry.HTTPStatus, tt.status)
			}
		})
	}
}

func TestLookupError_KnownCode(t *testing.T) {
	entry, ok := LookupError(ErrDocumentNotFound)
	if !ok {
		t.Fatal("expected ok=true for known code")
	}
	if entry.HTTPStatus != 404 {
		t.Errorf("got status %d, want 404", entry.HTTPStatus)
	}
	if entry.Message == "" {
		t.Error("expected non-empty message")
	}
}

func TestLookupError_UnknownCode(t *testing.T) {
	entry, ok := LookupError(ErrorCode("UNKNOWN_CODE"))
	if ok {
		t.Fatal("expected ok=false for unknown code")
	}
	if entry.HTTPStatus != 500 {
		t.Errorf("got status %d, want 500 (INTERNAL_ERROR fallback)", entry.HTTPStatus)
	}
}

func TestStatusCode_KnownCode(t *testing.T) {
	if got := StatusCode(ErrFileTooLarge); got != 413 {
		t.Errorf("got %d, want 413", got)
	}
}

func TestStatusCode_UnknownCode(t *testing.T) {
	if got := StatusCode(ErrorCode("NOPE")); got != 500 {
		t.Errorf("got %d, want 500", got)
	}
}

func newRequestWithCorrelation(correlationID string) *http.Request {
	ctx := context.Background()
	if correlationID != "" {
		rc := logger.RequestContext{CorrelationID: correlationID}
		ctx = logger.WithRequestContext(ctx, rc)
	}
	return httptest.NewRequest(http.MethodGet, "/test", nil).WithContext(ctx)
}

func TestWriteError_SetsHTTPStatus(t *testing.T) {
	w := httptest.NewRecorder()
	r := newRequestWithCorrelation("cid-1")

	WriteError(w, r, ErrDocumentNotFound, nil)

	if w.Code != 404 {
		t.Errorf("got status %d, want 404", w.Code)
	}
}

func TestWriteError_SetsContentType(t *testing.T) {
	w := httptest.NewRecorder()
	r := newRequestWithCorrelation("cid-2")

	WriteError(w, r, ErrInternalError, nil)

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("got Content-Type %q, want application/json", ct)
	}
}

func TestWriteError_SetsCorrelationIDHeader(t *testing.T) {
	w := httptest.NewRecorder()
	r := newRequestWithCorrelation("abc-123")

	WriteError(w, r, ErrInternalError, nil)

	if got := w.Header().Get("X-Correlation-Id"); got != "abc-123" {
		t.Errorf("got X-Correlation-Id %q, want %q", got, "abc-123")
	}
}

func TestWriteError_JSONBody_AllRequiredFields(t *testing.T) {
	w := httptest.NewRecorder()
	r := newRequestWithCorrelation("cid-req")

	WriteError(w, r, ErrAuthTokenMissing, nil)

	var resp ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ErrorCode != "AUTH_TOKEN_MISSING" {
		t.Errorf("error_code = %q", resp.ErrorCode)
	}
	if resp.Message == "" {
		t.Error("message is empty")
	}
	if resp.CorrelationID != "cid-req" {
		t.Errorf("correlation_id = %q, want cid-req", resp.CorrelationID)
	}
}

func TestWriteError_JSONBody_DetailsIncluded(t *testing.T) {
	w := httptest.NewRecorder()
	r := newRequestWithCorrelation("cid-det")
	details := map[string]any{"max_size_bytes": 20971520}

	WriteError(w, r, ErrFileTooLarge, details)

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(w.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := raw["details"]; !ok {
		t.Fatal("details key missing from response")
	}
	var det map[string]any
	if err := json.Unmarshal(raw["details"], &det); err != nil {
		t.Fatalf("unmarshal details: %v", err)
	}
	if det["max_size_bytes"] != float64(20971520) {
		t.Errorf("max_size_bytes = %v", det["max_size_bytes"])
	}
}

func TestWriteError_JSONBody_DetailsOmittedWhenNil(t *testing.T) {
	w := httptest.NewRecorder()
	r := newRequestWithCorrelation("cid-nil")

	WriteError(w, r, ErrInternalError, nil)

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(w.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := raw["details"]; ok {
		t.Error("details key should be omitted when nil")
	}
}

func TestWriteError_JSONBody_SuggestionOmittedWhenEmpty(t *testing.T) {
	w := httptest.NewRecorder()
	r := newRequestWithCorrelation("cid-sug")

	// DOCUMENT_DELETED has empty Suggestion
	WriteError(w, r, ErrDocumentDeleted, nil)

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(w.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := raw["suggestion"]; ok {
		t.Error("suggestion key should be omitted when empty")
	}
}

func TestWriteError_JSONBody_SuggestionIncluded(t *testing.T) {
	w := httptest.NewRecorder()
	r := newRequestWithCorrelation("cid-sug2")

	WriteError(w, r, ErrDocumentNotFound, nil)

	var resp ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Suggestion == "" {
		t.Error("expected non-empty suggestion for DOCUMENT_NOT_FOUND")
	}
}

func TestWriteError_CorrelationID_EmptyWhenNoContext(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/test", nil)

	WriteError(w, r, ErrInternalError, nil)

	var resp ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.CorrelationID != "" {
		t.Errorf("expected empty correlation_id, got %q", resp.CorrelationID)
	}
}

func TestWriteErrorWithMessage_OverridesMessage(t *testing.T) {
	w := httptest.NewRecorder()
	r := newRequestWithCorrelation("cid-msg")

	WriteErrorWithMessage(w, r, ErrValidationError, "Поле «title» обязательно.", nil)

	var resp ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Message != "Поле «title» обязательно." {
		t.Errorf("got message %q", resp.Message)
	}
}

func TestWriteErrorWithMessage_PreservesSuggestion(t *testing.T) {
	w := httptest.NewRecorder()
	r := newRequestWithCorrelation("cid-pres")

	WriteErrorWithMessage(w, r, ErrValidationError, "custom", nil)

	var resp ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	entry, _ := LookupError(ErrValidationError)
	if resp.Suggestion != entry.Suggestion {
		t.Errorf("suggestion = %q, want %q", resp.Suggestion, entry.Suggestion)
	}
}

func TestWriteErrorWithMessage_PreservesHTTPStatus(t *testing.T) {
	w := httptest.NewRecorder()
	r := newRequestWithCorrelation("cid-stat")

	WriteErrorWithMessage(w, r, ErrPermissionDenied, "custom", nil)

	if w.Code != 403 {
		t.Errorf("got status %d, want 403", w.Code)
	}
}

func TestErrorResponse_JSONFieldNames(t *testing.T) {
	resp := ErrorResponse{
		ErrorCode:     "TEST_CODE",
		Message:       "test message",
		CorrelationID: "cid-1",
		Suggestion:    "try again",
		Details:       map[string]string{"key": "val"},
	}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	expected := []string{"error_code", "message", "details", "correlation_id", "suggestion"}
	for _, key := range expected {
		if _, ok := raw[key]; !ok {
			t.Errorf("missing JSON key %q", key)
		}
	}
}

func TestErrorCatalog_CountsMatch(t *testing.T) {
	if len(allErrorCodes) != len(errorCatalog) {
		t.Errorf("allErrorCodes has %d entries, errorCatalog has %d — they must stay in sync",
			len(allErrorCodes), len(errorCatalog))
	}
}

func TestWriteError_UnknownCode_Returns500(t *testing.T) {
	w := httptest.NewRecorder()
	r := newRequestWithCorrelation("cid-unk")

	WriteError(w, r, ErrorCode("COMPLETELY_UNKNOWN"), nil)

	if w.Code != 500 {
		t.Errorf("got status %d, want 500", w.Code)
	}
	var resp ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// For unknown codes, LookupError returns the INTERNAL_ERROR entry,
	// but the error_code in the response should still be the original code.
	if resp.ErrorCode != "COMPLETELY_UNKNOWN" {
		t.Errorf("error_code = %q, want COMPLETELY_UNKNOWN", resp.ErrorCode)
	}
}
