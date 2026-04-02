package diff

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
	"contractpro/document-management/internal/egress/outbox"
	"contractpro/document-management/internal/infra/objectstorage"
)

// Logger is the minimal structured logging interface.
type Logger interface {
	Info(msg string, keysAndValues ...any)
	Warn(msg string, keysAndValues ...any)
	Error(msg string, keysAndValues ...any)
}

// DiffStorageService receives diff results from DP and persists them
// into object storage and the metadata store. It publishes confirmation
// events via the transactional outbox.
type DiffStorageService struct {
	transactor       port.Transactor
	versionRepo      port.VersionRepository
	diffRepo         port.DiffRepository
	auditRepo        port.AuditRepository
	objectStorage    port.ObjectStoragePort
	outboxWriter     *outbox.OutboxWriter
	fallbackResolver port.DocumentFallbackResolver
	logger           Logger
	newUUID          func() string
}

// Compile-time interface check.
var _ port.DiffStorageHandler = (*DiffStorageService)(nil)

// NewDiffStorageService creates a new service with the given dependencies.
// Panics if any required dependency is nil (programmer error at startup wiring).
func NewDiffStorageService(
	transactor port.Transactor,
	versionRepo port.VersionRepository,
	diffRepo port.DiffRepository,
	auditRepo port.AuditRepository,
	objectStorage port.ObjectStoragePort,
	outboxWriter *outbox.OutboxWriter,
	fallbackResolver port.DocumentFallbackResolver,
	logger Logger,
) *DiffStorageService {
	if transactor == nil {
		panic("diff: transactor must not be nil")
	}
	if versionRepo == nil {
		panic("diff: versionRepo must not be nil")
	}
	if diffRepo == nil {
		panic("diff: diffRepo must not be nil")
	}
	if auditRepo == nil {
		panic("diff: auditRepo must not be nil")
	}
	if objectStorage == nil {
		panic("diff: objectStorage must not be nil")
	}
	if outboxWriter == nil {
		panic("diff: outboxWriter must not be nil")
	}
	if fallbackResolver == nil {
		panic("diff: fallbackResolver must not be nil")
	}
	if logger == nil {
		panic("diff: logger must not be nil")
	}
	return &DiffStorageService{
		transactor:       transactor,
		versionRepo:      versionRepo,
		diffRepo:         diffRepo,
		auditRepo:        auditRepo,
		objectStorage:    objectStorage,
		outboxWriter:     outboxWriter,
		fallbackResolver: fallbackResolver,
		logger:           logger,
		newUUID:          generateUUID,
	}
}

// ---------------------------------------------------------------------------
// HandleDiffReady — store diff from DP.
// ---------------------------------------------------------------------------

// diffBlob is the merged JSON blob stored in Object Storage.
type diffBlob struct {
	TextDiffs       json.RawMessage `json:"text_diffs"`
	StructuralDiffs json.RawMessage `json:"structural_diffs"`
}

