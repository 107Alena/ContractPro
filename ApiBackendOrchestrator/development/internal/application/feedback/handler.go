// Package feedback implements the Feedback Service handler for
// POST /api/v1/contracts/{contract_id}/versions/{version_id}/feedback.
//
// The handler accepts user feedback (is_useful boolean + optional comment),
// validates that the version exists in DM, stores the feedback in Redis
// with a 30-day TTL (ASSUMPTION-ORCH-08), and returns 201 Created.
//
// RBAC: all roles (LAWYER, BUSINESS_USER, ORG_ADMIN) can submit feedback.
package feedback

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"contractpro/api-orchestrator/internal/domain/model"
	"contractpro/api-orchestrator/internal/domain/model/validation"
	"contractpro/api-orchestrator/internal/egress/dmclient"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"
)

// ---------------------------------------------------------------------------
// Consumer-side interfaces (dependency inversion)
// ---------------------------------------------------------------------------

// DMClient provides the DM operations needed by the feedback handler.
type DMClient interface {
	GetVersion(ctx context.Context, documentID, versionID string) (*dmclient.DocumentVersionWithArtifacts, error)
}

// KVStore provides ephemeral key-value storage for feedback persistence
// (ASSUMPTION-ORCH-08: Redis with 30-day TTL until DM supports USER_FEEDBACK).
type KVStore interface {
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
}

// Compile-time interface checks.
var _ DMClient = (*dmclient.Client)(nil)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// maxCommentLength is the maximum allowed length for the feedback comment
	// in Unicode code points (not bytes). From security.md input validation.
	maxCommentLength = 2000

	// feedbackTTL is the Redis TTL for stored feedback.
	// ASSUMPTION-ORCH-08: 30 days until DM supports USER_FEEDBACK artifact.
	feedbackTTL = 30 * 24 * time.Hour

	// maxBodySize limits the request body to prevent DoS (1 MB).
	maxBodySize = 1 << 20
)

// ---------------------------------------------------------------------------
// Request / Response DTOs
// ---------------------------------------------------------------------------

// FeedbackRequest is the JSON body for POST /feedback.
type FeedbackRequest struct {
	IsUseful *bool  `json:"is_useful"`
	Comment  string `json:"comment"`
}

// validate checks that the request is well-formed.
func (fr *FeedbackRequest) validate() *validation.ValidationError {
	vb := validation.NewBuilder()
	if fr.IsUseful == nil {
		vb.Add(validation.NewRequired("is_useful"))
	}
	comment := strings.TrimSpace(fr.Comment)
	if utf8.RuneCountInString(comment) > maxCommentLength {
		vb.Add(validation.NewTooLong("comment", maxCommentLength))
	}
	return vb.Build()
}

// FeedbackResponse is the 201 body.
type FeedbackResponse struct {
	FeedbackID string `json:"feedback_id"`
	CreatedAt  string `json:"created_at"`
}

// feedbackRecord is the JSON structure stored in Redis.
type feedbackRecord struct {
	FeedbackID     string `json:"feedback_id"`
	ContractID     string `json:"contract_id"`
	VersionID      string `json:"version_id"`
	OrganizationID string `json:"organization_id"`
	UserID         string `json:"user_id"`
	IsUseful       bool   `json:"is_useful"`
	Comment        string `json:"comment"`
	CreatedAt      string `json:"created_at"`
}

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

// Handler handles feedback submission requests.
type Handler struct {
	dm  DMClient
	kv  KVStore
	log *logger.Logger
}

// NewHandler creates a new feedback handler.
func NewHandler(dm DMClient, kv KVStore, log *logger.Logger) *Handler {
	return &Handler{
		dm:  dm,
		kv:  kv,
		log: log.With("component", "feedback-handler"),
	}
}

