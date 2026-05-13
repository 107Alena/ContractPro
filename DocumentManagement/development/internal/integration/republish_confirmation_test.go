package integration

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"contractpro/document-management/internal/config"
	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/ingress/consumer"
	"contractpro/document-management/internal/ingress/idempotency"
)

// ---------------------------------------------------------------------------
// DM-TASK-058 integration tests: producer→DM confirmation re-publish on
// duplicate event delivery.
//
// Scenario closed by this suite:
//   1. Producer publishes *Ready event (LIC analysis / RE reports / DP
//      processing artifacts / DP diff).
//   2. DM persists artifacts and publishes a *Persisted confirmation.
//   3. Producer crashes before the confirmation is observed.
//   4. RabbitMQ redelivers the *Ready event (at-least-once).
//   5. DM must NOT re-process the artifacts (idempotency skip) and MUST
//      re-publish the original *Persisted confirmation from the snapshot
//      persisted in IdempotencyRecord.ResultSnapshot.
//   6. Outbox-driven downstream notifications (version-analysis-ready,
//      version-reports-ready, version-artifacts-ready) must remain at-most
//      once: no duplication.
// ---------------------------------------------------------------------------

// republishTestHarness wires the test harness together with a real EventConsumer
// so the full ingress→idempotency→application pipeline is exercised.
type republishTestHarness struct {
	*testHarness
	consumer *consumer.EventConsumer
	metrics  *recordingConsumerMetrics
}

func newRepublishHarness(t *testing.T) *republishTestHarness {
	t.Helper()
	h := newTestHarness(t)
	metrics := &recordingConsumerMetrics{}
	c := consumer.NewEventConsumer(
		h.broker,           // subscriber
		h.broker,           // publisher
		h.idempotencyGuard, // idempotency
		h.logger,
		metrics,
		h.dlqPort,
		h.ingestion,
		h.query,
		h.diffService,
		h.artifactRepo,
		h.diffRepo,
		consumer.TopicConfig{
			DPArtifactsReady:    model.TopicDPArtifactsProcessingReady,
			DPSemanticTreeReq:   model.TopicDPRequestsSemanticTree,
			DPDiffReady:         model.TopicDPArtifactsDiffReady,
			LICArtifactsReady:   model.TopicLICArtifactsAnalysisReady,
			LICRequestArtifacts: model.TopicLICRequestsArtifacts,
			REArtifactsReady:    model.TopicREArtifactsReportsReady,
			RERequestArtifacts:  model.TopicRERequestsArtifacts,
		},
		config.RetryConfig{MaxAttempts: 3, BackoffBase: 0},
	)
	if err := c.Start(); err != nil {
		t.Fatalf("consumer Start: %v", err)
	}
	return &republishTestHarness{testHarness: h, consumer: c, metrics: metrics}
}

// recordingConsumerMetrics implements consumer.MetricsCollector for the
// integration tests; it captures the re-publish metric so assertions can
// verify the operator-facing signal.
type recordingConsumerMetrics struct {
	republished map[string]int
}

func (m *recordingConsumerMetrics) IncEventsReceived(_ string)        {}
func (m *recordingConsumerMetrics) IncEventsProcessed(_, _ string)    {}
func (m *recordingConsumerMetrics) IncRepublishedConfirmations(topic string) {
	if m.republished == nil {
		m.republished = make(map[string]int)
	}
	m.republished[topic]++
}

// ---------------------------------------------------------------------------
// LIC scenario: duplicate LegalAnalysisArtifactsReady re-publishes
// LegalAnalysisArtifactsPersisted and does not duplicate the
// version-analysis-ready downstream notification.
// ---------------------------------------------------------------------------

