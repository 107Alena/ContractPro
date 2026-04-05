package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
)

// maxRequestBodySize limits the size of JSON request bodies (1 MiB).
const maxRequestBodySize = 1 << 20

// Logger is the consumer-side logging interface.
type Logger interface {
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// BrokerPublisher is the consumer-side interface for publishing messages
// (used by the DLQ replay endpoint).
type BrokerPublisher interface {
	Publish(ctx context.Context, topic string, payload []byte) error
}

// Handler implements the DM REST API.
// All endpoints require an authenticated AuthContext (via authMiddleware).
type Handler struct {
	lifecycle port.DocumentLifecycleHandler
	versions  port.VersionManagementHandler
	queries   port.ArtifactQueryHandler
	diffs     port.DiffStorageHandler
	audit     port.AuditPort
	storage   port.ObjectStoragePort
	logger    Logger

	// DLQ replay dependencies (optional — nil disables the replay endpoint).
	dlqRepo        port.DLQRepository
	dlqBroker      BrokerPublisher
	dlqMaxReplay   int

	// Rate limiting (optional — nil disables rate limiting).
	rateLimiter    *OrgRateLimiter
	rateLimitMetrics RateLimitMetrics
}

// NewHandler creates a new API Handler.
// Panics if any required dependency is nil.
func NewHandler(
	lifecycle port.DocumentLifecycleHandler,
	versions port.VersionManagementHandler,
	queries port.ArtifactQueryHandler,
	diffs port.DiffStorageHandler,
	audit port.AuditPort,
	storage port.ObjectStoragePort,
	logger Logger,
) *Handler {
	if lifecycle == nil {
		panic("api: lifecycle handler is nil")
	}
	if versions == nil {
		panic("api: versions handler is nil")
	}
	if queries == nil {
		panic("api: queries handler is nil")
	}
	if diffs == nil {
		panic("api: diffs handler is nil")
	}
	if audit == nil {
		panic("api: audit port is nil")
	}
	if storage == nil {
		panic("api: storage port is nil")
	}
	if logger == nil {
		panic("api: logger is nil")
	}
	return &Handler{
		lifecycle: lifecycle,
		versions:  versions,
		queries:   queries,
		diffs:     diffs,
		audit:     audit,
		storage:   storage,
		logger:    logger,
	}
}

// WithDLQReplay enables the DLQ replay admin endpoint.
// All three parameters are required for replay to work.
func (h *Handler) WithDLQReplay(repo port.DLQRepository, broker BrokerPublisher, maxReplay int) {
	h.dlqRepo = repo
	h.dlqBroker = broker
	h.dlqMaxReplay = maxReplay
}

// WithRateLimit enables per-organization rate limiting on the API (BRE-009).
// Pass nil limiter to disable rate limiting.
func (h *Handler) WithRateLimit(limiter *OrgRateLimiter, metrics RateLimitMetrics) {
	h.rateLimiter = limiter
	h.rateLimitMetrics = metrics
}

// Mux returns an http.ServeMux with all API routes registered and middleware applied.
// Uses Go 1.22+ method-aware routing patterns.
func (h *Handler) Mux(apiRequests *prometheus.CounterVec, apiDuration *prometheus.HistogramVec) http.Handler {
	mux := http.NewServeMux()

	// --- Documents ---
	mux.HandleFunc("POST /api/v1/documents", h.createDocument)
	mux.HandleFunc("GET /api/v1/documents", h.listDocuments)
	mux.HandleFunc("GET /api/v1/documents/{document_id}", h.getDocument)
	mux.HandleFunc("DELETE /api/v1/documents/{document_id}", h.deleteDocument)
	mux.HandleFunc("POST /api/v1/documents/{document_id}/archive", h.archiveDocument)

	// --- Versions ---
	mux.HandleFunc("POST /api/v1/documents/{document_id}/versions", h.createVersion)
	mux.HandleFunc("GET /api/v1/documents/{document_id}/versions", h.listVersions)
	mux.HandleFunc("GET /api/v1/documents/{document_id}/versions/{version_id}", h.getVersion)

	// --- Artifacts ---
	mux.HandleFunc("GET /api/v1/documents/{document_id}/versions/{version_id}/artifacts", h.listArtifacts)
	mux.HandleFunc("GET /api/v1/documents/{document_id}/versions/{version_id}/artifacts/{artifact_type}", h.getArtifact)

	// --- Diffs ---
	mux.HandleFunc("GET /api/v1/documents/{document_id}/diffs/{base_version_id}/{target_version_id}", h.getDiff)

	// --- Audit (requires "admin" or "auditor" role) ---
	mux.Handle("GET /api/v1/audit",
		requireRole("admin", "auditor")(http.HandlerFunc(h.listAuditRecords)))

	// --- Admin: DLQ Replay (requires "admin" role) ---
	if h.dlqRepo != nil && h.dlqBroker != nil {
		mux.Handle("POST /api/v1/admin/dlq/replay",
			requireRole("admin")(http.HandlerFunc(h.replayDLQ)))
	}

	// Execution order: logging → metrics → auth → rateLimit → handler.
	var handler http.Handler = mux
	handler = rateLimitMiddleware(h.rateLimiter, h.rateLimitMetrics)(handler)
	handler = authMiddleware(handler)
	if apiRequests != nil && apiDuration != nil {
		handler = metricsMiddleware(apiRequests, apiDuration)(handler)
	}
	handler = loggingMiddleware(h.logger)(handler)

	return handler
}

// ---------------------------------------------------------------------------
// Documents
// ---------------------------------------------------------------------------

// createDocumentRequest is the JSON body for POST /documents.
type createDocumentRequest struct {
	Title string `json:"title"`
}

func (h *Handler) createDocument(w http.ResponseWriter, r *http.Request) {
	ac := authFromContext(r.Context())

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
	var req createDocumentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid JSON body")
		return
	}

	if req.Title == "" {
		writeErrorJSON(w, http.StatusBadRequest, "VALIDATION_ERROR", "title is required")
		return
	}

	doc, err := h.lifecycle.CreateDocument(r.Context(), port.CreateDocumentParams{
		OrganizationID:  ac.OrganizationID,
		Title:           req.Title,
		CreatedByUserID: ac.UserID,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, doc)
}

func (h *Handler) listDocuments(w http.ResponseWriter, r *http.Request) {
	ac := authFromContext(r.Context())
	page, size := parsePagination(r)

	var statusFilter *model.DocumentStatus
	if s := r.URL.Query().Get("status"); s != "" {
		st := model.DocumentStatus(s)
		if !isValidDocumentStatus(st) {
			writeErrorJSON(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid status filter")
			return
		}
		statusFilter = &st
	}

	result, err := h.lifecycle.ListDocuments(r.Context(), port.ListDocumentsParams{
		OrganizationID: ac.OrganizationID,
		StatusFilter:   statusFilter,
		Page:           page,
		PageSize:       size,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, PaginatedResponse{
		Items: result.Items,
		Total: result.TotalCount,
		Page:  result.Page,
		Size:  result.PageSize,
	})
}

func (h *Handler) getDocument(w http.ResponseWriter, r *http.Request) {
	ac := authFromContext(r.Context())
	docID := r.PathValue("document_id")

	doc, err := h.lifecycle.GetDocument(r.Context(), ac.OrganizationID, docID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, doc)
}

func (h *Handler) deleteDocument(w http.ResponseWriter, r *http.Request) {
	ac := authFromContext(r.Context())
	docID := r.PathValue("document_id")

	err := h.lifecycle.DeleteDocument(r.Context(), ac.OrganizationID, docID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	// Fetch updated document to return in response.
	doc, err := h.lifecycle.GetDocument(r.Context(), ac.OrganizationID, docID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, doc)
}

func (h *Handler) archiveDocument(w http.ResponseWriter, r *http.Request) {
	ac := authFromContext(r.Context())
	docID := r.PathValue("document_id")

	err := h.lifecycle.ArchiveDocument(r.Context(), ac.OrganizationID, docID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	doc, err := h.lifecycle.GetDocument(r.Context(), ac.OrganizationID, docID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, doc)
}

// ---------------------------------------------------------------------------
// Versions
// ---------------------------------------------------------------------------

// createVersionRequest is the JSON body for POST /documents/{id}/versions.
type createVersionRequest struct {
	SourceFileKey      string `json:"source_file_key"`
	SourceFileName     string `json:"source_file_name"`
	SourceFileSize     int64  `json:"source_file_size"`
	SourceFileChecksum string `json:"source_file_checksum"`
	OriginType         string `json:"origin_type"`
	OriginDescription  string `json:"origin_description"`
	ParentVersionID    string `json:"parent_version_id"`
}

func (h *Handler) createVersion(w http.ResponseWriter, r *http.Request) {
	ac := authFromContext(r.Context())
	docID := r.PathValue("document_id")

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
	var req createVersionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid JSON body")
		return
	}

	originType := model.OriginType(req.OriginType)
	if !isValidOriginType(originType) {
		writeErrorJSON(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid origin_type")
		return
	}

	version, err := h.versions.CreateVersion(r.Context(), port.CreateVersionParams{
		OrganizationID:     ac.OrganizationID,
		DocumentID:         docID,
		ParentVersionID:    req.ParentVersionID,
		OriginType:         originType,
		OriginDescription:  req.OriginDescription,
		SourceFileKey:      req.SourceFileKey,
		SourceFileName:     req.SourceFileName,
		SourceFileSize:     req.SourceFileSize,
		SourceFileChecksum: req.SourceFileChecksum,
		CreatedByUserID:    ac.UserID,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, version)
}

func (h *Handler) listVersions(w http.ResponseWriter, r *http.Request) {
	ac := authFromContext(r.Context())
	docID := r.PathValue("document_id")
	page, size := parsePagination(r)

	result, err := h.versions.ListVersions(r.Context(), port.ListVersionsParams{
		OrganizationID: ac.OrganizationID,
		DocumentID:     docID,
		Page:           page,
		PageSize:       size,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, PaginatedResponse{
		Items: result.Items,
		Total: result.TotalCount,
		Page:  result.Page,
		Size:  result.PageSize,
	})
}

func (h *Handler) getVersion(w http.ResponseWriter, r *http.Request) {
	ac := authFromContext(r.Context())
	docID := r.PathValue("document_id")
	versionID := r.PathValue("version_id")

	version, err := h.versions.GetVersion(r.Context(), ac.OrganizationID, docID, versionID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	// Fetch artifacts to build DocumentVersionWithArtifacts response.
	artifacts, err := h.queries.ListArtifacts(r.Context(), ac.OrganizationID, docID, versionID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, versionWithArtifactsResponse{
		DocumentVersion: version,
		Artifacts:       artifacts,
	})
}

// versionWithArtifactsResponse combines version metadata with its artifact list.
type versionWithArtifactsResponse struct {
	*model.DocumentVersion
	Artifacts []*model.ArtifactDescriptor `json:"artifacts"`
}

// ---------------------------------------------------------------------------
// Artifacts
// ---------------------------------------------------------------------------

func (h *Handler) listArtifacts(w http.ResponseWriter, r *http.Request) {
	ac := authFromContext(r.Context())
	docID := r.PathValue("document_id")
	versionID := r.PathValue("version_id")

	artifacts, err := h.queries.ListArtifacts(r.Context(), ac.OrganizationID, docID, versionID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	// Apply optional query filters.
	filtered := filterArtifacts(artifacts, r.URL.Query().Get("artifact_type"), r.URL.Query().Get("producer_domain"))

	writeJSON(w, http.StatusOK, struct {
		Items []*model.ArtifactDescriptor `json:"items"`
	}{Items: filtered})
}

// filterArtifacts applies client-side filtering by artifact_type and/or producer_domain.
func filterArtifacts(artifacts []*model.ArtifactDescriptor, artifactType, producerDomain string) []*model.ArtifactDescriptor {
	if artifactType == "" && producerDomain == "" {
		return artifacts
	}

	result := make([]*model.ArtifactDescriptor, 0, len(artifacts))
	for _, a := range artifacts {
		if artifactType != "" && string(a.ArtifactType) != artifactType {
			continue
		}
		if producerDomain != "" && string(a.ProducerDomain) != producerDomain {
			continue
		}
		result = append(result, a)
	}
	return result
}

const presignedURLTTL = 15 * time.Minute

// sourceFileType is the pseudo-artifact type for the original uploaded file.
// It is NOT a domain ArtifactType — the source file is stored on DocumentVersion,
// not as an ArtifactDescriptor. Exposed via the artifacts endpoint as a convenience.
const sourceFileType = "SOURCE_FILE"

func (h *Handler) getArtifact(w http.ResponseWriter, r *http.Request) {
	ac := authFromContext(r.Context())
	docID := r.PathValue("document_id")
	versionID := r.PathValue("version_id")
	rawType := r.PathValue("artifact_type")

	// SOURCE_FILE is an API-layer convenience — the original uploaded PDF is stored
	// as version.SourceFileKey, not as an ArtifactDescriptor. Handle it before
	// artifact type validation to keep the domain model clean.
	if rawType == sourceFileType {
		h.getSourceFile(w, r, ac, docID, versionID)
		return
	}

	artifactType := model.ArtifactType(rawType)

	// Validate artifact type against known types.
	if !isValidArtifactType(artifactType) {
		writeErrorJSON(w, http.StatusBadRequest, "VALIDATION_ERROR", "unknown artifact type")
		return
	}

	// For blob artifacts (PDF, DOCX), redirect to presigned URL.
	// Use ListArtifacts to verify existence (tenant isolation) without loading content.
	if artifactType.IsBlobArtifact() {
		artifacts, err := h.queries.ListArtifacts(r.Context(), ac.OrganizationID, docID, versionID)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		var descriptor *model.ArtifactDescriptor
		for _, a := range artifacts {
			if a.ArtifactType == artifactType {
				descriptor = a
				break
			}
		}
		if descriptor == nil {
			writeErrorJSON(w, http.StatusNotFound, "ARTIFACT_NOT_FOUND", "artifact not found")
			return
		}

		url, err := h.storage.GeneratePresignedURL(r.Context(), descriptor.StorageKey, presignedURLTTL)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		http.Redirect(w, r, url, http.StatusFound)
		return
	}

	// For JSON artifacts, return content inline.
	content, err := h.queries.GetArtifact(r.Context(), port.GetArtifactParams{
		OrganizationID: ac.OrganizationID,
		DocumentID:     docID,
		VersionID:      versionID,
		ArtifactType:   artifactType,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}

	w.Header().Set("Content-Type", content.ContentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content.Content)
}

// getSourceFile handles the SOURCE_FILE pseudo-artifact type.
// Looks up the version's SourceFileKey and redirects to a presigned URL.
func (h *Handler) getSourceFile(w http.ResponseWriter, r *http.Request, ac *AuthContext, docID, versionID string) {
	version, err := h.versions.GetVersion(r.Context(), ac.OrganizationID, docID, versionID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	if version.SourceFileKey == "" {
		writeErrorJSON(w, http.StatusNotFound, "ARTIFACT_NOT_FOUND", "source file not available for this version")
		return
	}

	url, err := h.storage.GeneratePresignedURL(r.Context(), version.SourceFileKey, presignedURLTTL)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	http.Redirect(w, r, url, http.StatusFound)
}

// ---------------------------------------------------------------------------
// Diffs
// ---------------------------------------------------------------------------

// diffResponse wraps the diff reference metadata with the diff content.
type diffResponse struct {
	DiffID              string          `json:"diff_id"`
	DocumentID          string          `json:"document_id"`
	BaseVersionID       string          `json:"base_version_id"`
	TargetVersionID     string          `json:"target_version_id"`
	TextDiffCount       int             `json:"text_diff_count"`
	StructuralDiffCount int             `json:"structural_diff_count"`
	TextDiffs           json.RawMessage `json:"text_diffs"`
	StructuralDiffs     json.RawMessage `json:"structural_diffs"`
	CreatedAt           time.Time       `json:"created_at"`
}

func (h *Handler) getDiff(w http.ResponseWriter, r *http.Request) {
	ac := authFromContext(r.Context())
	docID := r.PathValue("document_id")
	baseVersionID := r.PathValue("base_version_id")
	targetVersionID := r.PathValue("target_version_id")

	ref, data, err := h.diffs.GetDiff(r.Context(), port.GetDiffParams{
		OrganizationID:  ac.OrganizationID,
		DocumentID:      docID,
		BaseVersionID:   baseVersionID,
		TargetVersionID: targetVersionID,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}

	// Parse the diff blob to extract text_diffs and structural_diffs.
	var blob struct {
		TextDiffs       json.RawMessage `json:"text_diffs"`
		StructuralDiffs json.RawMessage `json:"structural_diffs"`
	}
	if err := json.Unmarshal(data, &blob); err != nil {
		h.logger.Error("failed to unmarshal diff blob", "err", err)
		writeErrorJSON(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to parse diff data")
		return
	}

	writeJSON(w, http.StatusOK, diffResponse{
		DiffID:              ref.DiffID,
		DocumentID:          ref.DocumentID,
		BaseVersionID:       ref.BaseVersionID,
		TargetVersionID:     ref.TargetVersionID,
		TextDiffCount:       ref.TextDiffCount,
		StructuralDiffCount: ref.StructuralDiffCount,
		TextDiffs:           blob.TextDiffs,
		StructuralDiffs:     blob.StructuralDiffs,
		CreatedAt:           ref.CreatedAt,
	})
}

// ---------------------------------------------------------------------------
// Audit
// ---------------------------------------------------------------------------

func (h *Handler) listAuditRecords(w http.ResponseWriter, r *http.Request) {
	ac := authFromContext(r.Context())
	page, size := parsePagination(r)

	q := r.URL.Query()
	params := port.AuditListParams{
		OrganizationID: ac.OrganizationID,
		DocumentID:     q.Get("document_id"),
		VersionID:      q.Get("version_id"),
		ActorID:        q.Get("actor_id"),
		Page:           page,
		PageSize:       size,
	}

	if v := q.Get("action"); v != "" {
		action := model.AuditAction(v)
		if !isValidAuditAction(action) {
			writeErrorJSON(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid 'action' parameter")
			return
		}
		params.Action = &action
	}
	if v := q.Get("actor_type"); v != "" {
		actorType := model.ActorType(v)
		if !isValidActorType(actorType) {
			writeErrorJSON(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid 'actor_type' parameter")
			return
		}
		params.ActorType = &actorType
	}
	if v := q.Get("from"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeErrorJSON(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid 'from' parameter: expected RFC3339 format")
			return
		}
		params.Since = &t
	}
	if v := q.Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeErrorJSON(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid 'to' parameter: expected RFC3339 format")
			return
		}
		params.Until = &t
	}

	records, total, err := h.audit.List(r.Context(), params)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, PaginatedResponse{
		Items: records,
		Total: total,
		Page:  page,
		Size:  size,
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// parsePagination extracts page and size query params with defaults.
func parsePagination(r *http.Request) (page, size int) {
	page = 1
	size = 20

	if v := r.URL.Query().Get("page"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p >= 1 {
			page = p
		}
	}
	if v := r.URL.Query().Get("size"); v != "" {
		if s, err := strconv.Atoi(v); err == nil && s >= 1 {
			if s > 100 {
				s = 100
			}
			size = s
		}
	}
	return page, size
}

// isValidDocumentStatus checks if a DocumentStatus is one of the known values.
func isValidDocumentStatus(s model.DocumentStatus) bool {
	for _, v := range model.AllDocumentStatuses {
		if v == s {
			return true
		}
	}
	return false
}

// isValidOriginType checks if an OriginType is one of the known values.
func isValidOriginType(t model.OriginType) bool {
	for _, v := range model.AllOriginTypes {
		if v == t {
			return true
		}
	}
	return false
}

// isValidArtifactType checks if an ArtifactType is one of the known values.
func isValidArtifactType(t model.ArtifactType) bool {
	for _, v := range model.AllArtifactTypes {
		if v == t {
			return true
		}
	}
	return false
}

// isValidAuditAction checks if an AuditAction is one of the known values.
func isValidAuditAction(a model.AuditAction) bool {
	for _, v := range model.AllAuditActions {
		if v == a {
			return true
		}
	}
	return false
}

// isValidActorType checks if an ActorType is one of the known values.
func isValidActorType(t model.ActorType) bool {
	for _, v := range model.AllActorTypes {
		if v == t {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Admin: DLQ Replay (REV-018, BRE-011)
// ---------------------------------------------------------------------------

type dlqReplayRequest struct {
	Category      string `json:"category"`       // optional: "ingestion", "query", "invalid"
	CorrelationID string `json:"correlation_id"`  // optional filter
	Limit         int    `json:"limit"`           // default 10, max 100
}

type dlqReplayResponse struct {
	Replayed int      `json:"replayed"`
	Skipped  int      `json:"skipped"`
	Errors   []string `json:"errors,omitempty"`
}

func (h *Handler) replayDLQ(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)

	var req dlqReplayRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid JSON body")
		return
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}

	maxReplay := h.dlqMaxReplay
	if maxReplay <= 0 {
		maxReplay = 3
	}

	var category model.DLQCategory
	if req.Category != "" {
		category = model.DLQCategory(req.Category)
		if category != model.DLQCategoryIngestion &&
			category != model.DLQCategoryQuery &&
			category != model.DLQCategoryInvalid {
			writeErrorJSON(w, http.StatusBadRequest, "VALIDATION_ERROR",
				"invalid category: must be ingestion, query, or invalid")
			return
		}
	}

	records, err := h.dlqRepo.FindByFilter(r.Context(), port.DLQFilterParams{
		Category:      category,
		CorrelationID: req.CorrelationID,
		MaxReplay:     maxReplay,
		Limit:         limit,
	})
	if err != nil {
		h.logger.Error("dlq replay: find records failed", "error", err)
		writeErrorJSON(w, http.StatusInternalServerError, "DATABASE_FAILED", "failed to query DLQ records")
		return
	}

	var resp dlqReplayResponse
	for _, rec := range records {
		if rec.ReplayCount >= maxReplay {
			resp.Skipped++
			continue
		}

		if pubErr := h.dlqBroker.Publish(r.Context(), rec.OriginalTopic, rec.OriginalMessage); pubErr != nil {
			h.logger.Error("dlq replay: publish failed",
				"id", rec.ID, "topic", rec.OriginalTopic, "error", pubErr)
			resp.Errors = append(resp.Errors, rec.ID+": publish failed")
			continue
		}

		if incErr := h.dlqRepo.IncrementReplayCount(r.Context(), rec.ID); incErr != nil {
			h.logger.Error("dlq replay: increment replay count failed",
				"id", rec.ID, "error", incErr)
		}

		resp.Replayed++
	}

	writeJSON(w, http.StatusOK, resp)
}
