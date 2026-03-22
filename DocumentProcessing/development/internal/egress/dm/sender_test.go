package dm

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"contractpro/document-processing/internal/config"
	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
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

// --- Helpers ---

func testBrokerConfig() config.BrokerConfig {
	return config.BrokerConfig{
		TopicArtifactsReady:  "dp.artifacts.processing-ready",
		TopicSemanticTreeReq: "dp.requests.semantic-tree",
		TopicDiffReady:       "dp.artifacts.diff-ready",
	}
}

func testTimestamp() time.Time {
	return time.Date(2026, 3, 22, 12, 0, 0, 0, time.UTC)
}

func testMeta() model.EventMeta {
	return model.EventMeta{
		CorrelationID: "corr-001",
		Timestamp:     testTimestamp(),
	}
}

func testArtifactsReadyEvent() model.DocumentProcessingArtifactsReady {
	return model.DocumentProcessingArtifactsReady{
		EventMeta:  testMeta(),
		JobID:      "job-art-1",
		DocumentID: "doc-art-1",
		OCRRaw: model.OCRRawArtifact{
			RawText: "OCR recognized text content",
			Status:  model.OCRStatusApplicable,
		},
		Text: model.ExtractedText{
			DocumentID: "doc-art-1",
			Pages: []model.PageText{
				{PageNumber: 1, Text: "Page one content"},
				{PageNumber: 2, Text: "Page two content"},
			},
		},
		Structure: model.DocumentStructure{
			DocumentID: "doc-art-1",
			Sections: []model.Section{
				{
					Number:  "1",
					Title:   "Предмет договора",
					Content: "Раздел о предмете договора",
					Clauses: []model.Clause{
						{Number: "1.1", Content: "Исполнитель обязуется..."},
					},
				},
			},
			Appendices: []model.Appendix{
				{Number: "1", Title: "Приложение 1", Content: "Приложение content"},
			},
			PartyDetails: []model.PartyDetails{
				{Name: "ООО Рога и Копыта", INN: "7701234567"},
			},
		},
		SemanticTree: model.SemanticTree{
			DocumentID: "doc-art-1",
			Root: &model.SemanticNode{
				ID:   "root",
				Type: model.NodeTypeRoot,
				Children: []*model.SemanticNode{
					{
						ID:      "sec-1",
						Type:    model.NodeTypeSection,
						Content: "Предмет договора",
					},
				},
			},
		},
		Warnings: []model.ProcessingWarning{
			{
				Code:    "LOW_QUALITY_OCR",
				Message: "OCR confidence below threshold",
				Stage:   model.ProcessingStageOCR,
			},
		},
	}
}

func testDiffReadyEvent() model.DocumentVersionDiffReady {
	return model.DocumentVersionDiffReady{
		EventMeta:       testMeta(),
		JobID:           "job-diff-1",
		DocumentID:      "doc-diff-1",
		BaseVersionID:   "v1",
		TargetVersionID: "v2",
		TextDiffs: []model.TextDiffEntry{
			{
				Type:       model.DiffTypeModified,
				Path:       "1.1",
				OldContent: "old clause text",
				NewContent: "new clause text",
			},
			{
				Type:       model.DiffTypeAdded,
				Path:       "2.1",
				NewContent: "added clause",
			},
		},
		StructuralDiffs: []model.StructuralDiffEntry{
			{
				Type:        model.DiffTypeRemoved,
				NodeType:    model.NodeTypeSection,
				NodeID:      "sec-3",
				Path:        "3",
				Description: "Section 3 removed",
			},
		},
	}
}

func testSemanticTreeRequest() model.GetSemanticTreeRequest {
	return model.GetSemanticTreeRequest{
		EventMeta:  testMeta(),
		JobID:      "job-tree-1",
		DocumentID: "doc-tree-1",
		VersionID:  "v1",
	}
}

// --- Tests ---

// 1. Interface Compliance

