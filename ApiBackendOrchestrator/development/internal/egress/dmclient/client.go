// Package dmclient provides an HTTP client for the Document Management (DM)
// REST API with retry logic and circuit breaker protection.
//
// The client follows the same resilience patterns as the objectstorage client:
// exponential backoff with jitter for retries, gobreaker circuit breaker for
// cascading-failure prevention, and per-attempt timeouts.
//
// Every request is enriched with organization, user, and correlation headers
// extracted from context.Context (set by the auth and logging middleware).
//
// The client returns raw DMError values with StatusCode and Body so that
// callers (handler layer) can translate them via model.MapDMError.
package dmclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/sony/gobreaker/v2"

	"contractpro/api-orchestrator/internal/config"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"
)

const (
	jitterFraction = 0.25

	// maxResponseBodySize caps response body reads to prevent OOM from
	// misbehaving or compromised DM service.
	maxResponseBodySize = 10 * 1024 * 1024 // 10 MB

	headerOrganizationID = "X-Organization-ID"
	headerUserID         = "X-User-ID"
	headerCorrelationID  = "X-Correlation-ID"
	headerContentType    = "Content-Type"

	contentTypeJSON = "application/json"
)

// DMClient defines the interface for interacting with the DM REST API.
// This interface enables testing and decouples callers from the concrete
// implementation.
type DMClient interface {
	// CreateDocument creates a new document in DM.
	CreateDocument(ctx context.Context, req CreateDocumentRequest) (*Document, error)

	// ListDocuments returns a paginated list of documents for the organization.
	ListDocuments(ctx context.Context, params ListDocumentsParams) (*DocumentList, error)

	// GetDocument returns a document with its current version.
	GetDocument(ctx context.Context, documentID string) (*DocumentWithCurrentVersion, error)

	// DeleteDocument performs a soft delete on a document.
	DeleteDocument(ctx context.Context, documentID string) (*Document, error)

	// ArchiveDocument archives a document.
	ArchiveDocument(ctx context.Context, documentID string) (*Document, error)

	// CreateVersion creates a new version for a document.
	CreateVersion(ctx context.Context, documentID string, req CreateVersionRequest) (*DocumentVersion, error)

	// ListVersions returns a paginated list of versions for a document.
	ListVersions(ctx context.Context, documentID string, params ListVersionsParams) (*VersionList, error)

	// GetVersion returns version metadata with artifact descriptors.
	GetVersion(ctx context.Context, documentID, versionID string) (*DocumentVersionWithArtifacts, error)

	// ListArtifacts returns artifact descriptors for a version.
	ListArtifacts(ctx context.Context, documentID, versionID string, params ListArtifactsParams) (*ArtifactDescriptorList, error)

	// GetArtifact retrieves an artifact. For JSON artifacts (200), Content is
	// populated. For blob artifacts (302), RedirectURL is populated with the
	// presigned S3 URL. The client does NOT follow redirects for this endpoint.
	GetArtifact(ctx context.Context, documentID, versionID, artifactType string) (*ArtifactResponse, error)

	// GetDiff returns the diff between two versions of a document.
	GetDiff(ctx context.Context, documentID, baseVersionID, targetVersionID string) (*VersionDiff, error)

	// ListAuditRecords returns a paginated list of audit records.
	ListAuditRecords(ctx context.Context, params ListAuditParams) (*AuditRecordList, error)
}

// Client implements DMClient with retry logic and circuit breaker.
type Client struct {
	httpClient   *http.Client
	baseURL      string
	cb           *gobreaker.CircuitBreaker[struct{}]
	timeoutRead  time.Duration
	timeoutWrite time.Duration
	retryMax     int
	retryBackoff time.Duration
	log          *logger.Logger
}

// Compile-time interface check.
var _ DMClient = (*Client)(nil)

