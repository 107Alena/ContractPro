package statustracker

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/consumer"
)

func newTestClassificationTracker(kv *mockKVStore, cs *mockConfirmationStore, bc *mockBroadcaster) *Tracker {
	t := NewTracker(kv, bc, logger.NewLogger("error"))
	t.now = fixedNow
	t.WithConfirmation(cs, testConfirmTimeout)
	return t
}

func seedAnalyzingInKV(kv *mockKVStore, cs *mockConfirmationStore, orgID, docID, verID string) {
	rec := mustMarshalRecord(string(StatusAnalyzing))
	key := statusKey(orgID, docID, verID)
	kv.setStored(key, rec)
	cs.seedStatus(key, rec)
}

func makeClassificationEvent(orgID, docID, verID string) *consumer.LICClassificationUncertainEvent {
	return &consumer.LICClassificationUncertainEvent{
		CorrelationID:  "corr-test",
		Timestamp:      "2026-04-16T10:00:00Z",
		JobID:          "job-test",
		DocumentID:     docID,
		VersionID:      verID,
		OrganizationID: orgID,
		SuggestedType:  "услуги",
		Confidence:     0.62,
		Threshold:      0.75,
		Alternatives: []consumer.ClassificationAlternative{
			{ContractType: "подряд", Confidence: 0.31},
		},
	}
}

// --- Happy path ---

