// Package confirmtype handles POST /contracts/{id}/versions/{vid}/confirm-type.
//
// When the LIC classification confidence is below threshold, the version enters
// AWAITING_USER_INPUT status. This handler processes the user's type selection:
// validates the contract type against a whitelist, atomically transitions the
// version back to ANALYZING, and publishes UserConfirmedType to LIC.
package confirmtype

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"contractpro/api-orchestrator/internal/application/statustracker"
	"contractpro/api-orchestrator/internal/domain/model"
	"contractpro/api-orchestrator/internal/domain/model/validation"
	"contractpro/api-orchestrator/internal/infra/kvstore"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"
)

// StatusTracker provides the atomic type confirmation transition.
type StatusTracker interface {
	ConfirmType(ctx context.Context, orgID, docID, verID string) error
}

// CommandPublisher publishes the UserConfirmedType command to LIC.
type CommandPublisher interface {
	PublishUserConfirmedType(ctx context.Context, cmd UserConfirmedTypeCommand) error
}

// UserConfirmedTypeCommand mirrors commandpub.UserConfirmedTypeCommand.
type UserConfirmedTypeCommand struct {
	JobID             string
	DocumentID        string
	VersionID         string
	OrganizationID    string
	ConfirmedByUserID string
	ContractType      string
}

// KVStore provides key-value operations for idempotency and metadata.
type KVStore interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
}

// Handler handles the type confirmation endpoint.
type Handler struct {
	tracker        StatusTracker
	publisher      CommandPublisher
	kv             KVStore
	log            *logger.Logger
	whitelist      map[string]struct{}
	idempotencyTTL time.Duration
}

// NewHandler creates a Handler for the confirm-type endpoint.
func NewHandler(
	tracker StatusTracker,
	publisher CommandPublisher,
	kv KVStore,
	log *logger.Logger,
	whitelistSlice []string,
	idempotencyTTL time.Duration,
) *Handler {
	wl := make(map[string]struct{}, len(whitelistSlice))
	for _, t := range whitelistSlice {
		wl[strings.TrimSpace(t)] = struct{}{}
	}
	return &Handler{
		tracker:        tracker,
		publisher:      publisher,
		kv:             kv,
		log:            log.With("component", "confirm-type-handler"),
		whitelist:      wl,
		idempotencyTTL: idempotencyTTL,
	}
}

// --- Request / Response DTOs ---

type confirmTypeRequest struct {
	ContractType    string `json:"contract_type"`
	ConfirmedByUser bool   `json:"confirmed_by_user"`
}

type confirmTypeResponse struct {
	ContractID string `json:"contract_id"`
	VersionID  string `json:"version_id"`
	Status     string `json:"status"`
	Message    string `json:"message"`
}

// confirmationMeta mirrors the struct stored by the classification-uncertain
// handler in statustracker. Decoupled to avoid a package dependency.
type confirmationMeta struct {
	OrganizationID string `json:"organization_id"`
	DocumentID     string `json:"document_id"`
	VersionID      string `json:"version_id"`
	JobID          string `json:"job_id"`
}

// --- Redis key helpers ---

const (
	idempotencyKeyPrefix   = "orch-user-confirmed-type"
	confirmationMetaPrefix = "confirmation:meta"
)

func idempotencyKey(versionID string) string {
	return idempotencyKeyPrefix + ":" + versionID
}

func confirmationMetaKey(versionID string) string {
	return confirmationMetaPrefix + ":" + versionID
}

