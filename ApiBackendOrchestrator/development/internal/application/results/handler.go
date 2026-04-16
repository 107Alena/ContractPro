package results

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"

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

// DMClient provides the DM operations needed by the results aggregator.
// Only the methods actually used are declared (interface segregation).
type DMClient interface {
	GetVersion(ctx context.Context, documentID, versionID string) (*dmclient.DocumentVersionWithArtifacts, error)
	GetArtifact(ctx context.Context, documentID, versionID, artifactType string) (*dmclient.ArtifactResponse, error)
}

// Compile-time check that *dmclient.Client satisfies DMClient.
var _ DMClient = (*dmclient.Client)(nil)

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

// Handler handles analysis results aggregation requests by fetching artifacts
// from the DM service.
type Handler struct {
	dm  DMClient
	log *logger.Logger
}

// NewHandler creates a new results aggregator handler.
func NewHandler(dm DMClient, log *logger.Logger) *Handler {
	return &Handler{
		dm:  dm,
		log: log.With("component", "results-handler"),
	}
}

// ---------------------------------------------------------------------------
// HandleResults — GET .../results
// ---------------------------------------------------------------------------

// HandleResults returns a handler for the aggregated results endpoint.
// Fetches all 7 artifact types concurrently. Restricted to LAWYER and
// ORG_ADMIN roles (BUSINESS_USER gets 403 from RBAC middleware; handler
// performs defense-in-depth check).
func (h *Handler) HandleResults() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		ac, ok := auth.AuthContextFrom(ctx)
		if !ok {
			model.WriteError(w, r, model.ErrAuthTokenMissing, nil)
			return
		}

		// Defense-in-depth: RBAC middleware already blocks BUSINESS_USER,
		// but handlers should not assume middleware ordering is correct.
		if ac.Role == auth.RoleBusinessUser {
			model.WriteError(w, r, model.ErrPermissionDenied, nil)
			return
		}

		contractID, ok := h.extractContractID(w, r)
		if !ok {
			return
		}
		versionID, ok := h.extractVersionID(w, r)
		if !ok {
			return
		}

		ctx = logger.WithDocumentID(ctx, contractID)
		ctx = logger.WithVersionID(ctx, versionID)
		r = r.WithContext(ctx)

		// Resolve version status from DM.
		version, err := h.dm.GetVersion(ctx, contractID, versionID)
		if err != nil {
			h.handleDMError(ctx, w, r, err, "GetVersion", "version")
			return
		}

		status := mapProcessingStatus(version.ArtifactStatus)
		statusMessage := mapProcessingStatusMessage(status)

		// If data is not available, return 200 with status and null fields.
		if !isDataAvailable(version.ArtifactStatus) {
			h.writeJSON(ctx, w, http.StatusOK, AnalysisResultsResponse{
				VersionID:     versionID,
				Status:        status,
				StatusMessage: statusMessage,
			})
			return
		}

		// Fetch all 7 artifact types concurrently.
		artifacts := h.fetchArtifactsParallel(ctx, contractID, versionID, artifactTypesResults)

		h.writeJSON(ctx, w, http.StatusOK, AnalysisResultsResponse{
			VersionID:       versionID,
			Status:          status,
			StatusMessage:   statusMessage,
			ContractType:    artifacts[ArtifactClassificationResult], // DM: CLASSIFICATION_RESULT → API: contract_type
			RiskProfile:     artifacts[ArtifactRiskProfile],
			Risks:           artifacts[ArtifactRiskAnalysis],
			Recommendations: artifacts[ArtifactRecommendations],
			Summary:         artifacts[ArtifactSummary],
			AggregateScore:  artifacts[ArtifactAggregateScore],
			KeyParameters:   artifacts[ArtifactKeyParameters],
		})
	}
}

// ---------------------------------------------------------------------------
// HandleRisks — GET .../risks
// ---------------------------------------------------------------------------

