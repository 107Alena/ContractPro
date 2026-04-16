// Package versions implements the version management handlers for the
// GET /api/v1/contracts/{contract_id}/versions,
// GET /api/v1/contracts/{contract_id}/versions/{version_id},
// GET /api/v1/contracts/{contract_id}/versions/{version_id}/status,
// POST /api/v1/contracts/{contract_id}/versions/upload, and
// POST /api/v1/contracts/{contract_id}/versions/{version_id}/recheck endpoints.
//
// List, get, and status handlers proxy to the Document Management (DM) service,
// mapping artifact_status to user-friendly processing_status. The upload handler
// accepts multipart/form-data, validates the PDF, uploads to S3, creates a
// version in DM with origin_type=RE_UPLOAD, and publishes a
// ProcessDocumentRequested command to DP. The recheck handler creates a new
// version in DM with origin_type=RE_CHECK using the same source file and
// publishes a ProcessDocumentRequested command.
package versions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"contractpro/api-orchestrator/internal/domain/model"
	"contractpro/api-orchestrator/internal/domain/model/validation"
	"contractpro/api-orchestrator/internal/egress/commandpub"
	"contractpro/api-orchestrator/internal/egress/dmclient"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"
)

// ---------------------------------------------------------------------------
// Consumer-side interfaces (dependency inversion)
// ---------------------------------------------------------------------------

// DMClient provides the DM operations needed by the version handlers.
type DMClient interface {
	GetDocument(ctx context.Context, documentID string) (*dmclient.DocumentWithCurrentVersion, error)
	ListVersions(ctx context.Context, documentID string, params dmclient.ListVersionsParams) (*dmclient.VersionList, error)
	GetVersion(ctx context.Context, documentID, versionID string) (*dmclient.DocumentVersionWithArtifacts, error)
	CreateVersion(ctx context.Context, documentID string, req dmclient.CreateVersionRequest) (*dmclient.DocumentVersion, error)
}

// ObjectStorage provides S3-compatible operations for file storage.
type ObjectStorage interface {
	PutObject(ctx context.Context, key string, data io.ReadSeeker, contentType string) error
	DeleteObject(ctx context.Context, key string) error
}

// CommandPublisher publishes processing commands to the DP domain.
type CommandPublisher interface {
	PublishProcessDocument(ctx context.Context, cmd commandpub.ProcessDocumentCommand) error
}

// KVStore provides key-value storage for upload tracking.
type KVStore interface {
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
}

// Compile-time checks.
var (
	_ DMClient         = (*dmclient.Client)(nil)
	_ CommandPublisher = (*commandpub.Publisher)(nil)
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	defaultPage = 1
	defaultSize = 20
	maxSize     = 100

	maxParseMemory = 10 << 20 // 10 MB for multipart form parsing
	pdfMagic       = "%PDF-"
	pdfMagicLen    = 5
	maxFilenameLen = 255
	contentTypePDF = "application/pdf"

	uploadTrackingTTL = 1 * time.Hour
)

// UUIDGenerator generates UUID v4 strings. Tests can replace with deterministic.
type UUIDGenerator func() string

func defaultUUIDGenerator() string {
	return uuid.New().String()
}

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

// Handler handles version management requests.
type Handler struct {
	dm        DMClient
	storage   ObjectStorage
	publisher CommandPublisher
	kv        KVStore
	log       *logger.Logger
	maxUpload int64
	uuidGen   UUIDGenerator
}

// NewHandler creates a version management handler.
//
// dm and log are required for all endpoints. storage, publisher, kv, and
// maxUpload are required only for the upload endpoint.
func NewHandler(
	dm DMClient,
	storage ObjectStorage,
	publisher CommandPublisher,
	kv KVStore,
	log *logger.Logger,
	maxUpload int64,
) *Handler {
	return &Handler{
		dm:        dm,
		storage:   storage,
		publisher: publisher,
		kv:        kv,
		log:       log.With("component", "versions-handler"),
		maxUpload: maxUpload,
		uuidGen:   defaultUUIDGenerator,
	}
}

// ---------------------------------------------------------------------------
// HandleList — GET /api/v1/contracts/{contract_id}/versions
// ---------------------------------------------------------------------------