func TestInterfaceCompliance(t *testing.T) {
	broker := &mockBroker{}
	sender := NewSender(broker, testBrokerConfig())

	var artifactSender port.DMArtifactSenderPort = sender
	if artifactSender == nil {
		t.Fatal("Sender does not satisfy DMArtifactSenderPort")
	}

	var treeRequester port.DMTreeRequesterPort = sender
	if treeRequester == nil {
		t.Fatal("Sender does not satisfy DMTreeRequesterPort")
	}
}

// 2. Correct Topic Routing

func TestSendArtifacts_CorrectTopic(t *testing.T) {
	broker := &mockBroker{}
	sender := NewSender(broker, testBrokerConfig())

	err := sender.SendArtifacts(context.Background(), testArtifactsReadyEvent())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if broker.topic != "dp.artifacts.processing-ready" {
		t.Errorf("topic = %q, want %q", broker.topic, "dp.artifacts.processing-ready")
	}
}

func TestSendDiffResult_CorrectTopic(t *testing.T) {
	broker := &mockBroker{}
	sender := NewSender(broker, testBrokerConfig())

	err := sender.SendDiffResult(context.Background(), testDiffReadyEvent())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if broker.topic != "dp.artifacts.diff-ready" {
		t.Errorf("topic = %q, want %q", broker.topic, "dp.artifacts.diff-ready")
	}
}

func TestRequestSemanticTree_CorrectTopic(t *testing.T) {
	broker := &mockBroker{}
	sender := NewSender(broker, testBrokerConfig())

	err := sender.RequestSemanticTree(context.Background(), testSemanticTreeRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if broker.topic != "dp.requests.semantic-tree" {
		t.Errorf("topic = %q, want %q", broker.topic, "dp.requests.semantic-tree")
	}
}

// 3. JSON Format

func TestSendArtifacts_JSONFormat(t *testing.T) {
	broker := &mockBroker{}
	sender := NewSender(broker, testBrokerConfig())

	_ = sender.SendArtifacts(context.Background(), testArtifactsReadyEvent())

	var m map[string]any
	if err := json.Unmarshal(broker.payload, &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, key := range []string{
		"correlation_id", "timestamp", "job_id", "document_id",
		"ocr_raw", "text", "structure", "semantic_tree",
	} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing key %q in JSON payload", key)
		}
	}
}

func TestSendDiffResult_JSONFormat(t *testing.T) {
	broker := &mockBroker{}
	sender := NewSender(broker, testBrokerConfig())

	_ = sender.SendDiffResult(context.Background(), testDiffReadyEvent())

	var m map[string]any
	if err := json.Unmarshal(broker.payload, &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, key := range []string{
		"correlation_id", "timestamp", "job_id", "document_id",
		"base_version_id", "target_version_id", "text_diffs", "structural_diffs",
	} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing key %q in JSON payload", key)
		}
	}
}

func TestRequestSemanticTree_JSONFormat(t *testing.T) {
	broker := &mockBroker{}
	sender := NewSender(broker, testBrokerConfig())

	_ = sender.RequestSemanticTree(context.Background(), testSemanticTreeRequest())

	var m map[string]any
	if err := json.Unmarshal(broker.payload, &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, key := range []string{
		"correlation_id", "timestamp", "job_id", "document_id", "version_id",
	} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing key %q in JSON payload", key)
		}
	}
}

// 4. Round-Trip