// NewClient creates a Client from configuration. The http.Client is configured
// with no automatic redirect following (needed for GetArtifact 302 handling).
func NewClient(cfg config.DMClientConfig, cbCfg config.CircuitBreakerConfig, log *logger.Logger) *Client {
	return newClient(
		&http.Client{
			// Disable automatic redirect following. The GetArtifact endpoint
			// returns 302 with a Location header that we need to capture.
			// For all other endpoints, DM does not issue redirects.
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		cfg.BaseURL,
		cbCfg,
		cfg.TimeoutRead,
		cfg.TimeoutWrite,
		cfg.RetryMax,
		cfg.RetryBackoff,
		log,
	)
}

// newClient is the shared constructor used by NewClient (production) and tests.
func newClient(
	httpClient *http.Client,
	baseURL string,
	cbCfg config.CircuitBreakerConfig,
	timeoutRead time.Duration,
	timeoutWrite time.Duration,
	retryMax int,
	retryBackoff time.Duration,
	log *logger.Logger,
) *Client {
	componentLog := log.With("component", "dm-client")

	cb := gobreaker.NewCircuitBreaker[struct{}](gobreaker.Settings{
		Name:        "dm-client",
		MaxRequests: uint32(cbCfg.MaxRequests),
		Timeout:     cbCfg.Timeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= uint32(cbCfg.FailureThreshold)
		},
		IsSuccessful: func(err error) bool {
			if err == nil {
				return true
			}
			return !isCBFailure(err)
		},
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			componentLog.Warn(context.Background(),
				"circuit breaker state change",
				"name", name,
				"from", from.String(),
				"to", to.String(),
			)
		},
	})

	return &Client{
		httpClient:   httpClient,
		baseURL:      baseURL,
		cb:           cb,
		timeoutRead:  timeoutRead,
		timeoutWrite: timeoutWrite,
		retryMax:     retryMax,
		retryBackoff: retryBackoff,
		log:          componentLog,
	}
}

// ---------------------------------------------------------------------------
// Public API methods
// ---------------------------------------------------------------------------

