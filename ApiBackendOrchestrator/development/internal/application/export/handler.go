// Package export implements the Export Service handler for
// GET /api/v1/contracts/{contract_id}/versions/{version_id}/export/{format}.
//
// The handler fetches the requested export artifact (EXPORT_PDF or EXPORT_DOCX)
// from the Document Management service and proxies the 302 redirect (presigned
// S3 URL) to the client. If the artifact is not yet ready, a 404 is returned.
//
// RBAC: LAWYER and ORG_ADMIN have full access. BUSINESS_USER is blocked by the
// RBAC middleware before reaching this handler; the handler performs a defense-
// in-depth check as well.
package export

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"contractpro/api-orchestrator/internal/domain/model"
	"contractpro/api-orchestrator/internal/domain/model/validation"
	"contractpro/api-orchestrator/internal/egress/dmclient"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"
)

// ---------------------------------------------------------------------------
// Consumer-side interface
// ---------------------------------------------------------------------------

// DMClient provides the DM operations needed by the export handler.
// Only the methods actually used are declared (interface segregation).
type DMClient interface {
	GetArtifact(ctx context.Context, documentID, versionID, artifactType string) (*dmclient.ArtifactResponse, error)
}

// Compile-time check that *dmclient.Client satisfies DMClient.
var _ DMClient = (*dmclient.Client)(nil)

// ---------------------------------------------------------------------------
// Format → artifact type mapping
// ---------------------------------------------------------------------------

// validFormats maps the user-facing format path parameter to the DM artifact type.
var validFormats = map[string]string{
	"pdf":  "EXPORT_PDF",
	"docx": "EXPORT_DOCX",
}

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

// Handler handles export requests by fetching artifacts from the DM service
// and proxying the presigned S3 URL redirect to the client.
type Handler struct {
	dm  DMClient
	log *logger.Logger
}

// NewHandler creates a new export handler.
func NewHandler(dm DMClient, log *logger.Logger) *Handler {
	return &Handler{
		dm:  dm,
		log: log.With("component", "export-handler"),
	}
}

// HandleExport returns a handler for the export endpoint.
//
// Route: GET /contracts/{contract_id}/versions/{version_id}/export/{format}
//
// The handler:
//  1. Defense-in-depth auth check (BUSINESS_USER blocked).
//  2. Validates contract_id and version_id as UUIDs.
//  3. Validates format as "pdf" or "docx" and maps to DM artifact type.
//  4. Calls DM GetArtifact to obtain the presigned S3 URL.
//  5. Validates redirect URL (scheme + host) and returns 302 Found.
//
// Error cases:
//   - Missing auth → 401 AUTH_TOKEN_MISSING
//   - BUSINESS_USER → 403 PERMISSION_DENIED
//   - Invalid UUID → 400 VALIDATION_ERROR
//   - Unsupported format → 400 VALIDATION_ERROR
//   - Artifact not ready → 404 ARTIFACT_NOT_FOUND
//   - DM unavailable → 502 DM_UNAVAILABLE
func (h *Handler) HandleExport() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// 0. Defense-in-depth: verify auth context and role.
		// RBAC middleware blocks BUSINESS_USER, but we double-check here.
		ac, ok := auth.AuthContextFrom(ctx)
		if !ok {
			model.WriteError(w, r, model.ErrAuthTokenMissing, nil)
			return
		}
		if ac.Role == auth.RoleBusinessUser {
			model.WriteError(w, r, model.ErrPermissionDenied, nil)
			return
		}

		// 1. Extract and validate path parameters.
		contractID, ok := h.extractUUIDParam(w, r, "contract_id")
		if !ok {
			return
		}
		versionID, ok := h.extractUUIDParam(w, r, "version_id")
		if !ok {
			return
		}

		// Enrich logging context with document/version IDs.
		ctx = logger.WithDocumentID(ctx, contractID)
		ctx = logger.WithVersionID(ctx, versionID)
		r = r.WithContext(ctx)

		// 2. Validate format.
		format := chi.URLParam(r, "format")
		artifactType, ok := validFormats[format]
		if !ok {
			vb := validation.NewBuilder()
			vb.Add(validation.NewInvalidEnum("format", []string{"pdf", "docx"}))
			model.WriteValidationError(w, r, vb.Build(), h.log)
			return
		}

		// 3. Fetch artifact from DM.
		h.log.Info(ctx, "exporting artifact",
			"format", format,
			"artifact_type", artifactType,
		)

		resp, err := h.dm.GetArtifact(ctx, contractID, versionID, artifactType)
		if err != nil {
			h.handleDMError(ctx, w, r, err, "GetArtifact")
			return
		}

		// 4. Handle response.
		if resp.RedirectURL != "" {
			// Validate redirect URL before proxying (prevent open redirect).
			if !isValidRedirectURL(resp.RedirectURL) {
				h.log.Error(ctx, "DM returned invalid redirect URL",
					"artifact_type", artifactType,
				)
				model.WriteError(w, r, model.ErrInternalError, nil)
				return
			}

			// DM returned 302 with presigned S3 URL — proxy to client.
			rc := logger.RequestContextFrom(ctx)
			w.Header().Set("X-Correlation-Id", rc.CorrelationID)
			http.Redirect(w, r, resp.RedirectURL, http.StatusFound)
			return
		}

		// Empty response: neither redirect nor content.
		if resp.Content == nil {
			h.log.Error(ctx, "DM returned empty artifact response",
				"artifact_type", artifactType,
			)
			model.WriteError(w, r, model.ErrInternalError, nil)
			return
		}

		// DM returned 200 with JSON content (unexpected for export artifacts,
		// but handle gracefully). This should not happen for EXPORT_PDF/DOCX.
		h.log.Warn(ctx, "DM returned content instead of redirect for export artifact",
			"artifact_type", artifactType,
		)
		rc := logger.RequestContextFrom(ctx)
		w.Header().Set("X-Correlation-Id", rc.CorrelationID)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(resp.Content)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// extractUUIDParam extracts and validates a path parameter as a UUID.