func TestSendArtifacts_RoundTrip(t *testing.T) {
	broker := &mockBroker{}
	sender := NewSender(broker, testBrokerConfig())
	want := testArtifactsReadyEvent()

	_ = sender.SendArtifacts(context.Background(), want)

	var got model.DocumentProcessingArtifactsReady
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
	if got.OCRRaw.Status != want.OCRRaw.Status {
		t.Errorf("OCRRaw.Status = %q, want %q", got.OCRRaw.Status, want.OCRRaw.Status)
	}
	if got.OCRRaw.RawText != want.OCRRaw.RawText {
		t.Errorf("OCRRaw.RawText = %q, want %q", got.OCRRaw.RawText, want.OCRRaw.RawText)
	}
	if len(got.Text.Pages) != len(want.Text.Pages) {
		t.Fatalf("Text.Pages length = %d, want %d", len(got.Text.Pages), len(want.Text.Pages))
	}
	for i := range want.Text.Pages {
		if got.Text.Pages[i].PageNumber != want.Text.Pages[i].PageNumber {
			t.Errorf("Text.Pages[%d].PageNumber = %d, want %d", i, got.Text.Pages[i].PageNumber, want.Text.Pages[i].PageNumber)
		}
		if got.Text.Pages[i].Text != want.Text.Pages[i].Text {
			t.Errorf("Text.Pages[%d].Text = %q, want %q", i, got.Text.Pages[i].Text, want.Text.Pages[i].Text)
		}
	}
	if len(got.Structure.Sections) != len(want.Structure.Sections) {
		t.Fatalf("Structure.Sections length = %d, want %d", len(got.Structure.Sections), len(want.Structure.Sections))
	}
	if got.Structure.Sections[0].Title != want.Structure.Sections[0].Title {
		t.Errorf("Structure.Sections[0].Title = %q, want %q", got.Structure.Sections[0].Title, want.Structure.Sections[0].Title)
	}
	if got.SemanticTree.Root == nil {
		t.Fatal("SemanticTree.Root is nil after round-trip")
	}
	if got.SemanticTree.Root.ID != want.SemanticTree.Root.ID {
		t.Errorf("SemanticTree.Root.ID = %q, want %q", got.SemanticTree.Root.ID, want.SemanticTree.Root.ID)
	}
	if len(got.SemanticTree.Root.Children) != len(want.SemanticTree.Root.Children) {
		t.Fatalf("SemanticTree.Root.Children length = %d, want %d", len(got.SemanticTree.Root.Children), len(want.SemanticTree.Root.Children))
	}
	if got.SemanticTree.Root.Children[0].Content != want.SemanticTree.Root.Children[0].Content {
		t.Errorf("SemanticTree.Root.Children[0].Content = %q, want %q", got.SemanticTree.Root.Children[0].Content, want.SemanticTree.Root.Children[0].Content)
	}
	if len(got.Warnings) != len(want.Warnings) {
		t.Fatalf("Warnings length = %d, want %d", len(got.Warnings), len(want.Warnings))
	}
	if got.Warnings[0].Code != want.Warnings[0].Code {
		t.Errorf("Warnings[0].Code = %q, want %q", got.Warnings[0].Code, want.Warnings[0].Code)
	}
	if got.Warnings[0].Message != want.Warnings[0].Message {
		t.Errorf("Warnings[0].Message = %q, want %q", got.Warnings[0].Message, want.Warnings[0].Message)
	}
	if got.Warnings[0].Stage != want.Warnings[0].Stage {
		t.Errorf("Warnings[0].Stage = %q, want %q", got.Warnings[0].Stage, want.Warnings[0].Stage)
	}
}

