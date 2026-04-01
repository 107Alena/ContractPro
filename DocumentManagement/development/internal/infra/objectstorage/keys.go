package objectstorage

import (
	"fmt"

	"contractpro/document-management/internal/domain/model"
)

// Content type constants for artifact storage.
const (
	ContentTypeJSON = "application/json"
	ContentTypePDF  = "application/pdf"
	ContentTypeDOCX = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
)

// ContentTypeForArtifact returns the HTTP content type for the given artifact type.
// Binary exports (PDF, DOCX) use their respective MIME types; all other
// artifacts (structured JSON data) use application/json.
func ContentTypeForArtifact(artifactType model.ArtifactType) string {
	switch artifactType {
	case model.ArtifactTypeExportPDF:
		return ContentTypePDF
	case model.ArtifactTypeExportDOCX:
		return ContentTypeDOCX
	default:
		return ContentTypeJSON
	}
}

// ArtifactKey builds the S3 object key for an artifact.
// Format: {organization_id}/{document_id}/{version_id}/{artifact_type}
func ArtifactKey(organizationID, documentID, versionID string, artifactType model.ArtifactType) string {
	return fmt.Sprintf("%s/%s/%s/%s", organizationID, documentID, versionID, string(artifactType))
}

// DiffKey builds the S3 object key for a version diff blob.
// Format: {organization_id}/{document_id}/diffs/{base_version_id}_{target_version_id}
func DiffKey(organizationID, documentID, baseVersionID, targetVersionID string) string {
	return fmt.Sprintf("%s/%s/diffs/%s_%s", organizationID, documentID, baseVersionID, targetVersionID)
}

// VersionPrefix returns the S3 key prefix for all objects belonging to a version.
// Used with DeleteByPrefix for version cleanup.
func VersionPrefix(organizationID, documentID, versionID string) string {
	return fmt.Sprintf("%s/%s/%s/", organizationID, documentID, versionID)
}

// DocumentPrefix returns the S3 key prefix for all objects belonging to a document.
// Used with DeleteByPrefix for document cleanup.
func DocumentPrefix(organizationID, documentID string) string {
	return fmt.Sprintf("%s/%s/", organizationID, documentID)
}
