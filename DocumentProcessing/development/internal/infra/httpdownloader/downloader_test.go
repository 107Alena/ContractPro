package httpdownloader

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"contractpro/document-processing/internal/domain/port"
)

// Tests that verify non-SSRF HTTP behavior use newDownloader(timeout, nil)
// to skip SSRF checking, because httptest.NewServer binds to 127.0.0.1.

func TestDownload_Success200(t *testing.T) {
	body := "PDF file content"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Length", "16")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	d := newDownloader(5*time.Second, nil)
	rc, cl, err := d.Download(context.Background(), srv.URL+"/file.pdf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer rc.Close()

	if cl != 16 {
		t.Errorf("expected content-length 16, got %d", cl)
	}

	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}
	if string(data) != body {
		t.Errorf("expected body %q, got %q", body, string(data))
	}
}

func TestDownload_Success200_Chunked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		// Flush forces chunked transfer encoding, no Content-Length header.
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("ResponseWriter does not implement Flusher")
		}
		_, _ = w.Write([]byte("chunk1"))
		flusher.Flush()
		_, _ = w.Write([]byte("chunk2"))
		flusher.Flush()
	}))
	defer srv.Close()

	d := newDownloader(5*time.Second, nil)
	rc, cl, err := d.Download(context.Background(), srv.URL+"/chunked")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer rc.Close()

	if cl != -1 {
		t.Errorf("expected content-length -1 for chunked, got %d", cl)
	}

	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}
	if string(data) != "chunk1chunk2" {
		t.Errorf("expected body %q, got %q", "chunk1chunk2", string(data))
	}
}

func TestDownload_HTTPStatusErrors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantCode   string
		wantRetry  bool
	}{
		{
			name:       "HTTP 404 → FILE_NOT_FOUND",
			statusCode: http.StatusNotFound,
			wantCode:   port.ErrCodeFileNotFound,
			wantRetry:  false,
		},
		{
			name:       "HTTP 403 → FILE_NOT_FOUND",
			statusCode: http.StatusForbidden,
			wantCode:   port.ErrCodeFileNotFound,
			wantRetry:  false,
		},
		{
			name:       "HTTP 500 → SERVICE_UNAVAILABLE retryable",
			statusCode: http.StatusInternalServerError,
			wantCode:   port.ErrCodeServiceUnavailable,
			wantRetry:  true,
		},
		{
			name:       "HTTP 502 → SERVICE_UNAVAILABLE retryable",
			statusCode: http.StatusBadGateway,
			wantCode:   port.ErrCodeServiceUnavailable,
			wantRetry:  true,
		},
		{
			name:       "HTTP 503 → SERVICE_UNAVAILABLE retryable",
			statusCode: http.StatusServiceUnavailable,
			wantCode:   port.ErrCodeServiceUnavailable,
			wantRetry:  true,
		},
		{
			name:       "HTTP 429 → SERVICE_UNAVAILABLE retryable",
			statusCode: http.StatusTooManyRequests,
			wantCode:   port.ErrCodeServiceUnavailable,
			wantRetry:  true,
		},
		{
			name:       "HTTP 400 → VALIDATION_ERROR non-retryable",
			statusCode: http.StatusBadRequest,
			wantCode:   port.ErrCodeValidation,
			wantRetry:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer srv.Close()

			d := newDownloader(5*time.Second, nil)
			_, _, err := d.Download(context.Background(), srv.URL+"/file")
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			code := port.ErrorCode(err)
			if code != tt.wantCode {
				t.Errorf("expected error code %q, got %q", tt.wantCode, code)
			}
			if port.IsRetryable(err) != tt.wantRetry {
				t.Errorf("expected retryable=%v, got %v", tt.wantRetry, port.IsRetryable(err))
			}
		})
	}
}

func TestDownload_ContextCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Delay response so context cancellation takes effect.
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := newDownloader(10*time.Second, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, _, err := d.Download(ctx, srv.URL+"/file")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestDownload_InvalidURL(t *testing.T) {
	d := newDownloader(5*time.Second, nil)

	_, _, err := d.Download(context.Background(), "://invalid-url")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if port.ErrorCode(err) != port.ErrCodeValidation {
		t.Errorf("expected error code %q, got %q", port.ErrCodeValidation, port.ErrorCode(err))
	}
}

func TestDownload_ConnectionRefused(t *testing.T) {
	// Find a port that is not listening.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}
	addr := listener.Addr().String()
	listener.Close() // close immediately so nothing is listening

	// Use newDownloader without SSRF control so the connection attempt reaches
	// the TCP level. With SSRF control, 127.0.0.1 would be blocked first.
	d := newDownloader(2*time.Second, nil)

	_, _, dlErr := d.Download(context.Background(), "http://"+addr+"/file")
	if dlErr == nil {
		t.Fatal("expected error, got nil")
	}
	if port.ErrorCode(dlErr) != port.ErrCodeServiceUnavailable {
		t.Errorf("expected error code %q, got %q", port.ErrCodeServiceUnavailable, port.ErrorCode(dlErr))
	}
	if !port.IsRetryable(dlErr) {
		t.Error("expected connection refused error to be retryable")
	}
}

func TestDownload_BodyReadableAfterSuccess(t *testing.T) {
	expected := "the pdf bytes here"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(expected))
	}))
	defer srv.Close()

	d := newDownloader(5*time.Second, nil)
	rc, _, err := d.Download(context.Background(), srv.URL+"/doc.pdf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}
	if string(data) != expected {
		t.Errorf("expected body %q, got %q", expected, string(data))
	}
}

