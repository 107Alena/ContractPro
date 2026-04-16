// Package upload implements the contract upload coordinator for the
// POST /api/v1/contracts/upload endpoint.
//
// The handler accepts multipart/form-data with a "file" (PDF) and "title"
// field, validates the input, uploads the file to S3 while streaming a
// SHA-256 checksum, creates a document and version in Document Management,
// publishes a ProcessDocumentRequested command, and saves tracking state in
// Redis.
//
// On partial failure the handler performs compensating cleanup:
//   - DM create-document or create-version failure after S3 upload -> delete S3 object
//   - Broker publish failure -> log CRITICAL (version already exists in DM)
//   - Redis save failure -> log WARN (non-critical tracking data)
package upload

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"contractpro/api-orchestrator/internal/domain/model"
	"contractpro/api-orchestrator/internal/domain/model/validation"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"
)

// ---------------------------------------------------------------------------
// Dependency interfaces (consumer-side, for testability)
// ---------------------------------------------------------------------------

// ObjectStorage provides S3-compatible operations for file storage.
type ObjectStorage interface {
	PutObject(ctx context.Context, key string, data io.ReadSeeker, contentType string) error
	DeleteObject(ctx context.Context, key string) error
}

// DMClient provides operations on the Document Management service.
type DMClient interface {
	CreateDocument(ctx context.Context, req CreateDocumentRequest) (*Document, error)
	CreateVersion(ctx context.Context, documentID string, req CreateVersionRequest) (*DocumentVersion, error)
}

// CommandPublisher publishes processing commands to the Document Processing
// domain via the message broker.
type CommandPublisher interface {
	PublishProcessDocument(ctx context.Context, cmd ProcessDocumentCommand) error
}

// KVStore provides key-value storage for upload tracking.
type KVStore interface {
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
}

// ---------------------------------------------------------------------------
// DTO types (local to upload package, decoupled from egress models)
// ---------------------------------------------------------------------------

// CreateDocumentRequest is the payload for creating a document in DM.
type CreateDocumentRequest struct {
	Title string `json:"title"`
}

// Document represents a DM document resource.
type Document struct {
	DocumentID string `json:"document_id"`
}

// CreateVersionRequest is the payload for creating a version in DM.
type CreateVersionRequest struct {
	SourceFileKey      string `json:"source_file_key"`
	SourceFileName     string `json:"source_file_name"`
	SourceFileSize     int64  `json:"source_file_size"`
	SourceFileChecksum string `json:"source_file_checksum"`
	OriginType         string `json:"origin_type"`
}

// DocumentVersion represents a DM document version resource.
type DocumentVersion struct {
	VersionID     string `json:"version_id"`
	VersionNumber int    `json:"version_number"`
}

// ProcessDocumentCommand is the command published to the broker.
type ProcessDocumentCommand struct {
	JobID              string `json:"job_id"`
	DocumentID         string `json:"document_id"`
	VersionID          string `json:"version_id"`
	OrganizationID     string `json:"organization_id"`
	RequestedByUserID  string `json:"requested_by_user_id"`
	SourceFileKey      string `json:"source_file_key"`
	SourceFileName     string `json:"source_file_name"`
	SourceFileSize     int64  `json:"source_file_size"`
	SourceFileChecksum string `json:"source_file_checksum"`
	SourceFileMIMEType string `json:"source_file_mime_type"`
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// maxParseMemory is the maximum memory used for multipart form parsing.
	// Beyond this, temporary files on disk are used.
	maxParseMemory = 10 << 20 // 10 MB

	// pdfMagic is the magic byte signature for PDF files.
	pdfMagic = "%PDF-"

	// pdfMagicLen is the number of bytes to read for magic byte verification.
	pdfMagicLen = 5

	// maxTitleLen is the maximum allowed title length in characters (not bytes).
	maxTitleLen = 500

	// maxFilenameLen is the maximum allowed sanitized filename length in bytes.
	maxFilenameLen = 255

	// uploadTrackingTTL is the TTL for upload tracking records in Redis.
	uploadTrackingTTL = 1 * time.Hour

	// contentTypePDF is the Content-Type for PDF files.
	contentTypePDF = "application/pdf"
)

// ---------------------------------------------------------------------------
// Upload response
// ---------------------------------------------------------------------------

// UploadResponse is the JSON body returned on successful upload (202 Accepted).
type UploadResponse struct {
	ContractID    string `json:"contract_id"`
	VersionID     string `json:"version_id"`
	VersionNumber int    `json:"version_number"`
	JobID         string `json:"job_id"`
	Status        string `json:"status"`
	Message       string `json:"message"`
}

// ---------------------------------------------------------------------------
// Upload tracking record (stored in Redis)
// ---------------------------------------------------------------------------

