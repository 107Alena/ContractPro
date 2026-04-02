package port

import (
	"context"
	"io"
	"time"

	"contractpro/document-management/internal/domain/model"
)

// ---------------------------------------------------------------------------
// Outbound ports: called by application services and implemented by
// infrastructure adapters.
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Transactor — unit-of-work abstraction for database transactions.
// ---------------------------------------------------------------------------

// Transactor executes a function within a database transaction.
// If the function returns nil, the transaction is committed.
// If the function returns an error, the transaction is rolled back.
// The context passed to fn carries the transaction handle as a context value;
// all repository methods called within the function must use this context.
// Implemented by: PostgreSQL adapter (infra layer).
type Transactor interface {
	WithTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

// ---------------------------------------------------------------------------
// Metadata repositories — one per aggregate root, all tenant-isolated.
// ---------------------------------------------------------------------------

// DocumentRepository provides CRUD for Document aggregates.
// All methods require organizationID for tenant isolation.
// Implemented by: PostgreSQL adapter (infra layer).
type DocumentRepository interface {
	// Insert creates a new document record.
	// Returns ErrCodeDocumentAlreadyExists on primary key conflict.
	Insert(ctx context.Context, doc *model.Document) error

	// FindByID retrieves a document by organization and document ID.
	// Returns ErrCodeDocumentNotFound if not found.
	FindByID(ctx context.Context, organizationID, documentID string) (*model.Document, error)

	// List returns a paginated list of documents for the organization,
	// optionally filtered by status.
	List(ctx context.Context, organizationID string, statusFilter *model.DocumentStatus, page, pageSize int) ([]*model.Document, int, error)

	// Update persists changes to an existing document (status, current_version_id, timestamps).
	// Returns ErrCodeDocumentNotFound if the document does not exist.
	Update(ctx context.Context, doc *model.Document) error

	// ExistsByID returns true if a document exists for the given organization.
	ExistsByID(ctx context.Context, organizationID, documentID string) (bool, error)
}

// VersionRepository provides CRUD for DocumentVersion entities.
// All methods require organizationID for tenant isolation.
// Implemented by: PostgreSQL adapter (infra layer).
type VersionRepository interface {
	// Insert creates a new version record.
	// Returns ErrCodeVersionAlreadyExists on primary key conflict.
	Insert(ctx context.Context, version *model.DocumentVersion) error

	// FindByID retrieves a version by organization, document, and version ID.
	// Returns ErrCodeVersionNotFound if not found.
	FindByID(ctx context.Context, organizationID, documentID, versionID string) (*model.DocumentVersion, error)

	// List returns a paginated list of versions for a document, ordered by version_number descending.
	List(ctx context.Context, organizationID, documentID string, page, pageSize int) ([]*model.DocumentVersion, int, error)

	// Update persists changes to a version (artifact_status transitions).
	// Returns ErrCodeVersionNotFound if the version does not exist.
	Update(ctx context.Context, version *model.DocumentVersion) error

	// NextVersionNumber returns the next sequential version number for a document.
	NextVersionNumber(ctx context.Context, organizationID, documentID string) (int, error)
}

// ArtifactRepository provides CRUD for ArtifactDescriptor entities.
// All methods require organizationID for tenant isolation.
// Implemented by: PostgreSQL adapter (infra layer).
type ArtifactRepository interface {
	// Insert creates a new artifact descriptor.
	// Returns ErrCodeArtifactAlreadyExists on primary key conflict or
	// unique constraint violation (version_id + artifact_type).
	Insert(ctx context.Context, descriptor *model.ArtifactDescriptor) error

	// FindByVersionAndType retrieves an artifact descriptor by version and type.
	// Returns ErrCodeArtifactNotFound if not found.
	FindByVersionAndType(ctx context.Context, organizationID, documentID, versionID string, artifactType model.ArtifactType) (*model.ArtifactDescriptor, error)

	// ListByVersion returns all artifact descriptors for a document version.
	ListByVersion(ctx context.Context, organizationID, documentID, versionID string) ([]*model.ArtifactDescriptor, error)

	// ListByVersionAndTypes returns artifact descriptors for the specified types.
	// Missing types are silently omitted (caller checks for completeness).
	ListByVersionAndTypes(ctx context.Context, organizationID, documentID, versionID string, artifactTypes []model.ArtifactType) ([]*model.ArtifactDescriptor, error)

	// DeleteByVersion removes all artifact descriptors for a version (used in cascade delete).
	DeleteByVersion(ctx context.Context, organizationID, documentID, versionID string) error
}

// DiffRepository provides CRUD for VersionDiffReference entities.
// All methods require organizationID for tenant isolation.
// Implemented by: PostgreSQL adapter (infra layer).
type DiffRepository interface {
	// Insert creates a new diff reference.
	// Returns ErrCodeDiffAlreadyExists on conflict (same version pair).
	Insert(ctx context.Context, ref *model.VersionDiffReference) error

	// FindByVersionPair retrieves a diff reference by base and target versions.
	// Returns ErrCodeDiffNotFound if not found.
	FindByVersionPair(ctx context.Context, organizationID, documentID, baseVersionID, targetVersionID string) (*model.VersionDiffReference, error)

	// ListByDocument returns all diff references for a document.
	ListByDocument(ctx context.Context, organizationID, documentID string) ([]*model.VersionDiffReference, error)

	// DeleteByDocument removes all diff references for a document (used in cascade delete).
	DeleteByDocument(ctx context.Context, organizationID, documentID string) error
}

// AuditRepository provides append-only storage and retrieval for AuditRecord entries.
// All methods require organizationID for tenant isolation.
// Implemented by: PostgreSQL adapter (infra layer).
type AuditRepository interface {
	// Insert creates a new audit record.
	Insert(ctx context.Context, record *model.AuditRecord) error

	// List returns audit records matching the given filter, ordered by created_at descending.
	List(ctx context.Context, params AuditListParams) ([]*model.AuditRecord, int, error)
}

// AuditListParams holds the filter and pagination parameters for listing audit records.
type AuditListParams struct {
	OrganizationID string
	DocumentID     string              // optional: filter by document
	VersionID      string              // optional: filter by version
	Action         *model.AuditAction  // optional: filter by action type
	ActorID        string              // optional: filter by actor (user_id or domain name)
	ActorType      *model.ActorType    // optional: filter by actor type
	Since          *time.Time          // optional: records after this timestamp
	Until          *time.Time          // optional: records before this timestamp
	Page           int                 // 1-based page number
	PageSize       int
}

// OutboxRepository provides transactional outbox pattern support.
// Events are written to the outbox table within the same database transaction
// as the business data. A separate relay process reads and publishes them.
// Implemented by: PostgreSQL adapter (infra layer).
type OutboxRepository interface {
	// Insert writes one or more outbox entries within the current transaction.
	Insert(ctx context.Context, entries ...OutboxEntry) error

	// FetchUnpublished retrieves up to limit outbox entries that have not been
	// published yet, ordered by aggregate_id and created_at for FIFO per-aggregate ordering.
	FetchUnpublished(ctx context.Context, limit int) ([]OutboxEntry, error)

	// MarkPublished marks the specified outbox entries as published (CONFIRMED).
	MarkPublished(ctx context.Context, ids []string) error

	// DeletePublished removes up to limit entries marked as published that are
	// older than the given threshold. Returns the number of deleted entries.
	// A limit of 0 means delete all matching entries (no limit).
	DeletePublished(ctx context.Context, olderThan time.Time, limit int) (int64, error)

	// PendingStats returns the count of PENDING entries and the age in seconds
	// of the oldest PENDING entry. Used by the outbox metrics collector (REV-022).
	// Returns (0, 0, nil) if there are no pending entries.
	PendingStats(ctx context.Context) (count int64, oldestAgeSeconds float64, err error)
}

// OutboxEntry represents a single event in the transactional outbox table.
type OutboxEntry struct {
	ID          string    // unique entry ID (UUID)
	AggregateID string    // partition key for FIFO ordering (= version_id, REV-010)
	Topic       string    // target broker topic
	Payload     []byte    // serialized event payload (JSON)
	Status      string    // PENDING or CONFIRMED
	CreatedAt   time.Time // when the entry was created
	PublishedAt time.Time // when the entry was published (zero if not yet published)
}

// ---------------------------------------------------------------------------
// Object storage — S3-compatible blob store for artifact content.
// ---------------------------------------------------------------------------

// ObjectStoragePort provides access to S3-compatible object storage for
// artifact content, diff blobs, and export files.
// Implemented by: Yandex Object Storage / S3 adapter (infra layer).
type ObjectStoragePort interface {
	// PutObject uploads an object to the specified key.
	PutObject(ctx context.Context, key string, data io.Reader, contentType string) error

	// GetObject retrieves an object by key.
	// The caller must close the returned ReadCloser.
	// Returns ErrCodeStorageFailed if the object does not exist or on infrastructure failure.
	GetObject(ctx context.Context, key string) (io.ReadCloser, error)

	// DeleteObject removes a single object by key.
	DeleteObject(ctx context.Context, key string) error

	// HeadObject checks if an object exists and returns its size.
	// Returns exists=false if the object does not exist (not an error).
	// Returns a non-nil error only on infrastructure failures.
	HeadObject(ctx context.Context, key string) (sizeBytes int64, exists bool, err error)

	// GeneratePresignedURL generates a time-limited URL for direct client download.
	GeneratePresignedURL(ctx context.Context, key string, expiry time.Duration) (string, error)

	// DeleteByPrefix removes all objects with the given key prefix (batch cleanup).
	// Best-effort: on partial failure, already-deleted objects are not restored;
	// the returned error indicates that some objects may remain.
	DeleteByPrefix(ctx context.Context, prefix string) error
}

// ---------------------------------------------------------------------------
// Confirmation publisher — publishes confirmation/response events back to
// the originating domain (DP, LIC, RE) after DM processes their request.
// ---------------------------------------------------------------------------

// ConfirmationPublisherPort publishes confirmation/response events back to
// the originating domain (DP, LIC, RE) after DM processes their request.
// Implemented by: Confirmation Publisher (egress layer).
type ConfirmationPublisherPort interface {
	PublishDPArtifactsPersisted(ctx context.Context, event model.DocumentProcessingArtifactsPersisted) error
	PublishDPArtifactsPersistFailed(ctx context.Context, event model.DocumentProcessingArtifactsPersistFailed) error
	PublishSemanticTreeProvided(ctx context.Context, event model.SemanticTreeProvided) error
	PublishArtifactsProvided(ctx context.Context, event model.ArtifactsProvided) error
	PublishDiffPersisted(ctx context.Context, event model.DocumentVersionDiffPersisted) error
	PublishDiffPersistFailed(ctx context.Context, event model.DocumentVersionDiffPersistFailed) error
	PublishLICArtifactsPersisted(ctx context.Context, event model.LegalAnalysisArtifactsPersisted) error
	PublishLICArtifactsPersistFailed(ctx context.Context, event model.LegalAnalysisArtifactsPersistFailed) error
	PublishREReportsPersisted(ctx context.Context, event model.ReportsArtifactsPersisted) error
	PublishREReportsPersistFailed(ctx context.Context, event model.ReportsArtifactsPersistFailed) error
}

// ---------------------------------------------------------------------------
// Notification publisher — publishes notification events to downstream
// domains and the orchestrator after DM completes internal state transitions.
// ---------------------------------------------------------------------------

// NotificationPublisherPort publishes notification events to downstream
// domains and the orchestrator after DM completes internal state transitions.
// Implemented by: Notification Publisher (egress layer).
type NotificationPublisherPort interface {
	PublishVersionProcessingArtifactsReady(ctx context.Context, event model.VersionProcessingArtifactsReady) error
	PublishVersionAnalysisArtifactsReady(ctx context.Context, event model.VersionAnalysisArtifactsReady) error
	PublishVersionReportsReady(ctx context.Context, event model.VersionReportsReady) error
	PublishVersionCreated(ctx context.Context, event model.VersionCreated) error
	PublishVersionPartiallyAvailable(ctx context.Context, event model.VersionPartiallyAvailable) error
}

// ---------------------------------------------------------------------------
// Message broker — event publishing for inter-domain communication.
// ---------------------------------------------------------------------------

// BrokerPublisherPort publishes events to the message broker.
// In the recommended architecture, application services write to the outbox
// (OutboxRepository) within the DB transaction, and a relay process uses
// BrokerPublisherPort to publish. Direct usage is also supported for
// non-transactional scenarios (e.g., DLQ).
// Implemented by: RabbitMQ adapter (infra layer).
type BrokerPublisherPort interface {
	// Publish sends an event to the specified topic.
	Publish(ctx context.Context, topic string, payload []byte) error
}

// ---------------------------------------------------------------------------
// Idempotency store — event deduplication via key-value store with TTL.
// ---------------------------------------------------------------------------

// IdempotencyStorePort provides event deduplication for incoming event handlers.
// Implemented by: Redis adapter (infra layer).
type IdempotencyStorePort interface {
	// Get retrieves the idempotency record for the given key.
	// Returns nil, nil if the key does not exist.
	Get(ctx context.Context, key string) (*model.IdempotencyRecord, error)

	// Set creates or updates an idempotency record with a TTL.
	Set(ctx context.Context, record *model.IdempotencyRecord, ttl time.Duration) error

	// SetNX atomically sets the idempotency record only if the key does not exist.
	// Returns true if the key was set (caller claimed it), false if the key already exists.
	// Used by the idempotency guard to atomically claim a PROCESSING lock.
	SetNX(ctx context.Context, record *model.IdempotencyRecord, ttl time.Duration) (bool, error)

	// Delete removes an idempotency record (used for cleanup on failure).
	Delete(ctx context.Context, key string) error
}

// ---------------------------------------------------------------------------
// Audit logging — application-level convenience port.
// ---------------------------------------------------------------------------

// AuditPort provides a high-level interface for recording audit events.
// Wraps AuditRepository to generate IDs and set timestamps automatically.
// Implemented by: Audit Service (application layer) or direct adapter.
type AuditPort interface {
	// Record persists a single audit record.
	Record(ctx context.Context, record *model.AuditRecord) error

	// List returns audit records matching the given filter.
	List(ctx context.Context, params AuditListParams) ([]*model.AuditRecord, int, error)
}

// ---------------------------------------------------------------------------
// Dead letter queue — failed messages for post-mortem analysis.
// ---------------------------------------------------------------------------

// DLQPort publishes failed messages to a Dead Letter Queue for post-mortem
// analysis and potential reprocessing.
// Implemented by: DLQ Sender (egress layer).
type DLQPort interface {
	// SendToDLQ publishes a failed message record to the dead letter queue.
	SendToDLQ(ctx context.Context, record model.DLQRecord) error
}

// ---------------------------------------------------------------------------
// Document fallback resolver — cross-tenant lookup for backward compatibility.
// ---------------------------------------------------------------------------

// DocumentFallbackResolver provides cross-tenant document lookup for backward
// compatibility with DP versions that don't send version_id or organization_id
// in events (REV-001/REV-002).
//
// TEMPORARY: this port exists until DP TASK-056 and TASK-057 are completed
// and all producer domains include version_id and organization_id in events.
//
// Implemented by: PostgreSQL adapter (infra layer).
type DocumentFallbackResolver interface {
	// ResolveByDocumentID retrieves organization_id and current_version_id
	// for a document by its document_id alone (no tenant filter).
	// Returns ErrCodeDocumentNotFound if the document does not exist.
	ResolveByDocumentID(ctx context.Context, documentID string) (organizationID string, currentVersionID string, err error)
}
