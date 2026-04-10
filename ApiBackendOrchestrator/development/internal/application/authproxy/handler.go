// Package authproxy implements the auth proxy handlers that forward
// authentication requests to the User & Organization Management (UOM) service.
//
// Public endpoints (no JWT required):
//   - POST /api/v1/auth/login   — email+password → token pair + user profile
//   - POST /api/v1/auth/refresh — refresh_token → new token pair
//   - POST /api/v1/auth/logout  — refresh_token → 204 (invalidate token)
//
// Protected endpoint (JWT required):
//   - GET /api/v1/users/me      — current user profile
//
// The handlers proxy requests to UOM and map UOM errors to orchestrator
// ErrorCodes via model.MapUOMError. Sensitive data (passwords, tokens) is
// never logged.
package authproxy

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"contractpro/api-orchestrator/internal/domain/model"
	"contractpro/api-orchestrator/internal/egress/uomclient"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Consumer-side interface
// ---------------------------------------------------------------------------

// UOMClient provides the UOM operations needed by the auth proxy handlers.
type UOMClient interface {
	Login(ctx context.Context, req uomclient.LoginRequest) (*uomclient.LoginResponse, error)
	Refresh(ctx context.Context, req uomclient.RefreshRequest) (*uomclient.RefreshResponse, error)
	Logout(ctx context.Context, req uomclient.LogoutRequest) error
	GetMe(ctx context.Context) (*uomclient.UserProfile, error)
}

// Compile-time check that *uomclient.Client satisfies UOMClient.
var _ UOMClient = (*uomclient.Client)(nil)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// maxAuthBodySize caps request body reads for auth endpoints to prevent
	// abuse. Auth payloads (email+password, refresh_token) are small.
	maxAuthBodySize = 4 * 1024 // 4 KB
)

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

// Handler handles auth proxy requests by forwarding them to the UOM service.
type Handler struct {
	uom UOMClient
	log *logger.Logger
}

// NewHandler creates a new auth proxy handler.
func NewHandler(uom UOMClient, log *logger.Logger) *Handler {
	return &Handler{
		uom: uom,
		log: log.With("component", "auth-handler"),
	}
}

// ---------------------------------------------------------------------------
// HandleLogin — POST /api/v1/auth/login
// ---------------------------------------------------------------------------

// loginRequest is the request body for the login endpoint.
type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// HandleLogin returns a handler that authenticates a user via UOM and returns
// a JWT token pair (access_token + refresh_token) along with the user profile.
func (h *Handler) HandleLogin() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := h.ensureCorrelationID(r)
		r = r.WithContext(ctx)

		var req loginRequest
		if !h.decodeBody(w, r, &req) {
			return
		}

		email := strings.TrimSpace(req.Email)
		if email == "" {
			model.WriteError(w, r, model.ErrValidationError,
				"Поле «email» обязательно для заполнения.")
			return
		}
		if strings.TrimSpace(req.Password) == "" {
			model.WriteError(w, r, model.ErrValidationError,
				"Поле «password» обязательно для заполнения.")
			return
		}

		h.log.Info(ctx, "login attempt", "email", email)

		resp, err := h.uom.Login(ctx, uomclient.LoginRequest{
			Email:    email,
			Password: req.Password,
		})
		if err != nil {
			h.handleUOMError(ctx, w, r, err, "Login")
			return
		}

		h.log.Info(ctx, "login successful", "email", email)
		h.writeJSON(ctx, w, http.StatusOK, resp)
	}
}

// ---------------------------------------------------------------------------
// HandleRefresh — POST /api/v1/auth/refresh
// ---------------------------------------------------------------------------

// refreshRequest is the request body for the refresh endpoint.
type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// HandleRefresh returns a handler that exchanges a refresh token for a new
// token pair via UOM.
func (h *Handler) HandleRefresh() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := h.ensureCorrelationID(r)
		r = r.WithContext(ctx)

		var req refreshRequest
		if !h.decodeBody(w, r, &req) {
			return
		}

		if strings.TrimSpace(req.RefreshToken) == "" {
			model.WriteError(w, r, model.ErrValidationError,
				"Поле «refresh_token» обязательно для заполнения.")
			return
		}

		h.log.Info(ctx, "token refresh attempt")

		resp, err := h.uom.Refresh(ctx, uomclient.RefreshRequest{
			RefreshToken: req.RefreshToken,
		})
		if err != nil {
			h.handleUOMError(ctx, w, r, err, "Refresh")
			return
		}

		h.log.Info(ctx, "token refresh successful")
		h.writeJSON(ctx, w, http.StatusOK, resp)
	}
}

// ---------------------------------------------------------------------------
// HandleLogout — POST /api/v1/auth/logout
// ---------------------------------------------------------------------------