// uploadTracking is the JSON value stored in Redis for upload state tracking.
type uploadTracking struct {
	JobID          string `json:"job_id"`
	DocumentID     string `json:"document_id"`
	VersionID      string `json:"version_id"`
	VersionNumber  int    `json:"version_number"`
	OrganizationID string `json:"organization_id"`
	UserID         string `json:"user_id"`
	Status         string `json:"status"`
	CreatedAt      string `json:"created_at"`
}

// ---------------------------------------------------------------------------
// UUIDGenerator (seam for testing)
// ---------------------------------------------------------------------------

// UUIDGenerator generates UUID v4 strings. The default implementation uses
// google/uuid. Tests can replace this with a deterministic generator.
type UUIDGenerator func() string

// defaultUUIDGenerator is the production UUID generator.
func defaultUUIDGenerator() string {
	return uuid.New().String()
}

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

// Handler coordinates the contract upload flow. It is stateless and safe for
// concurrent use by multiple goroutines.
type Handler struct {
	storage   ObjectStorage
	dm        DMClient
	publisher CommandPublisher
	kv        KVStore
	log       *logger.Logger
	maxSize   int64
	uuidGen   UUIDGenerator
}

// NewHandler creates a Handler with the given dependencies.
//
// maxSize is the maximum allowed file size in bytes (from config.UploadConfig.MaxSize).
// log should be the root logger; the handler creates a component-scoped child.
func NewHandler(
	storage ObjectStorage,
	dm DMClient,
	publisher CommandPublisher,
	kv KVStore,
	log *logger.Logger,
	maxSize int64,
) *Handler {
	return &Handler{
		storage:   storage,
		dm:        dm,
		publisher: publisher,
		kv:        kv,
		log:       log.With("component", "upload-handler"),
		maxSize:   maxSize,
		uuidGen:   defaultUUIDGenerator,
	}
}