func TestRepublish_LICArtifacts_DuplicateRedelivers_Confirmation(t *testing.T) {
	const (
		orgID     = "org-republish-lic"
		docID     = "doc-republish-lic"
		versionID = "ver-republish-lic"
		jobID     = "job-republish-lic"
	)
	rh := newRepublishHarness(t)
	rh.seedDocument(defaultDocument(orgID, docID))
	// LIC ingestion requires the version to be in PROCESSING_ARTIFACTS_RECEIVED
	// (DP must have already arrived). Seed the version directly in that state.
	ver := defaultVersion(orgID, docID, versionID)
	ver.ArtifactStatus = model.ArtifactStatusProcessingArtifactsReceived
	rh.seedVersion(ver)

	event := defaultLICEvent(orgID, docID, versionID, jobID, "corr-republish-lic")
	body := mustMarshalJSON(t, event)

	// --- First delivery ---
	if err := rh.broker.deliverToTopic(context.Background(), model.TopicLICArtifactsAnalysisReady, body); err != nil {
		t.Fatalf("first delivery: %v", err)
	}

	if rh.broker.publishedToTopic(model.TopicDMResponsesLICArtifactsPersisted) != 0 {
		t.Fatalf("first-time success goes through outbox, not direct publish; got direct publish")
	}

	// Verify the idempotency record carries a snapshot.
	idemKey := idempotency.KeyForLICArtifacts(jobID)
	rec := rh.idemStore.getRecord(idemKey)
	if rec == nil {
		t.Fatal("idempotency record missing after first delivery")
	}
	if rec.Status != model.IdempotencyStatusCompleted {
		t.Errorf("idempotency status = %s, want COMPLETED", rec.Status)
	}
	if rec.ResultSnapshot == "" {
		t.Fatal("idempotency.ResultSnapshot empty — re-publish would not work for next duplicate")
	}

	firstArtifacts := len(rh.artifactRepo.allArtifacts())
	firstOutbox := len(rh.outboxRepo.allEntries())
	firstAudit := len(rh.auditRepo.allRecords())

	// --- Second delivery: duplicate ---
	if err := rh.broker.deliverToTopic(context.Background(), model.TopicLICArtifactsAnalysisReady, body); err != nil {
		t.Fatalf("second delivery: %v", err)
	}

	// Side effects unchanged (idempotency Skip path).
	assertIntEqual(t, "artifacts unchanged", firstArtifacts, len(rh.artifactRepo.allArtifacts()))
	assertIntEqual(t, "outbox unchanged", firstOutbox, len(rh.outboxRepo.allEntries()))
	assertIntEqual(t, "audit unchanged", firstAudit, len(rh.auditRepo.allRecords()))

	// Confirmation re-published exactly once on the duplicate.
	if got := rh.broker.publishedToTopic(model.TopicDMResponsesLICArtifactsPersisted); got != 1 {
		t.Errorf("expected 1 re-published confirmation, got %d", got)
	}
	republishedPayload := rh.broker.lastPublishedTo(model.TopicDMResponsesLICArtifactsPersisted)
	if republishedPayload == nil {
		t.Fatal("missing re-published payload")
	}
	var conf model.LegalAnalysisArtifactsPersisted
	if err := json.Unmarshal(republishedPayload, &conf); err != nil {
		t.Fatalf("unmarshal republished payload: %v", err)
	}
	if conf.JobID != jobID {
		t.Errorf("republished JobID = %q, want %q", conf.JobID, jobID)
	}
	if conf.DocumentID != docID {
		t.Errorf("republished DocumentID = %q, want %q", conf.DocumentID, docID)
	}

	if rh.metrics.republished[model.TopicDMResponsesLICArtifactsPersisted] != 1 {
		t.Errorf("metric republished[lic-persisted] = %d, want 1",
			rh.metrics.republished[model.TopicDMResponsesLICArtifactsPersisted])
	}

	// --- Downstream notification (version-analysis-ready) must NOT duplicate ---
	// The notification is written to outbox at first-time success exactly once;
	// it is not re-emitted on duplicate (BRE-002 preserved for downstream).
	notifCount := 0
	for _, e := range rh.outboxRepo.allEntries() {
		if e.Topic == model.TopicDMEventsVersionAnalysisReady {
			notifCount++
		}
	}
	if notifCount != 1 {
		t.Errorf("downstream notification count = %d, want exactly 1 (no duplication on republish)", notifCount)
	}
}

// ---------------------------------------------------------------------------
// DP scenario: duplicate DocumentProcessingArtifactsReady re-publishes
// DocumentProcessingArtifactsPersisted.
// ---------------------------------------------------------------------------

func TestRepublish_DPArtifacts_DuplicateRedelivers_Confirmation(t *testing.T) {
	const (
		orgID     = "org-republish-dp"
		docID     = "doc-republish-dp"
		versionID = "ver-republish-dp"
		jobID     = "job-republish-dp"
	)
	rh := newRepublishHarness(t)
	rh.seedDocument(defaultDocument(orgID, docID))
	rh.seedVersion(defaultVersion(orgID, docID, versionID))

	event := defaultDPEvent(orgID, docID, versionID, jobID, "corr-republish-dp")
	body := mustMarshalJSON(t, event)

	if err := rh.broker.deliverToTopic(context.Background(), model.TopicDPArtifactsProcessingReady, body); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	firstArtifacts := len(rh.artifactRepo.allArtifacts())

	if err := rh.broker.deliverToTopic(context.Background(), model.TopicDPArtifactsProcessingReady, body); err != nil {
		t.Fatalf("second delivery: %v", err)
	}

	assertIntEqual(t, "artifacts unchanged after duplicate",
		firstArtifacts, len(rh.artifactRepo.allArtifacts()))
	if got := rh.broker.publishedToTopic(model.TopicDMResponsesArtifactsPersisted); got != 1 {
		t.Errorf("expected 1 re-publish for DP, got %d", got)
	}
	if rh.metrics.republished[model.TopicDMResponsesArtifactsPersisted] != 1 {
		t.Errorf("metric republished[dp-persisted] = %d, want 1",
			rh.metrics.republished[model.TopicDMResponsesArtifactsPersisted])
	}
}