// Handle returns the http.HandlerFunc for POST /confirm-type.
func (h *Handler) Handle() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// 1. Auth context.
		ac, ok := auth.AuthContextFrom(ctx)
		if !ok {
			model.WriteError(w, r, model.ErrAuthTokenMissing, nil)
			return
		}

		// 2. Path params.
		contractID := chi.URLParam(r, "contract_id")
		versionID := chi.URLParam(r, "version_id")
		if !isValidUUID(contractID) {
			verr := validation.NewBuilder().Add(validation.NewInvalidUUID("contract_id")).Build()
			model.WriteValidationError(w, r, verr, h.log)
			return
		}
		if !isValidUUID(versionID) {
			verr := validation.NewBuilder().Add(validation.NewInvalidUUID("version_id")).Build()
			model.WriteValidationError(w, r, verr, h.log)
			return
		}

		ctx = logger.WithDocumentID(ctx, contractID)
		ctx = logger.WithVersionID(ctx, versionID)

		// 3. Decode and validate request body.
		var req confirmTypeRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
			verr := validation.NewBuilder().Add(validation.NewInvalidFormat("body", "JSON")).Build()
			model.WriteValidationError(w, r, verr, h.log)
			return
		}

		vb := validation.NewBuilder()
		req.ContractType = strings.TrimSpace(req.ContractType)
		if req.ContractType == "" {
			vb.Add(validation.NewRequired("contract_type"))
		}
		if !req.ConfirmedByUser {
			vb.Add(validation.NewInvalidFormat("confirmed_by_user", "true"))
		}
		if req.ContractType != "" {
			if _, ok := h.whitelist[req.ContractType]; !ok {
				vb.Add(validation.NewNotInWhitelist("contract_type", len(h.whitelist)))
			}
		}
		if verr := vb.Build(); verr != nil {
			model.WriteValidationError(w, r, verr, h.log)
			return
		}

		// 4. Idempotency check.
		iKey := idempotencyKey(versionID)
		_, err := h.kv.Get(ctx, iKey)
		if err == nil {
			h.log.Debug(ctx, "confirm-type already processed (idempotency hit)",
				"version_id", versionID)
			h.writeResponse(w, r, contractID, versionID)
			return
		}
		if !errors.Is(err, kvstore.ErrKeyNotFound) {
			h.log.Error(ctx, "failed to check idempotency key",
				logger.ErrorAttr(err), "key", iKey)
			model.WriteError(w, r, model.ErrInternalError, nil)
			return
		}

		// 5. Read confirmation metadata.
		metaKey := confirmationMetaKey(versionID)
		metaJSON, err := h.kv.Get(ctx, metaKey)
		if err != nil {
			if errors.Is(err, kvstore.ErrKeyNotFound) {
				model.WriteError(w, r, model.ErrVersionNotFound, nil)
				return
			}
			h.log.Error(ctx, "failed to read confirmation metadata",
				logger.ErrorAttr(err), "key", metaKey)
			model.WriteError(w, r, model.ErrInternalError, nil)
			return
		}

		var meta confirmationMeta
		if err := json.Unmarshal([]byte(metaJSON), &meta); err != nil {
			h.log.Error(ctx, "failed to unmarshal confirmation metadata",
				logger.ErrorAttr(err), "key", metaKey)
			model.WriteError(w, r, model.ErrInternalError, nil)
			return
		}

		// 6. Atomic status transition: AWAITING_USER_INPUT → ANALYZING.
		if err := h.tracker.ConfirmType(ctx, meta.OrganizationID, meta.DocumentID, versionID); err != nil {
			if errors.Is(err, statustracker.ErrNotAwaitingInput) {
				model.WriteError(w, r, model.ErrVersionNotAwaitingInput, nil)
				return
			}
			h.log.Error(ctx, "ConfirmType failed",
				logger.ErrorAttr(err), "version_id", versionID)
			model.WriteError(w, r, model.ErrInternalError, nil)
			return
		}

		// 7. Publish UserConfirmedType command to LIC.
		cmd := UserConfirmedTypeCommand{
			JobID:             meta.JobID,
			DocumentID:        meta.DocumentID,
			VersionID:         versionID,
			OrganizationID:    ac.OrganizationID,
			ConfirmedByUserID: ac.UserID,
			ContractType:      req.ContractType,
		}
		if err := h.publisher.PublishUserConfirmedType(ctx, cmd); err != nil {
			h.log.Error(ctx, "CRITICAL: failed to publish UserConfirmedType after status transition",
				logger.ErrorAttr(err),
				"version_id", versionID,
				"contract_type", req.ContractType)
			model.WriteError(w, r, model.ErrBrokerUnavailable, nil)
			return
		}

		// 8. Set idempotency key (non-critical).
		if err := h.kv.Set(ctx, iKey, "1", h.idempotencyTTL); err != nil {
			h.log.Warn(ctx, "failed to set idempotency key",
				logger.ErrorAttr(err), "key", iKey)
		}

		// 9. Return 202 Accepted.
		h.log.Info(ctx, "type confirmed successfully",
			"version_id", versionID,
			"contract_type", req.ContractType,
			"confirmed_by", ac.UserID)
		h.writeResponse(w, r, contractID, versionID)
	}
}

func (h *Handler) writeResponse(w http.ResponseWriter, r *http.Request, contractID, versionID string) {
	rc := logger.RequestContextFrom(r.Context())
	w.Header().Set("X-Correlation-Id", rc.CorrelationID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)

	resp := confirmTypeResponse{
		ContractID: contractID,
		VersionID:  versionID,
		Status:     "ANALYZING",
		Message:    "Тип договора подтверждён. Анализ возобновлён.",
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.log.Error(r.Context(), "failed to encode response", logger.ErrorAttr(err))
	}
}

func isValidUUID(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil
}

// WhitelistValues returns the configured contract type whitelist as a slice.
// Exported for testing and introspection.
func (h *Handler) WhitelistValues() []string {
	out := make([]string, 0, len(h.whitelist))
	for k := range h.whitelist {
		out = append(out, k)
	}
	return out
}