// HandleList returns a handler for listing versions of a contract with
// pagination (page, size).
func (h *Handler) HandleList() http.HandlerFunc {
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

		page, ok := h.parseIntParam(w, r, "page", defaultPage, 1, 0)
		if !ok {
			return
		}
		size, ok := h.parseIntParam(w, r, "size", defaultSize, 1, maxSize)
		if !ok {
			return
		}

		result, err := h.dm.ListVersions(ctx, contractID, dmclient.ListVersionsParams{
			Page: page,
			Size: size,
		})
		if err != nil {
			h.handleDMError(ctx, w, r, err, "ListVersions", "document")
			return
		}

		items := make([]VersionResponse, 0, len(result.Items))
		for _, v := range result.Items {
			items = append(items, mapVersionToResponse(v))
		}

		h.writeJSON(ctx, w, http.StatusOK, VersionListResponse{
			Items: items,
			Total: result.Total,
			Page:  result.Page,
			Size:  result.Size,
		})
	}
}

// ---------------------------------------------------------------------------
// HandleGet — GET /api/v1/contracts/{contract_id}/versions/{version_id}
// ---------------------------------------------------------------------------

// HandleGet returns a handler for retrieving a single version with
// user-friendly processing status.
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
		versionID, ok := h.extractVersionID(w, r)
		if !ok {
			return
		}

		ctx = logger.WithDocumentID(ctx, contractID)
		ctx = logger.WithVersionID(ctx, versionID)
		r = r.WithContext(ctx)

		result, err := h.dm.GetVersion(ctx, contractID, versionID)
		if err != nil {
			h.handleDMError(ctx, w, r, err, "GetVersion", "version")
			return
		}

		h.writeJSON(ctx, w, http.StatusOK, mapVersionToResponse(result.DocumentVersion))
	}
}

// ---------------------------------------------------------------------------
// HandleStatus — GET /api/v1/contracts/{contract_id}/versions/{version_id}/status
// ---------------------------------------------------------------------------

// HandleStatus returns a handler for the lightweight status polling endpoint.
// Returns only version_id, status, message, and updated_at.
func (h *Handler) HandleStatus() http.HandlerFunc {
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

		result, err := h.dm.GetVersion(ctx, contractID, versionID)
		if err != nil {
			h.handleDMError(ctx, w, r, err, "GetVersion", "version")
			return
		}

		status := mapProcessingStatus(result.ArtifactStatus)

		// DM does not track when artifact_status last changed. We use the
		// current server time as the observation timestamp, which is the most
		// useful value for polling clients (cache invalidation, retry timing).
		h.writeJSON(ctx, w, http.StatusOK, VersionStatusResponse{
			VersionID: result.VersionID,
			Status:    status,
			Message:   mapProcessingStatusMessage(status),
			UpdatedAt: formatTime(time.Now().UTC()),
		})
	}
}

// ---------------------------------------------------------------------------
// HandleUpload — POST /api/v1/contracts/{contract_id}/versions/upload
// ---------------------------------------------------------------------------