func (c *Client) CreateDocument(ctx context.Context, req CreateDocumentRequest) (*Document, error) {
	var doc Document
	err := c.doJSON(ctx, "CreateDocument", http.MethodPost, "/documents", nil, req, &doc, c.timeoutWrite)
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

func (c *Client) ListDocuments(ctx context.Context, params ListDocumentsParams) (*DocumentList, error) {
	q := make(url.Values)
	if params.Page > 0 {
		q.Set("page", strconv.Itoa(params.Page))
	}
	if params.Size > 0 {
		q.Set("size", strconv.Itoa(params.Size))
	}
	if params.Status != "" {
		q.Set("status", params.Status)
	}

	var list DocumentList
	err := c.doGET(ctx, "ListDocuments", "/documents", q, &list)
	if err != nil {
		return nil, err
	}
	return &list, nil
}

func (c *Client) GetDocument(ctx context.Context, documentID string) (*DocumentWithCurrentVersion, error) {
	var doc DocumentWithCurrentVersion
	err := c.doGET(ctx, "GetDocument", "/documents/"+url.PathEscape(documentID), nil, &doc)
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

func (c *Client) DeleteDocument(ctx context.Context, documentID string) (*Document, error) {
	var doc Document
	err := c.doJSON(ctx, "DeleteDocument", http.MethodDelete, "/documents/"+url.PathEscape(documentID), nil, nil, &doc, c.timeoutWrite)
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

func (c *Client) ArchiveDocument(ctx context.Context, documentID string) (*Document, error) {
	var doc Document
	// POST with empty body — no request payload needed.
	err := c.doJSON(ctx, "ArchiveDocument", http.MethodPost, "/documents/"+url.PathEscape(documentID)+"/archive", nil, nil, &doc, c.timeoutWrite)
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

func (c *Client) CreateVersion(ctx context.Context, documentID string, req CreateVersionRequest) (*DocumentVersion, error) {
	var ver DocumentVersion
	err := c.doJSON(ctx, "CreateVersion", http.MethodPost, "/documents/"+url.PathEscape(documentID)+"/versions", nil, req, &ver, c.timeoutWrite)
	if err != nil {
		return nil, err
	}
	return &ver, nil
}

func (c *Client) ListVersions(ctx context.Context, documentID string, params ListVersionsParams) (*VersionList, error) {
	q := make(url.Values)
	if params.Page > 0 {
		q.Set("page", strconv.Itoa(params.Page))
	}
	if params.Size > 0 {
		q.Set("size", strconv.Itoa(params.Size))
	}

	var list VersionList
	err := c.doGET(ctx, "ListVersions", "/documents/"+url.PathEscape(documentID)+"/versions", q, &list)
	if err != nil {
		return nil, err
	}
	return &list, nil
}

func (c *Client) GetVersion(ctx context.Context, documentID, versionID string) (*DocumentVersionWithArtifacts, error) {
	var ver DocumentVersionWithArtifacts
	path := fmt.Sprintf("/documents/%s/versions/%s", url.PathEscape(documentID), url.PathEscape(versionID))
	err := c.doGET(ctx, "GetVersion", path, nil, &ver)
	if err != nil {
		return nil, err
	}
	return &ver, nil
}

func (c *Client) ListArtifacts(ctx context.Context, documentID, versionID string, params ListArtifactsParams) (*ArtifactDescriptorList, error) {
	q := make(url.Values)
	if params.ArtifactType != "" {
		q.Set("artifact_type", params.ArtifactType)
	}
	if params.ProducerDomain != "" {
		q.Set("producer_domain", params.ProducerDomain)
	}

	var list ArtifactDescriptorList
	path := fmt.Sprintf("/documents/%s/versions/%s/artifacts", url.PathEscape(documentID), url.PathEscape(versionID))
	err := c.doGET(ctx, "ListArtifacts", path, q, &list)
	if err != nil {
		return nil, err
	}
	return &list, nil
}

func (c *Client) GetArtifact(ctx context.Context, documentID, versionID, artifactType string) (*ArtifactResponse, error) {
	operation := "GetArtifact"
	path := fmt.Sprintf("/documents/%s/versions/%s/artifacts/%s", url.PathEscape(documentID), url.PathEscape(versionID), url.PathEscape(artifactType))
	fullURL := c.buildURL(path, nil)

	var result *ArtifactResponse
	err := c.executeWithRetry(ctx, operation, c.timeoutRead, func(attemptCtx context.Context) error {
		req, reqErr := http.NewRequestWithContext(attemptCtx, http.MethodGet, fullURL, nil)
		if reqErr != nil {
			return &DMError{Operation: operation, Retryable: false, Cause: reqErr}
		}
		c.setHeaders(ctx, req)

		resp, doErr := c.httpClient.Do(req)
		if doErr != nil {
			return mapTransportError(doErr, operation)
		}
		defer resp.Body.Close()

		switch {
		case resp.StatusCode == http.StatusOK:
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodySize))
			if readErr != nil {
				return &DMError{Operation: operation, Retryable: true, Cause: readErr}
			}
			result = &ArtifactResponse{Content: body}
			return nil

		case resp.StatusCode == http.StatusFound:
			// Drain body to reuse TCP connection; no need to read fully.
			io.Copy(io.Discard, resp.Body)
			location := resp.Header.Get("Location")
			if location == "" {
				return &DMError{
					Operation:  operation,
					StatusCode: resp.StatusCode,
					Retryable:  false,
					Cause:      fmt.Errorf("302 response missing Location header"),
				}
			}
			result = &ArtifactResponse{RedirectURL: location}
			return nil

		default:
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodySize))
			if readErr != nil {
				return &DMError{Operation: operation, Retryable: true, Cause: readErr}
			}
			return mapHTTPError(operation, resp.StatusCode, body)
		}
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) GetDiff(ctx context.Context, documentID, baseVersionID, targetVersionID string) (*VersionDiff, error) {
	var diff VersionDiff
	path := fmt.Sprintf("/documents/%s/diffs/%s/%s", url.PathEscape(documentID), url.PathEscape(baseVersionID), url.PathEscape(targetVersionID))
	err := c.doGET(ctx, "GetDiff", path, nil, &diff)
	if err != nil {
		return nil, err
	}
	return &diff, nil
}

func (c *Client) ListAuditRecords(ctx context.Context, params ListAuditParams) (*AuditRecordList, error) {
	q := make(url.Values)
	if params.DocumentID != "" {
		q.Set("document_id", params.DocumentID)
	}
	if params.VersionID != "" {
		q.Set("version_id", params.VersionID)
	}
	if params.Action != "" {
		q.Set("action", params.Action)
	}
	if params.ActorID != "" {
		q.Set("actor_id", params.ActorID)
	}
	if params.From != "" {
		q.Set("from", params.From)
	}
	if params.To != "" {
		q.Set("to", params.To)
	}
	if params.Page > 0 {
		q.Set("page", strconv.Itoa(params.Page))
	}
	if params.Size > 0 {
		q.Set("size", strconv.Itoa(params.Size))
	}

	var list AuditRecordList
	err := c.doGET(ctx, "ListAuditRecords", "/audit", q, &list)
	if err != nil {
		return nil, err
	}
	return &list, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// doGET is a convenience wrapper for read-only GET requests that return JSON.
func (c *Client) doGET(ctx context.Context, operation, path string, query url.Values, dest any) error {
	return c.doJSON(ctx, operation, http.MethodGet, path, query, nil, dest, c.timeoutRead)
}

// doJSON executes an HTTP request with JSON encoding for both request and
// response bodies. For GET and DELETE requests, reqBody may be nil.
func (c *Client) doJSON(
	ctx context.Context,
	operation string,
	method string,
	path string,
	query url.Values,
	reqBody any,
	dest any,
	timeout time.Duration,
) error {
	fullURL := c.buildURL(path, query)

	return c.executeWithRetry(ctx, operation, timeout, func(attemptCtx context.Context) error {
		var bodyReader io.Reader
		if reqBody != nil {
			encoded, encErr := json.Marshal(reqBody)
			if encErr != nil {
				return &DMError{Operation: operation, Retryable: false, Cause: encErr}
			}
			bodyReader = bytes.NewReader(encoded)
		}

		req, reqErr := http.NewRequestWithContext(attemptCtx, method, fullURL, bodyReader)
		if reqErr != nil {
			return &DMError{Operation: operation, Retryable: false, Cause: reqErr}
		}
		c.setHeaders(ctx, req)
		if reqBody != nil {
			req.Header.Set(headerContentType, contentTypeJSON)
		}

		resp, doErr := c.httpClient.Do(req)
		if doErr != nil {
			return mapTransportError(doErr, operation)
		}
		defer resp.Body.Close()

		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodySize))
		if readErr != nil {
			return &DMError{Operation: operation, Retryable: true, Cause: readErr}
		}

		if resp.StatusCode >= 400 {
			return mapHTTPError(operation, resp.StatusCode, body)
		}

		if dest != nil && len(body) > 0 {
			if decErr := json.Unmarshal(body, dest); decErr != nil {
				return &DMError{Operation: operation, Retryable: false, Cause: decErr}
			}
		}
		return nil
	})
}

// buildURL constructs a full URL from the base URL, path, and optional query params.
func (c *Client) buildURL(path string, query url.Values) string {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	return u
}

// setHeaders sets the required organizational, user, and correlation headers
// on the HTTP request. Values are extracted from the request context (set by
// the auth middleware and logger context middleware).
//
// Note: we always read from the ORIGINAL request context (ctx parameter),
// not from the per-attempt context, because the auth/logger context values
// are set on the original context by upstream middleware.
func (c *Client) setHeaders(ctx context.Context, req *http.Request) {
	ac, _ := auth.AuthContextFrom(ctx)
	rc := logger.RequestContextFrom(ctx)

	if ac.OrganizationID != "" {
		req.Header.Set(headerOrganizationID, ac.OrganizationID)
	}
	if ac.UserID != "" {
		req.Header.Set(headerUserID, ac.UserID)
	}
	if rc.CorrelationID != "" {
		req.Header.Set(headerCorrelationID, rc.CorrelationID)
	}
}

// executeWithRetry runs fn up to retryMax total attempts with exponential
// backoff. Note: retryMax means total attempts, not retry count (e.g.,
// retryMax=3 means 1 initial + 2 retries).
// Each attempt is guarded by the circuit breaker and a per-attempt timeout.
func (c *Client) executeWithRetry(
	ctx context.Context,
	operation string,
	timeout time.Duration,
	fn func(attemptCtx context.Context) error,
) error {
	var lastErr error
	for attempt := 0; attempt < c.retryMax; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if attempt > 0 {
			delay := c.backoffDelay(attempt)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		attemptCtx, cancel := context.WithTimeout(ctx, timeout)
		err := c.executeWithCB(attemptCtx, fn)
		cancel()

		if err == nil {
			return nil
		}
		lastErr = err

		if errors.Is(err, ErrCircuitOpen) {
			return err
		}
		if !isRetryable(err) {
			return err
		}

		if attempt < c.retryMax-1 {
			c.log.Warn(ctx, "retrying DM request",
				"operation", operation,
				"attempt", attempt+1,
				"max_attempts", c.retryMax,
				logger.ErrorAttr(err),
			)
		}
	}

	c.log.Error(ctx, "DM request failed after all retries",
		"operation", operation,
		"attempts", c.retryMax,
		logger.ErrorAttr(lastErr),
	)

	return lastErr
}

// executeWithCB wraps a function call with the gobreaker circuit breaker.
func (c *Client) executeWithCB(ctx context.Context, fn func(ctx context.Context) error) error {
	_, err := c.cb.Execute(func() (struct{}, error) {
		return struct{}{}, fn(ctx)
	})
	if err != nil {
		if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
			return fmt.Errorf("dmclient: %w", ErrCircuitOpen)
		}
		return err
	}
	return nil
}

// backoffDelay computes the delay before retry attempt n (1-indexed).
// Uses exponential backoff with 25% jitter:
//   - Attempt 1: retryBackoff + jitter   (e.g. 200ms + [0, 50ms])
//   - Attempt 2: retryBackoff*2 + jitter (e.g. 400ms + [0, 100ms])
//   - Attempt 3: retryBackoff*4 + jitter (e.g. 800ms + [0, 200ms])
func (c *Client) backoffDelay(attempt int) time.Duration {
	delay := c.retryBackoff * (1 << (attempt - 1))
	jitter := time.Duration(rand.Int64N(int64(float64(delay) * jitterFraction)))
	return delay + jitter
}