// HandleRisks returns a handler for the risk analysis endpoint.
// Restricted to LAWYER and ORG_ADMIN roles.
func (h *Handler) HandleRisks() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		ac, ok := auth.AuthContextFrom(ctx)
		if !ok {
			model.WriteError(w, r, model.ErrAuthTokenMissing, nil)
			return
		}

		if ac.Role == auth.RoleBusinessUser {
			model.WriteError(w, r, model.ErrPermissionDenied, nil)
			return
		}

		contractID, ok := h.extractContractID(w, r)
		if !ok {
			return
		}
		versionID, ok := h.extractVersionID(w, r)
		if !ok {
			return
		}

		ctx = logger.WithDocumentID(ctx, contractID)
		ctx = logger.WithVersionID(ctx, versionID)
		r = r.WithContext(ctx)

		version, err := h.dm.GetVersion(ctx, contractID, versionID)
		if err != nil {
			h.handleDMError(ctx, w, r, err, "GetVersion", "version")
			return
		}

		status := mapProcessingStatus(version.ArtifactStatus)
		statusMessage := mapProcessingStatusMessage(status)

		if !isDataAvailable(version.ArtifactStatus) {
			h.writeJSON(ctx, w, http.StatusOK, RiskListResponse{
				VersionID:     versionID,
				Status:        status,
				StatusMessage: statusMessage,
			})
			return
		}

		// Only 2 artifacts — sequential fetch is acceptable.
		artifacts := h.fetchArtifactsSequential(ctx, contractID, versionID, artifactTypesRisks)

		h.writeJSON(ctx, w, http.StatusOK, RiskListResponse{
			VersionID:     versionID,
			Status:        status,
			StatusMessage: statusMessage,
			Risks:         artifacts[ArtifactRiskAnalysis],
			RiskProfile:   artifacts[ArtifactRiskProfile],
		})
	}
}

// ---------------------------------------------------------------------------
// HandleSummary — GET .../summary
// ---------------------------------------------------------------------------

// HandleSummary returns a handler for the summary endpoint.
// Accessible to all roles (LAWYER, BUSINESS_USER, ORG_ADMIN).
func (h *Handler) HandleSummary() http.HandlerFunc {
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
		versionID, ok := h.extractVersionID(w, r)
		if !ok {
			return
		}

		ctx = logger.WithDocumentID(ctx, contractID)
		ctx = logger.WithVersionID(ctx, versionID)
		r = r.WithContext(ctx)

		version, err := h.dm.GetVersion(ctx, contractID, versionID)
		if err != nil {
			h.handleDMError(ctx, w, r, err, "GetVersion", "version")
			return
		}

		status := mapProcessingStatus(version.ArtifactStatus)
		statusMessage := mapProcessingStatusMessage(status)

		if !isDataAvailable(version.ArtifactStatus) {
			h.writeJSON(ctx, w, http.StatusOK, SummaryResponse{
				VersionID:     versionID,
				Status:        status,
				StatusMessage: statusMessage,
			})
			return
		}

		// Only 3 artifacts — sequential fetch is acceptable.
		artifacts := h.fetchArtifactsSequential(ctx, contractID, versionID, artifactTypesSummary)

		h.writeJSON(ctx, w, http.StatusOK, SummaryResponse{
			VersionID:      versionID,
			Status:         status,
			StatusMessage:  statusMessage,
			Summary:        artifacts[ArtifactSummary],
			AggregateScore: artifacts[ArtifactAggregateScore],
			KeyParameters:  artifacts[ArtifactKeyParameters],
		})
	}
}

// ---------------------------------------------------------------------------
// HandleRecommendations — GET .../recommendations
// ---------------------------------------------------------------------------

// HandleRecommendations returns a handler for the recommendations endpoint.
// Restricted to LAWYER and ORG_ADMIN roles.
func (h *Handler) HandleRecommendations() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		ac, ok := auth.AuthContextFrom(ctx)
		if !ok {
			model.WriteError(w, r, model.ErrAuthTokenMissing, nil)
			return
		}

		if ac.Role == auth.RoleBusinessUser {
			model.WriteError(w, r, model.ErrPermissionDenied, nil)
			return
		}

		contractID, ok := h.extractContractID(w, r)
		if !ok {
			return
		}
		versionID, ok := h.extractVersionID(w, r)
		if !ok {
			return
		}

		ctx = logger.WithDocumentID(ctx, contractID)
		ctx = logger.WithVersionID(ctx, versionID)
		r = r.WithContext(ctx)

		version, err := h.dm.GetVersion(ctx, contractID, versionID)
		if err != nil {
			h.handleDMError(ctx, w, r, err, "GetVersion", "version")
			return
		}

		status := mapProcessingStatus(version.ArtifactStatus)
		statusMessage := mapProcessingStatusMessage(status)

		if !isDataAvailable(version.ArtifactStatus) {
			h.writeJSON(ctx, w, http.StatusOK, RecommendationsResponse{
				VersionID:     versionID,
				Status:        status,
				StatusMessage: statusMessage,
			})
			return
		}

		// Only 1 artifact — sequential fetch.
		artifacts := h.fetchArtifactsSequential(ctx, contractID, versionID, artifactTypesRecommendations)

		h.writeJSON(ctx, w, http.StatusOK, RecommendationsResponse{
			VersionID:       versionID,
			Status:          status,
			StatusMessage:   statusMessage,
			Recommendations: artifacts[ArtifactRecommendations],
		})
	}
}