// HandleUpload returns a handler for uploading a new version of an existing
// contract. The flow:
//  1. Validate contract_id, extract auth context
//  2. Parse multipart form (body consumed early for DoS protection)
//  3. Validate file (PDF MIME, magic bytes, size)
//  4. DM GetDocument → validate exists, get current_version_id for parent
//  5. Sanitize filename, upload to S3 with SHA-256
//  6. DM CreateVersion (origin_type=RE_UPLOAD)
//  7. Publish ProcessDocumentRequested
//  8. Redis tracking (non-critical)
//  9. Return 202 Accepted
func (h *Handler) HandleUpload() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Step 1: Auth + contract_id validation.
		ac, ok := auth.AuthContextFrom(ctx)
		if !ok {
			model.WriteError(w, r, model.ErrAuthTokenMissing, nil)
			return
		}

		contractID, ok := h.extractContractID(w, r)
		if !ok {
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

		h.log.Info(ctx, "version upload started")

		// Step 2: Limit body and parse multipart (DoS protection before DM call).
		r.Body = http.MaxBytesReader(w, r.Body, h.maxUpload+1<<20)

		if err := r.ParseMultipartForm(maxParseMemory); err != nil {
			h.log.Warn(ctx, "failed to parse multipart form", logger.ErrorAttr(err))
			verr := validation.NewBuilder().Add(validation.NewInvalidFormat("body", "multipart/form-data")).Build()
			model.WriteValidationError(w, r, verr, h.log)
			return
		}
		defer func() {
			if r.MultipartForm != nil {
				_ = r.MultipartForm.RemoveAll()
			}
		}()

		// Step 3: Extract and validate file.
		file, header, err := r.FormFile("file")
		if err != nil {
			h.log.Warn(ctx, "failed to extract file from form", logger.ErrorAttr(err))
			verr := validation.NewBuilder().Add(validation.NewRequired("file")).Build()
			model.WriteValidationError(w, r, verr, h.log)
			return
		}
		defer file.Close()

		if header.Size > h.maxUpload {
			model.WriteError(w, r, model.ErrFileTooLarge,
				fmt.Sprintf("Размер файла %d байт превышает максимум %d байт.", header.Size, h.maxUpload))
			return
		}
		if header.Size == 0 {
			model.WriteError(w, r, model.ErrInvalidFile, "Файл пуст.")
			return
		}

		contentType := header.Header.Get("Content-Type")
		if contentType != contentTypePDF {
			model.WriteError(w, r, model.ErrUnsupportedFormat, nil)
			return
		}

		magic := make([]byte, pdfMagicLen)
		n, readErr := io.ReadFull(file, magic)
		if readErr != nil || n < pdfMagicLen {
			model.WriteError(w, r, model.ErrInvalidFile, "Не удалось прочитать заголовок файла.")
			return
		}
		if string(magic) != pdfMagic {
			model.WriteError(w, r, model.ErrInvalidFile,
				"Файл не является валидным PDF (отсутствует сигнатура %PDF-).")
			return
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			h.log.Error(ctx, "failed to seek file to start", logger.ErrorAttr(err))
			model.WriteError(w, r, model.ErrInternalError, nil)
			return
		}

		// Step 4: Validate document exists in DM.
		doc, err := h.dm.GetDocument(ctx, contractID)
		if err != nil {
			h.handleDMError(ctx, w, r, err, "GetDocument", "document")
			return
		}

		// Fast-fail on archived/deleted documents to avoid S3 upload that will
		// be rejected by DM CreateVersion anyway.
		if doc.Status == "ARCHIVED" {
			model.WriteError(w, r, model.ErrDocumentArchived, nil)
			return
		}
		if doc.Status == "DELETED" {
			model.WriteError(w, r, model.ErrDocumentDeleted, nil)
			return
		}

		// Derive parent_version_id from current version. If the document has no
		// versions (edge case: initial upload failed), create without parent.
		var parentVersionID string
		if doc.CurrentVersion != nil {
			parentVersionID = doc.CurrentVersion.VersionID

			// Reject upload if the current version is still being processed
			// to prevent duplicate DP jobs for the same document.
			as := doc.CurrentVersion.ArtifactStatus
			if as == "PENDING_UPLOAD" || as == "PENDING_PROCESSING" || as == "PROCESSING_IN_PROGRESS" {
				model.WriteError(w, r, model.ErrVersionStillProcessing, nil)
				return
			}
		}

		// Step 5: Sanitize filename and upload to S3.
		sanitizedName := sanitizeFilename(header.Filename)
		s3Key := fmt.Sprintf("uploads/%s/%s/%s", ac.OrganizationID, jobID, h.uuidGen())

		checksum, err := h.uploadWithChecksum(ctx, s3Key, file)
		if err != nil {
			h.log.Error(ctx, "S3 upload failed", logger.ErrorAttr(err))
			model.WriteError(w, r, model.ErrStorageUnavailable, nil)
			return
		}
		h.log.Info(ctx, "file uploaded to S3",
			"s3_key", s3Key,
			"checksum", checksum,
			"file_size", header.Size,
		)

		// Step 6: Create version in DM.
		createReq := dmclient.CreateVersionRequest{
			SourceFileKey:      s3Key,
			SourceFileName:     sanitizedName,
			SourceFileSize:     header.Size,
			SourceFileChecksum: checksum,
			OriginType:         "RE_UPLOAD",
		}
		if parentVersionID != "" {
			createReq.ParentVersionID = parentVersionID
		}

		ver, err := h.dm.CreateVersion(ctx, contractID, createReq)
		if err != nil {
			h.log.Error(ctx, "DM CreateVersion failed", logger.ErrorAttr(err))
			h.cleanupS3(ctx, s3Key)
			h.handleDMError(ctx, w, r, err, "CreateVersion", "version")
			return
		}

		ctx = logger.WithVersionID(ctx, ver.VersionID)
		r = r.WithContext(ctx)

		h.log.Info(ctx, "version created in DM",
			"version_id", ver.VersionID,
			"version_number", ver.VersionNumber,
		)

		// Step 7: Publish ProcessDocumentRequested command.
		cmd := commandpub.ProcessDocumentCommand{
			JobID:              jobID,
			DocumentID:         contractID,
			VersionID:          ver.VersionID,
			OrganizationID:     ac.OrganizationID,
			RequestedByUserID:  ac.UserID,
			SourceFileKey:      s3Key,
			SourceFileName:     sanitizedName,
			SourceFileSize:     header.Size,
			SourceFileChecksum: checksum,
			SourceFileMIMEType: contentTypePDF,
		}
		if err := h.publisher.PublishProcessDocument(ctx, cmd); err != nil {
			// CRITICAL: version exists in DM but the processing command was not
			// delivered. Do NOT roll back — manual intervention can re-publish.
			h.log.Error(ctx, "CRITICAL: failed to publish ProcessDocumentRequested command",
				logger.ErrorAttr(err),
				"document_id", contractID,
				"version_id", ver.VersionID,
				"job_id", jobID,
			)
			model.WriteError(w, r, model.ErrBrokerUnavailable, nil)
			return
		}

		h.log.Info(ctx, "ProcessDocumentRequested command published")

		// Step 8: Save tracking in Redis (non-critical).
		h.saveTracking(ctx, ac.OrganizationID, jobID, contractID, ver.VersionID, ver.VersionNumber, ac.UserID)

		// Step 9: Return 202 Accepted.
		resp := VersionUploadResponse{
			ContractID:    contractID,
			VersionID:     ver.VersionID,
			VersionNumber: ver.VersionNumber,
			JobID:         jobID,
			Status:        "UPLOADED",
			Message:       "Новая версия загружена и отправлена на обработку.",
		}

		w.Header().Set("X-Correlation-Id", correlationID)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			h.log.Error(ctx, "failed to encode upload response", logger.ErrorAttr(err))
		}
	}
}

