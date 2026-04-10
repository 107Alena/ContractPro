// Package uomclient provides an HTTP client for the User & Organization
// Management (UOM) REST API with retry logic.
//
// The client follows the same resilience patterns as the dmclient, adapted
// for authentication use cases:
//   - 2 total attempts (1 initial + 1 retry) for fast-fail UX
//   - Fixed 200ms backoff (no exponential — auth should be fast)
//   - NO circuit breaker (fast fail prioritized; auth queries are rare)
//   - 5s per-attempt timeout
//
// When UOM is unavailable:
//   - login/refresh/logout return 502 to the client
//   - JWT validation continues to work locally (public key validation)
//   - UOM is NOT part of the readiness probe (optional dependency)
package uomclient

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"contractpro/api-orchestrator/internal/config"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"
)

const (
	// maxResponseBodySize caps response body reads to prevent OOM.
	maxResponseBodySize = 1 * 1024 * 1024 // 1 MB (auth responses are small)

	headerCorrelationID = "X-Correlation-ID"
	headerContentType   = "Content-Type"
	headerAuthorization = "Authorization"

	contentTypeJSON = "application/json"

	// Default retry settings for UOM: 2 total attempts, fixed 200ms backoff.
	defaultRetryMax     = 2
	defaultRetryBackoff = 200 * time.Millisecond
)

// UOMClient defines the interface for interacting with the UOM REST API.
// This interface enables testing and decouples callers from the concrete
// implementation.
type UOMClient interface {
	// Login authenticates a user with email and password.
	// Returns tokens and user profile on success.
	Login(ctx context.Context, req LoginRequest) (*LoginResponse, error)

	// Refresh exchanges a refresh token for a new token pair.
	Refresh(ctx context.Context, req RefreshRequest) (*RefreshResponse, error)

	// Logout invalidates a refresh token. Returns nil on success (204).
	Logout(ctx context.Context, req LogoutRequest) error

	// GetMe returns the profile of the currently authenticated user.
	// The caller must provide a context with a valid AuthContext (JWT token).
	GetMe(ctx context.Context) (*UserProfile, error)
}

// Client implements UOMClient with retry logic (no circuit breaker).
type Client struct {
	httpClient   *http.Client
	baseURL      string
	timeout      time.Duration
	retryMax     int
	retryBackoff time.Duration
	log          *logger.Logger
}

// Compile-time interface check.
var _ UOMClient = (*Client)(nil)

// NewClient creates a Client from configuration.
func NewClient(cfg config.UOMClientConfig, log *logger.Logger) *Client {
	return newClient(
		&http.Client{},
		cfg.BaseURL,
		cfg.Timeout,
		defaultRetryMax,
		defaultRetryBackoff,
		log,
	)
}

// newClient is the shared constructor used by NewClient (production) and tests.
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
		log:          log.With("component", "uom-client"),
	}
}

// ---------------------------------------------------------------------------
// Public API methods
// ---------------------------------------------------------------------------

func (c *Client) Login(ctx context.Context, req LoginRequest) (*LoginResponse, error) {
	var resp LoginResponse
	err := c.doJSON(ctx, "Login", http.MethodPost, "/api/v1/auth/login", req, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) Refresh(ctx context.Context, req RefreshRequest) (*RefreshResponse, error) {
	var resp RefreshResponse
	err := c.doJSON(ctx, "Refresh", http.MethodPost, "/api/v1/auth/refresh", req, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) Logout(ctx context.Context, req LogoutRequest) error {
	return c.doJSON(ctx, "Logout", http.MethodPost, "/api/v1/auth/logout", req, nil)
}

func (c *Client) GetMe(ctx context.Context) (*UserProfile, error) {
	var profile UserProfile
	err := c.doJSON(ctx, "GetMe", http.MethodGet, "/api/v1/users/me", nil, &profile)
	if err != nil {
		return nil, err
	}
	return &profile, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// doJSON executes an HTTP request with JSON encoding for both request and
// response bodies. For GET requests, reqBody may be nil.
// For 204 responses, dest is not populated.
func (c *Client) doJSON(
	ctx context.Context,
	operation string,
	method string,
	path string,
	reqBody any,
	dest any,
) error {
	fullURL := c.baseURL + path

	return c.executeWithRetry(ctx, operation, func(attemptCtx context.Context) error {
		var bodyReader io.Reader
		if reqBody != nil {
			encoded, encErr := json.Marshal(reqBody)
			if encErr != nil {
				return &UOMError{Operation: operation, Retryable: false, Cause: encErr}
			}
			bodyReader = bytes.NewReader(encoded)
		}

		req, reqErr := http.NewRequestWithContext(attemptCtx, method, fullURL, bodyReader)
		if reqErr != nil {
			return &UOMError{Operation: operation, Retryable: false, Cause: reqErr}
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

		// 204 No Content — success with no body (e.g. Logout).
		if resp.StatusCode == http.StatusNoContent {
			io.Copy(io.Discard, resp.Body)
			return nil
		}

		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodySize))
		if readErr != nil {
			return &UOMError{Operation: operation, Retryable: true, Cause: readErr}
		}

		if resp.StatusCode >= 400 {
			return mapHTTPError(operation, resp.StatusCode, body)
		}

		if dest != nil && len(body) > 0 {
			if decErr := json.Unmarshal(body, dest); decErr != nil {
				return &UOMError{Operation: operation, Retryable: false, Cause: decErr}
			}
		}
		return nil
	})
}

// setHeaders sets correlation and authorization headers on the HTTP request.
// For auth endpoints (login/refresh/logout) there is no JWT in context —
// that's fine, the authorization header is simply omitted.
// For GetMe, the JWT is extracted from AuthContext.
func (c *Client) setHeaders(ctx context.Context, req *http.Request) {
	rc := logger.RequestContextFrom(ctx)
	if rc.CorrelationID != "" {
		req.Header.Set(headerCorrelationID, rc.CorrelationID)
	}

	// Forward the authorization header for endpoints that need it (GetMe).
	// Auth endpoints (login/refresh/logout) are called without JWT context.
	ac, ok := auth.AuthContextFrom(ctx)
	if ok && ac.UserID != "" {
		// We don't store the raw token in AuthContext (by design — security).
		// Instead, GetMe callers pass the token via a dedicated header upstream,
		// and the orchestrator's handler forwards it. For UOM client, we set
		// X-User-ID so UOM can identify the user.
		req.Header.Set("X-User-ID", ac.UserID)
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
			c.log.Warn(ctx, "retrying UOM request",
				"operation", operation,
				"attempt", attempt+1,
				"max_attempts", c.retryMax,
				logger.ErrorAttr(lastErr),
			)
		}
	}

	c.log.Error(ctx, "UOM request failed after all retries",
		"operation", operation,
		"attempts", c.retryMax,
		logger.ErrorAttr(lastErr),
	)

	return lastErr
}
