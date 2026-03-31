package model

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNewArtifactDescriptor(t *testing.T) {
	ad := NewArtifactDescriptor(
		"art-1", "ver-1", "doc-1", "org-1",
		ArtifactTypeSemanticTree, ProducerDomainDP,
		"org-1/doc-1/ver-1/SEMANTIC_TREE",
		4096, "sha256:xyz789", "1.0", "job-1", "corr-1",
	)

	if ad.ArtifactID != "art-1" {
		t.Errorf("expected artifact_id art-1, got %s", ad.ArtifactID)
	}
	if ad.ArtifactType != ArtifactTypeSemanticTree {
		t.Errorf("expected SEMANTIC_TREE, got %s", ad.ArtifactType)
	}
	if ad.ProducerDomain != ProducerDomainDP {
		t.Errorf("expected DP, got %s", ad.ProducerDomain)
	}
	if ad.CreatedAt.IsZero() {
		t.Error("expected non-zero created_at")
	}
}

func TestArtifactDescriptorJSONRoundTrip(t *testing.T) {
	now := time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC)
	ad := &ArtifactDescriptor{
		ArtifactID:     "art-123",
		VersionID:      "ver-456",
		DocumentID:     "doc-789",
		OrganizationID: "org-111",
		ArtifactType:   ArtifactTypeRiskAnalysis,
		ProducerDomain: ProducerDomainLIC,
		StorageKey:     "org-111/doc-789/ver-456/RISK_ANALYSIS",
		SizeBytes:      8192,
		ContentHash:    "sha256:abc",
		SchemaVersion:  "1.0",
		JobID:          "job-222",
		CorrelationID:  "corr-333",
		CreatedAt:      now,
	}

	data, err := json.Marshal(ad)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var restored ArtifactDescriptor
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if restored.ArtifactID != ad.ArtifactID {
		t.Errorf("artifact_id mismatch: %s != %s", restored.ArtifactID, ad.ArtifactID)
	}
	if restored.ArtifactType != ad.ArtifactType {
		t.Errorf("artifact_type mismatch: %s != %s", restored.ArtifactType, ad.ArtifactType)
	}
	if restored.ProducerDomain != ad.ProducerDomain {
		t.Errorf("producer_domain mismatch: %s != %s", restored.ProducerDomain, ad.ProducerDomain)
	}
	if restored.SizeBytes != ad.SizeBytes {
		t.Errorf("size_bytes mismatch: %d != %d", restored.SizeBytes, ad.SizeBytes)
	}
	if restored.ContentHash != ad.ContentHash {
		t.Errorf("content_hash mismatch: %s != %s", restored.ContentHash, ad.ContentHash)
	}
	if restored.SchemaVersion != ad.SchemaVersion {
		t.Errorf("schema_version mismatch: %s != %s", restored.SchemaVersion, ad.SchemaVersion)
	}
}

func TestArtifactTypeIsBlobArtifact(t *testing.T) {
	blobTypes := []ArtifactType{ArtifactTypeExportPDF, ArtifactTypeExportDOCX}
	for _, at := range blobTypes {
		if !at.IsBlobArtifact() {
			t.Errorf("expected %s to be blob artifact", at)
		}
	}

	jsonTypes := []ArtifactType{
		ArtifactTypeSemanticTree,
		ArtifactTypeRiskAnalysis,
		ArtifactTypeOCRRaw,
		ArtifactTypeExtractedText,
	}
	for _, at := range jsonTypes {
		if at.IsBlobArtifact() {
			t.Errorf("expected %s to NOT be blob artifact", at)
		}
	}
}

func TestArtifactTypesByProducerCompleteness(t *testing.T) {
	totalFromMap := 0
	for _, types := range ArtifactTypesByProducer {
		totalFromMap += len(types)
	}

	if totalFromMap != len(AllArtifactTypes) {
		t.Errorf("ArtifactTypesByProducer has %d types, AllArtifactTypes has %d", totalFromMap, len(AllArtifactTypes))
	}

	if len(ArtifactTypesByProducer[ProducerDomainDP]) != 5 {
		t.Errorf("expected 5 DP artifact types, got %d", len(ArtifactTypesByProducer[ProducerDomainDP]))
	}
	if len(ArtifactTypesByProducer[ProducerDomainLIC]) != 8 {
		t.Errorf("expected 8 LIC artifact types, got %d", len(ArtifactTypesByProducer[ProducerDomainLIC]))
	}
	if len(ArtifactTypesByProducer[ProducerDomainRE]) != 2 {
		t.Errorf("expected 2 RE artifact types, got %d", len(ArtifactTypesByProducer[ProducerDomainRE]))
	}
}

func TestAllArtifactTypesCount(t *testing.T) {
	expected := 15
	if len(AllArtifactTypes) != expected {
		t.Errorf("expected %d artifact types, got %d", expected, len(AllArtifactTypes))
	}
}

func TestAllProducerDomainsCount(t *testing.T) {
	expected := 3
	if len(AllProducerDomains) != expected {
		t.Errorf("expected %d producer domains, got %d", expected, len(AllProducerDomains))
	}
}
