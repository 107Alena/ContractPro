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
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"contractpro/api-orchestrator/internal/application/confirmtype"
	"contractpro/api-orchestrator/internal/domain/model"
	"contractpro/api-orchestrator/internal/domain/model/validation"
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
	ListDocumentsWithAnalysis(ctx context.Context, params dmclient.ListDocumentsParams) (*dmclient.DocumentAnalysisList, error)
	GetDocumentStats(ctx context.Context, params dmclient.DocumentStatsParams) (*dmclient.DocumentStats, error)
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

// validRiskLevels are the accepted values for the risk_level filter.
var validRiskLevels = map[string]struct{}{
	"high":   {},
	"medium": {},
	"low":    {},
}

// Contract-type enum validation reuses confirmtype.IsValidEnum /
// confirmtype.EnumValues as the single source of truth for the 12-value English
// LIC classification enum (ASSUMPTION-LIC-16), avoiding a duplicated whitelist.

// validUserProcessingStatuses are the accepted values for the processing_status
// filter (UserProcessingStatus enum). AWAITING_USER_INPUT is a valid enum value
// but is not supported for DM-side filtering (see HandleList).
var validUserProcessingStatuses = map[string]struct{}{
	"UPLOADED": {}, "QUEUED": {}, "PROCESSING": {}, "ANALYZING": {},
	"AWAITING_USER_INPUT": {}, "GENERATING_REPORTS": {}, "READY": {},
	"PARTIALLY_FAILED": {}, "FAILED": {}, "REJECTED": {},
}

// validSortFields / validOrders are the accepted values for sort and order.
var validSortFields = map[string]struct{}{
	"date": {}, "title": {}, "risk": {},
}
var validOrders = map[string]struct{}{
	"asc": {}, "desc": {},
}

// sortedKeys returns the keys of a set as a sorted slice (for error messages).
func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

// Handler handles contract CRUD requests by proxying to the DM service.
type Handler struct {
	dm              DMClient
	log             *logger.Logger
	analysisEnabled bool
	statsEnabled    bool
}

// NewHandler creates a new contract CRUD handler.
//
// analysisEnabled turns on the DM list-aggregation read-contract for
// GET /contracts (contract_type / risk_level / risk_counts and server-side
// filtering/sorting); when false the list falls back to the plain behavior and
// rejects the new filter/sort params (ORCH-TASK-056, ASSUMPTION-ORCH-17).
//
// statsEnabled turns on GET /contracts/stats, backed by the DM
// count-by-artifact_status read-contract (ORCH-TASK-057, ASSUMPTION-ORCH-18);
// when false the endpoint returns 503 FEATURE_NOT_AVAILABLE rather than calling
// a DM aggregate that may not be deployed yet.
func NewHandler(dm DMClient, log *logger.Logger, analysisEnabled, statsEnabled bool) *Handler {
	return &Handler{
		dm:              dm,
		log:             log.With("component", "contracts-handler"),
		analysisEnabled: analysisEnabled,
		statsEnabled:    statsEnabled,
	}
}

// ---------------------------------------------------------------------------
// HandleList — GET /api/v1/contracts
// ---------------------------------------------------------------------------

