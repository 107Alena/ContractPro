package ocr

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"contractpro/document-processing/internal/domain/port"
)

// --- Helpers ---

// newTestClient creates an httptest server with the given handler and returns
// a Client pointing at it, plus the server (for cleanup).
func newTestClient(handler http.HandlerFunc) (*Client, *httptest.Server) {
	srv := httptest.NewServer(handler)
	client := newClientWithHTTP(srv.Client(), srv.URL, "test-api-key", "test-folder-id")
	return client, srv
}

// --- TestRecognize_Success ---

func TestRecognize_Success(t *testing.T) {
	client, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":{"textAnnotation":{"fullText":"Hello, world!"}}}`))
	})
	defer srv.Close()

	text, err := client.Recognize(context.Background(), strings.NewReader("fake-pdf"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "Hello, world!" {
		t.Errorf("text = %q, want %q", text, "Hello, world!")
	}
}

// --- TestRecognize_Success_RequestFormat ---

func TestRecognize_Success_RequestFormat(t *testing.T) {
	pdfContent := []byte("fake-pdf-bytes")
	expectedB64 := base64.StdEncoding.EncodeToString(pdfContent)

	client, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		// Validate method.
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want %q", r.Method, http.MethodPost)
		}

		// Validate headers.
		if got := r.Header.Get("Authorization"); got != "Api-Key test-api-key" {
			t.Errorf("Authorization = %q, want %q", got, "Api-Key test-api-key")
		}
		if got := r.Header.Get("x-folder-id"); got != "test-folder-id" {
			t.Errorf("x-folder-id = %q, want %q", got, "test-folder-id")
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want %q", got, "application/json")
		}

		// Validate body JSON.
		var reqBody recognizeRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Errorf("failed to decode request body: %v", err)
			http.Error(w, "decode failed", http.StatusInternalServerError)
			return
		}
		if reqBody.MimeType != "application/pdf" {
			t.Errorf("mimeType = %q, want %q", reqBody.MimeType, "application/pdf")
		}
		if len(reqBody.LanguageCodes) != 2 || reqBody.LanguageCodes[0] != "ru" || reqBody.LanguageCodes[1] != "en" {
			t.Errorf("languageCodes = %v, want [ru en]", reqBody.LanguageCodes)
		}
		if reqBody.Content != expectedB64 {
			t.Errorf("content = %q, want %q", reqBody.Content, expectedB64)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":{"textAnnotation":{"fullText":"ok"}}}`))
	})
	defer srv.Close()

	_, err := client.Recognize(context.Background(), bytes.NewReader(pdfContent))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- TestRecognize_Success_EmptyText ---

func TestRecognize_Success_EmptyText(t *testing.T) {
	client, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":{"textAnnotation":{"fullText":""}}}`))
	})
	defer srv.Close()

	text, err := client.Recognize(context.Background(), strings.NewReader("pdf"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "" {
		t.Errorf("text = %q, want empty string", text)
	}
}

// --- TestRecognize_HTTPStatusErrors ---

func TestRecognize_HTTPStatusErrors(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		retryable bool
	}{
		{"400_BadRequest", 400, false},
		{"401_Unauthorized", 401, false},
		{"403_Forbidden", 403, false},
		{"429_TooManyRequests", 429, true},
		{"500_InternalServerError", 500, true},
		{"502_BadGateway", 502, true},
		{"503_ServiceUnavailable", 503, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte("error body"))
			})
			defer srv.Close()

			_, err := client.Recognize(context.Background(), strings.NewReader("pdf"))
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			if code := port.ErrorCode(err); code != port.ErrCodeOCRFailed {
				t.Errorf("error code = %q, want %q", code, port.ErrCodeOCRFailed)
			}
			if port.IsRetryable(err) != tt.retryable {
				t.Errorf("retryable = %v, want %v", port.IsRetryable(err), tt.retryable)
			}
			if !strings.Contains(err.Error(), "HTTP") {
				t.Errorf("error should contain 'HTTP', got %q", err.Error())
			}
		})
	}
}

// --- TestRecognize_ContextCanceled ---

func TestRecognize_ContextCanceled(t *testing.T) {
	client, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		// Server never responds — client context is already cancelled.
		time.Sleep(5 * time.Second)
	})
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := client.Recognize(ctx, strings.NewReader("pdf"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if port.IsDomainError(err) {
		t.Error("context.Canceled should not be wrapped in DomainError")
	}
}

// --- TestRecognize_ContextDeadlineExceeded ---

func TestRecognize_ContextDeadlineExceeded(t *testing.T) {
	client, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	})
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	// Give the deadline a moment to expire.
	time.Sleep(5 * time.Millisecond)

	_, err := client.Recognize(ctx, strings.NewReader("pdf"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}
}

// --- TestRecognize_ConnectionRefused ---

func TestRecognize_ConnectionRefused(t *testing.T) {
	// Create a server and immediately close it to get a refused port.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closedURL := srv.URL
	srv.Close()

	client := newClientWithHTTP(&http.Client{Timeout: 2 * time.Second}, closedURL, "key", "folder")

	_, err := client.Recognize(context.Background(), strings.NewReader("pdf"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if port.ErrorCode(err) != port.ErrCodeOCRFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeOCRFailed)
	}
	if !port.IsRetryable(err) {
		t.Error("connection refused should be retryable")
	}
}

// --- TestRecognize_InvalidJSON ---

