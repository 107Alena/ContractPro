// Package adminproxy implements the admin proxy handlers that forward
// policy and checklist management requests to the Organization Policy
// Management (OPM) service.
//
// All endpoints require ORG_ADMIN role (enforced by RBAC middleware).
// The handler performs defense-in-depth auth checks.
//
// Endpoints:
//   - GET  /api/v1/admin/policies              — list policies
//   - PUT  /api/v1/admin/policies/{policy_id}   — update policy
//   - GET  /api/v1/admin/checklists             — list checklists
//   - PUT  /api/v1/admin/checklists/{checklist_id} — update checklist
//
// OPM responses are proxied as json.RawMessage — the orchestrator does not
// interpret policy/checklist payloads (ASSUMPTION-ORCH-04).
package adminproxy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"contractpro/api-orchestrator/internal/domain/model"
	"contractpro/api-orchestrator/internal/domain/model/validation"
	"contractpro/api-orchestrator/internal/egress/opmclient"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"

	"github.com/go-chi/chi/v5"
)

// ---------------------------------------------------------------------------
// Consumer-side interface
// ---------------------------------------------------------------------------

// OPMClient provides the OPM operations needed by the admin proxy handlers.
type OPMClient interface {
	ListPolicies(ctx context.Context, orgID string) (json.RawMessage, error)
	UpdatePolicy(ctx context.Context, policyID string, body json.RawMessage) (json.RawMessage, error)
	ListChecklists(ctx context.Context, orgID string) (json.RawMessage, error)
	UpdateChecklist(ctx context.Context, checklistID string, body json.RawMessage) (json.RawMessage, error)
}

// Compile-time check that *opmclient.Client satisfies OPMClient.
var _ OPMClient = (*opmclient.Client)(nil)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// maxAdminBodySize caps request body reads for PUT endpoints to prevent
	// abuse. Policy/checklist payloads should be well under 1 MB.
	maxAdminBodySize = 1 * 1024 * 1024 // 1 MB
)

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

// Handler handles admin proxy requests by forwarding them to the OPM service.
type Handler struct {
	opm OPMClient
	log *logger.Logger
}

// NewHandler creates a new admin proxy handler.
func NewHandler(opm OPMClient, log *logger.Logger) *Handler {
	return &Handler{
		opm: opm,
		log: log.With("component", "admin-handler"),
	}
}

// ---------------------------------------------------------------------------
// HandleListPolicies — GET /api/v1/admin/policies
// ---------------------------------------------------------------------------

// HandleListPolicies returns a handler that lists organization policies via OPM.
func (h *Handler) HandleListPolicies() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		ac, ok := auth.AuthContextFrom(ctx)
		if !ok {
			model.WriteError(w, r, model.ErrAuthTokenMissing, nil)
			return
		}

		h.log.Info(ctx, "listing policies", "organization_id", ac.OrganizationID)

		data, err := h.opm.ListPolicies(ctx, ac.OrganizationID)
		if err != nil {
			h.handleOPMError(ctx, w, r, err, "ListPolicies", "policy")
			return
		}

		h.writeRawJSON(ctx, w, http.StatusOK, data)
	}
}

// ---------------------------------------------------------------------------
// HandleUpdatePolicy — PUT /api/v1/admin/policies/{policy_id}
// ---------------------------------------------------------------------------

// HandleUpdatePolicy returns a handler that updates a policy via OPM.
func (h *Handler) HandleUpdatePolicy() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		ac, ok := auth.AuthContextFrom(ctx)
		if !ok {
			model.WriteError(w, r, model.ErrAuthTokenMissing, nil)
			return
		}

		policyID := chi.URLParam(r, "policy_id")
		if policyID == "" {
			vb := validation.NewBuilder()
			vb.Add(validation.NewRequired("policy_id"))
			model.WriteValidationError(w, r, vb.Build(), h.log)
			return
		}

		body, ok := h.readBody(w, r)
		if !ok {
			return
		}

		h.log.Info(ctx, "updating policy",
			"policy_id", policyID,
			"organization_id", ac.OrganizationID,
			"user_id", ac.UserID,
		)

		data, err := h.opm.UpdatePolicy(ctx, policyID, body)
		if err != nil {
			h.handleOPMError(ctx, w, r, err, "UpdatePolicy", "policy")
			return
		}

		h.writeRawJSON(ctx, w, http.StatusOK, data)
	}
}

// ---------------------------------------------------------------------------
// HandleListChecklists — GET /api/v1/admin/checklists
// ---------------------------------------------------------------------------

// HandleListChecklists returns a handler that lists organization checklists
// via OPM.
func (h *Handler) HandleListChecklists() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		ac, ok := auth.AuthContextFrom(ctx)
		if !ok {
			model.WriteError(w, r, model.ErrAuthTokenMissing, nil)
			return
		}

		h.log.Info(ctx, "listing checklists", "organization_id", ac.OrganizationID)

		data, err := h.opm.ListChecklists(ctx, ac.OrganizationID)
		if err != nil {
			h.handleOPMError(ctx, w, r, err, "ListChecklists", "checklist")
			return
		}

		h.writeRawJSON(ctx, w, http.StatusOK, data)
	}
}