// --- Security: SSRF protection tests ---

func TestDownload_SSRFBlocked_Loopback(t *testing.T) {
	// Use the production constructor which includes SSRF control.
	d := NewDownloader(5 * time.Second)

	_, _, err := d.Download(context.Background(), "http://127.0.0.1:8080/file.pdf")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	code := port.ErrorCode(err)
	if code != port.ErrCodeSSRFBlocked {
		t.Errorf("expected error code %q, got %q (error: %v)", port.ErrCodeSSRFBlocked, code, err)
	}
	if port.IsRetryable(err) {
		t.Error("SSRF error should not be retryable")
	}
}

func TestDownload_SSRFBlocked_PrivateIP(t *testing.T) {
	d := NewDownloader(2 * time.Second)

	tests := []struct {
		name string
		url  string
	}{
		{"private_10", "http://10.0.0.1:80/file.pdf"},
		{"private_172_16", "http://172.16.0.1:80/file.pdf"},
		{"private_192_168", "http://192.168.1.1:80/file.pdf"},
		{"link_local_169_254", "http://169.254.169.254:80/latest/meta-data/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := d.Download(context.Background(), tt.url)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			code := port.ErrorCode(err)
			if code != port.ErrCodeSSRFBlocked {
				t.Errorf("expected error code %q, got %q (error: %v)", port.ErrCodeSSRFBlocked, code, err)
			}
		})
	}
}

// --- Security: Content-Type validation tests ---

func TestDownload_ContentType_PDF_Accepted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("PDF data"))
	}))
	defer srv.Close()

	d := newDownloader(5*time.Second, nil)
	rc, _, err := d.Download(context.Background(), srv.URL+"/file.pdf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rc.Close()
}

func TestDownload_ContentType_PDFWithCharset_Accepted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("PDF data"))
	}))
	defer srv.Close()

	d := newDownloader(5*time.Second, nil)
	rc, _, err := d.Download(context.Background(), srv.URL+"/file.pdf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rc.Close()
}

func TestDownload_ContentType_OctetStream_Accepted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("PDF data"))
	}))
	defer srv.Close()

	d := newDownloader(5*time.Second, nil)
	rc, _, err := d.Download(context.Background(), srv.URL+"/file.pdf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rc.Close()
}

func TestDownload_ContentType_Empty_Accepted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Don't set Content-Type at all.
		// Note: Go's http.ResponseWriter auto-detects, so we clear it.
		w.Header().Del("Content-Type")
		w.Header().Set("Content-Type", "")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("PDF data"))
	}))
	defer srv.Close()

	d := newDownloader(5*time.Second, nil)
	rc, _, err := d.Download(context.Background(), srv.URL+"/file.pdf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rc.Close()
}

func TestDownload_ContentType_HTML_Rejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>not a PDF</html>"))
	}))
	defer srv.Close()

	d := newDownloader(5*time.Second, nil)
	_, _, err := d.Download(context.Background(), srv.URL+"/file.pdf")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if port.ErrorCode(err) != port.ErrCodeInvalidFormat {
		t.Errorf("expected error code %q, got %q", port.ErrCodeInvalidFormat, port.ErrorCode(err))
	}
	if port.IsRetryable(err) {
		t.Error("Content-Type error should not be retryable")
	}
}

func TestDownload_ContentType_TextPlain_Rejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not a PDF"))
	}))
	defer srv.Close()

	d := newDownloader(5*time.Second, nil)
	_, _, err := d.Download(context.Background(), srv.URL+"/file.pdf")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if port.ErrorCode(err) != port.ErrCodeInvalidFormat {
		t.Errorf("expected error code %q, got %q", port.ErrCodeInvalidFormat, port.ErrorCode(err))
	}
}

// --- Unit tests for helper functions ---

func TestIsAcceptablePDFContentType(t *testing.T) {
	tests := []struct {
		ct     string
		accept bool
	}{
		{"application/pdf", true},
		{"Application/PDF", true},
		{"application/pdf; charset=utf-8", true},
		{"application/octet-stream", true},
		{"text/html", false},
		{"text/plain", false},
		{"image/jpeg", false},
		{"application/json", false},
		{"", true},           // empty → unparseable → allow through
		{";;;bad", true},     // unparseable → allow through
	}

	for _, tt := range tests {
		t.Run(tt.ct, func(t *testing.T) {
			got := isAcceptablePDFContentType(tt.ct)
			if got != tt.accept {
				t.Errorf("isAcceptablePDFContentType(%q) = %v, want %v", tt.ct, got, tt.accept)
			}
		})
	}
}

func TestSsrfControl(t *testing.T) {
	tests := []struct {
		name    string
		address string
		blocked bool
	}{
		{"zero_network", "0.0.0.0:80", true},
		{"loopback", "127.0.0.1:80", true},
		{"private_10", "10.0.0.1:443", true},
		{"private_172", "172.16.0.1:8080", true},
		{"private_192", "192.168.1.1:80", true},
		{"link_local", "169.254.169.254:80", true},
		{"public", "93.184.216.34:80", false},
		{"public_8888", "8.8.8.8:443", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ssrfControl("tcp", tt.address, nil)
			if tt.blocked {
				if err == nil {
					t.Fatal("expected SSRF error, got nil")
				}
				if port.ErrorCode(err) != port.ErrCodeSSRFBlocked {
					t.Errorf("expected %q, got %q", port.ErrCodeSSRFBlocked, port.ErrorCode(err))
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}