// HandleList returns a handler for listing contracts with pagination, optional
// filtering by document status, and — when list-aggregation is enabled
// (ORCH-TASK-056) — server-side filtering and sorting by contract_type,
// risk_level, processing_status, and creation period, with each row carrying
// the current version's contract_type / risk_level / risk_counts aggregate.
//
// The aggregate and the new filter/sort params are served via a single batch
// call to the DM list-aggregation read-contract (GET /documents?include=analysis)
// — one DM round-trip per page, never N+1. When list-aggregation is disabled
// the endpoint serves the plain (type/risk-null) list and, as a fail-safe,
// rejects the new filter/sort params with 400 rather than silently returning
// unfiltered data.
func (h *Handler) HandleList() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		if _, ok := auth.AuthContextFrom(ctx); !ok {
			model.WriteError(w, r, model.ErrAuthTokenMissing, nil)
			return
		}

		// Parse and validate pagination + base filters (status, search).
		page, ok := h.parseIntParam(w, r, "page", defaultPage, 1, 0)
		if !ok {
			return
		}
		size, ok := h.parseIntParam(w, r, "size", defaultSize, 1, maxSize)
		if !ok {
			return
		}

		q := r.URL.Query()
		status := q.Get("status")
		// NOTE: search is accepted and validated to maintain a stable external
		// API contract. DM's ListDocumentsParams does not yet support search, so
		// the value is not forwarded. When DM adds search support, we add a
		// Search field without breaking the orchestrator's external API.
		search := q.Get("search")

		vb := validation.NewBuilder()
		if status != "" {
			if _, valid := validStatuses[status]; !valid {
				vb.Add(validation.NewInvalidEnum("status", []string{"ACTIVE", "ARCHIVED", "DELETED"}))
			}
		}
		if search != "" && utf8.RuneCountInString(search) > maxSearchLen {
			vb.Add(validation.NewTooLong("search", maxSearchLen))
		}

		agg, aggPresent := readAggregationParams(q)

		// Feature flag OFF → serve the plain list. Fail-safe: never return data
		// that looks filtered but is not.
		if !h.analysisEnabled {
			if verr := vb.Build(); verr != nil {
				model.WriteValidationError(w, r, verr, h.log)
				return
			}
			if aggPresent {
				model.WriteErrorWithMessage(w, r, model.ErrValidationError,
					"Фильтрация и сортировка по типу договора, уровню риска, статусу обработки и периоду временно недоступны.", nil)
				return
			}
			h.servePlainList(ctx, w, r, page, size, status)
			return
		}

		// Feature flag ON → validate aggregation params and serve the enriched list.
		params := dmclient.ListDocumentsParams{
			Page:            page,
			Size:            size,
			Status:          status,
			IncludeAnalysis: true,
		}
		h.validateAndApplyAggregation(vb, agg, &params)

		if verr := vb.Build(); verr != nil {
			model.WriteValidationError(w, r, verr, h.log)
			return
		}

		h.serveAnalysisList(ctx, w, r, params)
	}
}

// aggregationParams holds the raw ORCH-TASK-056 filter/sort query values.
type aggregationParams struct {
	riskLevel        string
	contractTypes    []string
	processingStatus []string
	dateFrom         string
	dateTo           string
	sort             string
	order            string
}

// readAggregationParams extracts the new filter/sort query values and reports
// whether any of them is present.
func readAggregationParams(q url.Values) (aggregationParams, bool) {
	a := aggregationParams{
		riskLevel:        q.Get("risk_level"),
		contractTypes:    q["contract_type"],
		processingStatus: q["processing_status"],
		dateFrom:         q.Get("date_from"),
		dateTo:           q.Get("date_to"),
		sort:             q.Get("sort"),
		order:            q.Get("order"),
	}
	present := a.riskLevel != "" || len(a.contractTypes) > 0 || len(a.processingStatus) > 0 ||
		a.dateFrom != "" || a.dateTo != "" || a.sort != "" || a.order != ""
	return a, present
}