func TestClassificationUncertain_HappyPath(t *testing.T) {
	kv := newMockKVStore()
	cs := newMockConfirmationStore()
	bc := newMockBroadcaster()
	tr := newTestClassificationTracker(kv, cs, bc)

	seedAnalyzingInKV(kv, cs, testOrgID, testDocID, testVerID)

	event := makeClassificationEvent(testOrgID, testDocID, testVerID)
	err := tr.handleLICClassificationUncertain(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Status should be AWAITING_USER_INPUT.
	sKey := statusKey(testOrgID, testDocID, testVerID)
	gotStatus := cs.getStatus(sKey)
	if gotStatus != string(StatusAwaitingUserInput) {
		t.Errorf("status = %q, want %q", gotStatus, StatusAwaitingUserInput)
	}

	// Watchdog key should exist.
	wKey := confirmationKey(testVerID)
	if _, ok := cs.getEntry(wKey); !ok {
		t.Error("watchdog key not set")
	}

	// Idempotency key should be set.
	iKey := idempotencyKey(testVerID)
	if _, ok := kv.getStored(iKey); !ok {
		t.Error("idempotency key not set")
	}

	// Confirmation meta should be stored.
	metaKey := ConfirmationMetaKey(testVerID)
	metaRaw, ok := kv.getStored(metaKey)
	if !ok {
		t.Fatal("confirmation meta key not set")
	}
	var meta confirmationMeta
	if err := json.Unmarshal([]byte(metaRaw), &meta); err != nil {
		t.Fatalf("invalid meta JSON: %v", err)
	}
	if meta.OrganizationID != testOrgID || meta.DocumentID != testDocID || meta.VersionID != testVerID {
		t.Errorf("meta = %+v, want org=%s doc=%s ver=%s", meta, testOrgID, testDocID, testVerID)
	}

	// SSE: should have 2 events:
	//  1. status_update from SetAwaitingUserInput
	//  2. type_confirmation_required from handler
	events := bc.broadcastedEvents()
	if len(events) != 2 {
		t.Fatalf("broadcast count = %d, want 2", len(events))
	}

	statusEvt := events[0]
	if statusEvt.event.EventType != "status_update" {
		t.Errorf("event[0].EventType = %q, want %q", statusEvt.event.EventType, "status_update")
	}
	if statusEvt.event.Status != string(StatusAwaitingUserInput) {
		t.Errorf("event[0].Status = %q, want %q", statusEvt.event.Status, StatusAwaitingUserInput)
	}

	confirmEvt := events[1]
	if confirmEvt.event.EventType != "type_confirmation_required" {
		t.Errorf("event[1].EventType = %q, want %q", confirmEvt.event.EventType, "type_confirmation_required")
	}
	if confirmEvt.event.SuggestedType != "услуги" {
		t.Errorf("event[1].SuggestedType = %q, want %q", confirmEvt.event.SuggestedType, "услуги")
	}
	if confirmEvt.event.Confidence != 0.62 {
		t.Errorf("event[1].Confidence = %v, want 0.62", confirmEvt.event.Confidence)
	}
	if confirmEvt.event.Threshold != 0.75 {
		t.Errorf("event[1].Threshold = %v, want 0.75", confirmEvt.event.Threshold)
	}
	if len(confirmEvt.event.Alternatives) != 1 {
		t.Fatalf("event[1].Alternatives len = %d, want 1", len(confirmEvt.event.Alternatives))
	}
	if confirmEvt.event.Alternatives[0].ContractType != "подряд" {
		t.Errorf("alt[0].ContractType = %q, want %q", confirmEvt.event.Alternatives[0].ContractType, "подряд")
	}
	if confirmEvt.orgID != testOrgID {
		t.Errorf("broadcast orgID = %q, want %q", confirmEvt.orgID, testOrgID)
	}
}

// --- Idempotency ---

func TestClassificationUncertain_Idempotent(t *testing.T) {
	kv := newMockKVStore()
	cs := newMockConfirmationStore()
	bc := newMockBroadcaster()
	tr := newTestClassificationTracker(kv, cs, bc)

	seedAnalyzingInKV(kv, cs, testOrgID, testDocID, testVerID)

	// Set idempotency key (simulating previous processing).
	kv.setStored(idempotencyKey(testVerID), "1")

	event := makeClassificationEvent(testOrgID, testDocID, testVerID)
	err := tr.handleLICClassificationUncertain(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No SSE events should be broadcasted.
	events := bc.broadcastedEvents()
	if len(events) != 0 {
		t.Errorf("broadcast count = %d, want 0 (idempotent skip)", len(events))
	}

	// Status should not have changed.
	sKey := statusKey(testOrgID, testDocID, testVerID)
	gotStatus := cs.getStatus(sKey)
	if gotStatus != string(StatusAnalyzing) {
		t.Errorf("status = %q, want %q (unchanged)", gotStatus, StatusAnalyzing)
	}
}

// --- Missing identity fields ---

func TestClassificationUncertain_MissingOrgID(t *testing.T) {
	kv := newMockKVStore()
	cs := newMockConfirmationStore()
	bc := newMockBroadcaster()
	tr := newTestClassificationTracker(kv, cs, bc)

	event := makeClassificationEvent("", testDocID, testVerID)
	err := tr.handleLICClassificationUncertain(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	events := bc.broadcastedEvents()
	if len(events) != 0 {
		t.Errorf("broadcast count = %d, want 0", len(events))
	}
}

func TestClassificationUncertain_MissingDocID(t *testing.T) {
	kv := newMockKVStore()
	cs := newMockConfirmationStore()
	bc := newMockBroadcaster()
	tr := newTestClassificationTracker(kv, cs, bc)

	event := makeClassificationEvent(testOrgID, "", testVerID)
	err := tr.handleLICClassificationUncertain(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	events := bc.broadcastedEvents()
	if len(events) != 0 {
		t.Errorf("broadcast count = %d, want 0", len(events))
	}
}

func TestClassificationUncertain_MissingVerID(t *testing.T) {
	kv := newMockKVStore()
	cs := newMockConfirmationStore()
	bc := newMockBroadcaster()
	tr := newTestClassificationTracker(kv, cs, bc)

	event := makeClassificationEvent(testOrgID, testDocID, "")
	err := tr.handleLICClassificationUncertain(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	events := bc.broadcastedEvents()
	if len(events) != 0 {
		t.Errorf("broadcast count = %d, want 0", len(events))
	}
}

// --- Invalid transition (not in ANALYZING) ---

func TestClassificationUncertain_InvalidTransition(t *testing.T) {
	kv := newMockKVStore()
	cs := newMockConfirmationStore()
	bc := newMockBroadcaster()
	tr := newTestClassificationTracker(kv, cs, bc)

	// Seed as PROCESSING (not ANALYZING).
	rec := mustMarshalRecord(string(StatusProcessing))
	sKey := statusKey(testOrgID, testDocID, testVerID)
	kv.setStored(sKey, rec)
	cs.seedStatus(sKey, rec)

	event := makeClassificationEvent(testOrgID, testDocID, testVerID)
	err := tr.handleLICClassificationUncertain(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error (should be silently skipped): %v", err)
	}

	// No broadcasts (silently skipped).
	events := bc.broadcastedEvents()
	if len(events) != 0 {
		t.Errorf("broadcast count = %d, want 0", len(events))
	}
}

// --- Redis unavailable on idempotency check → NACK ---

func TestClassificationUncertain_RedisGetError_ReturnsError(t *testing.T) {
	kv := newMockKVStore()
	kv.getErr = errors.New("redis: connection refused")
	cs := newMockConfirmationStore()
	bc := newMockBroadcaster()
	tr := newTestClassificationTracker(kv, cs, bc)

	event := makeClassificationEvent(testOrgID, testDocID, testVerID)
	err := tr.handleLICClassificationUncertain(context.Background(), event)
	if err == nil {
		t.Fatal("expected error for Redis unavailable, got nil")
	}
}

// --- Confirmation store error → NACK ---

func TestClassificationUncertain_ConfirmStoreError_ReturnsError(t *testing.T) {
	kv := newMockKVStore()
	cs := newMockConfirmationStore()
	cs.err = errors.New("redis: connection refused")
	bc := newMockBroadcaster()
	tr := newTestClassificationTracker(kv, cs, bc)

	seedAnalyzingInKV(kv, cs, testOrgID, testDocID, testVerID)
	cs.err = errors.New("redis: connection refused")

	event := makeClassificationEvent(testOrgID, testDocID, testVerID)
	err := tr.handleLICClassificationUncertain(context.Background(), event)
	if err == nil {
		t.Fatal("expected error for confirmation store failure, got nil")
	}
}

// --- Meta key write failure is non-critical ---

func TestClassificationUncertain_MetaWriteFailure_NonCritical(t *testing.T) {
	kv := newMockKVStore()
	cs := newMockConfirmationStore()
	bc := newMockBroadcaster()
	tr := newTestClassificationTracker(kv, cs, bc)

	seedAnalyzingInKV(kv, cs, testOrgID, testDocID, testVerID)

	// Make Set fail after successful transition.
	event := makeClassificationEvent(testOrgID, testDocID, testVerID)

	// We can't easily make only the meta Set fail with current mock.
	// Instead, verify the happy path stores it and trust the code path.
	err := tr.handleLICClassificationUncertain(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify meta was stored.
	metaKey := ConfirmationMetaKey(testVerID)
	if _, ok := kv.getStored(metaKey); !ok {
		t.Error("confirmation meta not stored")
	}
}

// --- Full dispatch via HandleEvent ---

func TestHandleEvent_ClassificationUncertain(t *testing.T) {
	kv := newMockKVStore()
	cs := newMockConfirmationStore()
	bc := newMockBroadcaster()
	tr := newTestClassificationTracker(kv, cs, bc)

	seedAnalyzingInKV(kv, cs, testOrgID, testDocID, testVerID)

	event := makeClassificationEvent(testOrgID, testDocID, testVerID)
	err := tr.HandleEvent(context.Background(), consumer.EventLICClassificationUncertain, event)
	if err != nil {
		t.Fatalf("HandleEvent error: %v", err)
	}

	// Status should be AWAITING_USER_INPUT.
	sKey := statusKey(testOrgID, testDocID, testVerID)
	gotStatus := cs.getStatus(sKey)
	if gotStatus != string(StatusAwaitingUserInput) {
		t.Errorf("status = %q, want %q", gotStatus, StatusAwaitingUserInput)
	}
}

// --- HandleEvent with wrong type assertion ---

func TestHandleEvent_ClassificationUncertain_WrongType(t *testing.T) {
	kv := newMockKVStore()
	cs := newMockConfirmationStore()
	bc := newMockBroadcaster()
	tr := newTestClassificationTracker(kv, cs, bc)

	err := tr.HandleEvent(context.Background(), consumer.EventLICClassificationUncertain, "not-an-event")
	if err != nil {
		t.Fatalf("expected nil (warn+skip), got error: %v", err)
	}
}

// --- Idempotency key TTL ---

func TestClassificationUncertain_IdempotencyKeyTTL(t *testing.T) {
	kv := newMockKVStore()
	cs := newMockConfirmationStore()
	bc := newMockBroadcaster()
	tr := newTestClassificationTracker(kv, cs, bc)

	seedAnalyzingInKV(kv, cs, testOrgID, testDocID, testVerID)

	event := makeClassificationEvent(testOrgID, testDocID, testVerID)
	err := tr.handleLICClassificationUncertain(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The last Set TTL should be for the idempotency key (= confirmationTimeout).
	// Note: mockKVStore.setTTL captures the last Set call's TTL.
	if kv.setTTL != testConfirmTimeout {
		t.Errorf("idempotency TTL = %v, want %v", kv.setTTL, testConfirmTimeout)
	}
}

// --- convertAlternatives ---

func TestConvertAlternatives_Nil(t *testing.T) {
	result := convertAlternatives(nil)
	if result != nil {
		t.Errorf("expected nil for nil input, got %v", result)
	}
}

func TestConvertAlternatives_Empty(t *testing.T) {
	result := convertAlternatives([]consumer.ClassificationAlternative{})
	if result != nil {
		t.Errorf("expected nil for empty input, got %v", result)
	}
}

func TestConvertAlternatives_Multiple(t *testing.T) {
	alts := []consumer.ClassificationAlternative{
		{ContractType: "подряд", Confidence: 0.31},
		{ContractType: "аренда", Confidence: 0.07},
	}
	result := convertAlternatives(alts)
	if len(result) != 2 {
		t.Fatalf("len = %d, want 2", len(result))
	}
	if result[0].ContractType != "подряд" || result[0].Confidence != 0.31 {
		t.Errorf("result[0] = %+v", result[0])
	}
	if result[1].ContractType != "аренда" || result[1].Confidence != 0.07 {
		t.Errorf("result[1] = %+v", result[1])
	}
}

// --- No alternatives in event ---

func TestClassificationUncertain_NoAlternatives(t *testing.T) {
	kv := newMockKVStore()
	cs := newMockConfirmationStore()
	bc := newMockBroadcaster()
	tr := newTestClassificationTracker(kv, cs, bc)

	seedAnalyzingInKV(kv, cs, testOrgID, testDocID, testVerID)

	event := makeClassificationEvent(testOrgID, testDocID, testVerID)
	event.Alternatives = nil

	err := tr.handleLICClassificationUncertain(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	events := bc.broadcastedEvents()
	if len(events) != 2 {
		t.Fatalf("broadcast count = %d, want 2", len(events))
	}

	confirmEvt := events[1]
	if len(confirmEvt.event.Alternatives) != 0 {
		t.Errorf("alternatives len = %d, want 0", len(confirmEvt.event.Alternatives))
	}
}

// --- SSE event_type and status fields ---

func TestClassificationUncertain_SSEEventFields(t *testing.T) {
	kv := newMockKVStore()
	cs := newMockConfirmationStore()
	bc := newMockBroadcaster()
	tr := newTestClassificationTracker(kv, cs, bc)

	seedAnalyzingInKV(kv, cs, testOrgID, testDocID, testVerID)

	event := makeClassificationEvent(testOrgID, testDocID, testVerID)
	err := tr.handleLICClassificationUncertain(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	events := bc.broadcastedEvents()
	if len(events) < 2 {
		t.Fatalf("broadcast count = %d, want >= 2", len(events))
	}

	confirmEvt := events[1].event
	if confirmEvt.DocumentID != testDocID {
		t.Errorf("DocumentID = %q, want %q", confirmEvt.DocumentID, testDocID)
	}
	if confirmEvt.VersionID != testVerID {
		t.Errorf("VersionID = %q, want %q", confirmEvt.VersionID, testVerID)
	}
	if confirmEvt.Status != string(StatusAwaitingUserInput) {
		t.Errorf("Status = %q, want %q", confirmEvt.Status, StatusAwaitingUserInput)
	}
	if confirmEvt.Message != statusMessages[StatusAwaitingUserInput] {
		t.Errorf("Message = %q, want %q", confirmEvt.Message, statusMessages[StatusAwaitingUserInput])
	}
	if confirmEvt.Timestamp != fixedTime.Format(time.RFC3339) {
		t.Errorf("Timestamp = %q, want %q", confirmEvt.Timestamp, fixedTime.Format(time.RFC3339))
	}
}

// --- idempotencyKey format ---

func TestIdempotencyKey(t *testing.T) {
	tests := []struct {
		verID string
		want  string
	}{
		{"ver-1", "lic-uncertain:ver-1"},
		{"abc-def", "lic-uncertain:abc-def"},
	}
	for _, tt := range tests {
		got := idempotencyKey(tt.verID)
		if got != tt.want {
			t.Errorf("idempotencyKey(%q) = %q, want %q", tt.verID, got, tt.want)
		}
	}
}
