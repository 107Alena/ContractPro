package integration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"contractpro/document-management/internal/domain/model"
)

// seedDocWithVersion seeds a document whose current version has the given
// artifact_status, and (optionally) sets the document status.
func seedDocWithVersion(t *testing.T, h *testHarness, orgID, docID, versionID string, status model.ArtifactStatus, docStatus model.DocumentStatus) {
	t.Helper()
	ver := defaultVersion(orgID, docID, versionID)
	ver.ArtifactStatus = status
	h.seedVersion(ver)

	doc := defaultDocument(orgID, docID)
	doc.CurrentVersionID = versionID
	doc.Status = docStatus
	h.seedDocument(doc)
}

func TestIntegration_DocumentStats_GroupingAndNotStarted(t *testing.T) {
	h := newTestHarness(t)
	const org = "11111111-1111-1111-1111-111111111111"

	// Two ACTIVE docs PENDING, one ACTIVE FULLY_READY.
	seedDocWithVersion(t, h, org, "doc-1", "ver-1", model.ArtifactStatusPending, model.DocumentStatusActive)
	seedDocWithVersion(t, h, org, "doc-2", "ver-2", model.ArtifactStatusPending, model.DocumentStatusActive)
	seedDocWithVersion(t, h, org, "doc-3", "ver-3", model.ArtifactStatusFullyReady, model.DocumentStatusActive)

	// One ACTIVE doc without a current version → not_started.
	noVer := defaultDocument(org, "doc-4")
	noVer.Status = model.DocumentStatusActive
	h.seedDocument(noVer)

	stats, err := h.docRepo.CountCurrentVersionsByArtifactStatus(context.Background(), org, false)
	require.NoError(t, err)

	assert.Equal(t, 2, stats.ByArtifactStatus[model.ArtifactStatusPending])
	assert.Equal(t, 1, stats.ByArtifactStatus[model.ArtifactStatusFullyReady])
	assert.Equal(t, 1, stats.NotStarted)
	assert.Equal(t, 4, stats.Total)
}

func TestIntegration_DocumentStats_ArchivedAndDeleted(t *testing.T) {
	h := newTestHarness(t)
	const org = "22222222-2222-2222-2222-222222222222"

	seedDocWithVersion(t, h, org, "doc-a", "ver-a", model.ArtifactStatusPending, model.DocumentStatusActive)
	seedDocWithVersion(t, h, org, "doc-b", "ver-b", model.ArtifactStatusFullyReady, model.DocumentStatusArchived)
	seedDocWithVersion(t, h, org, "doc-c", "ver-c", model.ArtifactStatusPending, model.DocumentStatusDeleted)

	// Default: ACTIVE only. ARCHIVED + DELETED excluded.
	active, err := h.docRepo.CountCurrentVersionsByArtifactStatus(context.Background(), org, false)
	require.NoError(t, err)
	assert.Equal(t, 1, active.Total)
	assert.Equal(t, 1, active.ByArtifactStatus[model.ArtifactStatusPending])
	assert.Equal(t, 0, active.ByArtifactStatus[model.ArtifactStatusFullyReady])

	// include_archived: ACTIVE + ARCHIVED, DELETED still excluded.
	withArchived, err := h.docRepo.CountCurrentVersionsByArtifactStatus(context.Background(), org, true)
	require.NoError(t, err)
	assert.Equal(t, 2, withArchived.Total)
	assert.Equal(t, 1, withArchived.ByArtifactStatus[model.ArtifactStatusPending])
	assert.Equal(t, 1, withArchived.ByArtifactStatus[model.ArtifactStatusFullyReady])
}

func TestIntegration_DocumentStats_TenantScoping(t *testing.T) {
	h := newTestHarness(t)
	const orgA = "33333333-3333-3333-3333-333333333333"
	const orgB = "44444444-4444-4444-4444-444444444444"

	seedDocWithVersion(t, h, orgA, "doc-a1", "ver-a1", model.ArtifactStatusPending, model.DocumentStatusActive)
	seedDocWithVersion(t, h, orgB, "doc-b1", "ver-b1", model.ArtifactStatusFullyReady, model.DocumentStatusActive)
	seedDocWithVersion(t, h, orgB, "doc-b2", "ver-b2", model.ArtifactStatusFullyReady, model.DocumentStatusActive)

	statsA, err := h.docRepo.CountCurrentVersionsByArtifactStatus(context.Background(), orgA, false)
	require.NoError(t, err)
	assert.Equal(t, 1, statsA.Total)
	assert.Equal(t, 1, statsA.ByArtifactStatus[model.ArtifactStatusPending])

	statsB, err := h.docRepo.CountCurrentVersionsByArtifactStatus(context.Background(), orgB, false)
	require.NoError(t, err)
	assert.Equal(t, 2, statsB.Total)
	assert.Equal(t, 2, statsB.ByArtifactStatus[model.ArtifactStatusFullyReady])
}