// logoutRequest is the request body for the logout endpoint.
type logoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// HandleLogout returns a handler that invalidates a refresh token via UOM.
func (h *Handler) HandleLogout() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := h.ensureCorrelationID(r)
		r = r.WithContext(ctx)

		var req logoutRequest
		if !h.decodeBody(w, r, &req) {
			return
		}

		if strings.TrimSpace(req.RefreshToken) == "" {
			model.WriteError(w, r, model.ErrValidationError,
				"Поле «refresh_token» обязательно для заполнения.")
			return
		}

		h.log.Info(ctx, "logout attempt")

		err := h.uom.Logout(ctx, uomclient.LogoutRequest{
			RefreshToken: req.RefreshToken,
		})
		if err != nil {
			h.handleUOMError(ctx, w, r, err, "Logout")
			return
		}

		h.log.Info(ctx, "logout successful")
		w.WriteHeader(http.StatusNoContent)
	}
}

// ---------------------------------------------------------------------------
// HandleGetMe — GET /api/v1/users/me
// ---------------------------------------------------------------------------

// HandleGetMe returns a handler that retrieves the current user's profile
// from UOM. This endpoint requires JWT authentication (AuthContext must be
// present in the request context).
//
// Note: ensureCorrelationID is not called here because this endpoint sits
// behind the auth middleware, which always generates or preserves the
// correlation ID in the request context.
func (h *Handler) HandleGetMe() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		if _, ok := auth.AuthContextFrom(ctx); !ok {
			model.WriteError(w, r, model.ErrAuthTokenMissing, nil)
			return
		}

		h.log.Info(ctx, "fetching user profile")

		profile, err := h.uom.GetMe(ctx)
		if err != nil {
			h.handleUOMError(ctx, w, r, err, "GetMe")
			return
		}

		h.writeJSON(ctx, w, http.StatusOK, profile)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// ensureCorrelationID ensures that public auth endpoints (which bypass the
// auth middleware) have a correlation ID in the request context. If one is
// already present (e.g. from X-Correlation-Id header), it is preserved.
func (h *Handler) ensureCorrelationID(r *http.Request) context.Context {
	ctx := r.Context()
	rc := logger.RequestContextFrom(ctx)
	if rc.CorrelationID != "" {
		return ctx
	}
	rc.CorrelationID = uuid.New().String()
	return logger.WithRequestContext(ctx, rc)
}

// decodeBody reads and decodes the JSON request body into dest.
// It applies MaxBytesReader to prevent abuse and returns false (with an
// error already written to the response) if decoding fails.
func (h *Handler) decodeBody(w http.ResponseWriter, r *http.Request, dest any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodySize)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dest); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			model.WriteError(w, r, model.ErrValidationError,
				"Тело запроса превышает допустимый размер.")
			return false
		}
		model.WriteError(w, r, model.ErrValidationError,
			"Некорректный формат JSON в теле запроса.")
		return false
	}
	return true
}

// handleUOMError classifies a UOM client error and writes the appropriate
// HTTP error response. Sensitive data (passwords, tokens) is never logged.
//
// Classification:
//  1. context.Canceled/DeadlineExceeded → 502 AUTH_SERVICE_UNAVAILABLE
//  2. UOMError with StatusCode > 0 → MapUOMError → WriteError
//  3. UOMError with StatusCode == 0 → transport error → 502 AUTH_SERVICE_UNAVAILABLE
//  4. Unknown error → 500 INTERNAL_ERROR
func (h *Handler) handleUOMError(ctx context.Context, w http.ResponseWriter, r *http.Request, err error, operation string) {
	if errors.Is(err, context.Canceled) {
		h.log.Warn(ctx, "UOM request cancelled",
			"operation", operation,
		)
		model.WriteError(w, r, model.ErrAuthServiceUnavailable, nil)
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		h.log.Warn(ctx, "UOM request timed out",
			"operation", operation,
		)
		model.WriteError(w, r, model.ErrAuthServiceUnavailable, nil)
		return
	}

	var uomErr *uomclient.UOMError
	if errors.As(err, &uomErr) {
		if uomErr.StatusCode > 0 {
			code := model.MapUOMError(uomErr.StatusCode, uomErr.Body)
			h.log.Warn(ctx, "UOM returned error",
				"operation", operation,
				"status_code", uomErr.StatusCode,
				"error_code", string(code),
			)
			model.WriteError(w, r, code, nil)
			return
		}
		// Transport error (no HTTP status).
		h.log.Error(ctx, "UOM transport error",
			"operation", operation,
			logger.ErrorAttr(err),
		)
		model.WriteError(w, r, model.ErrAuthServiceUnavailable, nil)
		return
	}

	h.log.Error(ctx, "unexpected error from UOM client",
		"operation", operation,
		logger.ErrorAttr(err),
	)
	model.WriteError(w, r, model.ErrInternalError, nil)
}

// writeJSON writes a JSON response with the given status code.
func (h *Handler) writeJSON(ctx context.Context, w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		h.log.Error(ctx, "failed to encode JSON response",
			logger.ErrorAttr(err),
		)
	}
}
