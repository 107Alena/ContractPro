// Package opmclient provides an HTTP client for the Organization Policy
// Management (OPM) REST API with retry logic.
//
// The client follows the same resilience patterns as the uomclient, adapted
// for admin policy/checklist management:
//   - 2 total attempts (1 initial + 1 retry)
//   - Fixed 200ms backoff
//   - NO circuit breaker (admin calls are infrequent)
//   - 5s per-attempt timeout
//
// OPM is an optional dependency. When ORCH_OPM_BASE_URL is empty, NewOPMClient
// returns a DisabledClient that rejects all requests with ErrOPMDisabled.
//
// All methods use json.RawMessage for request and response bodies because OPM
// is not yet designed (ASSUMPTION-ORCH-04). The orchestrator acts as a
// transparent proxy for admin endpoints.
package opmclient

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"

	"contractpro/api-orchestrator/internal/config"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"
)

const (
	// maxResponseBodySize caps response body reads to prevent OOM.
	maxResponseBodySize = 1 * 1024 * 1024 // 1 MB

	headerOrganizationID = "X-Organization-ID"
	headerUserID         = "X-User-ID"
	headerCorrelationID  = "X-Correlation-Id"
	headerContentType    = "Content-Type"

	contentTypeJSON = "application/json"

	// Default retry settings: 2 total attempts, fixed 200ms backoff.
	defaultRetryMax     = 2
	defaultRetryBackoff = 200 * time.Millisecond
)

// OPMClient defines the interface for interacting with the OPM REST API.
// All methods use json.RawMessage because OPM is not yet designed
// (ASSUMPTION-ORCH-04) and the orchestrator acts as a transparent proxy.
type OPMClient interface {
	// ListPolicies returns policies for the organization.
	// GET /api/v1/policies?organization_id={orgID}
	ListPolicies(ctx context.Context, orgID string) (json.RawMessage, error)

	// UpdatePolicy updates a policy by ID.
	// PUT /api/v1/policies/{policyID}
	UpdatePolicy(ctx context.Context, policyID string, body json.RawMessage) (json.RawMessage, error)

	// ListChecklists returns checklists for the organization.
	// GET /api/v1/checklists?organization_id={orgID}
	ListChecklists(ctx context.Context, orgID string) (json.RawMessage, error)

	// UpdateChecklist updates a checklist by ID.
	// PUT /api/v1/checklists/{checklistID}
	UpdateChecklist(ctx context.Context, checklistID string, body json.RawMessage) (json.RawMessage, error)
}

// Client implements OPMClient with retry logic (no circuit breaker).
type Client struct {
	httpClient   *http.Client
	baseURL      string
	timeout      time.Duration
	retryMax     int
	retryBackoff time.Duration
	log          *logger.Logger
}

// Compile-time interface checks.
var _ OPMClient = (*Client)(nil)
var _ OPMClient = (*DisabledClient)(nil)

// DisabledClient is returned when ORCH_OPM_BASE_URL is empty.
// Every method returns ErrOPMDisabled, which the handler maps to
// OPM_UNAVAILABLE (502).
type DisabledClient struct {
	log *logger.Logger
}

// NewOPMClient creates the appropriate OPMClient implementation.
// When cfg.BaseURL is empty, returns a DisabledClient.
func NewOPMClient(cfg config.OPMClientConfig, log *logger.Logger) OPMClient {
	if cfg.BaseURL == "" {
		return &DisabledClient{log: log.With("component", "opm-client-disabled")}
	}
	return newClient(
		&http.Client{},
		cfg.BaseURL,
		cfg.Timeout,
		defaultRetryMax,
		defaultRetryBackoff,
		log,
	)
}

// newClient is the shared constructor used by NewOPMClient (production) and tests.
func newClient(
	httpClient *http.Client,
	baseURL string,
	timeout time.Duration,
	retryMax int,
	retryBackoff time.Duration,
	log *logger.Logger,
) *Client {
	return &Client{
		httpClient:   httpClient,
		baseURL:      baseURL,
		timeout:      timeout,
		retryMax:     retryMax,
		retryBackoff: retryBackoff,
		log:          log.With("component", "opm-client"),
	}
}

// ---------------------------------------------------------------------------
// Client public API
// ---------------------------------------------------------------------------

func (c *Client) ListPolicies(ctx context.Context, orgID string) (json.RawMessage, error) {
	q := url.Values{}
	q.Set("organization_id", orgID)
	return c.doRawGET(ctx, "ListPolicies", "/api/v1/policies", q)
}

func (c *Client) UpdatePolicy(ctx context.Context, policyID string, body json.RawMessage) (json.RawMessage, error) {
	path := "/api/v1/policies/" + url.PathEscape(policyID)
	return c.doRawPUT(ctx, "UpdatePolicy", path, body)
}