// HandleDiffReady processes a DocumentVersionDiffReady event from DP:
// validates both versions exist, stores the merged diff blob in Object
// Storage, creates a VersionDiffReference in the metadata store, and
// publishes a DiffPersisted confirmation via the transactional outbox.
//
// Idempotency (REV-028): if a diff already exists for the version pair
// (unique constraint violation), HandleDiffReady writes a DiffPersisted
// event for the current job_id without overwriting the existing diff.
func (s *DiffStorageService) HandleDiffReady(ctx context.Context, event model.DocumentVersionDiffReady) error {
	if err := ctx.Err(); err != nil {
		return port.NewTimeoutError("context cancelled before diff storage", err)
	}

	// REV-002: resolve organization_id if missing.
	if event.OrgID == "" {
		orgID, _, err := s.fallbackResolver.ResolveByDocumentID(ctx, event.DocumentID)
		if err != nil {
			s.logger.Error("REV-002 fallback: failed to resolve organization_id",
				"document_id", event.DocumentID, "error", err)
			return err
		}
		s.logger.Warn("REV-002 fallback: resolved organization_id from DB (event field was empty)",
			"document_id", event.DocumentID, "organization_id", orgID)
		event.OrgID = orgID
	}

	if err := validateDiffRequired(event.OrgID, event.JobID, event.DocumentID, event.BaseVersionID, event.TargetVersionID); err != nil {
		return err
	}

	s.logger.Info("diff storage started",
		"document_id", event.DocumentID,
		"base_version_id", event.BaseVersionID,
		"target_version_id", event.TargetVersionID,
		"job_id", event.JobID,
	)

	// Step 1: Validate both versions exist and belong to the same document.
	if _, err := s.versionRepo.FindByID(ctx, event.OrgID, event.DocumentID, event.BaseVersionID); err != nil {
		return err
	}
	if _, err := s.versionRepo.FindByID(ctx, event.OrgID, event.DocumentID, event.TargetVersionID); err != nil {
		return err
	}

	// Step 2: Idempotency pre-check (REV-028). If a diff already exists for
	// this version pair, skip the blob upload entirely and just confirm.
	// This avoids overwriting the existing blob with potentially different
	// content from a reprocessing job, preserving data integrity.
	existing, findErr := s.diffRepo.FindByVersionPair(ctx, event.OrgID, event.DocumentID,
		event.BaseVersionID, event.TargetVersionID)
	if findErr != nil && port.ErrorCode(findErr) != port.ErrCodeDiffNotFound {
		return findErr
	}
	if existing != nil {
		s.logger.Info("diff already exists for version pair, sending confirmation",
			"base_version_id", event.BaseVersionID,
			"target_version_id", event.TargetVersionID,
			"job_id", event.JobID,
		)
		meta := model.EventMeta{CorrelationID: event.CorrelationID, Timestamp: time.Now().UTC()}
		return s.transactor.WithTransaction(ctx, func(txCtx context.Context) error {
			return s.outboxWriter.Write(txCtx, event.TargetVersionID,
				model.TopicDMResponsesDiffPersisted,
				model.DocumentVersionDiffPersisted{
					EventMeta:  meta,
					JobID:      event.JobID,
					DocumentID: event.DocumentID,
				},
			)
		})
	}

	// Step 3: Merge diffs into a single blob.
	blob := diffBlob{
		TextDiffs:       ensureJSONArray(event.TextDiffs),
		StructuralDiffs: ensureJSONArray(event.StructuralDiffs),
	}
	blobData, err := json.Marshal(blob)
	if err != nil {
		return port.NewValidationError(fmt.Sprintf("failed to marshal diff blob: %v", err))
	}

	// Step 4: Upload blob to Object Storage.
	storageKey := objectstorage.DiffKey(event.OrgID, event.DocumentID, event.BaseVersionID, event.TargetVersionID)
	if err := s.objectStorage.PutObject(ctx, storageKey, bytes.NewReader(blobData), objectstorage.ContentTypeJSON); err != nil {
		return port.NewStorageError(fmt.Sprintf("put diff blob %s", storageKey), err)
	}

	contentHash := sha256Hex(blobData)
	meta := model.EventMeta{CorrelationID: event.CorrelationID, Timestamp: time.Now().UTC()}

	// Step 5: DB transaction — insert diff reference, audit, outbox.
	if err := s.transactor.WithTransaction(ctx, func(txCtx context.Context) error {
		// 5a. Insert diff reference.
		ref := model.NewVersionDiffReference(
			s.newUUID(), event.DocumentID, event.OrgID,
			event.BaseVersionID, event.TargetVersionID,
			storageKey,
			event.TextDiffCount, event.StructuralDiffCount,
			event.JobID, event.CorrelationID,
		)
		if err := s.diffRepo.Insert(txCtx, ref); err != nil {
			return err
		}

		// 5b. Insert audit record.
		auditDetails, marshalErr := json.Marshal(map[string]any{
			"base_version_id":       event.BaseVersionID,
			"target_version_id":     event.TargetVersionID,
			"text_diff_count":       event.TextDiffCount,
			"structural_diff_count": event.StructuralDiffCount,
			"storage_key":           storageKey,
			"content_hash":          contentHash,
			"size_bytes":            len(blobData),
		})
		if marshalErr != nil {
			s.logger.Warn("failed to marshal audit details", "error", marshalErr)
		}
		audit := model.NewAuditRecord(
			s.newUUID(), event.OrgID,
			model.AuditActionDiffSaved,
			model.ActorTypeDomain, string(model.ProducerDomainDP),
		).WithDocument(event.DocumentID).
			WithVersion(event.TargetVersionID).
			WithJob(event.JobID, event.CorrelationID).
			WithDetails(auditDetails)
		if err := s.auditRepo.Insert(txCtx, audit); err != nil {
			return err
		}

		// 5c. Write outbox event (DiffPersisted).
		return s.outboxWriter.Write(txCtx, event.TargetVersionID,
			model.TopicDMResponsesDiffPersisted,
			model.DocumentVersionDiffPersisted{
				EventMeta:  meta,
				JobID:      event.JobID,
				DocumentID: event.DocumentID,
			},
		)
	}); err != nil {
		s.logger.Error("diff storage transaction failed",
			"document_id", event.DocumentID,
			"base_version_id", event.BaseVersionID,
			"target_version_id", event.TargetVersionID,
			"error", err,
		)
		s.compensateDiffBlob(storageKey)
		return err
	}

	s.logger.Info("diff stored",
		"document_id", event.DocumentID,
		"base_version_id", event.BaseVersionID,
		"target_version_id", event.TargetVersionID,
		"job_id", event.JobID,
	)
	return nil
}