// Handle returns an http.HandlerFunc for POST /api/v1/contracts/upload.
//
// The handler expects the request to pass through the auth middleware first,
// which populates auth.AuthContext in the request context.
func (h *Handler) Handle() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// ---------------------------------------------------------------
		// Step 0: Extract auth context
		// ---------------------------------------------------------------
		ac, ok := auth.AuthContextFrom(ctx)
		if !ok {
			model.WriteError(w, r, model.ErrAuthTokenMissing, nil)
			return
		}

		// ---------------------------------------------------------------
		// Step 1: Generate correlation_id and job_id
		// ---------------------------------------------------------------
		correlationID := h.uuidGen()
		jobID := h.uuidGen()

		// Enrich context for structured logging.
		ctx = logger.WithRequestContext(ctx, logger.RequestContext{
			CorrelationID:  correlationID,
			OrganizationID: ac.OrganizationID,
			UserID:         ac.UserID,
			JobID:          jobID,
		})
		// Update the request with the enriched context so that downstream
		// WriteError calls can read the correlation_id.
		r = r.WithContext(ctx)

		h.log.Info(ctx, "upload started")

		// ---------------------------------------------------------------
		// Step 2: Limit request body size (DoS protection)
		// ---------------------------------------------------------------
		// MaxBytesReader limits the total request body to maxSize + 1 MB
		// overhead for multipart framing. Without this, an attacker could
		// send a multi-GB body that spills to temp files and exhausts disk.
		r.Body = http.MaxBytesReader(w, r.Body, h.maxSize+1<<20)

		// ---------------------------------------------------------------
		// Step 3: Parse multipart form
		// ---------------------------------------------------------------
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

		// ---------------------------------------------------------------
		// Step 3: Extract and validate title + file (accumulated)
		// ---------------------------------------------------------------
		title := strings.TrimSpace(r.FormValue("title"))
		file, header, fileErr := r.FormFile("file")

		vb := validation.NewBuilder()
		if title == "" {
			vb.Add(validation.NewRequired("title"))
		} else {
			if utf8.RuneCountInString(title) > maxTitleLen {
				vb.Add(validation.NewTooLong("title", maxTitleLen))
			}
			// Reject titles containing control characters (defense-in-depth).
			if containsControlChars(title) {
				vb.Add(validation.NewInvalidFormat("title", "текст без управляющих символов"))
			}
		}
		if fileErr != nil {
			vb.Add(validation.NewRequired("file"))
		}
		if verr := vb.Build(); verr != nil {
			if file != nil {
				file.Close()
			}
			model.WriteValidationError(w, r, verr, h.log)
			return
		}
		defer file.Close()

		// ---------------------------------------------------------------
		// Step 5: Validate file size
		// ---------------------------------------------------------------
		if header.Size > h.maxSize {
			model.WriteError(w, r, model.ErrFileTooLarge,
				fmt.Sprintf("Размер файла %d байт превышает максимум %d байт.", header.Size, h.maxSize))
			return
		}
		if header.Size == 0 {
			model.WriteError(w, r, model.ErrInvalidFile, "Файл пуст.")
			return
		}

		// ---------------------------------------------------------------
		// Step 6: Validate MIME type
		// ---------------------------------------------------------------
		contentType := header.Header.Get("Content-Type")
		if contentType != contentTypePDF {
			model.WriteError(w, r, model.ErrUnsupportedFormat, nil)
			return
		}

		// ---------------------------------------------------------------
		// Step 7: Verify PDF magic bytes
		// ---------------------------------------------------------------
		magic := make([]byte, pdfMagicLen)
		n, err := io.ReadFull(file, magic)
		if err != nil || n < pdfMagicLen {
			model.WriteError(w, r, model.ErrInvalidFile, "Не удалось прочитать заголовок файла.")
			return
		}
		if string(magic) != pdfMagic {
			model.WriteError(w, r, model.ErrInvalidFile,
				"Файл не является валидным PDF (отсутствует сигнатура %PDF-).")
			return
		}
		// Seek back to the beginning so the full file is uploaded.
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			h.log.Error(ctx, "failed to seek file to start", logger.ErrorAttr(err))
			model.WriteError(w, r, model.ErrInternalError, nil)
			return
		}

		// ---------------------------------------------------------------
		// Step 8: Sanitize filename
		// ---------------------------------------------------------------
		sanitizedName := sanitizeFilename(header.Filename)

		// ---------------------------------------------------------------
		// Step 9: Compute SHA-256 while streaming to S3
		// ---------------------------------------------------------------
		s3Key := fmt.Sprintf("uploads/%s/%s/%s", ac.OrganizationID, jobID, h.uuidGen())
		checksum, err := h.uploadWithChecksum(ctx, s3Key, file, header.Size)
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

		// ---------------------------------------------------------------
		// Step 10: Create document in DM
		// ---------------------------------------------------------------
		doc, err := h.dm.CreateDocument(ctx, CreateDocumentRequest{Title: title})
		if err != nil {
			h.log.Error(ctx, "DM CreateDocument failed", logger.ErrorAttr(err))
			h.cleanupS3(ctx, s3Key)
			model.WriteError(w, r, model.ErrDMUnavailable, nil)
			return
		}
		documentID := doc.DocumentID

		// Enrich context with document_id.
		ctx = logger.WithDocumentID(ctx, documentID)
		r = r.WithContext(ctx)

		h.log.Info(ctx, "document created in DM", "document_id", documentID)

		// ---------------------------------------------------------------
		// Step 11: Create version in DM
		// ---------------------------------------------------------------
		ver, err := h.dm.CreateVersion(ctx, documentID, CreateVersionRequest{
			SourceFileKey:      s3Key,
			SourceFileName:     sanitizedName,
			SourceFileSize:     header.Size,
			SourceFileChecksum: checksum,
			OriginType:         "UPLOAD",
		})
		if err != nil {
			// Log with orphan_document_id so operations can sweep orphaned
			// documents that were created but never got a version.
			h.log.Error(ctx, "DM CreateVersion failed; orphan document created",
				logger.ErrorAttr(err),
				"orphan_document_id", documentID,
			)
			h.cleanupS3(ctx, s3Key)
			model.WriteError(w, r, model.ErrDMUnavailable, nil)
			return
		}
		versionID := ver.VersionID
		versionNumber := ver.VersionNumber

		// Enrich context with version_id.
		ctx = logger.WithVersionID(ctx, versionID)
		r = r.WithContext(ctx)

		h.log.Info(ctx, "version created in DM",
			"version_id", versionID,
			"version_number", versionNumber,
		)

		// ---------------------------------------------------------------
		// Step 12: Publish ProcessDocumentRequested command
		// ---------------------------------------------------------------
		cmd := ProcessDocumentCommand{
			JobID:              jobID,
			DocumentID:         documentID,
			VersionID:          versionID,
			OrganizationID:     ac.OrganizationID,
			RequestedByUserID:  ac.UserID,
			SourceFileKey:      s3Key,
			SourceFileName:     sanitizedName,
			SourceFileSize:     header.Size,
			SourceFileChecksum: checksum,
			SourceFileMIMEType: contentTypePDF,
		}
		if err := h.publisher.PublishProcessDocument(ctx, cmd); err != nil {
			// CRITICAL: version exists in DM with PENDING status but the
			// processing command was not delivered. Do NOT roll back — the
			// document is in a consistent state, just not progressing.
			// Operations should investigate and manually re-publish.
			h.log.Error(ctx, "CRITICAL: failed to publish ProcessDocumentRequested command",
				logger.ErrorAttr(err),
				"document_id", documentID,
				"version_id", versionID,
				"job_id", jobID,
			)
			model.WriteError(w, r, model.ErrBrokerUnavailable, nil)
			return
		}

		h.log.Info(ctx, "ProcessDocumentRequested command published")

		// ---------------------------------------------------------------
		// Step 13: Save upload tracking in Redis
		// ---------------------------------------------------------------
		h.saveTracking(ctx, ac.OrganizationID, jobID, documentID, versionID, versionNumber, ac.UserID)

		// ---------------------------------------------------------------
		// Step 14: Return 202 Accepted
		// ---------------------------------------------------------------
		resp := UploadResponse{
			ContractID:    documentID,
			VersionID:     versionID,
			VersionNumber: versionNumber,
			JobID:         jobID,
			Status:        "UPLOADED",
			Message:       "Документ загружен и отправлен на обработку.",
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
// Internal helpers
// ---------------------------------------------------------------------------

// uploadWithChecksum uploads the file to S3 while computing a SHA-256 checksum.
// The file must implement io.ReadSeeker for retry support in the S3 client.
//
// Implementation: wraps the file in a checksumReadSeeker that computes the hash
// as data is read. After the S3 client finishes reading (including any retries),
// the final hash is returned.
func (h *Handler) uploadWithChecksum(
	ctx context.Context,
	key string,
	file multipart.File,
	size int64,
) (string, error) {
	hasher := sha256.New()
	crs := &checksumReadSeeker{
		rs:     file,
		hasher: hasher,
	}

	if err := h.storage.PutObject(ctx, key, crs, contentTypePDF); err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// checksumReadSeeker wraps an io.ReadSeeker and computes a hash while reading.
// On Seek(0, SeekStart), the hash is reset so that retries produce a correct
// final checksum.
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

// Seek delegates to the underlying reader and resets the hash when the
// reader is rewound to the beginning. This supports the S3 client's retry
// pattern: Seek(0, SeekStart) before re-reading. Non-zero seeks do NOT
// reset the hash — callers relying on the checksum MUST only retry from
// offset 0.
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

// cleanupS3 deletes the uploaded object from S3 as a compensating action.
// Errors are logged but not propagated — the original error takes priority.
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

// saveTracking writes the upload tracking record to Redis. Failures are logged
// at WARN level but do not fail the overall upload operation because tracking
// data is non-critical.
func (h *Handler) saveTracking(
	ctx context.Context,
	orgID, jobID, documentID, versionID string,
	versionNumber int,
	userID string,
) {
	key := fmt.Sprintf("upload:%s:%s", orgID, jobID)
	record := uploadTracking{
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

// sanitizeFilename cleans a filename for safe storage:
//   - normalizes backslashes to forward slashes (handles Windows paths)
//   - extracts the base name (removes directory path components)
//   - removes null bytes and control characters (0x00-0x1F, 0x7F)
//   - removes path traversal sequences (../, ..\, ..)
//   - truncates to maxFilenameLen bytes
//   - falls back to "upload.pdf" if the result is empty or "."
func sanitizeFilename(name string) string {
	// Normalize Windows backslash separators to forward slashes so that
	// filepath.Base (which is OS-dependent) works correctly on all platforms.
	name = strings.ReplaceAll(name, "\\", "/")

	// Extract base name to strip directory components.
	name = filepath.Base(name)

	// Remove null bytes and control characters.
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		if r == 0 {
			continue
		}
		if r < 0x20 || r == 0x7F {
			continue
		}
		b.WriteRune(r)
	}
	name = b.String()

	// Remove path traversal sequences.
	name = strings.ReplaceAll(name, "../", "")
	name = strings.ReplaceAll(name, "..\\", "")
	name = strings.ReplaceAll(name, "..", "")

	// Trim whitespace that may result from cleaning.
	name = strings.TrimSpace(name)

	// Truncate to maxFilenameLen bytes at a rune boundary to avoid
	// breaking multi-byte UTF-8 sequences (Cyrillic, CJK, etc.).
	if len(name) > maxFilenameLen {
		truncated := name
		for len(truncated) > maxFilenameLen {
			_, size := utf8.DecodeLastRuneInString(truncated)
			truncated = truncated[:len(truncated)-size]
		}
		name = truncated
	}

	// Fallback for empty names or bare dot (filepath.Base returns "." for
	// empty input and for ".").
	if name == "" || name == "." {
		name = "upload.pdf"
	}

	return name
}

// containsControlChars returns true if s contains any C0 control characters
// (0x00-0x1F) or DEL (0x7F). Newlines and tabs are included because they are
// inappropriate in a document title.
func containsControlChars(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7F {
			return true
		}
	}
	return false
}
