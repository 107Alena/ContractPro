package httpdownloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"contractpro/document-processing/internal/domain/port"
)

// Downloader implements SourceFileDownloaderPort using an HTTP client.
// It downloads files by URL, classifies HTTP errors into domain errors,
// and limits redirects to 3.
type Downloader struct {
	client *http.Client
}

// NewDownloader creates a Downloader with the given request timeout.
// The HTTP client is configured to allow at most 3 redirects.
func NewDownloader(timeout time.Duration) *Downloader {
	return &Downloader{
		client: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				MaxIdleConnsPerHost: 5,
				IdleConnTimeout:     90 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second,
			},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 3 {
					return errors.New("too many redirects (max 3)")
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

	return resp.Body, resp.ContentLength, nil
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
// Context errors are passed through raw.
// Network errors → SERVICE_UNAVAILABLE (retryable).
func classifyTransportError(err error) error {
	// Unwrap url.Error to check for context errors underneath.
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
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
