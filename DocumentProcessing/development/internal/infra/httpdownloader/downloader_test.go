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

func TestDownload_Success200(t *testing.T) {
	body := "PDF file content"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "16")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	d := NewDownloader(5 * time.Second)
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

	d := NewDownloader(5 * time.Second)
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
		name        string
		statusCode  int
		wantCode    string
		wantRetry   bool
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

			d := NewDownloader(5 * time.Second)
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

	d := NewDownloader(10 * time.Second)

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
	d := NewDownloader(5 * time.Second)

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

	d := NewDownloader(2 * time.Second)

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
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(expected))
	}))
	defer srv.Close()

	d := NewDownloader(5 * time.Second)
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