// validateAndApplyAggregation validates the aggregation params, appending any
// problems to vb, and copies the validated values into params (mapping
// user-facing processing_status to DM artifact_status).
func (h *Handler) validateAndApplyAggregation(vb *validation.ValidationErrorBuilder, a aggregationParams, params *dmclient.ListDocumentsParams) {
	if a.riskLevel != "" {
		if _, ok := validRiskLevels[a.riskLevel]; !ok {
			vb.Add(validation.NewInvalidEnum("risk_level", sortedKeys(validRiskLevels)))
		} else {
			params.RiskLevel = a.riskLevel
		}
	}

	for _, ct := range a.contractTypes {
		if !confirmtype.IsValidEnum(ct) {
			vb.Add(validation.NewInvalidEnum("contract_type", confirmtype.EnumValues()))
			break
		}
	}
	if len(a.contractTypes) > 0 {
		params.ContractTypes = a.contractTypes
	}

	// processing_status → DM artifact_status set (OR semantics).
	var artifactStatuses []string
	for _, ps := range a.processingStatus {
		if _, ok := validUserProcessingStatuses[ps]; !ok {
			vb.Add(validation.NewInvalidEnum("processing_status", sortedKeys(validUserProcessingStatuses)))
			continue
		}
		statuses, supported := artifactStatusesForUserStatus(ps)
		if !supported {
			// Valid enum value but not filterable DM-side in this increment.
			vb.Add(validation.NewInvalidFormat("processing_status",
				"поддерживаемый для фильтрации статус (AWAITING_USER_INPUT пока не поддерживается)"))
			continue
		}
		artifactStatuses = append(artifactStatuses, statuses...)
	}
	if len(artifactStatuses) > 0 {
		params.ArtifactStatuses = artifactStatuses
	}

	// Date range filter (created_at). Accepts date-only or RFC3339. The
	// inversion check compares at UTC day granularity so that mixing date-only
	// and datetime forms across the two params never yields a false 400 for the
	// same calendar day (the period chips operate on whole days).
	from, fromOK := parseDateParam(vb, "date_from", a.dateFrom)
	to, toOK := parseDateParam(vb, "date_to", a.dateTo)
	if fromOK && toOK && truncateToUTCDay(from).After(truncateToUTCDay(to)) {
		vb.Add(validation.NewInvalidFormat("date_from", "значение не позднее date_to"))
	}
	params.DateFrom = a.dateFrom
	params.DateTo = a.dateTo

	if a.sort != "" {
		if _, ok := validSortFields[a.sort]; !ok {
			vb.Add(validation.NewInvalidEnum("sort", sortedKeys(validSortFields)))
		} else {
			params.Sort = a.sort
		}
	}
	if a.order != "" {
		if _, ok := validOrders[a.order]; !ok {
			vb.Add(validation.NewInvalidEnum("order", sortedKeys(validOrders)))
		} else {
			params.Order = a.order
		}
	}
}

// parseDateParam validates an ISO-8601 date (date-only or RFC3339) query value.
// An empty value is valid (no bound) and returns ok=false to signal "absent".
func parseDateParam(vb *validation.ValidationErrorBuilder, field, value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, true
	}
	if t, err := time.Parse("2006-01-02", value); err == nil {
		return t, true
	}
	vb.Add(validation.NewInvalidDate(field, "ISO-8601 (YYYY-MM-DD или RFC3339)"))
	return time.Time{}, false
}