// ---------------------------------------------------------------------------
// HandleRecheck — POST /api/v1/contracts/{contract_id}/versions/{version_id}/recheck
// ---------------------------------------------------------------------------

// HandleRecheck returns a handler for re-checking a contract version using the
// same source file. The flow:
//  1. Validate contract_id, version_id, extract auth context
//  2. DM GetVersion → get source file metadata + check processing status
//  3. Check version is not still processing (non-terminal artifact_status)
//  4. DM CreateVersion (origin_type=RE_CHECK, same source file)
//  5. Publish ProcessDocumentRequested
//  6. Redis tracking (non-critical)
//  7. Return 202 Accepted
func (h *Handler) HandleRecheck() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Step 1: Auth + contract_id + version_id validation.
		ac, ok := auth.AuthContextFrom(ctx)
		if !ok {
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

		correlationID := h.uuidGen()
		jobID := h.uuidGen()

		ctx = logger.WithRequestContext(ctx, logger.RequestContext{
			CorrelationID:  correlationID,
			OrganizationID: ac.OrganizationID,
			UserID:         ac.UserID,
			JobID:          jobID,
		})
		ctx = logger.WithDocumentID(ctx, contractID)
		ctx = logger.WithVersionID(ctx, versionID)
		r = r.WithContext(ctx)

		h.log.Info(ctx, "recheck started")

		// Step 2: Fetch the version from DM to get source file metadata.
		ver, err := h.dm.GetVersion(ctx, contractID, versionID)
		if err != nil {
			h.handleDMError(ctx, w, r, err, "GetVersion", "version")
			return
		}

		// Step 3: Reject if version is still being processed.
		if isStillProcessing(ver.ArtifactStatus) {
			model.WriteError(w, r, model.ErrVersionStillProcessing, nil)
			return
		}

		// Guard: source file metadata must be present. This can be missing if
		// the parent version was partially created (e.g., upload failed mid-way).
		if ver.SourceFileKey == "" {
			h.log.Error(ctx, "parent version has empty source_file_key",
				"parent_version_id", versionID,
			)
			model.WriteError(w, r, model.ErrInternalError, nil)
			return
		}

		// Step 4: Create a new version in DM with origin_type=RE_CHECK,
		// reusing the same source file from the parent version.
		createReq := dmclient.CreateVersionRequest{
			SourceFileKey:      ver.SourceFileKey,
			SourceFileName:     ver.SourceFileName,
			SourceFileSize:     ver.SourceFileSize,
			SourceFileChecksum: ver.SourceFileChecksum,
			OriginType:         "RE_CHECK",
			ParentVersionID:    versionID,
		}

		newVer, err := h.dm.CreateVersion(ctx, contractID, createReq)
		if err != nil {
			h.log.Error(ctx, "DM CreateVersion failed for recheck", logger.ErrorAttr(err))
			h.handleDMError(ctx, w, r, err, "CreateVersion", "version")
			return
		}

		ctx = logger.WithVersionID(ctx, newVer.VersionID)
		r = r.WithContext(ctx)

		h.log.Info(ctx, "recheck version created in DM",
			"new_version_id", newVer.VersionID,
			"new_version_number", newVer.VersionNumber,
			"parent_version_id", versionID,
		)

		// Step 5: Publish ProcessDocumentRequested command.
		// CRITICAL: if this fails, a version exists in DM but the processing
		// command was not delivered. Do NOT roll back — manual intervention
		// can re-publish the command.
		cmd := commandpub.ProcessDocumentCommand{
			JobID:              jobID,
			DocumentID:         contractID,
			VersionID:          newVer.VersionID,
			OrganizationID:     ac.OrganizationID,
			RequestedByUserID:  ac.UserID,
			SourceFileKey:      ver.SourceFileKey,
			SourceFileName:     ver.SourceFileName,
			SourceFileSize:     ver.SourceFileSize,
			SourceFileChecksum: ver.SourceFileChecksum,
			SourceFileMIMEType: contentTypePDF,
		}
		if err := h.publisher.PublishProcessDocument(ctx, cmd); err != nil {
			h.log.Error(ctx, "CRITICAL: failed to publish ProcessDocumentRequested for recheck",
				logger.ErrorAttr(err),
				"document_id", contractID,
				"new_version_id", newVer.VersionID,
				"job_id", jobID,
			)
			model.WriteError(w, r, model.ErrBrokerUnavailable, nil)
			return
		}

		h.log.Info(ctx, "ProcessDocumentRequested command published for recheck")

		// Step 6: Save tracking in Redis (non-critical).
		h.saveTracking(ctx, ac.OrganizationID, jobID, contractID, newVer.VersionID, newVer.VersionNumber, ac.UserID)

		// Step 7: Return 202 Accepted.
		resp := VersionUploadResponse{
			ContractID:    contractID,
			VersionID:     newVer.VersionID,
			VersionNumber: newVer.VersionNumber,
			JobID:         jobID,
			Status:        "UPLOADED",
			Message:       "Повторная проверка запущена.",
		}

		w.Header().Set("X-Correlation-Id", correlationID)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			h.log.Error(ctx, "failed to encode recheck response", logger.ErrorAttr(err))
		}
	}
}