// ---------------------------------------------------------------------------
// RE scenario: duplicate ReportsArtifactsReady re-publishes
// ReportsArtifactsPersisted.
// ---------------------------------------------------------------------------

func TestRepublish_REArtifacts_DuplicateRedelivers_Confirmation(t *testing.T) {
	const (
		orgID     = "org-republish-re"
		docID     = "doc-republish-re"
		versionID = "ver-republish-re"
		jobID     = "job-republish-re"
	)
	rh := newRepublishHarness(t)
	rh.seedDocument(defaultDocument(orgID, docID))
	// RE ingestion requires version to be in ANALYSIS_ARTIFACTS_RECEIVED.
	ver := defaultVersion(orgID, docID, versionID)
	ver.ArtifactStatus = model.ArtifactStatusAnalysisArtifactsReceived
	rh.seedVersion(ver)

	// defaultREEvent pre-seeds the claim-check blobs in object storage.
	event := defaultREEvent(rh.testHarness, orgID, docID, versionID, jobID, "corr-republish-re")
	body := mustMarshalJSON(t, event)

	if err := rh.broker.deliverToTopic(context.Background(), model.TopicREArtifactsReportsReady, body); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	firstArtifacts := len(rh.artifactRepo.allArtifacts())

	if err := rh.broker.deliverToTopic(context.Background(), model.TopicREArtifactsReportsReady, body); err != nil {
		t.Fatalf("second delivery: %v", err)
	}

	assertIntEqual(t, "artifacts unchanged after duplicate",
		firstArtifacts, len(rh.artifactRepo.allArtifacts()))
	if got := rh.broker.publishedToTopic(model.TopicDMResponsesREReportsPersisted); got != 1 {
		t.Errorf("expected 1 re-publish for RE, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Diff scenario: duplicate DocumentVersionDiffReady re-publishes
// DocumentVersionDiffPersisted via the same code path.
// ---------------------------------------------------------------------------

func TestRepublish_DiffReady_DuplicateRedelivers_Confirmation(t *testing.T) {
	const (
		orgID     = "org-republish-diff"
		docID     = "doc-republish-diff"
		baseVerID = "ver-republish-diff-base"
		tgtVerID  = "ver-republish-diff-target"
		jobID     = "job-republish-diff"
	)
	rh := newRepublishHarness(t)
	rh.seedDocument(defaultDocument(orgID, docID))
	rh.seedVersion(defaultVersion(orgID, docID, baseVerID))
	rh.seedVersion(defaultVersion(orgID, docID, tgtVerID))

	event := model.DocumentVersionDiffReady{
		EventMeta:           model.EventMeta{CorrelationID: "corr-republish-diff"},
		JobID:               jobID,
		OrgID:               orgID,
		DocumentID:          docID,
		BaseVersionID:       baseVerID,
		TargetVersionID:     tgtVerID,
		TextDiffs:           json.RawMessage("[]"),
		StructuralDiffs:     json.RawMessage("[]"),
		TextDiffCount:       0,
		StructuralDiffCount: 0,
	}
	// Required Timestamp field; the consumer-side validation requires non-zero.
	event.Timestamp = time.Now().UTC()
	body := mustMarshalJSON(t, event)

	if err := rh.broker.deliverToTopic(context.Background(), model.TopicDPArtifactsDiffReady, body); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	firstDiffs := len(rh.diffRepo.allDiffs())

	if err := rh.broker.deliverToTopic(context.Background(), model.TopicDPArtifactsDiffReady, body); err != nil {
		t.Fatalf("second delivery: %v", err)
	}
	assertIntEqual(t, "diff entries unchanged", firstDiffs, len(rh.diffRepo.allDiffs()))

	if got := rh.broker.publishedToTopic(model.TopicDMResponsesDiffPersisted); got != 1 {
		t.Errorf("expected 1 re-publish for diff, got %d", got)
	}
	if rh.metrics.republished[model.TopicDMResponsesDiffPersisted] != 1 {
		t.Errorf("metric republished[diff-persisted] = %d, want 1",
			rh.metrics.republished[model.TopicDMResponsesDiffPersisted])
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func mustMarshalJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}