func TestSendDiffResult_RoundTrip(t *testing.T) {
	broker := &mockBroker{}
	sender := NewSender(broker, testBrokerConfig())
	want := testDiffReadyEvent()

	_ = sender.SendDiffResult(context.Background(), want)

	var got model.DocumentVersionDiffReady
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
	if got.BaseVersionID != want.BaseVersionID {
		t.Errorf("BaseVersionID = %q, want %q", got.BaseVersionID, want.BaseVersionID)
	}
	if got.TargetVersionID != want.TargetVersionID {
		t.Errorf("TargetVersionID = %q, want %q", got.TargetVersionID, want.TargetVersionID)
	}
	if len(got.TextDiffs) != len(want.TextDiffs) {
		t.Fatalf("TextDiffs length = %d, want %d", len(got.TextDiffs), len(want.TextDiffs))
	}
	for i := range want.TextDiffs {
		if got.TextDiffs[i].Type != want.TextDiffs[i].Type {
			t.Errorf("TextDiffs[%d].Type = %q, want %q", i, got.TextDiffs[i].Type, want.TextDiffs[i].Type)
		}
		if got.TextDiffs[i].Path != want.TextDiffs[i].Path {
			t.Errorf("TextDiffs[%d].Path = %q, want %q", i, got.TextDiffs[i].Path, want.TextDiffs[i].Path)
		}
		if got.TextDiffs[i].OldContent != want.TextDiffs[i].OldContent {
			t.Errorf("TextDiffs[%d].OldContent = %q, want %q", i, got.TextDiffs[i].OldContent, want.TextDiffs[i].OldContent)
		}
		if got.TextDiffs[i].NewContent != want.TextDiffs[i].NewContent {
			t.Errorf("TextDiffs[%d].NewContent = %q, want %q", i, got.TextDiffs[i].NewContent, want.TextDiffs[i].NewContent)
		}
	}
	if len(got.StructuralDiffs) != len(want.StructuralDiffs) {
		t.Fatalf("StructuralDiffs length = %d, want %d", len(got.StructuralDiffs), len(want.StructuralDiffs))
	}
	if got.StructuralDiffs[0].Type != want.StructuralDiffs[0].Type {
		t.Errorf("StructuralDiffs[0].Type = %q, want %q", got.StructuralDiffs[0].Type, want.StructuralDiffs[0].Type)
	}
	if got.StructuralDiffs[0].NodeType != want.StructuralDiffs[0].NodeType {
		t.Errorf("StructuralDiffs[0].NodeType = %q, want %q", got.StructuralDiffs[0].NodeType, want.StructuralDiffs[0].NodeType)
	}
	if got.StructuralDiffs[0].NodeID != want.StructuralDiffs[0].NodeID {
		t.Errorf("StructuralDiffs[0].NodeID = %q, want %q", got.StructuralDiffs[0].NodeID, want.StructuralDiffs[0].NodeID)
	}
	if got.StructuralDiffs[0].Description != want.StructuralDiffs[0].Description {
		t.Errorf("StructuralDiffs[0].Description = %q, want %q", got.StructuralDiffs[0].Description, want.StructuralDiffs[0].Description)
	}
}

func TestRequestSemanticTree_RoundTrip(t *testing.T) {
	broker := &mockBroker{}
	sender := NewSender(broker, testBrokerConfig())
	want := testSemanticTreeRequest()

	_ = sender.RequestSemanticTree(context.Background(), want)

	var got model.GetSemanticTreeRequest
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
	if got.VersionID != want.VersionID {
		t.Errorf("VersionID = %q, want %q", got.VersionID, want.VersionID)
	}
}

// 5. Error Passthrough

func TestSend_BrokerError(t *testing.T) {
	brokerErr := port.NewBrokerError("connection lost", errors.New("tcp reset"))
	broker := &mockBroker{err: brokerErr}
	sender := NewSender(broker, testBrokerConfig())

	err := sender.SendArtifacts(context.Background(), testArtifactsReadyEvent())
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

func TestSend_ContextCanceled(t *testing.T) {
	broker := &mockBroker{err: context.Canceled}
	sender := NewSender(broker, testBrokerConfig())

	err := sender.SendDiffResult(context.Background(), testDiffReadyEvent())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want errors.Is(context.Canceled)", err)
	}
}

func TestSend_ContextDeadlineExceeded(t *testing.T) {
	broker := &mockBroker{err: context.DeadlineExceeded}
	sender := NewSender(broker, testBrokerConfig())

	err := sender.RequestSemanticTree(context.Background(), testSemanticTreeRequest())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error = %v, want errors.Is(context.DeadlineExceeded)", err)
	}
}

// 6. Marshal Error