// isStillProcessing returns true if the artifact_status indicates the version
// is still being processed and cannot be rechecked.
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
		verr := validation.NewBuilder().Add(validation.NewInvalidUUID("contract_id")).Build()
		model.WriteValidationError(w, r, verr, h.log)
		return "", false
	}
	return id, true
}

func (h *Handler) extractVersionID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := chi.URLParam(r, "version_id")
	if id == "" || uuid.Validate(id) != nil {
		verr := validation.NewBuilder().Add(validation.NewInvalidUUID("version_id")).Build()
		model.WriteValidationError(w, r, verr, h.log)
		return "", false
	}
	return id, true
}

func (h *Handler) parseIntParam(w http.ResponseWriter, r *http.Request, name string, defaultVal, min, max int) (int, bool) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return defaultVal, true
	}

	val, err := strconv.Atoi(raw)
	if err != nil {
		expected := "целое число >= " + strconv.Itoa(min)
		if max > 0 {
			expected = "целое число от " + strconv.Itoa(min) + " до " + strconv.Itoa(max)
		}
		verr := validation.NewBuilder().Add(validation.NewInvalidFormat(name, expected)).Build()
		model.WriteValidationError(w, r, verr, h.log)
		return 0, false
	}
	if (min > 0 && val < min) || (max > 0 && val > max) {
		var verr *validation.ValidationError
		if max > 0 {
			verr = validation.NewBuilder().Add(validation.NewOutOfRange(name, min, max)).Build()
		} else {
			verr = validation.NewBuilder().Add(validation.NewInvalidFormat(name, "целое число >= "+strconv.Itoa(min))).Build()
		}
		model.WriteValidationError(w, r, verr, h.log)
		return 0, false
	}

	return val, true
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