// HandleSubmit returns a handler for the feedback submission endpoint.
//
// Route: POST /contracts/{contract_id}/versions/{version_id}/feedback
//
// Flow:
//  1. Auth context extraction.
//  2. Validate contract_id and version_id as UUIDs.
//  3. Parse and validate JSON body (is_useful required, comment optional ≤2000).
//  4. DM GetVersion — validate version exists.
//  5. Generate feedback_id, store in Redis (TTL 30 days).
//  6. Return 201 Created with feedback_id and created_at.
func (h *Handler) HandleSubmit() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Step 1: Auth context.
		ac, ok := auth.AuthContextFrom(ctx)
		if !ok {
			model.WriteError(w, r, model.ErrAuthTokenMissing, nil)
			return
		}

		// Step 2: Extract and validate path parameters.
		contractID, ok := h.extractUUIDParam(w, r, "contract_id")
		if !ok {
			return
		}
		versionID, ok := h.extractUUIDParam(w, r, "version_id")
		if !ok {
			return
		}

		// Enrich logging context.
		ctx = logger.WithDocumentID(ctx, contractID)
		ctx = logger.WithVersionID(ctx, versionID)
		r = r.WithContext(ctx)

		// Step 3: Parse and validate JSON body.
		r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
		var req FeedbackRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			verr := validation.NewBuilder().Add(validation.NewInvalidFormat("body", "JSON")).Build()
			model.WriteValidationError(w, r, verr, h.log)
			return
		}

		// Trim comment whitespace before validation.
		req.Comment = strings.TrimSpace(req.Comment)

		if verr := req.validate(); verr != nil {
			model.WriteValidationError(w, r, verr, h.log)
			return
		}

		// Step 4: Validate version exists in DM.
		_, err := h.dm.GetVersion(ctx, contractID, versionID)
		if err != nil {
			h.handleDMError(ctx, w, r, err, "GetVersion")
			return
		}

		// Step 5: Generate feedback_id and store in Redis.
		feedbackID := uuid.New().String()
		now := time.Now().UTC()

		record := feedbackRecord{
			FeedbackID:     feedbackID,
			ContractID:     contractID,
			VersionID:      versionID,
			OrganizationID: ac.OrganizationID,
			UserID:         ac.UserID,
			IsUseful:       *req.IsUseful,
			Comment:        req.Comment,
			CreatedAt:      now.Format(time.RFC3339),
		}

		recordJSON, err := json.Marshal(record)
		if err != nil {
			h.log.Error(ctx, "failed to marshal feedback record",
				logger.ErrorAttr(err),
			)
			model.WriteError(w, r, model.ErrInternalError, nil)
			return
		}

		key := feedbackKey(ac.OrganizationID, versionID, feedbackID)
		if err := h.kv.Set(ctx, key, string(recordJSON), feedbackTTL); err != nil {
			// Redis failure is non-critical — log and still return success
			// to the user. The feedback data is lost but the user experience
			// should not be degraded. This aligns with the fallback nature
			// of Redis storage per ASSUMPTION-ORCH-08.
			h.log.Warn(ctx, "failed to store feedback in Redis (non-critical)",
				"feedback_id", feedbackID,
				logger.ErrorAttr(err),
			)
		}

		h.log.Info(ctx, "feedback submitted",
			"feedback_id", feedbackID,
			"is_useful", *req.IsUseful,
		)

		// Step 6: Return 201 Created.
		resp := FeedbackResponse{
			FeedbackID: feedbackID,
			CreatedAt:  now.Format(time.RFC3339),
		}
		h.writeJSON(ctx, w, http.StatusCreated, resp)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// feedbackKey returns the Redis key for storing feedback.
func feedbackKey(orgID, versionID, feedbackID string) string {
	return "feedback:" + orgID + ":" + versionID + ":" + feedbackID
}

// extractUUIDParam extracts and validates a path parameter as a UUID.
func (h *Handler) extractUUIDParam(w http.ResponseWriter, r *http.Request, name string) (string, bool) {
	id := chi.URLParam(r, name)
	if id == "" || uuid.Validate(id) != nil {
		verr := validation.NewBuilder().Add(validation.NewInvalidUUID(name)).Build()
		model.WriteValidationError(w, r, verr, h.log)
		return "", false
	}
	return id, true
}

// handleDMError classifies a DM client error and writes the appropriate HTTP
// error response. Uses "version" as the resource hint for DM error mapping.
func (h *Handler) handleDMError(ctx context.Context, w http.ResponseWriter, r *http.Request, err error, operation string) {
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
			code := model.MapDMError(dmErr.StatusCode, dmErr.Body, "version")
			model.WriteError(w, r, code, nil)
			return
		}
		h.log.Error(ctx, "DM transport error",
			"operation", operation,
			logger.ErrorAttr(err),
		)
		model.WriteError(w, r, model.ErrDMUnavailable, nil)
		return
	}

	h.log.Error(ctx, "unexpected error from DM client",
		"operation", operation,
		logger.ErrorAttr(err),
	)
	model.WriteError(w, r, model.ErrInternalError, nil)
}

// writeJSON encodes v as JSON and writes it with the given status code.
func (h *Handler) writeJSON(ctx context.Context, w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		h.log.Error(ctx, "failed to encode JSON response",
			logger.ErrorAttr(err),
		)
	}
}