func TestPublishJSON_MarshalError(t *testing.T) {
	broker := &mockBroker{}
	sender := NewSender(broker, testBrokerConfig())

	// Channels are not JSON-serializable.
	err := sender.publishJSON(context.Background(), "any-topic", make(chan int))
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
		t.Error("marshal errors should NOT be retryable (deterministic programming error)")
	}
}

// 7. Constructor Validation

func TestNewSender_NilBrokerPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for nil broker, got none")
		}
	}()
	NewSender(nil, testBrokerConfig())
}

func TestNewSender_EmptyArtifactsReadyTopicPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for empty TopicArtifactsReady, got none")
		}
	}()
	cfg := testBrokerConfig()
	cfg.TopicArtifactsReady = ""
	NewSender(&mockBroker{}, cfg)
}

func TestNewSender_EmptySemanticTreeReqTopicPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for empty TopicSemanticTreeReq, got none")
		}
	}()
	cfg := testBrokerConfig()
	cfg.TopicSemanticTreeReq = ""
	NewSender(&mockBroker{}, cfg)
}

func TestNewSender_EmptyDiffReadyTopicPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for empty TopicDiffReady, got none")
		}
	}()
	cfg := testBrokerConfig()
	cfg.TopicDiffReady = ""
	NewSender(&mockBroker{}, cfg)
}

// 8. Context Forwarding

func TestSend_ForwardsContext(t *testing.T) {
	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "test-val")

	ctxBroker := &ctxCapturingBroker{}
	sender := NewSender(ctxBroker, testBrokerConfig())

	_ = sender.SendArtifacts(ctx, testArtifactsReadyEvent())

	if ctxBroker.ctx == nil {
		t.Fatal("context was not captured")
	}
	if ctxBroker.ctx.Value(ctxKey{}) != "test-val" {
		t.Error("context was not forwarded to broker.Publish")
	}
}

// ctxCapturingBroker captures the context passed to Publish.
type ctxCapturingBroker struct {
	ctx context.Context
}

func (m *ctxCapturingBroker) Publish(ctx context.Context, _ string, _ []byte) error {
	m.ctx = ctx
	return nil
}

// 9. OmitEmpty Behavior

func TestSendArtifacts_OmitsEmptyWarnings(t *testing.T) {
	broker := &mockBroker{}
	sender := NewSender(broker, testBrokerConfig())

	event := testArtifactsReadyEvent()
	event.Warnings = nil // explicitly empty

	_ = sender.SendArtifacts(context.Background(), event)

	var m map[string]any
	if err := json.Unmarshal(broker.payload, &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := m["warnings"]; ok {
		t.Error("expected 'warnings' key to be omitted when empty, but it was present")
	}
}

func TestSendDiffResult_EmptyDiffsPresent(t *testing.T) {
	broker := &mockBroker{}
	sender := NewSender(broker, testBrokerConfig())

	event := testDiffReadyEvent()
	event.TextDiffs = []model.TextDiffEntry{}
	event.StructuralDiffs = []model.StructuralDiffEntry{}

	_ = sender.SendDiffResult(context.Background(), event)

	var m map[string]any
	if err := json.Unmarshal(broker.payload, &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := m["text_diffs"]; !ok {
		t.Error("expected 'text_diffs' key to be present for empty slice (no omitempty), but it was omitted")
	}
	if _, ok := m["structural_diffs"]; !ok {
		t.Error("expected 'structural_diffs' key to be present for empty slice (no omitempty), but it was omitted")
	}
}

// 10. Warnings field presence

func TestSendArtifacts_WarningsPresent(t *testing.T) {
	broker := &mockBroker{}
	sender := NewSender(broker, testBrokerConfig())

	event := testArtifactsReadyEvent() // has Warnings populated

	_ = sender.SendArtifacts(context.Background(), event)

	var m map[string]any
	if err := json.Unmarshal(broker.payload, &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := m["warnings"]; !ok {
		t.Error("expected 'warnings' key to be present when slice is non-empty, but it was omitted")
	}
}