// Returns the ID and true if valid, or writes a 400 error and returns false.
func (h *Handler) extractUUIDParam(w http.ResponseWriter, r *http.Request, name string) (string, bool) {
	id := chi.URLParam(r, name)
	if id == "" || uuid.Validate(id) != nil {
		vb := validation.NewBuilder()
		vb.Add(validation.NewInvalidUUID(name))
		model.WriteValidationError(w, r, vb.Build(), h.log)
		return "", false
	}
	return id, true
}

// isValidRedirectURL validates a redirect URL from DM to prevent open redirect
// attacks. Only HTTP(S) URLs with a non-empty host are allowed. Schemes like
// javascript:, data:, or relative paths are rejected.
func isValidRedirectURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	// Only allow http/https schemes (http for local dev with MinIO).
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return false
	}
	// Must have a host (prevents relative-path or empty-host bypasses).
	return parsed.Host != ""
}

// handleDMError classifies a DM client error and writes the appropriate HTTP
// error response. Uses "artifact" as the resource hint for DM error mapping.
func (h *Handler) handleDMError(ctx context.Context, w http.ResponseWriter, r *http.Request, err error, operation string) {
	// Context cancellation/deadline — treat as transient service error.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		h.log.Warn(ctx, "request context done during DM call",
			"operation", operation,
			logger.ErrorAttr(err),
		)
		model.WriteError(w, r, model.ErrDMUnavailable, nil)
		return
	}

	if errors.Is(err, dmclient.ErrCircuitOpen) {
		h.log.Warn(ctx, "DM circuit breaker open",
			"operation", operation,
			logger.ErrorAttr(err),
		)
		model.WriteError(w, r, model.ErrDMUnavailable, nil)
		return
	}

	var dmErr *dmclient.DMError
	if errors.As(err, &dmErr) {
		if dmErr.StatusCode > 0 {
			code := model.MapDMError(dmErr.StatusCode, dmErr.Body, "artifact")
			model.WriteError(w, r, code, nil)
			return
		}
		// Transport error (no HTTP status).
		h.log.Error(ctx, "DM transport error",
			"operation", operation,
			logger.ErrorAttr(err),
		)
		model.WriteError(w, r, model.ErrDMUnavailable, nil)
		return
	}

	// Unknown error type.
	h.log.Error(ctx, "unexpected DM error",
		"operation", operation,
		logger.ErrorAttr(err),
	)
	model.WriteError(w, r, model.ErrInternalError, nil)
}