// ---------------------------------------------------------------------------
// GetDiff — retrieve diff reference and content.
// ---------------------------------------------------------------------------

// GetDiff retrieves the diff reference and raw blob content for a version pair.
// Returns ErrCodeDiffNotFound if no diff exists for the pair.
func (s *DiffStorageService) GetDiff(ctx context.Context, params port.GetDiffParams) (*model.VersionDiffReference, []byte, error) {
	if err := validateGetDiffParams(params); err != nil {
		return nil, nil, err
	}

	ref, err := s.diffRepo.FindByVersionPair(ctx, params.OrganizationID, params.DocumentID,
		params.BaseVersionID, params.TargetVersionID)
	if err != nil {
		return nil, nil, err
	}

	reader, err := s.objectStorage.GetObject(ctx, ref.StorageKey)
	if err != nil {
		return nil, nil, port.NewStorageError(
			fmt.Sprintf("get diff blob %s", ref.StorageKey), err)
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, nil, port.NewStorageError(
			fmt.Sprintf("read diff blob %s", ref.StorageKey), err)
	}

	return ref, data, nil
}

// ---------------------------------------------------------------------------
// Private helpers.
// ---------------------------------------------------------------------------

// validateDiffRequired checks the 5 required fields for HandleDiffReady.
func validateDiffRequired(orgID, jobID, documentID, baseVersionID, targetVersionID string) error {
	if orgID == "" {
		return port.NewValidationError("organization_id is required")
	}
	if jobID == "" {
		return port.NewValidationError("job_id is required")
	}
	if documentID == "" {
		return port.NewValidationError("document_id is required")
	}
	if baseVersionID == "" {
		return port.NewValidationError("base_version_id is required")
	}
	if targetVersionID == "" {
		return port.NewValidationError("target_version_id is required")
	}
	if baseVersionID == targetVersionID {
		return port.NewValidationError("base_version_id and target_version_id must differ")
	}
	return nil
}

// validateGetDiffParams checks the 4 required fields for GetDiff.
func validateGetDiffParams(params port.GetDiffParams) error {
	if params.OrganizationID == "" {
		return port.NewValidationError("organization_id is required")
	}
	if params.DocumentID == "" {
		return port.NewValidationError("document_id is required")
	}
	if params.BaseVersionID == "" {
		return port.NewValidationError("base_version_id is required")
	}
	if params.TargetVersionID == "" {
		return port.NewValidationError("target_version_id is required")
	}
	if params.BaseVersionID == params.TargetVersionID {
		return port.NewValidationError("base_version_id and target_version_id must differ")
	}
	return nil
}

// ensureJSONArray returns "[]" if data is nil or empty, otherwise returns data as-is.
// This ensures deterministic JSON output with empty arrays instead of null.
func ensureJSONArray(data json.RawMessage) json.RawMessage {
	if len(data) == 0 {
		return json.RawMessage("[]")
	}
	return data
}

// compensateDiffBlob deletes the uploaded blob from Object Storage on failure.
// Uses context.Background() because the original request context may have
// been cancelled. Best-effort: errors are logged but not propagated.
func (s *DiffStorageService) compensateDiffBlob(storageKey string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.objectStorage.DeleteObject(ctx, storageKey); err != nil {
		s.logger.Warn("compensation: failed to delete diff blob",
			"key", storageKey, "error", err,
		)
	}
}

// sha256Hex computes SHA-256 hex digest of data.
func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// generateUUID produces a UUID v4 using crypto/rand.
// Panics if crypto/rand fails (broken system CSPRNG — fatal condition).
func generateUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("diff: crypto/rand failed: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
