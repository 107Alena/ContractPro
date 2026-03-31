package model

import "time"

// ArtifactType represents the type of artifact attached to a document version.
type ArtifactType string

const (
	// DP (Document Processing) artifacts.
	ArtifactTypeOCRRaw             ArtifactType = "OCR_RAW"
	ArtifactTypeExtractedText      ArtifactType = "EXTRACTED_TEXT"
	ArtifactTypeDocumentStructure  ArtifactType = "DOCUMENT_STRUCTURE"
	ArtifactTypeSemanticTree       ArtifactType = "SEMANTIC_TREE"
	ArtifactTypeProcessingWarnings ArtifactType = "PROCESSING_WARNINGS"

	// LIC (Legal Intelligence Core) artifacts.
	ArtifactTypeClassificationResult ArtifactType = "CLASSIFICATION_RESULT"
	ArtifactTypeKeyParameters        ArtifactType = "KEY_PARAMETERS"
	ArtifactTypeRiskAnalysis         ArtifactType = "RISK_ANALYSIS"
	ArtifactTypeRiskProfile          ArtifactType = "RISK_PROFILE"
	ArtifactTypeRecommendations      ArtifactType = "RECOMMENDATIONS"
	ArtifactTypeSummary              ArtifactType = "SUMMARY"
	ArtifactTypeDetailedReport       ArtifactType = "DETAILED_REPORT"
	ArtifactTypeAggregateScore       ArtifactType = "AGGREGATE_SCORE"

	// RE (Reporting Engine) artifacts.
	ArtifactTypeExportPDF  ArtifactType = "EXPORT_PDF"
	ArtifactTypeExportDOCX ArtifactType = "EXPORT_DOCX"
)

// AllArtifactTypes returns all valid artifact types.
var AllArtifactTypes = []ArtifactType{
	ArtifactTypeOCRRaw,
	ArtifactTypeExtractedText,
	ArtifactTypeDocumentStructure,
	ArtifactTypeSemanticTree,
	ArtifactTypeProcessingWarnings,
	ArtifactTypeClassificationResult,
	ArtifactTypeKeyParameters,
	ArtifactTypeRiskAnalysis,
	ArtifactTypeRiskProfile,
	ArtifactTypeRecommendations,
	ArtifactTypeSummary,
	ArtifactTypeDetailedReport,
	ArtifactTypeAggregateScore,
	ArtifactTypeExportPDF,
	ArtifactTypeExportDOCX,
}

// ProducerDomain represents the domain that created the artifact.
type ProducerDomain string

const (
	ProducerDomainDP  ProducerDomain = "DP"
	ProducerDomainLIC ProducerDomain = "LIC"
	ProducerDomainRE  ProducerDomain = "RE"
)

// AllProducerDomains returns all valid producer domains.
var AllProducerDomains = []ProducerDomain{
	ProducerDomainDP,
	ProducerDomainLIC,
	ProducerDomainRE,
}

// ArtifactTypesByProducer maps each producer domain to its artifact types.
var ArtifactTypesByProducer = map[ProducerDomain][]ArtifactType{
	ProducerDomainDP: {
		ArtifactTypeOCRRaw,
		ArtifactTypeExtractedText,
		ArtifactTypeDocumentStructure,
		ArtifactTypeSemanticTree,
		ArtifactTypeProcessingWarnings,
	},
	ProducerDomainLIC: {
		ArtifactTypeClassificationResult,
		ArtifactTypeKeyParameters,
		ArtifactTypeRiskAnalysis,
		ArtifactTypeRiskProfile,
		ArtifactTypeRecommendations,
		ArtifactTypeSummary,
		ArtifactTypeDetailedReport,
		ArtifactTypeAggregateScore,
	},
	ProducerDomainRE: {
		ArtifactTypeExportPDF,
		ArtifactTypeExportDOCX,
	},
}

// IsBlobArtifact returns true for artifact types stored as binary blobs (PDF, DOCX).
func (t ArtifactType) IsBlobArtifact() bool {
	return t == ArtifactTypeExportPDF || t == ArtifactTypeExportDOCX
}

// ArtifactDescriptor represents metadata for a single artifact attached to a document version.
type ArtifactDescriptor struct {
	ArtifactID     string         `json:"artifact_id"`
	VersionID      string         `json:"version_id"`
	DocumentID     string         `json:"document_id"`
	OrganizationID string         `json:"organization_id"`
	ArtifactType   ArtifactType   `json:"artifact_type"`
	ProducerDomain ProducerDomain `json:"producer_domain"`
	StorageKey     string         `json:"storage_key"`
	SizeBytes      int64          `json:"size_bytes"`
	ContentHash    string         `json:"content_hash"`
	SchemaVersion  string         `json:"schema_version"`
	JobID          string         `json:"job_id"`
	CorrelationID  string         `json:"correlation_id"`
	CreatedAt      time.Time      `json:"created_at"`
}

// NewArtifactDescriptor creates a new ArtifactDescriptor.
func NewArtifactDescriptor(
	artifactID, versionID, documentID, organizationID string,
	artifactType ArtifactType,
	producerDomain ProducerDomain,
	storageKey string,
	sizeBytes int64,
	contentHash, schemaVersion, jobID, correlationID string,
) *ArtifactDescriptor {
	return &ArtifactDescriptor{
		ArtifactID:     artifactID,
		VersionID:      versionID,
		DocumentID:     documentID,
		OrganizationID: organizationID,
		ArtifactType:   artifactType,
		ProducerDomain: producerDomain,
		StorageKey:     storageKey,
		SizeBytes:      sizeBytes,
		ContentHash:    contentHash,
		SchemaVersion:  schemaVersion,
		JobID:          jobID,
		CorrelationID:  correlationID,
		CreatedAt:      time.Now().UTC(),
	}
}
