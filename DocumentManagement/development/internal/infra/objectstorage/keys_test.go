package objectstorage

import (
	"testing"

	"contractpro/document-management/internal/domain/model"

	"github.com/stretchr/testify/assert"
)

func TestArtifactKey(t *testing.T) {
	got := ArtifactKey("org-1", "doc-2", "ver-3", model.ArtifactTypeSemanticTree)
	assert.Equal(t, "org-1/doc-2/ver-3/SEMANTIC_TREE", got)
}

func TestDiffKey(t *testing.T) {
	got := DiffKey("org-1", "doc-2", "ver-a", "ver-b")
	assert.Equal(t, "org-1/doc-2/diffs/ver-a_ver-b", got)
}

func TestVersionPrefix(t *testing.T) {
	got := VersionPrefix("org-1", "doc-2", "ver-3")
	assert.Equal(t, "org-1/doc-2/ver-3/", got)
	assert.Equal(t, '/', rune(got[len(got)-1]), "must end with /")
}

func TestDocumentPrefix(t *testing.T) {
	got := DocumentPrefix("org-1", "doc-2")
	assert.Equal(t, "org-1/doc-2/", got)
	assert.Equal(t, '/', rune(got[len(got)-1]), "must end with /")
}

func TestContentTypeForArtifact_JSON(t *testing.T) {
	jsonTypes := []model.ArtifactType{
		model.ArtifactTypeOCRRaw,
		model.ArtifactTypeExtractedText,
		model.ArtifactTypeDocumentStructure,
		model.ArtifactTypeSemanticTree,
		model.ArtifactTypeProcessingWarnings,
		model.ArtifactTypeClassificationResult,
		model.ArtifactTypeKeyParameters,
		model.ArtifactTypeRiskAnalysis,
		model.ArtifactTypeRiskProfile,
		model.ArtifactTypeRecommendations,
		model.ArtifactTypeSummary,
		model.ArtifactTypeDetailedReport,
		model.ArtifactTypeAggregateScore,
	}
	for _, at := range jsonTypes {
		assert.Equal(t, ContentTypeJSON, ContentTypeForArtifact(at), "expected JSON content type for %s", at)
	}
}

func TestContentTypeForArtifact_PDF(t *testing.T) {
	assert.Equal(t, ContentTypePDF, ContentTypeForArtifact(model.ArtifactTypeExportPDF))
}

func TestContentTypeForArtifact_DOCX(t *testing.T) {
	assert.Equal(t, ContentTypeDOCX, ContentTypeForArtifact(model.ArtifactTypeExportDOCX))
}

func TestContentTypeForArtifact_AllTypes(t *testing.T) {
	for _, at := range model.AllArtifactTypes {
		ct := ContentTypeForArtifact(at)
		assert.NotEmpty(t, ct, "content type must not be empty for %s", at)
	}
}
