// Package contracts implements the contract CRUD handlers for the
// GET /api/v1/contracts, GET /api/v1/contracts/{contract_id},
// POST /api/v1/contracts/{contract_id}/archive, and
// DELETE /api/v1/contracts/{contract_id} endpoints.
//
// The handlers proxy requests to the Document Management (DM) service,
// mapping the DM "document" concept to the user-facing "contract" concept
// (ASSUMPTION-ORCH-12). DM errors are mapped to orchestrator ErrorCodes via
// model.MapDMError.
package contracts

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"contractpro/api-orchestrator/internal/domain/model"
	"contractpro/api-orchestrator/internal/egress/dmclient"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"
)

// ---------------------------------------------------------------------------
// Consumer-side interface
// ---------------------------------------------------------------------------

// DMClient provides the Document Management operations needed by the contract
// CRUD handlers.
type DMClient interface {
	ListDocuments(ctx context.Context, params dmclient.ListDocumentsParams) (*dmclient.DocumentList, error)
	GetDocument(ctx context.Context, documentID string) (*dmclient.DocumentWithCurrentVersion, error)
	DeleteDocument(ctx context.Context, documentID string) (*dmclient.Document, error)
	ArchiveDocument(ctx context.Context, documentID string) (*dmclient.Document, error)
}

// Compile-time check that *dmclient.Client satisfies DMClient.
var _ DMClient = (*dmclient.Client)(nil)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	defaultPage  = 1
	defaultSize  = 20
	maxSize      = 100
	maxSearchLen = 200 // max rune count for search query
)

var validStatuses = map[string]struct{}{
	"ACTIVE":   {},
	"ARCHIVED": {},
	"DELETED":  {},
}

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

// Handler handles contract CRUD requests by proxying to the DM service.
type Handler struct {
	dm  DMClient
	log *logger.Logger
}

// NewHandler creates a new contract CRUD handler.
func NewHandler(dm DMClient, log *logger.Logger) *Handler {
	return &Handler{
		dm:  dm,
		log: log.With("component", "contracts-handler"),
	}
}

// ---------------------------------------------------------------------------
// HandleList — GET /api/v1/contracts
// ---------------------------------------------------------------------------

// HandleList returns a handler for listing contracts with pagination and
// optional filtering by status.
func (h *Handler) HandleList() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		if _, ok := auth.AuthContextFrom(ctx); !ok {
			model.WriteError(w, r, model.ErrAuthTokenMissing, nil)
			return
		}

		// Parse and validate query parameters.
		page, ok := h.parseIntParam(w, r, "page", defaultPage, 1, 0)
		if !ok {
			return
		}
		size, ok := h.parseIntParam(w, r, "size", defaultSize, 1, maxSize)
		if !ok {
			return
		}

		status := r.URL.Query().Get("status")
		if status != "" {
			if _, valid := validStatuses[status]; !valid {
				model.WriteError(w, r, model.ErrValidationError,
					"Параметр «status» должен быть одним из: ACTIVE, ARCHIVED, DELETED.")
				return
			}
		}

		// NOTE: search parameter is accepted and validated to maintain a stable
		// external API contract. DM's ListDocumentsParams does not yet support
		// search, so the value is not forwarded. When DM adds search support,
		// we add a Search field to ListDocumentsParams without breaking the
		// orchestrator's external API.
		search := r.URL.Query().Get("search")
		if search != "" && utf8.RuneCountInString(search) > maxSearchLen {
			model.WriteError(w, r, model.ErrValidationError,
				"Параметр «search» не должен превышать 200 символов.")
			return
		}

		result, err := h.dm.ListDocuments(ctx, dmclient.ListDocumentsParams{
			Page:   page,
			Size:   size,
			Status: status,
		})
		if err != nil {
			h.handleDMError(ctx, w, r, err, "ListDocuments", "document")
			return
		}

		items := make([]ContractSummary, 0, len(result.Items))
		for _, doc := range result.Items {
			items = append(items, mapDocumentToContractSummary(doc))
		}

		h.writeJSON(ctx, w, http.StatusOK, ContractListResponse{
			Items: items,
			Total: result.Total,
			Page:  result.Page,
			Size:  result.Size,
		})
	}
}

