package confirmation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"contractpro/document-management/internal/config"
	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
)

// --- Mock ---

// mockBroker captures the topic and payload of the last Publish call.
type mockBroker struct {
	topic   string
	payload []byte
	err     error // if set, Publish returns this error
}

func (m *mockBroker) Publish(_ context.Context, topic string, payload []byte) error {
	m.topic = topic
	m.payload = payload
	return m.err
}

// Compile-time check: mockBroker satisfies BrokerPublisher.
var _ BrokerPublisher = (*mockBroker)(nil)

// ctxCapturingBroker captures the context passed to Publish.
type ctxCapturingBroker struct {
	ctx context.Context
}

func (m *ctxCapturingBroker) Publish(ctx context.Context, _ string, _ []byte) error {
	m.ctx = ctx
	return nil
}

// --- Helpers ---

func testBrokerConfig() config.BrokerConfig {
	return config.BrokerConfig{
		TopicDMResponsesArtifactsPersisted:        "dm.responses.artifacts-persisted",
		TopicDMResponsesArtifactsPersistFailed:    "dm.responses.artifacts-persist-failed",
		TopicDMResponsesSemanticTreeProvided:      "dm.responses.semantic-tree-provided",
		TopicDMResponsesArtifactsProvided:         "dm.responses.artifacts-provided",
		TopicDMResponsesDiffPersisted:             "dm.responses.diff-persisted",
		TopicDMResponsesDiffPersistFailed:         "dm.responses.diff-persist-failed",
		TopicDMResponsesLICArtifactsPersisted:     "dm.responses.lic-artifacts-persisted",
		TopicDMResponsesLICArtifactsPersistFailed: "dm.responses.lic-artifacts-persist-failed",
		TopicDMResponsesREReportsPersisted:        "dm.responses.re-reports-persisted",
		TopicDMResponsesREReportsPersistFailed:    "dm.responses.re-reports-persist-failed",
	}
}

func testTimestamp() time.Time {
	return time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
}

func testMeta() model.EventMeta {
	return model.EventMeta{
		CorrelationID: "corr-001",
		Timestamp:     testTimestamp(),
	}
}

func newTestPublisher(broker BrokerPublisher) *ConfirmationPublisher {
	return NewConfirmationPublisher(broker, testBrokerConfig())
}

// --- Interface Compliance ---

func TestInterfaceCompliance(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)

	var iface port.ConfirmationPublisherPort = pub
	if iface == nil {
		t.Fatal("ConfirmationPublisher does not satisfy ConfirmationPublisherPort")
	}
}

// --- Constructor Validation ---

func TestNewConfirmationPublisher_NilBrokerPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil broker, got none")
		}
	}()
	NewConfirmationPublisher(nil, testBrokerConfig())
}

func TestNewConfirmationPublisher_EmptyTopicPanics(t *testing.T) {
	topicFields := []string{
		"TopicDMResponsesArtifactsPersisted",
		"TopicDMResponsesArtifactsPersistFailed",
		"TopicDMResponsesSemanticTreeProvided",
		"TopicDMResponsesArtifactsProvided",
		"TopicDMResponsesDiffPersisted",
		"TopicDMResponsesDiffPersistFailed",
		"TopicDMResponsesLICArtifactsPersisted",
		"TopicDMResponsesLICArtifactsPersistFailed",
		"TopicDMResponsesREReportsPersisted",
		"TopicDMResponsesREReportsPersistFailed",
	}

	for _, field := range topicFields {
		t.Run(field, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("expected panic for empty %s, got none", field)
				}
			}()
			cfg := testBrokerConfig()
			// Zero out the specific field.
			switch field {
			case "TopicDMResponsesArtifactsPersisted":
				cfg.TopicDMResponsesArtifactsPersisted = ""
			case "TopicDMResponsesArtifactsPersistFailed":
				cfg.TopicDMResponsesArtifactsPersistFailed = ""
			case "TopicDMResponsesSemanticTreeProvided":
				cfg.TopicDMResponsesSemanticTreeProvided = ""
			case "TopicDMResponsesArtifactsProvided":
				cfg.TopicDMResponsesArtifactsProvided = ""
			case "TopicDMResponsesDiffPersisted":
				cfg.TopicDMResponsesDiffPersisted = ""
			case "TopicDMResponsesDiffPersistFailed":
				cfg.TopicDMResponsesDiffPersistFailed = ""
			case "TopicDMResponsesLICArtifactsPersisted":
				cfg.TopicDMResponsesLICArtifactsPersisted = ""
			case "TopicDMResponsesLICArtifactsPersistFailed":
				cfg.TopicDMResponsesLICArtifactsPersistFailed = ""
			case "TopicDMResponsesREReportsPersisted":
				cfg.TopicDMResponsesREReportsPersisted = ""
			case "TopicDMResponsesREReportsPersistFailed":
				cfg.TopicDMResponsesREReportsPersistFailed = ""
			}
			NewConfirmationPublisher(&mockBroker{}, cfg)
		})
	}
}