func (c *Client) ListChecklists(ctx context.Context, orgID string) (json.RawMessage, error) {
	q := url.Values{}
	q.Set("organization_id", orgID)
	return c.doRawGET(ctx, "ListChecklists", "/api/v1/checklists", q)
}

func (c *Client) UpdateChecklist(ctx context.Context, checklistID string, body json.RawMessage) (json.RawMessage, error) {
	path := "/api/v1/checklists/" + url.PathEscape(checklistID)
	return c.doRawPUT(ctx, "UpdateChecklist", path, body)
}

// ---------------------------------------------------------------------------
// DisabledClient public API — all methods return ErrOPMDisabled.
// ---------------------------------------------------------------------------

func (d *DisabledClient) ListPolicies(ctx context.Context, _ string) (json.RawMessage, error) {
	d.log.Warn(ctx, "OPM is disabled, rejecting ListPolicies request")
	return nil, &OPMError{Operation: "ListPolicies", Retryable: false, Cause: ErrOPMDisabled}
}

func (d *DisabledClient) UpdatePolicy(ctx context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
	d.log.Warn(ctx, "OPM is disabled, rejecting UpdatePolicy request")
	return nil, &OPMError{Operation: "UpdatePolicy", Retryable: false, Cause: ErrOPMDisabled}
}

func (d *DisabledClient) ListChecklists(ctx context.Context, _ string) (json.RawMessage, error) {
	d.log.Warn(ctx, "OPM is disabled, rejecting ListChecklists request")
	return nil, &OPMError{Operation: "ListChecklists", Retryable: false, Cause: ErrOPMDisabled}
}

func (d *DisabledClient) UpdateChecklist(ctx context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
	d.log.Warn(ctx, "OPM is disabled, rejecting UpdateChecklist request")
	return nil, &OPMError{Operation: "UpdateChecklist", Retryable: false, Cause: ErrOPMDisabled}
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// doRawGET executes a GET request and returns the raw response body.
func (c *Client) doRawGET(ctx context.Context, operation, path string, query url.Values) (json.RawMessage, error) {
	return c.doRaw(ctx, operation, http.MethodGet, path, query, nil)
}

// doRawPUT executes a PUT request with a raw JSON body and returns the response.
func (c *Client) doRawPUT(ctx context.Context, operation, path string, body json.RawMessage) (json.RawMessage, error) {
	return c.doRaw(ctx, operation, http.MethodPut, path, nil, body)
}

// doRaw is the core request executor. It returns the raw response body as
// json.RawMessage because the orchestrator proxies OPM responses without
// type-mapping (ASSUMPTION-ORCH-04).
func (c *Client) doRaw(
	ctx context.Context,
	operation string,
	method string,
	path string,
	query url.Values,
	reqBody json.RawMessage,
) (json.RawMessage, error) {
	fullURL := c.buildURL(path, query)

	var result json.RawMessage
	err := c.executeWithRetry(ctx, operation, func(attemptCtx context.Context) error {
		var bodyReader io.Reader
		if reqBody != nil {
			bodyReader = bytes.NewReader(reqBody)
		}

		req, reqErr := http.NewRequestWithContext(attemptCtx, method, fullURL, bodyReader)
		if reqErr != nil {
			return &OPMError{Operation: operation, Retryable: false, Cause: reqErr}
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
			return &OPMError{Operation: operation, Retryable: true, Cause: readErr}
		}

		if resp.StatusCode >= 400 {
			return mapHTTPError(operation, resp.StatusCode, body)
		}

		if len(body) > 0 {
			result = json.RawMessage(body)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// setHeaders sets organization, user, and correlation headers on the request.
// OPM endpoints need X-Organization-ID for tenant isolation and
// X-Correlation-Id for distributed tracing.
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

// executeWithRetry runs fn up to retryMax total attempts with fixed backoff.
// Note: retryMax means total attempts, not retry count (e.g.,
// retryMax=2 means 1 initial + 1 retry).
func (c *Client) executeWithRetry(
	ctx context.Context,
	operation string,
	fn func(attemptCtx context.Context) error,
) error {
	var lastErr error
	for attempt := 0; attempt < c.retryMax; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(c.retryBackoff):
			}
		}

		attemptCtx, cancel := context.WithTimeout(ctx, c.timeout)
		err := fn(attemptCtx)
		cancel()

		if err == nil {
			return nil
		}
		lastErr = err

		if !isRetryable(err) {
			return err
		}

		if attempt < c.retryMax-1 {
			c.log.Warn(ctx, "retrying OPM request",
				"operation", operation,
				"attempt", attempt+1,
				"max_attempts", c.retryMax,
				logger.ErrorAttr(lastErr),
			)
		}
	}

	c.log.Error(ctx, "OPM request failed after all retries",
		"operation", operation,
		"attempts", c.retryMax,
		logger.ErrorAttr(lastErr),
	)

	return lastErr
}

// buildURL constructs a full URL from path and optional query parameters.
func (c *Client) buildURL(path string, query url.Values) string {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	return u
}