// ---------------------------------------------------------------------------
// Upload helpers
// ---------------------------------------------------------------------------

// uploadWithChecksum uploads the file to S3 while computing a SHA-256 checksum.
func (h *Handler) uploadWithChecksum(ctx context.Context, key string, file io.ReadSeeker) (string, error) {
	hasher := sha256.New()
	crs := &checksumReadSeeker{rs: file, hasher: hasher}

	if err := h.storage.PutObject(ctx, key, crs, contentTypePDF); err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// checksumReadSeeker wraps an io.ReadSeeker and computes a hash while reading.
// On Seek(0, SeekStart), the hash is reset so retries produce a correct checksum.
type checksumReadSeeker struct {
	rs     io.ReadSeeker
	hasher interface {
		io.Writer
		Sum(b []byte) []byte
		Reset()
	}
}

func (c *checksumReadSeeker) Read(p []byte) (int, error) {
	n, err := c.rs.Read(p)
	if n > 0 {
		c.hasher.Write(p[:n])
	}
	return n, err
}

func (c *checksumReadSeeker) Seek(offset int64, whence int) (int64, error) {
	pos, err := c.rs.Seek(offset, whence)
	if err != nil {
		return pos, err
	}
	if offset == 0 && whence == io.SeekStart {
		c.hasher.Reset()
	}
	return pos, nil
}

func (h *Handler) cleanupS3(ctx context.Context, key string) {
	if err := h.storage.DeleteObject(ctx, key); err != nil {
		h.log.Error(ctx, "failed to cleanup S3 object after error",
			logger.ErrorAttr(err),
			"s3_key", key,
		)
	} else {
		h.log.Info(ctx, "cleaned up S3 object after error", "s3_key", key)
	}
}

func (h *Handler) saveTracking(
	ctx context.Context,
	orgID, jobID, documentID, versionID string,
	versionNumber int,
	userID string,
) {
	key := fmt.Sprintf("upload:%s:%s", orgID, jobID)
	record := struct {
		JobID          string `json:"job_id"`
		DocumentID     string `json:"document_id"`
		VersionID      string `json:"version_id"`
		VersionNumber  int    `json:"version_number"`
		OrganizationID string `json:"organization_id"`
		UserID         string `json:"user_id"`
		Status         string `json:"status"`
		CreatedAt      string `json:"created_at"`
	}{
		JobID:          jobID,
		DocumentID:     documentID,
		VersionID:      versionID,
		VersionNumber:  versionNumber,
		OrganizationID: orgID,
		UserID:         userID,
		Status:         "UPLOADED",
		CreatedAt:      time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.Marshal(record)
	if err != nil {
		h.log.Warn(ctx, "failed to marshal upload tracking record", logger.ErrorAttr(err))
		return
	}
	if err := h.kv.Set(ctx, key, string(data), uploadTrackingTTL); err != nil {
		h.log.Warn(ctx, "failed to save upload tracking to Redis",
			logger.ErrorAttr(err),
			"redis_key", key,
		)
	}
}

// sanitizeFilename cleans a filename for safe storage.
func sanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(name)

	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		if r == 0 || r < 0x20 || r == 0x7F {
			continue
		}
		b.WriteRune(r)
	}
	name = b.String()

	name = strings.ReplaceAll(name, "../", "")
	name = strings.ReplaceAll(name, "..\\", "")
	name = strings.ReplaceAll(name, "..", "")
	name = strings.TrimSpace(name)

	if len(name) > maxFilenameLen {
		for len(name) > maxFilenameLen {
			_, size := utf8.DecodeLastRuneInString(name)
			name = name[:len(name)-size]
		}
	}

	if name == "" || name == "." {
		name = "upload.pdf"
	}

	return name
}
