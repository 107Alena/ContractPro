package notification

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
		TopicDMEventsVersionArtifactsReady:     "dm.events.version-artifacts-ready",
		TopicDMEventsVersionAnalysisReady:      "dm.events.version-analysis-ready",
		TopicDMEventsVersionReportsReady:       "dm.events.version-reports-ready",
		TopicDMEventsVersionCreated:            "dm.events.version-created",
		TopicDMEventsVersionPartiallyAvailable: "dm.events.version-partially-available",
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

func newTestPublisher(broker BrokerPublisher) *NotificationPublisher {
	return NewNotificationPublisher(broker, testBrokerConfig())
}

// --- Interface Compliance ---

func TestInterfaceCompliance(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)

	var iface port.NotificationPublisherPort = pub
	if iface == nil {
		t.Fatal("NotificationPublisher does not satisfy NotificationPublisherPort")
	}
}

// --- Constructor Validation ---

func TestNewNotificationPublisher_NilBrokerPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil broker, got none")
		}
	}()
	NewNotificationPublisher(nil, testBrokerConfig())
}

func TestNewNotificationPublisher_EmptyTopicPanics(t *testing.T) {
	topicFields := []string{
		"TopicDMEventsVersionArtifactsReady",
		"TopicDMEventsVersionAnalysisReady",
		"TopicDMEventsVersionReportsReady",
		"TopicDMEventsVersionCreated",
		"TopicDMEventsVersionPartiallyAvailable",
	}

	for _, field := range topicFields {
		t.Run(field, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("expected panic for empty %s, got none", field)
				}
			}()
			cfg := testBrokerConfig()
			switch field {
			case "TopicDMEventsVersionArtifactsReady":
				cfg.TopicDMEventsVersionArtifactsReady = ""
			case "TopicDMEventsVersionAnalysisReady":
				cfg.TopicDMEventsVersionAnalysisReady = ""
			case "TopicDMEventsVersionReportsReady":
				cfg.TopicDMEventsVersionReportsReady = ""
			case "TopicDMEventsVersionCreated":
				cfg.TopicDMEventsVersionCreated = ""
			case "TopicDMEventsVersionPartiallyAvailable":
				cfg.TopicDMEventsVersionPartiallyAvailable = ""
			}
			NewNotificationPublisher(&mockBroker{}, cfg)
		})
	}
}

// --- Correct Topic Tests ---