// ---------------------------------------------------------------------------
// HandleGet — GET /api/v1/contracts/{contract_id}
// ---------------------------------------------------------------------------

// HandleGet returns a handler for retrieving a single contract with its
// current version and user-friendly processing status.
func (h *Handler) HandleGet() http.HandlerFunc {
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

		ctx = logger.WithDocumentID(ctx, contractID)
		r = r.WithContext(ctx)

		result, err := h.dm.GetDocument(ctx, contractID)
		if err != nil {
			h.handleDMError(ctx, w, r, err, "GetDocument", "document")
			return
		}

		h.writeJSON(ctx, w, http.StatusOK, mapDocumentWithVersionToContractDetails(*result))
	}
}

// ---------------------------------------------------------------------------
// HandleArchive — POST /api/v1/contracts/{contract_id}/archive
// ---------------------------------------------------------------------------

// HandleArchive returns a handler for archiving a contract.
func (h *Handler) HandleArchive() http.HandlerFunc {
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

		ctx = logger.WithDocumentID(ctx, contractID)
		r = r.WithContext(ctx)

		h.log.Info(ctx, "archiving contract")

		result, err := h.dm.ArchiveDocument(ctx, contractID)
		if err != nil {
			h.handleDMError(ctx, w, r, err, "ArchiveDocument", "document")
			return
		}

		h.writeJSON(ctx, w, http.StatusOK, mapDocumentToContractSummary(*result))
	}
}

// ---------------------------------------------------------------------------
// HandleDelete — DELETE /api/v1/contracts/{contract_id}
// ---------------------------------------------------------------------------

// HandleDelete returns a handler for soft-deleting a contract.
func (h *Handler) HandleDelete() http.HandlerFunc {
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

		ctx = logger.WithDocumentID(ctx, contractID)
		r = r.WithContext(ctx)

		h.log.Info(ctx, "deleting contract")

		result, err := h.dm.DeleteDocument(ctx, contractID)
		if err != nil {
			h.handleDMError(ctx, w, r, err, "DeleteDocument", "document")
			return
		}

		h.writeJSON(ctx, w, http.StatusOK, mapDocumentToContractSummary(*result))
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// extractContractID extracts and validates the contract_id path parameter.
// Returns the ID and true if valid, or writes a 400 error and returns false.
func (h *Handler) extractContractID(w http.ResponseWriter, r *http.Request) (string, bool) {
	contractID := chi.URLParam(r, "contract_id")
	if contractID == "" || uuid.Validate(contractID) != nil {
		model.WriteError(w, r, model.ErrValidationError,
			"Параметр «contract_id» должен быть валидным UUID.")
		return "", false
	}
	return contractID, true
}

// parseIntParam parses an integer query parameter with validation.
// If the parameter is absent, defaultVal is used.
// If min > 0, values below min are rejected.
// If max > 0, values above max are rejected.
// Returns the parsed value and true, or writes a validation error and returns false.
func (h *Handler) parseIntParam(w http.ResponseWriter, r *http.Request, name string, defaultVal, min, max int) (int, bool) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return defaultVal, true
	}

	val, err := strconv.Atoi(raw)
	if err != nil || (min > 0 && val < min) || (max > 0 && val > max) {
		var msg string
		if max > 0 {
			msg = "Параметр «" + name + "» должен быть целым числом от " +
				strconv.Itoa(min) + " до " + strconv.Itoa(max) + "."
		} else {
			msg = "Параметр «" + name + "» должен быть целым числом >= " +
				strconv.Itoa(min) + "."
		}
		model.WriteError(w, r, model.ErrValidationError, msg)
		return 0, false
	}

	return val, true
}

// handleDMError classifies a DM client error and writes the appropriate HTTP
// error response. It handles three categories:
//  1. Circuit breaker open → 502 DM_UNAVAILABLE
//  2. DMError with HTTP status → MapDMError → WriteError
//  3. Transport/unknown error → 502 DM_UNAVAILABLE or 500 INTERNAL_ERROR
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
		// Transport error (no HTTP status).
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