func TestRecognize_InvalidJSON(t *testing.T) {
	client, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{invalid json`))
	})
	defer srv.Close()

	_, err := client.Recognize(context.Background(), strings.NewReader("pdf"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if port.ErrorCode(err) != port.ErrCodeOCRFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeOCRFailed)
	}
	if port.IsRetryable(err) {
		t.Error("invalid JSON should not be retryable")
	}
}

// --- TestRecognize_MissingFullText ---

func TestRecognize_MissingFullText(t *testing.T) {
	client, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":{}}`))
	})
	defer srv.Close()

	text, err := client.Recognize(context.Background(), strings.NewReader("pdf"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "" {
		t.Errorf("text = %q, want empty string (zero value)", text)
	}
}

// --- TestRecognize_EmptyReader ---

func TestRecognize_EmptyReader(t *testing.T) {
	client, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		// Empty input should still produce a valid request; API returns 400.
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad request: empty content"))
	})
	defer srv.Close()

	_, err := client.Recognize(context.Background(), bytes.NewReader(nil))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if port.ErrorCode(err) != port.ErrCodeOCRFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeOCRFailed)
	}
	if port.IsRetryable(err) {
		t.Error("400 should not be retryable")
	}
}

// --- TestRecognize_LargeInput ---

func TestRecognize_LargeInput(t *testing.T) {
	largeData := bytes.Repeat([]byte("A"), 1024*1024) // 1 MB
	expectedB64 := base64.StdEncoding.EncodeToString(largeData)

	client, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		var reqBody recognizeRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Errorf("failed to decode request body: %v", err)
			http.Error(w, "decode failed", http.StatusInternalServerError)
			return
		}
		if reqBody.Content != expectedB64 {
			t.Errorf("base64 content length = %d, want %d", len(reqBody.Content), len(expectedB64))
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":{"textAnnotation":{"fullText":"large"}}}`))
	})
	defer srv.Close()

	text, err := client.Recognize(context.Background(), bytes.NewReader(largeData))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "large" {
		t.Errorf("text = %q, want %q", text, "large")
	}
}

// --- TestRecognize_ReaderError ---

type errorReader struct{ err error }

func (r *errorReader) Read([]byte) (int, error) { return 0, r.err }

func TestRecognize_ReaderError(t *testing.T) {
	client, srv := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called when reader fails")
	})
	defer srv.Close()

	_, err := client.Recognize(context.Background(), &errorReader{err: errors.New("disk read failed")})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if port.ErrorCode(err) != port.ErrCodeOCRFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeOCRFailed)
	}
	if !port.IsRetryable(err) {
		t.Error("reader error should be retryable")
	}
}

// --- TestInterfaceCompliance ---

func TestInterfaceCompliance(t *testing.T) {
	var _ port.OCRServicePort = (*Client)(nil)
}

// --- mapError tests ---

func TestMapError_ContextCanceled(t *testing.T) {
	err := mapError(context.Canceled, "test")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if port.IsDomainError(err) {
		t.Error("context.Canceled should not be wrapped in DomainError")
	}
}

func TestMapError_ContextDeadlineExceeded(t *testing.T) {
	err := mapError(context.DeadlineExceeded, "test")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}
	if port.IsDomainError(err) {
		t.Error("context.DeadlineExceeded should not be wrapped in DomainError")
	}
}

func TestMapError_NetworkError(t *testing.T) {
	netErr := &net.OpError{
		Op:  "dial",
		Net: "tcp",
		Err: errors.New("connection refused"),
	}
	err := mapError(netErr, "send request")

	if port.ErrorCode(err) != port.ErrCodeOCRFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeOCRFailed)
	}
	if !port.IsRetryable(err) {
		t.Error("network error should be retryable")
	}
	if !strings.Contains(err.Error(), "send request") {
		t.Errorf("error should contain operation, got %q", err.Error())
	}
}

// --- mapHTTPStatus tests ---

func TestMapHTTPStatus_429(t *testing.T) {
	err := mapHTTPStatus(429, "rate limited")
	if port.ErrorCode(err) != port.ErrCodeOCRFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeOCRFailed)
	}
	if !port.IsRetryable(err) {
		t.Error("429 should be retryable")
	}
}

func TestMapHTTPStatus_500(t *testing.T) {
	err := mapHTTPStatus(500, "internal server error")
	if port.ErrorCode(err) != port.ErrCodeOCRFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeOCRFailed)
	}
	if !port.IsRetryable(err) {
		t.Error("500 should be retryable")
	}
}

func TestMapHTTPStatus_400(t *testing.T) {
	err := mapHTTPStatus(400, "bad request")
	if port.ErrorCode(err) != port.ErrCodeOCRFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeOCRFailed)
	}
	if port.IsRetryable(err) {
		t.Error("400 should not be retryable")
	}
}

func TestMapHTTPStatus_401(t *testing.T) {
	err := mapHTTPStatus(401, "unauthorized")
	if port.ErrorCode(err) != port.ErrCodeOCRFailed {
		t.Errorf("error code = %q, want %q", port.ErrorCode(err), port.ErrCodeOCRFailed)
	}
	if port.IsRetryable(err) {
		t.Error("401 should not be retryable")
	}
}

func TestMapHTTPStatus_ErrorMessageContainsBody(t *testing.T) {
	err := mapHTTPStatus(503, "service temporarily unavailable")
	if !strings.Contains(err.Error(), "service temporarily unavailable") {
		t.Errorf("error should contain body text, got %q", err.Error())
	}
}
