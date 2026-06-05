package lifecycle

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
)

func TestGetDocumentStats_Happy(t *testing.T) {
	d := newTestDeps()
	var capturedOrg string
	var capturedArchived bool
	d.docRepo.statsFn = func(_ context.Context, orgID string, includeArchived bool) (*port.DocumentStats, error) {
		capturedOrg = orgID
		capturedArchived = includeArchived
		return &port.DocumentStats{
			ByArtifactStatus: map[model.ArtifactStatus]int{
				model.ArtifactStatusPending:    2,
				model.ArtifactStatusFullyReady: 4,
			},
			NotStarted: 1,
			Total:      7,
		}, nil
	}

	stats, err := d.newService().GetDocumentStats(context.Background(), "org-1", true)
	require.NoError(t, err)
	assert.Equal(t, "org-1", capturedOrg)
	assert.True(t, capturedArchived)
	assert.Equal(t, 2, stats.ByArtifactStatus[model.ArtifactStatusPending])
	assert.Equal(t, 4, stats.ByArtifactStatus[model.ArtifactStatusFullyReady])
	assert.Equal(t, 1, stats.NotStarted)
	assert.Equal(t, 7, stats.Total)
}

func TestGetDocumentStats_EmptyOrg(t *testing.T) {
	d := newTestDeps()
	_, err := d.newService().GetDocumentStats(context.Background(), "", false)
	require.Error(t, err)
	assert.Equal(t, port.ErrCodeValidation, port.ErrorCode(err))
}

func TestGetDocumentStats_RepoError(t *testing.T) {
	d := newTestDeps()
	d.docRepo.statsFn = func(_ context.Context, _ string, _ bool) (*port.DocumentStats, error) {
		return nil, port.NewDatabaseError("boom", errors.New("db down"))
	}

	_, err := d.newService().GetDocumentStats(context.Background(), "org-1", false)
	require.Error(t, err)
	assert.Equal(t, port.ErrCodeDatabaseFailed, port.ErrorCode(err))
	assert.NotEmpty(t, d.logger.errors)
}

func TestGetDocumentStats_NilMapNormalized(t *testing.T) {
	d := newTestDeps()
	d.docRepo.statsFn = func(_ context.Context, _ string, _ bool) (*port.DocumentStats, error) {
		// Repo returns a stats with nil map (defensive normalization path).
		return &port.DocumentStats{}, nil
	}

	stats, err := d.newService().GetDocumentStats(context.Background(), "org-1", false)
	require.NoError(t, err)
	assert.NotNil(t, stats.ByArtifactStatus)
	assert.Equal(t, 0, stats.Total)
}
