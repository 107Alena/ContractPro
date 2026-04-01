package idempotency

import (
	"context"
	"fmt"

	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
)

// ArtifactFallback creates a FallbackChecker that verifies whether artifacts
// for a given producer domain have already been persisted in the database.
// It queries artifact_descriptors by version and expected artifact types,
// then checks if any descriptor was created by the same job_id.
//
// This is used for ingestion events (dp.artifacts.processing-ready,
// lic.artifacts.analysis-ready, re.artifacts.reports-ready) when Redis
// is unavailable.
//
// Panics if producer is not in ArtifactTypesByProducer (programming error).
func ArtifactFallback(
	repo port.ArtifactRepository,
	orgID, docID, versionID, jobID string,
	producer model.ProducerDomain,
) FallbackChecker {
	expectedTypes := model.ArtifactTypesByProducer[producer]
	if len(expectedTypes) == 0 {
		panic(fmt.Sprintf("idempotency: unknown producer domain %q", producer))
	}

	return func(ctx context.Context) (bool, error) {
		descriptors, err := repo.ListByVersionAndTypes(ctx, orgID, docID, versionID, expectedTypes)
		if err != nil {
			return false, err
		}

		for _, d := range descriptors {
			if d.JobID == jobID {
				return true, nil
			}
		}
		return false, nil
	}
}

// DiffFallback creates a FallbackChecker that verifies whether a diff
// between two versions has already been persisted in the database.
// Existence check is sufficient because the unique constraint
// (base_version_id, target_version_id) guarantees one diff per version pair.
//
// This is used for dp.artifacts.diff-ready events when Redis is unavailable.
func DiffFallback(
	repo port.DiffRepository,
	orgID, docID, baseVersionID, targetVersionID string,
) FallbackChecker {
	return func(ctx context.Context) (bool, error) {
		_, err := repo.FindByVersionPair(ctx, orgID, docID, baseVersionID, targetVersionID)
		if err != nil {
			if port.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	}
}
