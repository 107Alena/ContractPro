package port

import (
	"context"

	"contractpro/document-management/internal/domain/model"
)

// ---------------------------------------------------------------------------
// Inbound ports: called by the ingress layer (REST handlers, event consumers)
// and implemented by application services.
// ---------------------------------------------------------------------------

// DocumentLifecycleHandler manages the lifecycle of documents: creation,
// retrieval, listing, archiving, and deletion.
// Implemented by: Document Lifecycle Service (application layer).
// Called by: REST API handlers (ingress layer).
type DocumentLifecycleHandler interface {
	// CreateDocument creates a new document in ACTIVE status.
	CreateDocument(ctx context.Context, params CreateDocumentParams) (*model.Document, error)

	// GetDocument returns a document by ID within the given organization.
	// Returns ErrCodeDocumentNotFound if the document does not exist.
	GetDocument(ctx context.Context, organizationID, documentID string) (*model.Document, error)

	// ListDocuments returns a paginated list of documents for an organization,
	// optionally filtered by status.
	ListDocuments(ctx context.Context, params ListDocumentsParams) (*PageResult[*model.Document], error)

	// ArchiveDocument transitions a document from ACTIVE to ARCHIVED.
	// Returns ErrCodeDocumentNotFound or ErrCodeStatusTransition on failure.
	ArchiveDocument(ctx context.Context, organizationID, documentID string) error

	// DeleteDocument transitions a document to DELETED (soft delete).
	// Returns ErrCodeDocumentNotFound or ErrCodeStatusTransition on failure.
	DeleteDocument(ctx context.Context, organizationID, documentID string) error
}

// CreateDocumentParams holds the input for creating a new document.
type CreateDocumentParams struct {
	OrganizationID string
	Title          string
	CreatedByUserID string
}

// ListDocumentsParams holds the input for listing documents with pagination and filtering.
type ListDocumentsParams struct {
	OrganizationID string
	StatusFilter   *model.DocumentStatus // nil means no filter
	Page           int                   // 1-based page number
	PageSize       int
}

// PageResult is a generic pagination wrapper.
type PageResult[T any] struct {
	Items      []T
	TotalCount int
	Page       int
	PageSize   int
}

// VersionManagementHandler manages document version creation and retrieval.
// Implemented by: Version Management Service (application layer).
// Called by: REST API handlers (ingress layer).
type VersionManagementHandler interface {
	// CreateVersion creates a new document version.
	// Validates that the document exists and is in ACTIVE status.
	// Returns ErrCodeDocumentNotFound or ErrCodeInvalidStatus on failure.
	CreateVersion(ctx context.Context, params CreateVersionParams) (*model.DocumentVersion, error)

	// GetVersion returns a specific version by ID within the given organization and document.
	// Returns ErrCodeVersionNotFound if the version does not exist.
	GetVersion(ctx context.Context, organizationID, documentID, versionID string) (*model.DocumentVersion, error)

	// ListVersions returns a paginated list of versions for a document.
	ListVersions(ctx context.Context, params ListVersionsParams) (*PageResult[*model.DocumentVersion], error)
}

// CreateVersionParams holds the input for creating a new document version.
type CreateVersionParams struct {
	OrganizationID     string
	DocumentID         string
	ParentVersionID    string // empty for the first version
	OriginType         model.OriginType
	OriginDescription  string
	SourceFileKey      string
	SourceFileName     string
	SourceFileSize     int64
	SourceFileChecksum string
	CreatedByUserID    string
}

// ListVersionsParams holds the input for listing versions with pagination.
type ListVersionsParams struct {
	OrganizationID string
	DocumentID     string
	Page           int // 1-based page number
	PageSize       int
}

// ArtifactIngestionHandler receives artifact payloads from producer domains
// (DP, LIC, RE) via event consumers and persists them into storage.
// Implemented by: Artifact Ingestion Service (application layer).
// Called by: Event consumers (ingress layer).
type ArtifactIngestionHandler interface {
	// HandleDPArtifacts processes a DP artifacts-ready event: validates the
	// version, stores each artifact in object storage, creates artifact
	// descriptors in the metadata store, transitions artifact status, and
	// publishes confirmation/notification events.
	HandleDPArtifacts(ctx context.Context, event model.DocumentProcessingArtifactsReady) error

	// HandleLICArtifacts processes a LIC analysis artifacts-ready event.
	HandleLICArtifacts(ctx context.Context, event model.LegalAnalysisArtifactsReady) error

	// HandleREArtifacts processes a RE reports artifacts-ready event.
	// Uses the claim-check pattern: binary blobs are already in object storage;
	// DM only records the artifact descriptors.
	HandleREArtifacts(ctx context.Context, event model.ReportsArtifactsReady) error
}

// ArtifactQueryHandler serves artifact retrieval requests from other domains
// (async via events) and from the REST API (sync).
// Implemented by: Artifact Query Service (application layer).
// Called by: Event consumers (ingress layer), REST API handlers.
type ArtifactQueryHandler interface {
	// HandleGetSemanticTree processes a GetSemanticTreeRequest from DP.
	// Retrieves the semantic tree artifact and publishes a SemanticTreeProvided response.
	HandleGetSemanticTree(ctx context.Context, event model.GetSemanticTreeRequest) error

	// HandleGetArtifacts processes a GetArtifactsRequest from LIC/RE.
	// Retrieves the requested artifacts and publishes an ArtifactsProvided response.
	HandleGetArtifacts(ctx context.Context, event model.GetArtifactsRequest) error

	// GetArtifact retrieves a single artifact's content by type (sync API).
	// Returns the raw content, its MIME content type, and any error.
	// Returns ErrCodeArtifactNotFound if the artifact does not exist.
	GetArtifact(ctx context.Context, params GetArtifactParams) (*ArtifactContent, error)

	// ListArtifacts returns all artifact descriptors for a document version.
	ListArtifacts(ctx context.Context, organizationID, documentID, versionID string) ([]*model.ArtifactDescriptor, error)
}

// GetArtifactParams holds the input for retrieving a single artifact.
type GetArtifactParams struct {
	OrganizationID string
	DocumentID     string
	VersionID      string
	ArtifactType   model.ArtifactType
}

// ArtifactContent holds the raw content and metadata for a retrieved artifact.
type ArtifactContent struct {
	Content     []byte
	ContentType string
}

// DiffStorageHandler receives diff results from DP and serves diff queries.
// Implemented by: Diff Storage Service (application layer).
// Called by: Event consumers (ingress layer), REST API handlers.
type DiffStorageHandler interface {
	// HandleDiffReady processes a DocumentVersionDiffReady event from DP:
	// stores the diff content in object storage, creates a VersionDiffReference
	// in the metadata store, and publishes a confirmation event.
	HandleDiffReady(ctx context.Context, event model.DocumentVersionDiffReady) error

	// GetDiff retrieves the diff reference and content for a version pair.
	// Returns the reference metadata, raw diff content, and any error.
	// Returns ErrCodeDiffNotFound if no diff exists for the version pair.
	GetDiff(ctx context.Context, params GetDiffParams) (*model.VersionDiffReference, []byte, error)
}

// GetDiffParams holds the input for retrieving a diff between two versions.
type GetDiffParams struct {
	OrganizationID  string
	DocumentID      string
	BaseVersionID   string
	TargetVersionID string
}
