package idempotency

import (
	"context"
	"testing"
	"time"

	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
)

// ---------------------------------------------------------------------------
// Mock ArtifactRepository (minimal for fallback tests)
// ---------------------------------------------------------------------------

type mockArtifactRepo struct {
	descriptors []*model.ArtifactDescriptor
	err         error
}

func (m *mockArtifactRepo) Insert(_ context.Context, _ *model.ArtifactDescriptor) error {
	return nil
}
func (m *mockArtifactRepo) FindByVersionAndType(_ context.Context, _, _, _ string, _ model.ArtifactType) (*model.ArtifactDescriptor, error) {
	return nil, nil
}
func (m *mockArtifactRepo) ListByVersion(_ context.Context, _, _, _ string) ([]*model.ArtifactDescriptor, error) {
	return nil, nil
}
func (m *mockArtifactRepo) ListByVersionAndTypes(_ context.Context, _, _, _ string, _ []model.ArtifactType) ([]*model.ArtifactDescriptor, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.descriptors, nil
}
func (m *mockArtifactRepo) DeleteByVersion(_ context.Context, _, _, _ string) error {
	return nil
}

// ---------------------------------------------------------------------------
// Mock DiffRepository (minimal for fallback tests)
// ---------------------------------------------------------------------------

type mockDiffRepo struct {
	ref *model.VersionDiffReference
	err error
}

func (m *mockDiffRepo) Insert(_ context.Context, _ *model.VersionDiffReference) error {
	return nil
}
func (m *mockDiffRepo) FindByVersionPair(_ context.Context, _, _, _, _ string) (*model.VersionDiffReference, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.ref == nil {
		return nil, port.NewDiffNotFoundError("base", "target")
	}
	return m.ref, nil
}
func (m *mockDiffRepo) ListByDocument(_ context.Context, _, _ string) ([]*model.VersionDiffReference, error) {
	return nil, nil
}
func (m *mockDiffRepo) DeleteByDocument(_ context.Context, _, _ string) error {
	return nil
}

// ---------------------------------------------------------------------------
// ArtifactFallback Tests
// ---------------------------------------------------------------------------

func TestArtifactFallback_MatchingJobID_ReturnsTrue(t *testing.T) {
	repo := &mockArtifactRepo{
		descriptors: []*model.ArtifactDescriptor{
			{ArtifactType: model.ArtifactTypeOCRRaw, JobID: "job-123"},
			{ArtifactType: model.ArtifactTypeExtractedText, JobID: "job-123"},
		},
	}

	checker := ArtifactFallback(repo, "org-1", "doc-1", "ver-1", "job-123", model.ProducerDomainDP)
	processed, err := checker(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !processed {
		t.Error("expected alreadyProcessed=true when matching job_id found")
	}
}

func TestArtifactFallback_DifferentJobID_ReturnsFalse(t *testing.T) {
	repo := &mockArtifactRepo{
		descriptors: []*model.ArtifactDescriptor{
			{ArtifactType: model.ArtifactTypeOCRRaw, JobID: "old-job"},
		},
	}

	checker := ArtifactFallback(repo, "org-1", "doc-1", "ver-1", "new-job", model.ProducerDomainDP)
	processed, err := checker(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if processed {
		t.Error("expected alreadyProcessed=false when no matching job_id")
	}
}

func TestArtifactFallback_NoArtifacts_ReturnsFalse(t *testing.T) {
	repo := &mockArtifactRepo{descriptors: nil}

	checker := ArtifactFallback(repo, "org-1", "doc-1", "ver-1", "job-1", model.ProducerDomainDP)
	processed, err := checker(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if processed {
		t.Error("expected alreadyProcessed=false when no artifacts exist")
	}
}

func TestArtifactFallback_RepoError_ReturnsError(t *testing.T) {
	repo := &mockArtifactRepo{
		err: port.NewDatabaseError("query failed", nil),
	}

	checker := ArtifactFallback(repo, "org-1", "doc-1", "ver-1", "job-1", model.ProducerDomainDP)
	_, err := checker(context.Background())
	if err == nil {
		t.Error("expected error when repo fails")
	}
}

func TestArtifactFallback_LICProducer(t *testing.T) {
	repo := &mockArtifactRepo{
		descriptors: []*model.ArtifactDescriptor{
			{ArtifactType: model.ArtifactTypeRiskAnalysis, JobID: "lic-job-1"},
		},
	}

	checker := ArtifactFallback(repo, "org-1", "doc-1", "ver-1", "lic-job-1", model.ProducerDomainLIC)
	processed, err := checker(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !processed {
		t.Error("expected alreadyProcessed=true for LIC artifacts with matching job_id")
	}
}

func TestArtifactFallback_REProducer(t *testing.T) {
	repo := &mockArtifactRepo{
		descriptors: []*model.ArtifactDescriptor{
			{ArtifactType: model.ArtifactTypeExportPDF, JobID: "re-job-1"},
		},
	}

	checker := ArtifactFallback(repo, "org-1", "doc-1", "ver-1", "re-job-1", model.ProducerDomainRE)
	processed, err := checker(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !processed {
		t.Error("expected alreadyProcessed=true for RE artifacts with matching job_id")
	}
}

func TestArtifactFallback_UnknownProducer_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on unknown ProducerDomain")
		}
	}()
	repo := &mockArtifactRepo{}
	ArtifactFallback(repo, "org-1", "doc-1", "ver-1", "job-1", model.ProducerDomain("UNKNOWN"))
}

// ---------------------------------------------------------------------------
// DiffFallback Tests
// ---------------------------------------------------------------------------

func TestDiffFallback_DiffExists_ReturnsTrue(t *testing.T) {
	repo := &mockDiffRepo{
		ref: &model.VersionDiffReference{
			DiffID:          "diff-1",
			DocumentID:      "doc-1",
			OrganizationID:  "org-1",
			BaseVersionID:   "ver-1",
			TargetVersionID: "ver-2",
			StorageKey:      "some-key",
			CreatedAt:       time.Now().UTC(),
		},
	}

	checker := DiffFallback(repo, "org-1", "doc-1", "ver-1", "ver-2")
	processed, err := checker(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !processed {
		t.Error("expected alreadyProcessed=true when diff exists")
	}
}

func TestDiffFallback_DiffNotFound_ReturnsFalse(t *testing.T) {
	repo := &mockDiffRepo{ref: nil}

	checker := DiffFallback(repo, "org-1", "doc-1", "ver-1", "ver-2")
	processed, err := checker(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if processed {
		t.Error("expected alreadyProcessed=false when diff not found")
	}
}

func TestDiffFallback_RepoError_ReturnsError(t *testing.T) {
	repo := &mockDiffRepo{
		err: port.NewDatabaseError("connection lost", nil),
	}

	checker := DiffFallback(repo, "org-1", "doc-1", "ver-1", "ver-2")
	_, err := checker(context.Background())
	if err == nil {
		t.Error("expected error when repo fails")
	}
}
