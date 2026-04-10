// Package comparison implements the Comparison Coordinator handlers for
// POST /api/v1/contracts/{contract_id}/compare and
// GET /api/v1/contracts/{contract_id}/versions/{base_version_id}/diff/{target_version_id}.
//
// HandleCompare validates both versions, publishes a CompareDocumentVersionsRequested
// command to DP via RabbitMQ, and returns 202 Accepted.
// HandleGetDiff fetches the comparison result from DM and returns it as-is.
package comparison

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"contractpro/api-orchestrator/internal/domain/model"
	"contractpro/api-orchestrator/internal/egress/commandpub"
	"contractpro/api-orchestrator/internal/egress/dmclient"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"
)

// ---------------------------------------------------------------------------
// Consumer-side interfaces (dependency inversion)
// ---------------------------------------------------------------------------

// DMClient provides the DM operations needed by comparison handlers.
type DMClient interface {
	GetVersion(ctx context.Context, documentID, versionID string) (*dmclient.DocumentVersionWithArtifacts, error)
	GetDiff(ctx context.Context, documentID, baseVersionID, targetVersionID string) (*dmclient.VersionDiff, error)
}

// CommandPublisher publishes comparison commands to the DP domain.
type CommandPublisher interface {
	PublishCompareVersions(ctx context.Context, cmd commandpub.CompareVersionsCommand) error
}

// Compile-time interface checks.
var (
	_ DMClient         = (*dmclient.Client)(nil)
	_ CommandPublisher = (*commandpub.Publisher)(nil)
)

// ---------------------------------------------------------------------------
// Request / Response DTOs
// ---------------------------------------------------------------------------

// CompareRequest is the JSON body for POST /compare.
type CompareRequest struct {
	BaseVersionID   string `json:"base_version_id"`
	TargetVersionID string `json:"target_version_id"`
}

// validate checks that both version IDs are valid UUIDs and not equal.
func (cr *CompareRequest) validate() (string, bool) {
	if cr.BaseVersionID == "" {
		return "Поле «base_version_id» обязательно для заполнения.", false
	}
	if uuid.Validate(cr.BaseVersionID) != nil {
		return "Поле «base_version_id» должно быть валидным UUID.", false
	}
	if cr.TargetVersionID == "" {
		return "Поле «target_version_id» обязательно для заполнения.", false
	}
	if uuid.Validate(cr.TargetVersionID) != nil {
		return "Поле «target_version_id» должно быть валидным UUID.", false
	}
	if cr.BaseVersionID == cr.TargetVersionID {
		return "Версии для сравнения должны быть различными (base_version_id == target_version_id).", false
	}
	return "", true
}

// CompareAcceptedResponse is the 202 body for POST /compare.
type CompareAcceptedResponse struct {
	ContractID      string `json:"contract_id"`
	JobID           string `json:"job_id"`
	BaseVersionID   string `json:"base_version_id"`
	TargetVersionID string `json:"target_version_id"`
	Status          string `json:"status"`
	Message         string `json:"message"`
}

// ---------------------------------------------------------------------------
// UUIDGenerator
// ---------------------------------------------------------------------------

// UUIDGenerator generates UUID v4 strings. Tests can replace with deterministic.
type UUIDGenerator func() string

func defaultUUIDGenerator() string {
	return uuid.New().String()
}

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

// Handler handles comparison requests.
type Handler struct {
	dm      DMClient
	cmdPub  CommandPublisher
	log     *logger.Logger
	uuidGen UUIDGenerator
}

// NewHandler creates a comparison handler.
func NewHandler(dm DMClient, cmdPub CommandPublisher, log *logger.Logger) *Handler {
	return &Handler{
		dm:      dm,
		cmdPub:  cmdPub,
		log:     log.With("component", "comparison-handler"),
		uuidGen: defaultUUIDGenerator,
	}
}

// ---------------------------------------------------------------------------
// HandleCompare — POST /api/v1/contracts/{contract_id}/compare
// ---------------------------------------------------------------------------

