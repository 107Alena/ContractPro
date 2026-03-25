package httpdownloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	"contractpro/document-processing/internal/domain/port"
)

// Downloader implements SourceFileDownloaderPort using an HTTP client.
// It downloads files by URL, classifies HTTP errors into domain errors,
// limits redirects to 3, and blocks connections to private/internal IPs
// (SSRF defense-in-depth at the TCP connection level).
type Downloader struct {
	client *http.Client
}

// acceptablePDFTypes lists Content-Type media types accepted for PDF downloads.
// application/octet-stream is allowed because some S3-compatible storage
// services return it as the default Content-Type.
var acceptablePDFTypes = map[string]bool{
	"application/pdf":          true,
	"application/octet-stream": true,
}

// NewDownloader creates a Downloader with the given request timeout.
// The HTTP client is configured to:
//   - Allow at most 3 redirects
//   - Block connections to private/loopback/link-local IPs (SSRF protection)
//   - Validate Content-Type header on responses
func NewDownloader(timeout time.Duration) *Downloader {
	return newDownloader(timeout, ssrfControl)
}

// newDownloader creates a Downloader with a custom dialer control function.
// Pass nil for control to disable SSRF checking (used in unit tests
// where httptest.Server listens on 127.0.0.1).
func newDownloader(timeout time.Duration, control func(string, string, syscall.RawConn) error) *Downloader {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	if control != nil {
		dialer.Control = control
	}
	return &Downloader{
		client: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				DialContext:         dialer.DialContext,
				MaxIdleConns:        10,
				MaxIdleConnsPerHost: 5,
				IdleConnTimeout:     90 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second,
			},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 3 {
					return errors.New("too many redirects (max 3)")
				}
				// Block redirects to non-HTTP schemes (defense-in-depth).
				if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
					return port.NewSSRFBlockedError(
						fmt.Sprintf("redirect to disallowed scheme %q", req.URL.Scheme),
					)
				}
				return nil
			},
		},
	}
}

// Download performs an HTTP GET request for the given fileURL.
// On success (HTTP 200), it returns the response body, Content-Length
// (-1 if unknown/chunked), and nil error.
// The caller is responsible for closing the returned io.ReadCloser.
//
// Non-200 responses and network errors are classified into domain errors:
//   - 404/403 -> FILE_NOT_FOUND (non-retryable)
//   - 408/429/502/503/504 -> SERVICE_UNAVAILABLE (retryable)
//   - Other 4xx -> VALIDATION_ERROR (non-retryable)
//   - Other 5xx -> SERVICE_UNAVAILABLE (retryable)
//   - Context errors -> passed through raw
//   - Network/URL errors -> SERVICE_UNAVAILABLE or VALIDATION_ERROR
//   - Connection to private IP -> SSRF_BLOCKED (non-retryable)
//   - Non-PDF Content-Type -> INVALID_FORMAT (non-retryable)
func (d *Downloader) Download(ctx context.Context, fileURL string) (io.ReadCloser, int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, 0, classifyRequestError(err)
	}
	req.Header.Set("User-Agent", "ContractPro-DP/1.0")

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, 0, classifyTransportError(err)
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, 0, classifyHTTPStatus(resp.StatusCode)
	}

	// Check Content-Type header: reject clearly non-PDF responses.
	// Empty/missing Content-Type is allowed (common for S3 pre-signed URLs).
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		if !isAcceptablePDFContentType(ct) {
			_ = resp.Body.Close()
			return nil, 0, port.NewInvalidFormatError(
				fmt.Sprintf("unexpected content-type %q, expected application/pdf", ct),
			)
		}
	}

	return resp.Body, resp.ContentLength, nil
}

// isAcceptablePDFContentType parses the media type from a Content-Type header
// value and checks it against the acceptable types list.
func isAcceptablePDFContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		// Unparseable Content-Type — allow through, PDF magic bytes check will catch non-PDFs.
		return true
	}
	return acceptablePDFTypes[strings.ToLower(mediaType)]
}

// classifyRequestError converts request creation errors into domain errors.
// Invalid URL → VALIDATION_ERROR.
func classifyRequestError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return port.NewValidationError(fmt.Sprintf("invalid file URL: %v", urlErr))
	}
	return port.NewValidationError(fmt.Sprintf("failed to create download request: %v", err))
}

// classifyTransportError converts HTTP transport errors into domain errors.
// Context errors are passed through raw. SSRF errors are passed through.
// Network errors → SERVICE_UNAVAILABLE (retryable).
func classifyTransportError(err error) error {
	// Unwrap url.Error to check for context errors underneath.
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}

	// Pass through SSRF_BLOCKED errors from the dialer control function.
	if port.IsDomainError(err) {
		return err
	}

	// Check for invalid URL inside transport error (e.g., unsupported scheme).
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		// url.Error wrapping an url.InvalidHostError or similar → validation.
		var invalidHostErr url.InvalidHostError
		if errors.As(err, &invalidHostErr) {
			return port.NewValidationError(fmt.Sprintf("invalid file URL: %v", err))
		}
	}

	return port.NewServiceUnavailableError("file download failed", err)
}

// classifyHTTPStatus converts non-200 HTTP status codes into domain errors.
func classifyHTTPStatus(status int) error {
	switch status {
	case http.StatusNotFound, http.StatusForbidden:
		return port.NewFileNotFoundError(
			fmt.Sprintf("file not accessible: HTTP %d", status), nil,
		)
	case http.StatusRequestTimeout, http.StatusTooManyRequests,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return port.NewServiceUnavailableError(
			fmt.Sprintf("upstream server error: HTTP %d", status), nil,
		)
	default:
		if status >= 500 {
			return port.NewServiceUnavailableError(
				fmt.Sprintf("upstream server error: HTTP %d", status), nil,
			)
		}
		return port.NewValidationError(
			fmt.Sprintf("download rejected: HTTP %d", status),
		)
	}
}

// compile-time check: Downloader implements SourceFileDownloaderPort.
var _ port.SourceFileDownloaderPort = (*Downloader)(nil)