func TestPublishVersionProcessingArtifactsReady_CorrectTopic(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)

	err := pub.PublishVersionProcessingArtifactsReady(context.Background(), model.VersionProcessingArtifactsReady{
		EventMeta: testMeta(), DocumentID: "d-1", VersionID: "v-1", OrgID: "org-1",
		ArtifactTypes: []model.ArtifactType{model.ArtifactTypeSemanticTree},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if broker.topic != "dm.events.version-artifacts-ready" {
		t.Errorf("topic = %q, want %q", broker.topic, "dm.events.version-artifacts-ready")
	}
}

func TestPublishVersionAnalysisArtifactsReady_CorrectTopic(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)

	err := pub.PublishVersionAnalysisArtifactsReady(context.Background(), model.VersionAnalysisArtifactsReady{
		EventMeta: testMeta(), DocumentID: "d-1", VersionID: "v-1", OrgID: "org-1",
		ArtifactTypes: []model.ArtifactType{model.ArtifactTypeRiskAnalysis},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if broker.topic != "dm.events.version-analysis-ready" {
		t.Errorf("topic = %q, want %q", broker.topic, "dm.events.version-analysis-ready")
	}
}

func TestPublishVersionReportsReady_CorrectTopic(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)

	err := pub.PublishVersionReportsReady(context.Background(), model.VersionReportsReady{
		EventMeta: testMeta(), DocumentID: "d-1", VersionID: "v-1", OrgID: "org-1",
		ArtifactTypes: []model.ArtifactType{model.ArtifactTypeExportPDF},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if broker.topic != "dm.events.version-reports-ready" {
		t.Errorf("topic = %q, want %q", broker.topic, "dm.events.version-reports-ready")
	}
}

func TestPublishVersionCreated_CorrectTopic(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)

	err := pub.PublishVersionCreated(context.Background(), model.VersionCreated{
		EventMeta: testMeta(), DocumentID: "d-1", VersionID: "v-1",
		VersionNumber: 1, OrgID: "org-1", OriginType: model.OriginTypeUpload,
		CreatedByUserID: "user-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if broker.topic != "dm.events.version-created" {
		t.Errorf("topic = %q, want %q", broker.topic, "dm.events.version-created")
	}
}

func TestPublishVersionPartiallyAvailable_CorrectTopic(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)

	err := pub.PublishVersionPartiallyAvailable(context.Background(), model.VersionPartiallyAvailable{
		EventMeta: testMeta(), DocumentID: "d-1", VersionID: "v-1", OrgID: "org-1",
		ArtifactStatus: model.ArtifactStatusProcessingArtifactsReceived,
		AvailableTypes: []model.ArtifactType{model.ArtifactTypeSemanticTree},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if broker.topic != "dm.events.version-partially-available" {
		t.Errorf("topic = %q, want %q", broker.topic, "dm.events.version-partially-available")
	}
}

// --- JSON Format Tests ---

func TestPublishVersionProcessingArtifactsReady_JSONFormat(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)

	_ = pub.PublishVersionProcessingArtifactsReady(context.Background(), model.VersionProcessingArtifactsReady{
		EventMeta: testMeta(), DocumentID: "d-1", VersionID: "v-1", OrgID: "org-1",
		ArtifactTypes: []model.ArtifactType{model.ArtifactTypeSemanticTree},
	})

	var m map[string]any
	if err := json.Unmarshal(broker.payload, &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, key := range []string{"correlation_id", "timestamp", "document_id", "version_id", "organization_id", "artifact_types"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing key %q in JSON payload", key)
		}
	}
}

func TestPublishVersionCreated_JSONFormat(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)

	_ = pub.PublishVersionCreated(context.Background(), model.VersionCreated{
		EventMeta: testMeta(), DocumentID: "d-1", VersionID: "v-1",
		VersionNumber: 2, OrgID: "org-1", OriginType: model.OriginTypeReUpload,
		ParentVersionID: "v-0", CreatedByUserID: "user-1",
	})

	var m map[string]any
	if err := json.Unmarshal(broker.payload, &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, key := range []string{"correlation_id", "timestamp", "document_id", "version_id", "version_number", "organization_id", "origin_type", "parent_version_id", "created_by_user_id"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing key %q in JSON payload", key)
		}
	}
}

func TestPublishVersionPartiallyAvailable_JSONFormat(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)

	_ = pub.PublishVersionPartiallyAvailable(context.Background(), model.VersionPartiallyAvailable{
		EventMeta: testMeta(), DocumentID: "d-1", VersionID: "v-1", OrgID: "org-1",
		ArtifactStatus: model.ArtifactStatusProcessingArtifactsReceived,
		AvailableTypes: []model.ArtifactType{model.ArtifactTypeSemanticTree},
		FailedStage:    "legal_analysis",
		ErrorMessage:   "timeout",
	})

	var m map[string]any
	if err := json.Unmarshal(broker.payload, &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, key := range []string{"correlation_id", "timestamp", "document_id", "version_id", "organization_id", "artifact_status", "available_types", "failed_stage", "error_message"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing key %q in JSON payload", key)
		}
	}
}

// --- Round-Trip Tests ---

func TestPublishVersionProcessingArtifactsReady_RoundTrip(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)
	want := model.VersionProcessingArtifactsReady{
		EventMeta: testMeta(), DocumentID: "d-1", VersionID: "v-1", OrgID: "org-1",
		ArtifactTypes: []model.ArtifactType{
			model.ArtifactTypeOCRRaw, model.ArtifactTypeExtractedText,
			model.ArtifactTypeDocumentStructure, model.ArtifactTypeSemanticTree,
			model.ArtifactTypeProcessingWarnings,
		},
	}

	_ = pub.PublishVersionProcessingArtifactsReady(context.Background(), want)

	var got model.VersionProcessingArtifactsReady
	if err := json.Unmarshal(broker.payload, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.CorrelationID != want.CorrelationID {
		t.Errorf("CorrelationID = %q, want %q", got.CorrelationID, want.CorrelationID)
	}
	if !got.Timestamp.Equal(want.Timestamp) {
		t.Errorf("Timestamp = %v, want %v", got.Timestamp, want.Timestamp)
	}
	if got.DocumentID != want.DocumentID {
		t.Errorf("DocumentID = %q, want %q", got.DocumentID, want.DocumentID)
	}
	if got.VersionID != want.VersionID {
		t.Errorf("VersionID = %q, want %q", got.VersionID, want.VersionID)
	}
	if got.OrgID != want.OrgID {
		t.Errorf("OrgID = %q, want %q", got.OrgID, want.OrgID)
	}
	if len(got.ArtifactTypes) != 5 {
		t.Errorf("expected 5 artifact_types, got %d", len(got.ArtifactTypes))
	}
}

func TestPublishVersionAnalysisArtifactsReady_RoundTrip(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)
	want := model.VersionAnalysisArtifactsReady{
		EventMeta: testMeta(), DocumentID: "d-2", VersionID: "v-2", OrgID: "org-2",
		ArtifactTypes: []model.ArtifactType{
			model.ArtifactTypeClassificationResult, model.ArtifactTypeRiskAnalysis,
		},
	}

	_ = pub.PublishVersionAnalysisArtifactsReady(context.Background(), want)

	var got model.VersionAnalysisArtifactsReady
	if err := json.Unmarshal(broker.payload, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.CorrelationID != want.CorrelationID {
		t.Errorf("CorrelationID = %q, want %q", got.CorrelationID, want.CorrelationID)
	}
	if len(got.ArtifactTypes) != 2 {
		t.Errorf("expected 2 artifact_types, got %d", len(got.ArtifactTypes))
	}
}

func TestPublishVersionReportsReady_RoundTrip(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)
	want := model.VersionReportsReady{
		EventMeta: testMeta(), DocumentID: "d-3", VersionID: "v-3", OrgID: "org-3",
		ArtifactTypes: []model.ArtifactType{model.ArtifactTypeExportPDF, model.ArtifactTypeExportDOCX},
	}

	_ = pub.PublishVersionReportsReady(context.Background(), want)

	var got model.VersionReportsReady
	if err := json.Unmarshal(broker.payload, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.CorrelationID != want.CorrelationID {
		t.Errorf("CorrelationID = %q, want %q", got.CorrelationID, want.CorrelationID)
	}
	if len(got.ArtifactTypes) != 2 {
		t.Errorf("expected 2 artifact_types, got %d", len(got.ArtifactTypes))
	}
}

func TestPublishVersionCreated_RoundTrip(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)
	want := model.VersionCreated{
		EventMeta: testMeta(), DocumentID: "d-4", VersionID: "v-4",
		VersionNumber: 3, OrgID: "org-4", OriginType: model.OriginTypeRecommendationApplied,
		ParentVersionID: "v-3", CreatedByUserID: "user-1",
	}

	_ = pub.PublishVersionCreated(context.Background(), want)

	var got model.VersionCreated
	if err := json.Unmarshal(broker.payload, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.CorrelationID != want.CorrelationID {
		t.Errorf("CorrelationID = %q, want %q", got.CorrelationID, want.CorrelationID)
	}
	if got.VersionNumber != want.VersionNumber {
		t.Errorf("VersionNumber = %d, want %d", got.VersionNumber, want.VersionNumber)
	}
	if got.OriginType != want.OriginType {
		t.Errorf("OriginType = %q, want %q", got.OriginType, want.OriginType)
	}
	if got.ParentVersionID != want.ParentVersionID {
		t.Errorf("ParentVersionID = %q, want %q", got.ParentVersionID, want.ParentVersionID)
	}
	if got.CreatedByUserID != want.CreatedByUserID {
		t.Errorf("CreatedByUserID = %q, want %q", got.CreatedByUserID, want.CreatedByUserID)
	}
}

func TestPublishVersionPartiallyAvailable_RoundTrip(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)
	want := model.VersionPartiallyAvailable{
		EventMeta: testMeta(), DocumentID: "d-5", VersionID: "v-5", OrgID: "org-5",
		ArtifactStatus: model.ArtifactStatusProcessingArtifactsReceived,
		AvailableTypes: []model.ArtifactType{model.ArtifactTypeOCRRaw, model.ArtifactTypeExtractedText},
		FailedStage:    "legal_analysis",
		ErrorMessage:   "LIC timeout",
	}

	_ = pub.PublishVersionPartiallyAvailable(context.Background(), want)

	var got model.VersionPartiallyAvailable
	if err := json.Unmarshal(broker.payload, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.CorrelationID != want.CorrelationID {
		t.Errorf("CorrelationID = %q, want %q", got.CorrelationID, want.CorrelationID)
	}
	if got.ArtifactStatus != want.ArtifactStatus {
		t.Errorf("ArtifactStatus = %q, want %q", got.ArtifactStatus, want.ArtifactStatus)
	}
	if len(got.AvailableTypes) != 2 {
		t.Errorf("expected 2 available_types, got %d", len(got.AvailableTypes))
	}
	if got.FailedStage != want.FailedStage {
		t.Errorf("FailedStage = %q, want %q", got.FailedStage, want.FailedStage)
	}
	if got.ErrorMessage != want.ErrorMessage {
		t.Errorf("ErrorMessage = %q, want %q", got.ErrorMessage, want.ErrorMessage)
	}
}

// --- Error Passthrough Tests ---

func TestPublish_BrokerError(t *testing.T) {
	brokerErr := port.NewBrokerError("connection lost", errors.New("tcp reset"))
	broker := &mockBroker{err: brokerErr}
	pub := newTestPublisher(broker)

	err := pub.PublishVersionCreated(context.Background(), model.VersionCreated{
		EventMeta: testMeta(), DocumentID: "d-1", VersionID: "v-1",
		VersionNumber: 1, OrgID: "org-1", OriginType: model.OriginTypeUpload,
		CreatedByUserID: "user-1",
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
}

func TestPublish_ContextCanceled(t *testing.T) {
	broker := &mockBroker{err: context.Canceled}
	pub := newTestPublisher(broker)

	err := pub.PublishVersionProcessingArtifactsReady(context.Background(), model.VersionProcessingArtifactsReady{
		EventMeta: testMeta(), DocumentID: "d-1", VersionID: "v-1", OrgID: "org-1",
		ArtifactTypes: []model.ArtifactType{model.ArtifactTypeSemanticTree},
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

	err := pub.PublishVersionReportsReady(context.Background(), model.VersionReportsReady{
		EventMeta: testMeta(), DocumentID: "d-1", VersionID: "v-1", OrgID: "org-1",
		ArtifactTypes: []model.ArtifactType{model.ArtifactTypeExportPDF},
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
	pub := NewNotificationPublisher(cb, testBrokerConfig())

	_ = pub.PublishVersionCreated(ctx, model.VersionCreated{
		EventMeta: testMeta(), DocumentID: "d-1", VersionID: "v-1",
		VersionNumber: 1, OrgID: "org-1", OriginType: model.OriginTypeUpload,
		CreatedByUserID: "user-1",
	})

	if cb.ctx == nil {
		t.Fatal("context was not captured")
	}
	if cb.ctx.Value(ctxKey{}) != "test-val" {
		t.Error("context was not forwarded to broker.Publish")
	}
}

// --- OmitEmpty Tests ---

func TestPublishVersionCreated_OmitsEmptyParentVersionID(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)

	_ = pub.PublishVersionCreated(context.Background(), model.VersionCreated{
		EventMeta: testMeta(), DocumentID: "d-1", VersionID: "v-1",
		VersionNumber: 1, OrgID: "org-1", OriginType: model.OriginTypeUpload,
		CreatedByUserID: "user-1",
		// No ParentVersionID.
	})

	var m map[string]any
	if err := json.Unmarshal(broker.payload, &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := m["parent_version_id"]; ok {
		t.Error("expected 'parent_version_id' to be omitted for first version")
	}
}

func TestPublishVersionPartiallyAvailable_OmitsEmptyOptionalFields(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)

	_ = pub.PublishVersionPartiallyAvailable(context.Background(), model.VersionPartiallyAvailable{
		EventMeta: testMeta(), DocumentID: "d-1", VersionID: "v-1", OrgID: "org-1",
		ArtifactStatus: model.ArtifactStatusProcessingArtifactsReceived,
		AvailableTypes: []model.ArtifactType{model.ArtifactTypeSemanticTree},
		// No FailedStage, ErrorMessage.
	})

	var m map[string]any
	if err := json.Unmarshal(broker.payload, &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, key := range []string{"failed_stage", "error_message"} {
		if _, ok := m[key]; ok {
			t.Errorf("expected %q to be omitted when empty", key)
		}
	}
}

// --- Correlation ID Propagation Test ---

func TestPublish_CorrelationIDPreserved(t *testing.T) {
	broker := &mockBroker{}
	pub := newTestPublisher(broker)

	meta := model.EventMeta{CorrelationID: "trace-xyz-789", Timestamp: testTimestamp()}

	events := []struct {
		name string
		fn   func()
	}{
		{"VersionProcessingArtifactsReady", func() {
			_ = pub.PublishVersionProcessingArtifactsReady(context.Background(), model.VersionProcessingArtifactsReady{
				EventMeta: meta, DocumentID: "d-1", VersionID: "v-1", OrgID: "org-1",
				ArtifactTypes: []model.ArtifactType{model.ArtifactTypeSemanticTree},
			})
		}},
		{"VersionCreated", func() {
			_ = pub.PublishVersionCreated(context.Background(), model.VersionCreated{
				EventMeta: meta, DocumentID: "d-1", VersionID: "v-1",
				VersionNumber: 1, OrgID: "org-1", OriginType: model.OriginTypeUpload,
				CreatedByUserID: "user-1",
			})
		}},
		{"VersionPartiallyAvailable", func() {
			_ = pub.PublishVersionPartiallyAvailable(context.Background(), model.VersionPartiallyAvailable{
				EventMeta: meta, DocumentID: "d-1", VersionID: "v-1", OrgID: "org-1",
				ArtifactStatus: model.ArtifactStatusProcessingArtifactsReceived,
				AvailableTypes: []model.ArtifactType{model.ArtifactTypeSemanticTree},
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
			if m["correlation_id"] != "trace-xyz-789" {
				t.Errorf("correlation_id = %v, want %q", m["correlation_id"], "trace-xyz-789")
			}
		})
	}
}