// --- Correct Topic Tests ---

func TestPublishDPArtifactsPersisted_CorrectTopic(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)

	err := pub.PublishDPArtifactsPersisted(context.Background(), model.DocumentProcessingArtifactsPersisted{
		EventMeta: testMeta(), JobID: "j-1", DocumentID: "d-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if broker.topic != "dm.responses.artifacts-persisted" {
		t.Errorf("topic = %q, want %q", broker.topic, "dm.responses.artifacts-persisted")
	}
}

func TestPublishDPArtifactsPersistFailed_CorrectTopic(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)

	err := pub.PublishDPArtifactsPersistFailed(context.Background(), model.DocumentProcessingArtifactsPersistFailed{
		EventMeta: testMeta(), JobID: "j-1", DocumentID: "d-1", ErrorMessage: "fail",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if broker.topic != "dm.responses.artifacts-persist-failed" {
		t.Errorf("topic = %q, want %q", broker.topic, "dm.responses.artifacts-persist-failed")
	}
}

func TestPublishSemanticTreeProvided_CorrectTopic(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)

	err := pub.PublishSemanticTreeProvided(context.Background(), model.SemanticTreeProvided{
		EventMeta: testMeta(), JobID: "j-1", DocumentID: "d-1", VersionID: "v-1",
		SemanticTree: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if broker.topic != "dm.responses.semantic-tree-provided" {
		t.Errorf("topic = %q, want %q", broker.topic, "dm.responses.semantic-tree-provided")
	}
}

func TestPublishArtifactsProvided_CorrectTopic(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)

	err := pub.PublishArtifactsProvided(context.Background(), model.ArtifactsProvided{
		EventMeta: testMeta(), JobID: "j-1", DocumentID: "d-1", VersionID: "v-1",
		Artifacts: map[model.ArtifactType]json.RawMessage{model.ArtifactTypeSemanticTree: json.RawMessage(`{}`)},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if broker.topic != "dm.responses.artifacts-provided" {
		t.Errorf("topic = %q, want %q", broker.topic, "dm.responses.artifacts-provided")
	}
}

func TestPublishDiffPersisted_CorrectTopic(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)

	err := pub.PublishDiffPersisted(context.Background(), model.DocumentVersionDiffPersisted{
		EventMeta: testMeta(), JobID: "j-1", DocumentID: "d-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if broker.topic != "dm.responses.diff-persisted" {
		t.Errorf("topic = %q, want %q", broker.topic, "dm.responses.diff-persisted")
	}
}

func TestPublishDiffPersistFailed_CorrectTopic(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)

	err := pub.PublishDiffPersistFailed(context.Background(), model.DocumentVersionDiffPersistFailed{
		EventMeta: testMeta(), JobID: "j-1", DocumentID: "d-1", ErrorMessage: "fail",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if broker.topic != "dm.responses.diff-persist-failed" {
		t.Errorf("topic = %q, want %q", broker.topic, "dm.responses.diff-persist-failed")
	}
}

func TestPublishLICArtifactsPersisted_CorrectTopic(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)

	err := pub.PublishLICArtifactsPersisted(context.Background(), model.LegalAnalysisArtifactsPersisted{
		EventMeta: testMeta(), JobID: "j-1", DocumentID: "d-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if broker.topic != "dm.responses.lic-artifacts-persisted" {
		t.Errorf("topic = %q, want %q", broker.topic, "dm.responses.lic-artifacts-persisted")
	}
}

func TestPublishLICArtifactsPersistFailed_CorrectTopic(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)

	err := pub.PublishLICArtifactsPersistFailed(context.Background(), model.LegalAnalysisArtifactsPersistFailed{
		EventMeta: testMeta(), JobID: "j-1", DocumentID: "d-1", ErrorMessage: "fail",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if broker.topic != "dm.responses.lic-artifacts-persist-failed" {
		t.Errorf("topic = %q, want %q", broker.topic, "dm.responses.lic-artifacts-persist-failed")
	}
}

func TestPublishREReportsPersisted_CorrectTopic(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)

	err := pub.PublishREReportsPersisted(context.Background(), model.ReportsArtifactsPersisted{
		EventMeta: testMeta(), JobID: "j-1", DocumentID: "d-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if broker.topic != "dm.responses.re-reports-persisted" {
		t.Errorf("topic = %q, want %q", broker.topic, "dm.responses.re-reports-persisted")
	}
}

func TestPublishREReportsPersistFailed_CorrectTopic(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)

	err := pub.PublishREReportsPersistFailed(context.Background(), model.ReportsArtifactsPersistFailed{
		EventMeta: testMeta(), JobID: "j-1", DocumentID: "d-1", ErrorMessage: "fail",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if broker.topic != "dm.responses.re-reports-persist-failed" {
		t.Errorf("topic = %q, want %q", broker.topic, "dm.responses.re-reports-persist-failed")
	}
}

// --- JSON Format Tests ---

func TestPublishDPArtifactsPersisted_JSONFormat(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)

	_ = pub.PublishDPArtifactsPersisted(context.Background(), model.DocumentProcessingArtifactsPersisted{
		EventMeta: testMeta(), JobID: "j-1", DocumentID: "d-1",
	})

	var m map[string]any
	if err := json.Unmarshal(broker.payload, &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, key := range []string{"correlation_id", "timestamp", "job_id", "document_id"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing key %q in JSON payload", key)
		}
	}
}

func TestPublishDPArtifactsPersistFailed_JSONFormat(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)

	_ = pub.PublishDPArtifactsPersistFailed(context.Background(), model.DocumentProcessingArtifactsPersistFailed{
		EventMeta: testMeta(), JobID: "j-1", DocumentID: "d-1",
		ErrorCode: "STORAGE_FAILED", ErrorMessage: "fail", IsRetryable: true,
	})

	var m map[string]any
	if err := json.Unmarshal(broker.payload, &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, key := range []string{"correlation_id", "timestamp", "job_id", "document_id", "error_code", "error_message", "is_retryable"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing key %q in JSON payload", key)
		}
	}
}

func TestPublishArtifactsProvided_JSONFormat(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)

	_ = pub.PublishArtifactsProvided(context.Background(), model.ArtifactsProvided{
		EventMeta: testMeta(), JobID: "j-1", DocumentID: "d-1", VersionID: "v-1",
		Artifacts:    map[model.ArtifactType]json.RawMessage{model.ArtifactTypeSemanticTree: json.RawMessage(`{}`)},
		MissingTypes: []model.ArtifactType{model.ArtifactTypeRiskAnalysis},
	})

	var m map[string]any
	if err := json.Unmarshal(broker.payload, &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, key := range []string{"correlation_id", "timestamp", "job_id", "document_id", "version_id", "artifacts", "missing_types"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing key %q in JSON payload", key)
		}
	}
}

// --- Round-Trip Tests ---

func TestPublishDPArtifactsPersisted_RoundTrip(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)
	want := model.DocumentProcessingArtifactsPersisted{
		EventMeta: testMeta(), JobID: "j-1", DocumentID: "d-1",
	}

	_ = pub.PublishDPArtifactsPersisted(context.Background(), want)

	var got model.DocumentProcessingArtifactsPersisted
	if err := json.Unmarshal(broker.payload, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.CorrelationID != want.CorrelationID {
		t.Errorf("CorrelationID = %q, want %q", got.CorrelationID, want.CorrelationID)
	}
	if !got.Timestamp.Equal(want.Timestamp) {
		t.Errorf("Timestamp = %v, want %v", got.Timestamp, want.Timestamp)
	}
	if got.JobID != want.JobID {
		t.Errorf("JobID = %q, want %q", got.JobID, want.JobID)
	}
	if got.DocumentID != want.DocumentID {
		t.Errorf("DocumentID = %q, want %q", got.DocumentID, want.DocumentID)
	}
}

func TestPublishDPArtifactsPersistFailed_RoundTrip(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)
	want := model.DocumentProcessingArtifactsPersistFailed{
		EventMeta: testMeta(), JobID: "j-2", DocumentID: "d-2",
		ErrorCode: "STORAGE_FAILED", ErrorMessage: "S3 timeout", IsRetryable: true,
	}

	_ = pub.PublishDPArtifactsPersistFailed(context.Background(), want)

	var got model.DocumentProcessingArtifactsPersistFailed
	if err := json.Unmarshal(broker.payload, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.CorrelationID != want.CorrelationID {
		t.Errorf("CorrelationID = %q, want %q", got.CorrelationID, want.CorrelationID)
	}
	if got.ErrorCode != want.ErrorCode {
		t.Errorf("ErrorCode = %q, want %q", got.ErrorCode, want.ErrorCode)
	}
	if got.ErrorMessage != want.ErrorMessage {
		t.Errorf("ErrorMessage = %q, want %q", got.ErrorMessage, want.ErrorMessage)
	}
	if got.IsRetryable != want.IsRetryable {
		t.Errorf("IsRetryable = %v, want %v", got.IsRetryable, want.IsRetryable)
	}
}

func TestPublishSemanticTreeProvided_RoundTrip(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)
	rawTree := `{"root":{"id":"n1"}}`
	want := model.SemanticTreeProvided{
		EventMeta: testMeta(), JobID: "j-3", DocumentID: "d-3", VersionID: "v-3",
		SemanticTree: json.RawMessage(rawTree),
	}

	_ = pub.PublishSemanticTreeProvided(context.Background(), want)

	var got model.SemanticTreeProvided
	if err := json.Unmarshal(broker.payload, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.CorrelationID != want.CorrelationID {
		t.Errorf("CorrelationID = %q, want %q", got.CorrelationID, want.CorrelationID)
	}
	if string(got.SemanticTree) != rawTree {
		t.Errorf("SemanticTree = %s, want %s", got.SemanticTree, rawTree)
	}
	if got.VersionID != want.VersionID {
		t.Errorf("VersionID = %q, want %q", got.VersionID, want.VersionID)
	}
}

func TestPublishArtifactsProvided_RoundTrip(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)
	want := model.ArtifactsProvided{
		EventMeta: testMeta(), JobID: "j-4", DocumentID: "d-4", VersionID: "v-4",
		Artifacts: map[model.ArtifactType]json.RawMessage{
			model.ArtifactTypeSemanticTree: json.RawMessage(`{"root":{}}`),
		},
		MissingTypes: []model.ArtifactType{model.ArtifactTypeRiskAnalysis},
	}

	_ = pub.PublishArtifactsProvided(context.Background(), want)

	var got model.ArtifactsProvided
	if err := json.Unmarshal(broker.payload, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.CorrelationID != want.CorrelationID {
		t.Errorf("CorrelationID = %q, want %q", got.CorrelationID, want.CorrelationID)
	}
	if len(got.Artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(got.Artifacts))
	}
	if string(got.Artifacts[model.ArtifactTypeSemanticTree]) != `{"root":{}}` {
		t.Errorf("artifact content mismatch")
	}
	if len(got.MissingTypes) != 1 || got.MissingTypes[0] != model.ArtifactTypeRiskAnalysis {
		t.Errorf("MissingTypes mismatch")
	}
}

func TestPublishDiffPersisted_RoundTrip(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)
	want := model.DocumentVersionDiffPersisted{
		EventMeta: testMeta(), JobID: "j-5", DocumentID: "d-5",
	}

	_ = pub.PublishDiffPersisted(context.Background(), want)

	var got model.DocumentVersionDiffPersisted
	if err := json.Unmarshal(broker.payload, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.CorrelationID != want.CorrelationID {
		t.Errorf("CorrelationID = %q, want %q", got.CorrelationID, want.CorrelationID)
	}
	if got.JobID != want.JobID {
		t.Errorf("JobID = %q, want %q", got.JobID, want.JobID)
	}
}

func TestPublishDiffPersistFailed_RoundTrip(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)
	want := model.DocumentVersionDiffPersistFailed{
		EventMeta: testMeta(), JobID: "j-6", DocumentID: "d-6",
		ErrorCode: "VERSIONS_NOT_FOUND", ErrorMessage: "base not found", IsRetryable: false,
	}

	_ = pub.PublishDiffPersistFailed(context.Background(), want)

	var got model.DocumentVersionDiffPersistFailed
	if err := json.Unmarshal(broker.payload, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ErrorCode != want.ErrorCode {
		t.Errorf("ErrorCode = %q, want %q", got.ErrorCode, want.ErrorCode)
	}
	if got.IsRetryable != want.IsRetryable {
		t.Errorf("IsRetryable = %v, want %v", got.IsRetryable, want.IsRetryable)
	}
}

func TestPublishLICArtifactsPersisted_RoundTrip(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)
	want := model.LegalAnalysisArtifactsPersisted{
		EventMeta: testMeta(), JobID: "j-7", DocumentID: "d-7",
	}

	_ = pub.PublishLICArtifactsPersisted(context.Background(), want)

	var got model.LegalAnalysisArtifactsPersisted
	if err := json.Unmarshal(broker.payload, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.CorrelationID != want.CorrelationID {
		t.Errorf("CorrelationID = %q, want %q", got.CorrelationID, want.CorrelationID)
	}
	if got.JobID != want.JobID {
		t.Errorf("JobID = %q, want %q", got.JobID, want.JobID)
	}
}

func TestPublishLICArtifactsPersistFailed_RoundTrip(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)
	want := model.LegalAnalysisArtifactsPersistFailed{
		EventMeta: testMeta(), JobID: "j-8", DocumentID: "d-8",
		ErrorMessage: "DB error", IsRetryable: true,
	}

	_ = pub.PublishLICArtifactsPersistFailed(context.Background(), want)

	var got model.LegalAnalysisArtifactsPersistFailed
	if err := json.Unmarshal(broker.payload, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ErrorMessage != want.ErrorMessage {
		t.Errorf("ErrorMessage = %q, want %q", got.ErrorMessage, want.ErrorMessage)
	}
	if got.IsRetryable != want.IsRetryable {
		t.Errorf("IsRetryable = %v, want %v", got.IsRetryable, want.IsRetryable)
	}
}

func TestPublishREReportsPersisted_RoundTrip(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)
	want := model.ReportsArtifactsPersisted{
		EventMeta: testMeta(), JobID: "j-9", DocumentID: "d-9",
	}

	_ = pub.PublishREReportsPersisted(context.Background(), want)

	var got model.ReportsArtifactsPersisted
	if err := json.Unmarshal(broker.payload, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.CorrelationID != want.CorrelationID {
		t.Errorf("CorrelationID = %q, want %q", got.CorrelationID, want.CorrelationID)
	}
	if got.JobID != want.JobID {
		t.Errorf("JobID = %q, want %q", got.JobID, want.JobID)
	}
}

func TestPublishREReportsPersistFailed_RoundTrip(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)
	want := model.ReportsArtifactsPersistFailed{
		EventMeta: testMeta(), JobID: "j-10", DocumentID: "d-10",
		ErrorCode: "STORAGE_FULL", ErrorMessage: "bucket full", IsRetryable: true,
	}

	_ = pub.PublishREReportsPersistFailed(context.Background(), want)

	var got model.ReportsArtifactsPersistFailed
	if err := json.Unmarshal(broker.payload, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ErrorCode != want.ErrorCode {
		t.Errorf("ErrorCode = %q, want %q", got.ErrorCode, want.ErrorCode)
	}
	if got.IsRetryable != want.IsRetryable {
		t.Errorf("IsRetryable = %v, want %v", got.IsRetryable, want.IsRetryable)
	}
}

// --- Error Passthrough Tests ---

func TestPublish_BrokerError(t *testing.T) {
	brokerErr := port.NewBrokerError("connection lost", errors.New("tcp reset"))
	broker := &mockBroker{err: brokerErr}
	pub := newTestPublisher(broker)

	err := pub.PublishDPArtifactsPersisted(context.Background(), model.DocumentProcessingArtifactsPersisted{
		EventMeta: testMeta(), JobID: "j-1", DocumentID: "d-1",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, brokerErr) {
		t.Errorf("error = %v, want errors.Is to match broker error", err)
	}
	if !port.IsDomainError(err) {
		t.Errorf("expected DomainError, got %T", err)
	}
	if !port.IsRetryable(err) {
		t.Error("broker errors should be retryable")
	}
	if port.ErrorCode(err) != port.ErrCodeBrokerFailed {
		t.Errorf("ErrorCode = %q, want %q", port.ErrorCode(err), port.ErrCodeBrokerFailed)
	}
}

func TestPublish_ContextCanceled(t *testing.T) {
	broker := &mockBroker{err: context.Canceled}
	pub := newTestPublisher(broker)

	err := pub.PublishDPArtifactsPersisted(context.Background(), model.DocumentProcessingArtifactsPersisted{
		EventMeta: testMeta(), JobID: "j-1", DocumentID: "d-1",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want errors.Is(context.Canceled)", err)
	}
}

func TestPublish_ContextDeadlineExceeded(t *testing.T) {
	broker := &mockBroker{err: context.DeadlineExceeded}
	pub := newTestPublisher(broker)

	err := pub.PublishLICArtifactsPersisted(context.Background(), model.LegalAnalysisArtifactsPersisted{
		EventMeta: testMeta(), JobID: "j-1", DocumentID: "d-1",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error = %v, want errors.Is(context.DeadlineExceeded)", err)
	}
}

// --- Marshal Error Test ---

func TestPublishJSON_MarshalError(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)

	err := pub.publishJSON(context.Background(), "any-topic", make(chan int))
	if err == nil {
		t.Fatal("expected error for un-marshalable value, got nil")
	}
	if !port.IsDomainError(err) {
		t.Errorf("expected DomainError, got %T", err)
	}
	if port.ErrorCode(err) != port.ErrCodeBrokerFailed {
		t.Errorf("ErrorCode = %q, want %q", port.ErrorCode(err), port.ErrCodeBrokerFailed)
	}
	if port.IsRetryable(err) {
		t.Error("marshal errors should NOT be retryable")
	}
}

// --- Context Forwarding Test ---

func TestPublish_ForwardsContext(t *testing.T) {
	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "test-val")

	cb := &ctxCapturingBroker{}
	pub := NewConfirmationPublisher(cb, testBrokerConfig())

	_ = pub.PublishDPArtifactsPersisted(ctx, model.DocumentProcessingArtifactsPersisted{
		EventMeta: testMeta(), JobID: "j-1", DocumentID: "d-1",
	})

	if cb.ctx == nil {
		t.Fatal("context was not captured")
	}
	if cb.ctx.Value(ctxKey{}) != "test-val" {
		t.Error("context was not forwarded to broker.Publish")
	}
}

// --- OmitEmpty Tests ---

func TestPublishDPArtifactsPersistFailed_OmitsEmptyErrorCode(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)

	_ = pub.PublishDPArtifactsPersistFailed(context.Background(), model.DocumentProcessingArtifactsPersistFailed{
		EventMeta: testMeta(), JobID: "j-1", DocumentID: "d-1",
		ErrorMessage: "fail", // no ErrorCode
	})

	var m map[string]any
	if err := json.Unmarshal(broker.payload, &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := m["error_code"]; ok {
		t.Error("expected 'error_code' to be omitted when empty")
	}
}

func TestPublishSemanticTreeProvided_OmitsEmptyOptionalFields(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)

	_ = pub.PublishSemanticTreeProvided(context.Background(), model.SemanticTreeProvided{
		EventMeta: testMeta(), JobID: "j-1", DocumentID: "d-1", VersionID: "v-1",
		SemanticTree: json.RawMessage(`{}`),
		// No error fields set.
	})

	var m map[string]any
	if err := json.Unmarshal(broker.payload, &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, key := range []string{"error_code", "error_message", "is_retryable"} {
		if _, ok := m[key]; ok {
			t.Errorf("expected %q to be omitted when zero-valued", key)
		}
	}
}

func TestPublishArtifactsProvided_OmitsEmptyOptionalFields(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)

	_ = pub.PublishArtifactsProvided(context.Background(), model.ArtifactsProvided{
		EventMeta: testMeta(), JobID: "j-1", DocumentID: "d-1", VersionID: "v-1",
		Artifacts: map[model.ArtifactType]json.RawMessage{model.ArtifactTypeSemanticTree: json.RawMessage(`{}`)},
		// No MissingTypes, ErrorCode, ErrorMessage.
	})

	var m map[string]any
	if err := json.Unmarshal(broker.payload, &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, key := range []string{"missing_types", "error_code", "error_message"} {
		if _, ok := m[key]; ok {
			t.Errorf("expected %q to be omitted when empty", key)
		}
	}
}

// --- Correlation ID Propagation Test ---

func TestPublish_CorrelationIDPreserved(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)

	meta := model.EventMeta{CorrelationID: "trace-abc-123", Timestamp: testTimestamp()}

	// Test across multiple event types.
	events := []struct {
		name string
		fn   func()
	}{
		{"DPArtifactsPersisted", func() {
			_ = pub.PublishDPArtifactsPersisted(context.Background(), model.DocumentProcessingArtifactsPersisted{
				EventMeta: meta, JobID: "j-1", DocumentID: "d-1",
			})
		}},
		{"LICArtifactsPersisted", func() {
			_ = pub.PublishLICArtifactsPersisted(context.Background(), model.LegalAnalysisArtifactsPersisted{
				EventMeta: meta, JobID: "j-1", DocumentID: "d-1",
			})
		}},
		{"REReportsPersisted", func() {
			_ = pub.PublishREReportsPersisted(context.Background(), model.ReportsArtifactsPersisted{
				EventMeta: meta, JobID: "j-1", DocumentID: "d-1",
			})
		}},
	}

	for _, tc := range events {
		t.Run(tc.name, func(t *testing.T) {
			tc.fn()

			var m map[string]any
			if err := json.Unmarshal(broker.payload, &m); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}
			if m["correlation_id"] != "trace-abc-123" {
				t.Errorf("correlation_id = %v, want %q", m["correlation_id"], "trace-abc-123")
			}
		})
	}
}