// ---------------------------------------------------------------------------
// HandleUpdateChecklist — PUT /api/v1/admin/checklists/{checklist_id}
// ---------------------------------------------------------------------------

// HandleUpdateChecklist returns a handler that updates a checklist via OPM.
func (h *Handler) HandleUpdateChecklist() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		ac, ok := auth.AuthContextFrom(ctx)
		if !ok {
			model.WriteError(w, r, model.ErrAuthTokenMissing, nil)
			return
		}

		checklistID := chi.URLParam(r, "checklist_id")
		if checklistID == "" {
			vb := validation.NewBuilder()
			vb.Add(validation.NewRequired("checklist_id"))
			model.WriteValidationError(w, r, vb.Build(), h.log)
			return
		}

		body, ok := h.readBody(w, r)
		if !ok {
			return
		}

		h.log.Info(ctx, "updating checklist",
			"checklist_id", checklistID,
			"organization_id", ac.OrganizationID,
			"user_id", ac.UserID,
		)

		data, err := h.opm.UpdateChecklist(ctx, checklistID, body)
		if err != nil {
			h.handleOPMError(ctx, w, r, err, "UpdateChecklist", "checklist")
			return
		}

		h.writeRawJSON(ctx, w, http.StatusOK, data)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// readBody reads the request body as raw JSON, applying MaxBytesReader for
// DoS protection and validating that the body is non-empty valid JSON.
// Returns false (with error already written) if the body is invalid.
func (h *Handler) readBody(w http.ResponseWriter, r *http.Request) (json.RawMessage, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxAdminBodySize)

	data, err := io.ReadAll(r.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			vb := validation.NewBuilder()
			vb.Add(validation.NewInvalidFormat("body", "максимум 1 МБ"))
			model.WriteValidationError(w, r, vb.Build(), h.log)
			return nil, false
		}
		model.WriteError(w, r, model.ErrInternalError, nil)
		return nil, false
	}

	if len(data) == 0 {
		vb := validation.NewBuilder()
		vb.Add(validation.NewRequired("body"))
		model.WriteValidationError(w, r, vb.Build(), h.log)
		return nil, false
	}

	if !json.Valid(data) {
		vb := validation.NewBuilder()
		vb.Add(validation.NewInvalidFormat("body", "JSON"))
		model.WriteValidationError(w, r, vb.Build(), h.log)
		return nil, false
	}

	return json.RawMessage(data), true
}

// handleOPMError classifies an OPM client error and writes the appropriate
// HTTP error response.
//
// Classification:
//  1. context.Canceled/DeadlineExceeded → 502 OPM_UNAVAILABLE
//  2. ErrOPMDisabled → 502 OPM_UNAVAILABLE (OPM not configured)
//  3. OPMError with StatusCode > 0 → MapOPMError → WriteError
//  4. OPMError with StatusCode == 0 → transport error → 502 OPM_UNAVAILABLE
//  5. Unknown error → 500 INTERNAL_ERROR
func (h *Handler) handleOPMError(ctx context.Context, w http.ResponseWriter, r *http.Request, err error, operation, resourceHint string) {
	if errors.Is(err, context.Canceled) {
		h.log.Warn(ctx, "OPM request cancelled",
			"operation", operation,
		)
		model.WriteError(w, r, model.ErrOPMUnavailable, nil)
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		h.log.Warn(ctx, "OPM request timed out",
			"operation", operation,
		)
		model.WriteError(w, r, model.ErrOPMUnavailable, nil)
		return
	}

	if errors.Is(err, opmclient.ErrOPMDisabled) {
		h.log.Warn(ctx, "OPM is disabled",
			"operation", operation,
		)
		model.WriteError(w, r, model.ErrOPMUnavailable, nil)
		return
	}

	var opmErr *opmclient.OPMError
	if errors.As(err, &opmErr) {
		if opmErr.StatusCode > 0 {
			code := model.MapOPMError(opmErr.StatusCode, opmErr.Body, resourceHint)
			h.log.Warn(ctx, "OPM returned error",
				"operation", operation,
				"status_code", opmErr.StatusCode,
				"error_code", string(code),
				"resource_hint", resourceHint,
			)
			model.WriteError(w, r, code, nil)
			return
		}
		// Transport error (no HTTP status).
		h.log.Error(ctx, "OPM transport error",
			"operation", operation,
			logger.ErrorAttr(err),
		)
		model.WriteError(w, r, model.ErrOPMUnavailable, nil)
		return
	}

	h.log.Error(ctx, "unexpected error from OPM client",
		"operation", operation,
		logger.ErrorAttr(err),
	)
	model.WriteError(w, r, model.ErrInternalError, nil)
}

// writeRawJSON writes a pre-serialized JSON response with the given status.
// If data is nil, writes an empty JSON object.
func (h *Handler) writeRawJSON(ctx context.Context, w http.ResponseWriter, status int, data json.RawMessage) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	out := data
	if out == nil {
		out = json.RawMessage(`{}`)
	}

	if _, err := w.Write(out); err != nil {
		h.log.Error(ctx, "failed to write JSON response",
			logger.ErrorAttr(err),
		)
	}
}