// ---------------------------------------------------------------------------
// Artifact fetching
// ---------------------------------------------------------------------------

// fetchArtifactsParallel fetches multiple artifact types concurrently using
// goroutines and sync.WaitGroup. Each goroutine writes to its own slot in the
// result map via a unique key, so no mutex is needed for the map itself — we
// protect the map with a mutex only because map writes from concurrent
// goroutines are not safe even with distinct keys in Go.
//
// Error handling per artifact:
//   - DM 404 (artifact not found) → null (skip gracefully)
//   - Any other DM error → log warning, set null (graceful degradation)
func (h *Handler) fetchArtifactsParallel(
	ctx context.Context,
	documentID, versionID string,
	types []string,
) map[string]json.RawMessage {
	var (
		mu   sync.Mutex
		wg   sync.WaitGroup
		result = make(map[string]json.RawMessage, len(types))
	)

	wg.Add(len(types))
	for _, at := range types {
		go func(artifactType string) {
			defer wg.Done()

			content := h.fetchSingleArtifact(ctx, documentID, versionID, artifactType)

			mu.Lock()
			result[artifactType] = content
			mu.Unlock()
		}(at)
	}

	wg.Wait()
	return result
}

// fetchArtifactsSequential fetches artifact types one at a time. Suitable for
// endpoints with 1-3 artifacts where the overhead of goroutines is not
// justified.
func (h *Handler) fetchArtifactsSequential(
	ctx context.Context,
	documentID, versionID string,
	types []string,
) map[string]json.RawMessage {
	result := make(map[string]json.RawMessage, len(types))
	for _, at := range types {
		result[at] = h.fetchSingleArtifact(ctx, documentID, versionID, at)
	}
	return result
}

// fetchSingleArtifact fetches one artifact from DM. Returns the raw JSON
// content on success, or nil (JSON null) on any error.
//
// Error classification:
//   - 404 from DM → expected (artifact not yet produced), silent skip
//   - Circuit breaker open → log warning, return nil
//   - Other DM error → log warning, return nil (graceful degradation)
func (h *Handler) fetchSingleArtifact(
	ctx context.Context,
	documentID, versionID, artifactType string,
) json.RawMessage {
	resp, err := h.dm.GetArtifact(ctx, documentID, versionID, artifactType)
	if err != nil {
		// Check if this is a 404 — expected when artifact doesn't exist yet.
		if isDMNotFound(err) {
			h.log.Debug(ctx, "artifact not found in DM (expected for partial results)",
				"artifact_type", artifactType,
			)
			return nil
		}

		// All other errors: log and return null (graceful degradation).
		h.log.Warn(ctx, "failed to fetch artifact from DM",
			"artifact_type", artifactType,
			logger.ErrorAttr(err),
		)
		return nil
	}

	// For JSON artifacts, return the raw content.
	if resp.Content != nil {
		return resp.Content
	}

	// Redirect responses (blob artifacts) are not expected for analysis
	// artifacts. Log without the URL to avoid leaking presigned credentials.
	if resp.RedirectURL != "" {
		h.log.Warn(ctx, "unexpected redirect response for analysis artifact",
			"artifact_type", artifactType,
		)
	}
	return nil
}

// isDMNotFound returns true if the error represents a DM 404 response.
func isDMNotFound(err error) bool {
	var dmErr *dmclient.DMError
	if errors.As(err, &dmErr) {
		return dmErr.StatusCode == http.StatusNotFound
	}
	return false
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// extractContractID extracts and validates the contract_id path parameter.
// Returns the ID and true if valid, or writes a 400 error and returns false.
func (h *Handler) extractContractID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := chi.URLParam(r, "contract_id")
	if id == "" || uuid.Validate(id) != nil {
		verr := validation.NewBuilder().Add(validation.NewInvalidUUID("contract_id")).Build()
		model.WriteValidationError(w, r, verr, h.log)
		return "", false
	}
	return id, true
}

// extractVersionID extracts and validates the version_id path parameter.
// Returns the ID and true if valid, or writes a 400 error and returns false.
func (h *Handler) extractVersionID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := chi.URLParam(r, "version_id")
	if id == "" || uuid.Validate(id) != nil {
		verr := validation.NewBuilder().Add(validation.NewInvalidUUID("version_id")).Build()
		model.WriteValidationError(w, r, verr, h.log)
		return "", false
	}
	return id, true
}

// handleDMError classifies a DM client error and writes the appropriate HTTP
// error response. Identical logic to contracts/handler.go and versions/handler.go
// for consistency across all handler packages.
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