// truncateToUTCDay reduces t to midnight UTC of its calendar day, so date-range
// inversion is compared at day granularity regardless of input format/offset.
func truncateToUTCDay(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

// servePlainList serves the legacy (no-aggregation) contracts list.
func (h *Handler) servePlainList(ctx context.Context, w http.ResponseWriter, r *http.Request, page, size int, status string) {
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

// serveAnalysisList serves the enriched contracts list via the DM
// list-aggregation read-contract (single batch call, no N+1).
func (h *Handler) serveAnalysisList(ctx context.Context, w http.ResponseWriter, r *http.Request, params dmclient.ListDocumentsParams) {
	result, err := h.dm.ListDocumentsWithAnalysis(ctx, params)
	if err != nil {
		h.handleDMError(ctx, w, r, err, "ListDocumentsWithAnalysis", "document")
		return
	}

	items := make([]ContractSummary, 0, len(result.Items))
	withAnalysis := 0
	for _, doc := range result.Items {
		if doc.Analysis != nil {
			withAnalysis++
		}
		items = append(items, mapDocumentWithAnalysisToContractSummary(doc))
	}

	// Sanity check: if DM ignored include=analysis (e.g. an older deployment),
	// every item lacks an analysis aggregate. Warn so a misconfigured feature
	// flag is visible in operations.
	if len(result.Items) > 0 && withAnalysis == 0 {
		h.log.Warn(ctx, "DM returned no analysis aggregates despite include=analysis; "+
			"DM list-aggregation may be unavailable (check ORCH_CONTRACTS_LIST_ANALYSIS_ENABLED vs DM deployment)")
	}

	h.writeJSON(ctx, w, http.StatusOK, ContractListResponse{
		Items: items,
		Total: result.Total,
		Page:  result.Page,
		Size:  result.Size,
	})
}

// ---------------------------------------------------------------------------
// HandleStats — GET /api/v1/contracts/stats
// ---------------------------------------------------------------------------

// HandleStats returns a handler for the dashboard contract-statistics aggregate
// (ORCH-TASK-057). It scopes by the caller's organization (the DM client injects
// X-Organization-ID from the auth context) and returns counts by processing
// status, computed from a SINGLE DM count-by-artifact_status call — never N+1.
//
// The endpoint is gated by the stats feature flag because its backing DM
// read-contract (GET /documents/stats, DM-TASK-059, ASSUMPTION-ORCH-18) may not
// be deployed yet. When the flag is OFF the handler does NOT call DM and returns
// 503 FEATURE_NOT_AVAILABLE — distinct from 502 DM_UNAVAILABLE, which means a
// deployed DM actually failed.
func (h *Handler) HandleStats() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		if _, ok := auth.AuthContextFrom(ctx); !ok {
			model.WriteError(w, r, model.ErrAuthTokenMissing, nil)
			return
		}

		includeArchived, ok := h.parseBoolParam(w, r, "include_archived", false)
		if !ok {
			return
		}

		// Fail-safe: do not call a DM aggregate that may not be deployed.
		if !h.statsEnabled {
			h.log.Warn(ctx, "contract stats requested while disabled "+
				"(ORCH_CONTRACTS_STATS_ENABLED=false; DM-TASK-059 not deployed)")
			model.WriteError(w, r, model.ErrFeatureNotAvailable, nil)
			return
		}

		stats, err := h.dm.GetDocumentStats(ctx, dmclient.DocumentStatsParams{
			IncludeArchived: includeArchived,
		})
		if err != nil {
			h.handleDMError(ctx, w, r, err, "GetDocumentStats", "document")
			return
		}

		result, unknownStatuses, totalMismatch := mapDocumentStatsToContractStats(*stats, time.Now())
		if len(unknownStatuses) > 0 {
			h.log.Warn(ctx, "unrecognized DM artifact_status in stats; counted as processing",
				"artifact_statuses", unknownStatuses,
			)
		}
		if totalMismatch {
			h.log.Warn(ctx, "DM stats total mismatch; recomputed from buckets",
				"dm_total", stats.Total,
				"computed_total", result.Total,
			)
		}

		// Dashboard aggregate is org-scoped and stale-tolerant: allow brief
		// private caching to shield DM from refresh storms (never shared — would
		// leak one org's counts to another).
		w.Header().Set("Cache-Control", "private, max-age=30")
		h.writeJSON(ctx, w, http.StatusOK, result)
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
		vb := validation.NewBuilder()
		vb.Add(validation.NewInvalidUUID("contract_id"))
		model.WriteValidationError(w, r, vb.Build(), h.log)
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
		vb := validation.NewBuilder()
		if max > 0 {
			vb.Add(validation.NewOutOfRange(name, min, max))
		} else {
			vb.Add(validation.NewInvalidFormat(name, fmt.Sprintf("целое число >= %d", min)))
		}
		model.WriteValidationError(w, r, vb.Build(), h.log)
		return 0, false
	}

	return val, true
}

// parseBoolParam parses a boolean query parameter (true/false/1/0). If absent,
// defaultVal is used. Returns the parsed value and true, or writes a validation
// error and returns false.
func (h *Handler) parseBoolParam(w http.ResponseWriter, r *http.Request, name string, defaultVal bool) (bool, bool) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return defaultVal, true
	}

	val, err := strconv.ParseBool(raw)
	if err != nil {
		vb := validation.NewBuilder()
		vb.Add(validation.NewInvalidFormat(name, "булево значение (true или false)"))
		model.WriteValidationError(w, r, vb.Build(), h.log)
		return false, false
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