// HandleCompare returns a handler that initiates a version comparison.
//
// Flow:
//  1. Auth context + contract_id extraction
//  2. Parse and validate JSON body (base_version_id, target_version_id)
//  3. DM GetVersion for base → validate not still processing
//  4. DM GetVersion for target → validate not still processing
//  5. Publish CompareDocumentVersionsRequested command
//  6. Return 202 Accepted
func (h *Handler) HandleCompare() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Step 1: Auth + contract_id.
		ac, ok := auth.AuthContextFrom(ctx)
		if !ok {
			model.WriteError(w, r, model.ErrAuthTokenMissing, nil)
			return
		}

		contractID, ok := h.extractContractID(w, r)
		if !ok {
			return
		}

		// Step 2: Parse JSON body (limit to 1 MB to prevent DoS).
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		var req CompareRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			model.WriteError(w, r, model.ErrValidationError,
				"Невалидный JSON в теле запроса.")
			return
		}

		if msg, valid := req.validate(); !valid {
			model.WriteError(w, r, model.ErrValidationError, msg)
			return
		}

		correlationID := h.uuidGen()
		jobID := h.uuidGen()

		ctx = logger.WithRequestContext(ctx, logger.RequestContext{
			CorrelationID:  correlationID,
			OrganizationID: ac.OrganizationID,
			UserID:         ac.UserID,
			JobID:          jobID,
		})
		ctx = logger.WithDocumentID(ctx, contractID)
		r = r.WithContext(ctx)

		h.log.Info(ctx, "comparison requested",
			"base_version_id", req.BaseVersionID,
			"target_version_id", req.TargetVersionID,
		)

		// Step 3: Validate base version exists and is not still processing.
		baseVer, err := h.dm.GetVersion(ctx, contractID, req.BaseVersionID)
		if err != nil {
			h.handleDMError(ctx, w, r, err, "GetVersion(base)", "version")
			return
		}
		if isStillProcessing(baseVer.ArtifactStatus) {
			model.WriteError(w, r, model.ErrVersionStillProcessing,
				"Базовая версия ещё обрабатывается. Дождитесь завершения обработки.")
			return
		}

		// Step 4: Validate target version exists and is not still processing.
		targetVer, err := h.dm.GetVersion(ctx, contractID, req.TargetVersionID)
		if err != nil {
			h.handleDMError(ctx, w, r, err, "GetVersion(target)", "version")
			return
		}
		if isStillProcessing(targetVer.ArtifactStatus) {
			model.WriteError(w, r, model.ErrVersionStillProcessing,
				"Целевая версия ещё обрабатывается. Дождитесь завершения обработки.")
			return
		}

		// Step 5: Publish CompareDocumentVersionsRequested command.
		cmd := commandpub.CompareVersionsCommand{
			JobID:             jobID,
			DocumentID:        contractID,
			OrganizationID:    ac.OrganizationID,
			RequestedByUserID: ac.UserID,
			BaseVersionID:     req.BaseVersionID,
			TargetVersionID:   req.TargetVersionID,
		}
		if err := h.cmdPub.PublishCompareVersions(ctx, cmd); err != nil {
			h.log.Error(ctx, "CRITICAL: failed to publish CompareDocumentVersionsRequested",
				logger.ErrorAttr(err),
				"document_id", contractID,
				"base_version_id", req.BaseVersionID,
				"target_version_id", req.TargetVersionID,
				"job_id", jobID,
			)
			model.WriteError(w, r, model.ErrBrokerUnavailable, nil)
			return
		}

		h.log.Info(ctx, "CompareDocumentVersionsRequested published")

		// Step 6: Return 202 Accepted.
		resp := CompareAcceptedResponse{
			ContractID:      contractID,
			JobID:           jobID,
			BaseVersionID:   req.BaseVersionID,
			TargetVersionID: req.TargetVersionID,
			Status:          "COMPARISON_QUEUED",
			Message:         "Сравнение версий запущено.",
		}

		w.Header().Set("X-Correlation-Id", correlationID)
		h.writeJSON(ctx, w, http.StatusAccepted, resp)
	}
}

// ---------------------------------------------------------------------------
// HandleGetDiff — GET /api/v1/contracts/{contract_id}/versions/{base_version_id}/diff/{target_version_id}
// ---------------------------------------------------------------------------

// HandleGetDiff returns a handler that retrieves a version comparison result
// from DM and returns it as-is.
func (h *Handler) HandleGetDiff() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		if _, ok := auth.AuthContextFrom(ctx); !ok {
			model.WriteError(w, r, model.ErrAuthTokenMissing, nil)
			return
		}

		contractID, ok := h.extractContractID(w, r)
		if !ok {
			return
		}
		baseVersionID, ok := h.extractURLParam(w, r, "base_version_id")
		if !ok {
			return
		}
		targetVersionID, ok := h.extractURLParam(w, r, "target_version_id")
		if !ok {
			return
		}

		ctx = logger.WithDocumentID(ctx, contractID)
		r = r.WithContext(ctx)

		diff, err := h.dm.GetDiff(ctx, contractID, baseVersionID, targetVersionID)
		if err != nil {
			h.handleDMError(ctx, w, r, err, "GetDiff", "diff")
			return
		}

		h.writeJSON(ctx, w, http.StatusOK, diff)
	}
}

// ---------------------------------------------------------------------------
// isStillProcessing
// ---------------------------------------------------------------------------

// isStillProcessing returns true if the artifact_status indicates the version
// is still being processed and cannot be used for comparison.
func isStillProcessing(artifactStatus string) bool {
	switch artifactStatus {
	case "FULLY_READY", "PARTIALLY_AVAILABLE", "PROCESSING_FAILED", "REJECTED":
		return false
	default:
		return true
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (h *Handler) extractContractID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := chi.URLParam(r, "contract_id")
	if id == "" || uuid.Validate(id) != nil {
		model.WriteError(w, r, model.ErrValidationError,
			"Параметр «contract_id» должен быть валидным UUID.")
		return "", false
	}
	return id, true
}

func (h *Handler) extractURLParam(w http.ResponseWriter, r *http.Request, name string) (string, bool) {
	id := chi.URLParam(r, name)
	if id == "" || uuid.Validate(id) != nil {
		model.WriteError(w, r, model.ErrValidationError,
			"Параметр «"+name+"» должен быть валидным UUID.")
		return "", false
	}
	return id, true
}

func (h *Handler) handleDMError(ctx context.Context, w http.ResponseWriter, r *http.Request, err error, operation, resourceHint string) {
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
			code := model.MapDMError(dmErr.StatusCode, dmErr.Body, resourceHint)
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

func (h *Handler) writeJSON(ctx context.Context, w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		h.log.Error(ctx, "failed to encode JSON response",
			logger.ErrorAttr(err),
		)
	}
}
