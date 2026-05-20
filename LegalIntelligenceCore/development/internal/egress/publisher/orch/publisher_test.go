package orch

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
	"contractpro/legal-intelligence-core/internal/infra/broker"
)

// -----------------------------------------------------------------------------
// Test fakes (in-package — same hermetic boundary as the production source).
// -----------------------------------------------------------------------------

// fakePublisher is a concurrency-safe in-memory Publisher seam. Captures the
// last invocation's exchange / routingKey / payload and an atomically counted
// call total so the success-on-concurrency test (T26) can pin the publish
// count without mutex contention on the assertion.
type fakePublisher struct {
	mu             sync.Mutex
	calledCount    atomic.Int64
	lastExchange   string
	lastRoutingKey string
	lastPayload    []byte
	// returnErr is returned from every Publish call; nil ⇒ success.
	returnErr error
}

func (f *fakePublisher) Publish(_ context.Context, exchange, routingKey string, payload []byte) error {
	f.calledCount.Add(1)
	f.mu.Lock()
	f.lastExchange = exchange
	f.lastRoutingKey = routingKey
	// Defensive copy: the caller hands us a slice from json.Marshal; we
	// may need to inspect it after additional calls (concurrency test).
	cp := make([]byte, len(payload))
	copy(cp, payload)
	f.lastPayload = cp
	f.mu.Unlock()
	return f.returnErr
}

func (f *fakePublisher) Calls() int64 { return f.calledCount.Load() }

// fakeMetricRecord captures one IncPublish call.
type fakeMetricRecord struct {
	Topic   string
	Outcome PublishOutcome
}

// fakeMetrics is a concurrency-safe in-memory Metrics seam.
type fakeMetrics struct {
	mu      sync.Mutex
	records []fakeMetricRecord
}

func (f *fakeMetrics) IncPublish(topic string, outcome PublishOutcome) {
	f.mu.Lock()
	f.records = append(f.records, fakeMetricRecord{Topic: topic, Outcome: outcome})
	f.mu.Unlock()
}

func (f *fakeMetrics) Records() []fakeMetricRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeMetricRecord, len(f.records))
	copy(out, f.records)
	return out
}

// fakeClock returns a fixed time.Time.
type fakeClock struct {
	now time.Time
}

func (f fakeClock) Now() time.Time { return f.now }

// fakeLogger captures Warn/Error call counts.
type fakeLogger struct {
	mu         sync.Mutex
	warnCount  int
	errorCount int
}

func (f *fakeLogger) Warn(context.Context, string, ...any) {
	f.mu.Lock()
	f.warnCount++
	f.mu.Unlock()
}

func (f *fakeLogger) Error(context.Context, string, ...any) {
	f.mu.Lock()
	f.errorCount++
	f.mu.Unlock()
}

// -----------------------------------------------------------------------------
// Test fixtures
// -----------------------------------------------------------------------------

const (
	testExchange       = "lic.events"
	testCorrelationID  = "corr-1"
	testJobID          = "job-1"
	testDocumentID     = "doc-1"
	testVersionID      = "ver-1"
	testOrganizationID = "org-1"
)

// fixedTime is the deterministic clock tick used across tests. UTC.
var fixedTime = time.Date(2026, 5, 20, 10, 30, 45, 123456789, time.UTC)

// boolPtr is a tiny helper for *bool fixtures.
func boolPtr(b bool) *bool { return &b }

// validInProgress builds a baseline IN_PROGRESS event with all 5 IDs +
// Stage set + no FAILED-only fields. Used as the baseline for tests that
// vary one field at a time.
func validInProgress() port.LICStatusChangedEvent {
	return port.LICStatusChangedEvent{
		CorrelationID:  testCorrelationID,
		Timestamp:      "STALE-WILL-BE-REWRITTEN",
		JobID:          testJobID,
		DocumentID:     testDocumentID,
		VersionID:      testVersionID,
		OrganizationID: testOrganizationID,
		Status:         model.StatusInProgress,
		Stage:          model.StageReceived,
	}
}

// validCompleted builds a baseline COMPLETED event (no Stage, no FAILED
// fields).
func validCompleted() port.LICStatusChangedEvent {
	return port.LICStatusChangedEvent{
		CorrelationID:  testCorrelationID,
		Timestamp:      "STALE-WILL-BE-REWRITTEN",
		JobID:          testJobID,
		DocumentID:     testDocumentID,
		VersionID:      testVersionID,
		OrganizationID: testOrganizationID,
		Status:         model.StatusCompleted,
	}
}

// validFailed builds a baseline FAILED event with a publishable ErrorCode
// and IsRetryable set. ErrorMessage is intentionally caller-supplied junk
// so the catalog-rewrite contract (T18/T19) can be pinned.
func validFailed() port.LICStatusChangedEvent {
	return port.LICStatusChangedEvent{
		CorrelationID:  testCorrelationID,
		Timestamp:      "STALE-WILL-BE-REWRITTEN",
		JobID:          testJobID,
		DocumentID:     testDocumentID,
		VersionID:      testVersionID,
		OrganizationID: testOrganizationID,
		Status:         model.StatusFailed,
		Stage:          model.StageAgentRiskDetection,
		ErrorCode:      model.ErrCodeDMArtifactsTimeout,
		ErrorMessage:   "", // catalog-rewrite path (T18)
		IsRetryable:    boolPtr(true),
	}
}

// newTestPublisher builds a fresh StatusPublisher with the supplied seams.
// Optional publisher; nil ⇒ a fresh fakePublisher.
func newTestPublisher(t *testing.T, pub Publisher, m Metrics, c Clock, l Logger) *StatusPublisher {
	t.Helper()
	if pub == nil {
		pub = &fakePublisher{}
	}
	p, err := NewStatusPublisher(StatusPublisherConfig{Exchange: testExchange}, StatusPublisherDeps{
		Publisher: pub,
		Metrics:   m,
		Clock:     c,
		Logger:    l,
	})
	if err != nil {
		t.Fatalf("NewStatusPublisher: %v", err)
	}
	return p
}

func requireOneRecord(t *testing.T, m *fakeMetrics) fakeMetricRecord {
	t.Helper()
	recs := m.Records()
	if len(recs) != 1 {
		t.Fatalf("want exactly 1 metric record; got %d (%+v)", len(recs), recs)
	}
	return recs[0]
}

// -----------------------------------------------------------------------------
// T1: success path IN_PROGRESS — publish on Exchange/topic, payload contains
// all 5 IDs + Status + Stage + timestamp; metric=success.
// -----------------------------------------------------------------------------

func TestPublishStatus_Success_PublishesEnvelope(t *testing.T) {
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	clk := fakeClock{now: fixedTime}
	p := newTestPublisher(t, pub, met, clk, &fakeLogger{})

	if err := p.PublishStatus(context.Background(), validInProgress()); err != nil {
		t.Fatalf("PublishStatus: %v", err)
	}

	// Publisher called once on configured exchange + status-changed topic.
	if got := pub.Calls(); got != 1 {
		t.Fatalf("publisher calls: want 1; got %d", got)
	}
	if pub.lastExchange != testExchange {
		t.Errorf("exchange: want %q; got %q", testExchange, pub.lastExchange)
	}
	if pub.lastRoutingKey != "lic.events.status-changed" {
		t.Errorf("routingKey: want %q; got %q", "lic.events.status-changed", pub.lastRoutingKey)
	}

	// Payload contains all required fields.
	var got map[string]json.RawMessage
	if err := json.Unmarshal(pub.lastPayload, &got); err != nil {
		t.Fatalf("payload not valid JSON: %v", err)
	}
	wantKeys := []string{
		"correlation_id", "timestamp", "job_id", "document_id",
		"version_id", "organization_id", "status", "stage",
	}
	for _, k := range wantKeys {
		if _, ok := got[k]; !ok {
			t.Errorf("payload missing field %q", k)
		}
	}
	// FAILED-only fields MUST be absent for IN_PROGRESS.
	for _, k := range []string{"error_code", "error_message", "is_retryable"} {
		if _, present := got[k]; present {
			t.Errorf("payload must NOT contain %q for IN_PROGRESS; got %s", k, string(pub.lastPayload))
		}
	}

	// Status / Stage round-trip.
	var typed struct {
		Status         string `json:"status"`
		Stage          string `json:"stage"`
		OrganizationID string `json:"organization_id"`
	}
	if err := json.Unmarshal(pub.lastPayload, &typed); err != nil {
		t.Fatalf("payload typed unmarshal: %v", err)
	}
	if typed.Status != string(model.StatusInProgress) {
		t.Errorf("status: want %q; got %q", model.StatusInProgress, typed.Status)
	}
	if typed.Stage != string(model.StageReceived) {
		t.Errorf("stage: want %q; got %q", model.StageReceived, typed.Stage)
	}
	if typed.OrganizationID != testOrganizationID {
		t.Errorf("organization_id: want %q; got %q", testOrganizationID, typed.OrganizationID)
	}

	// Metric: exactly one record, topic+outcome=success.
	rec := requireOneRecord(t, met)
	if rec.Topic != "lic.events.status-changed" {
		t.Errorf("metric topic: want %q; got %q", "lic.events.status-changed", rec.Topic)
	}
	if rec.Outcome != PublishOutcomeSuccess {
		t.Errorf("metric outcome: want %q; got %q", PublishOutcomeSuccess, rec.Outcome)
	}

	// Timestamp on wire equals clock.Now (T2 anchor — full assertion in T2).
	var ts struct {
		Timestamp string `json:"timestamp"`
	}
	if err := json.Unmarshal(pub.lastPayload, &ts); err != nil {
		t.Fatalf("payload timestamp unmarshal: %v", err)
	}
	if ts.Timestamp != fixedTime.Format(time.RFC3339Nano) {
		t.Errorf("timestamp: want %q; got %q", fixedTime.Format(time.RFC3339Nano), ts.Timestamp)
	}
}

// -----------------------------------------------------------------------------
// T2: timestamp = clock.Now().Format(RFC3339Nano) UTC + caller-side variable
// unchanged (value semantics).
// -----------------------------------------------------------------------------

func TestPublishStatus_Timestamp_RewrittenInWire_CallerUnchanged(t *testing.T) {
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	clk := fakeClock{now: fixedTime}
	p := newTestPublisher(t, pub, met, clk, &fakeLogger{})

	caller := validInProgress()
	caller.Timestamp = "STALE-CALLER-VALUE"
	originalTS := caller.Timestamp

	if err := p.PublishStatus(context.Background(), caller); err != nil {
		t.Fatalf("PublishStatus: %v", err)
	}

	// Wire payload carries the clock-supplied timestamp.
	var got struct {
		Timestamp string `json:"timestamp"`
	}
	if err := json.Unmarshal(pub.lastPayload, &got); err != nil {
		t.Fatalf("payload not valid JSON: %v", err)
	}
	wantTS := fixedTime.Format(time.RFC3339Nano)
	if got.Timestamp != wantTS {
		t.Errorf("wire timestamp: want %q; got %q", wantTS, got.Timestamp)
	}
	parsed, err := time.Parse(time.RFC3339Nano, got.Timestamp)
	if err != nil {
		t.Fatalf("wire timestamp not RFC3339Nano-parseable: %v", err)
	}
	if parsed.Location().String() != "UTC" {
		t.Errorf("wire timestamp not UTC: %s", parsed.Location().String())
	}

	// Caller-side payload variable MUST be unchanged (value semantics —
	// PublishStatus takes payload by value).
	if caller.Timestamp != originalTS {
		t.Errorf("caller-side payload Timestamp mutated: want %q; got %q",
			originalTS, caller.Timestamp)
	}
}

// -----------------------------------------------------------------------------
// T3..T7: 5 envelope-ID validation failures.
// -----------------------------------------------------------------------------

func TestPublishStatus_IDValidation(t *testing.T) {
	cases := []struct {
		name       string
		mutate     func(p *port.LICStatusChangedEvent)
		wantReason string
	}{
		{"T3_empty_correlation_id", func(p *port.LICStatusChangedEvent) { p.CorrelationID = "" }, reasonMissingCorrelationID},
		{"T4_empty_job_id", func(p *port.LICStatusChangedEvent) { p.JobID = "" }, reasonMissingJobID},
		{"T5_empty_document_id", func(p *port.LICStatusChangedEvent) { p.DocumentID = "" }, reasonMissingDocumentID},
		{"T6_empty_version_id", func(p *port.LICStatusChangedEvent) { p.VersionID = "" }, reasonMissingVersionID},
		{"T7_empty_organization_id", func(p *port.LICStatusChangedEvent) { p.OrganizationID = "" }, reasonMissingOrganizationID},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			pub := &fakePublisher{}
			met := &fakeMetrics{}
			p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

			payload := validInProgress()
			tc.mutate(&payload)

			err := p.PublishStatus(context.Background(), payload)

			var pe *PublishError
			if !errors.As(err, &pe) {
				t.Fatalf("want *PublishError; got %T %v", err, err)
			}
			if pe.Reason != tc.wantReason {
				t.Errorf("reason: want %q; got %q", tc.wantReason, pe.Reason)
			}
			if pe.Cause != nil {
				t.Errorf("cause: want nil; got %v", pe.Cause)
			}
			if got := pub.Calls(); got != 0 {
				t.Errorf("publisher must NOT be called on validation failure; got %d calls", got)
			}
			rec := requireOneRecord(t, met)
			if rec.Outcome != PublishOutcomeInvalid {
				t.Errorf("metric outcome: want %q; got %q", PublishOutcomeInvalid, rec.Outcome)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// T8: invalid Status — empty + "FOO" both rejected via IsValid.
// -----------------------------------------------------------------------------

func TestPublishStatus_InvalidStatus(t *testing.T) {
	cases := []struct {
		name   string
		status model.ExternalStatus
	}{
		{"empty_status", model.ExternalStatus("")},
		{"unknown_status_FOO", model.ExternalStatus("FOO")},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			pub := &fakePublisher{}
			met := &fakeMetrics{}
			p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

			payload := validInProgress()
			payload.Status = tc.status

			err := p.PublishStatus(context.Background(), payload)
			var pe *PublishError
			if !errors.As(err, &pe) {
				t.Fatalf("want *PublishError; got %T %v", err, err)
			}
			if pe.Reason != reasonInvalidStatus {
				t.Errorf("reason: want %q; got %q", reasonInvalidStatus, pe.Reason)
			}
			if got := pub.Calls(); got != 0 {
				t.Errorf("publisher must NOT be called; got %d", got)
			}
			rec := requireOneRecord(t, met)
			if rec.Outcome != PublishOutcomeInvalid {
				t.Errorf("metric outcome: want %q; got %q", PublishOutcomeInvalid, rec.Outcome)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// T9: invalid Stage (non-empty + !IsValid) → reasonInvalidStage.
// -----------------------------------------------------------------------------

func TestPublishStatus_InvalidStage(t *testing.T) {
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	payload := validInProgress()
	payload.Stage = model.Stage("STAGE_BOGUS")

	err := p.PublishStatus(context.Background(), payload)
	var pe *PublishError
	if !errors.As(err, &pe) {
		t.Fatalf("want *PublishError; got %T %v", err, err)
	}
	if pe.Reason != reasonInvalidStage {
		t.Errorf("reason: want %q; got %q", reasonInvalidStage, pe.Reason)
	}
	if got := pub.Calls(); got != 0 {
		t.Errorf("publisher must NOT be called; got %d", got)
	}
	rec := requireOneRecord(t, met)
	if rec.Outcome != PublishOutcomeInvalid {
		t.Errorf("metric outcome: want %q; got %q", PublishOutcomeInvalid, rec.Outcome)
	}
}

// -----------------------------------------------------------------------------
// T10: empty Stage allowed for COMPLETED — success, payload omits "stage".
// -----------------------------------------------------------------------------

func TestPublishStatus_EmptyStage_Allowed_Completed(t *testing.T) {
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	if err := p.PublishStatus(context.Background(), validCompleted()); err != nil {
		t.Fatalf("PublishStatus on COMPLETED with empty Stage: %v", err)
	}
	if got := pub.Calls(); got != 1 {
		t.Errorf("publisher calls: want 1; got %d", got)
	}

	var got map[string]json.RawMessage
	if err := json.Unmarshal(pub.lastPayload, &got); err != nil {
		t.Fatalf("payload not valid JSON: %v", err)
	}
	if _, present := got["stage"]; present {
		t.Errorf("payload must NOT contain stage when empty (omitempty); got %s", string(pub.lastPayload))
	}
	// FAILED-only fields MUST be absent for COMPLETED.
	for _, k := range []string{"error_code", "error_message", "is_retryable"} {
		if _, present := got[k]; present {
			t.Errorf("payload must NOT contain %q for COMPLETED; got %s", k, string(pub.lastPayload))
		}
	}

	rec := requireOneRecord(t, met)
	if rec.Outcome != PublishOutcomeSuccess {
		t.Errorf("metric outcome: want %q; got %q", PublishOutcomeSuccess, rec.Outcome)
	}
}

// -----------------------------------------------------------------------------
// T11: FAILED + missing ErrorCode → reasonMissingErrorCode.
// -----------------------------------------------------------------------------

func TestPublishStatus_Failed_MissingErrorCode(t *testing.T) {
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	payload := validFailed()
	payload.ErrorCode = ""

	err := p.PublishStatus(context.Background(), payload)
	var pe *PublishError
	if !errors.As(err, &pe) {
		t.Fatalf("want *PublishError; got %T %v", err, err)
	}
	if pe.Reason != reasonMissingErrorCode {
		t.Errorf("reason: want %q; got %q", reasonMissingErrorCode, pe.Reason)
	}
	if got := pub.Calls(); got != 0 {
		t.Errorf("publisher must NOT be called; got %d", got)
	}
	rec := requireOneRecord(t, met)
	if rec.Outcome != PublishOutcomeInvalid {
		t.Errorf("metric outcome: want %q; got %q", PublishOutcomeInvalid, rec.Outcome)
	}
}

// -----------------------------------------------------------------------------
// T12: FAILED + non-publishable ErrorCode (INVALID_MESSAGE_SCHEMA) →
// reasonNonPublishableErrorCode.
// -----------------------------------------------------------------------------

func TestPublishStatus_Failed_NonPublishableErrorCode(t *testing.T) {
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	payload := validFailed()
	payload.ErrorCode = model.ErrCodeInvalidMessageSchema

	err := p.PublishStatus(context.Background(), payload)
	var pe *PublishError
	if !errors.As(err, &pe) {
		t.Fatalf("want *PublishError; got %T %v", err, err)
	}
	if pe.Reason != reasonNonPublishableErrorCode {
		t.Errorf("reason: want %q; got %q", reasonNonPublishableErrorCode, pe.Reason)
	}
	if got := pub.Calls(); got != 0 {
		t.Errorf("publisher must NOT be called; got %d", got)
	}
	rec := requireOneRecord(t, met)
	if rec.Outcome != PublishOutcomeInvalid {
		t.Errorf("metric outcome: want %q; got %q", PublishOutcomeInvalid, rec.Outcome)
	}
}

// -----------------------------------------------------------------------------
// T13: FAILED + unknown ErrorCode ("BOGUS") → reasonNonPublishableErrorCode
// (IsPublishableToOrchestrator returns false for unregistered codes).
// -----------------------------------------------------------------------------

func TestPublishStatus_Failed_UnknownErrorCode(t *testing.T) {
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	payload := validFailed()
	payload.ErrorCode = model.ErrorCode("BOGUS")

	err := p.PublishStatus(context.Background(), payload)
	var pe *PublishError
	if !errors.As(err, &pe) {
		t.Fatalf("want *PublishError; got %T %v", err, err)
	}
	if pe.Reason != reasonNonPublishableErrorCode {
		t.Errorf("reason: want %q; got %q", reasonNonPublishableErrorCode, pe.Reason)
	}
	if got := pub.Calls(); got != 0 {
		t.Errorf("publisher must NOT be called; got %d", got)
	}
}

// -----------------------------------------------------------------------------
// T14: FAILED + IsRetryable == nil → reasonMissingRetryable.
// -----------------------------------------------------------------------------

func TestPublishStatus_Failed_MissingRetryable(t *testing.T) {
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	payload := validFailed()
	payload.IsRetryable = nil

	err := p.PublishStatus(context.Background(), payload)
	var pe *PublishError
	if !errors.As(err, &pe) {
		t.Fatalf("want *PublishError; got %T %v", err, err)
	}
	if pe.Reason != reasonMissingRetryable {
		t.Errorf("reason: want %q; got %q", reasonMissingRetryable, pe.Reason)
	}
	if got := pub.Calls(); got != 0 {
		t.Errorf("publisher must NOT be called; got %d", got)
	}
}

// -----------------------------------------------------------------------------
// T15: IN_PROGRESS + ErrorCode set → reasonUnexpectedFailureFields
// (stale FAILED-fields leak guard).
// -----------------------------------------------------------------------------

func TestPublishStatus_InProgress_StaleErrorCode_Rejected(t *testing.T) {
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	payload := validInProgress()
	payload.ErrorCode = model.ErrCodeAnalysisTimeout

	err := p.PublishStatus(context.Background(), payload)
	var pe *PublishError
	if !errors.As(err, &pe) {
		t.Fatalf("want *PublishError; got %T %v", err, err)
	}
	if pe.Reason != reasonUnexpectedFailureFields {
		t.Errorf("reason: want %q; got %q", reasonUnexpectedFailureFields, pe.Reason)
	}
	if got := pub.Calls(); got != 0 {
		t.Errorf("publisher must NOT be called; got %d", got)
	}
}

// -----------------------------------------------------------------------------
// T16: COMPLETED + ErrorMessage set → reasonUnexpectedFailureFields.
// -----------------------------------------------------------------------------

func TestPublishStatus_Completed_StaleErrorMessage_Rejected(t *testing.T) {
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	payload := validCompleted()
	payload.ErrorMessage = "leftover from a previous transition"

	err := p.PublishStatus(context.Background(), payload)
	var pe *PublishError
	if !errors.As(err, &pe) {
		t.Fatalf("want *PublishError; got %T %v", err, err)
	}
	if pe.Reason != reasonUnexpectedFailureFields {
		t.Errorf("reason: want %q; got %q", reasonUnexpectedFailureFields, pe.Reason)
	}
	if got := pub.Calls(); got != 0 {
		t.Errorf("publisher must NOT be called; got %d", got)
	}
}

// -----------------------------------------------------------------------------
// T17: COMPLETED + IsRetryable != nil → reasonUnexpectedFailureFields.
// -----------------------------------------------------------------------------

func TestPublishStatus_Completed_StaleRetryable_Rejected(t *testing.T) {
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	payload := validCompleted()
	payload.IsRetryable = boolPtr(false)

	err := p.PublishStatus(context.Background(), payload)
	var pe *PublishError
	if !errors.As(err, &pe) {
		t.Fatalf("want *PublishError; got %T %v", err, err)
	}
	if pe.Reason != reasonUnexpectedFailureFields {
		t.Errorf("reason: want %q; got %q", reasonUnexpectedFailureFields, pe.Reason)
	}
	if got := pub.Calls(); got != 0 {
		t.Errorf("publisher must NOT be called; got %d", got)
	}
}

// -----------------------------------------------------------------------------
// T18: FAILED success path — wire ErrorMessage rewritten from
// LookupErrorSpec(code).UserMessage (RU). Caller supplied "" → wire gets the
// catalog string.
// -----------------------------------------------------------------------------

func TestPublishStatus_Failed_ErrorMessage_RewrittenFromCatalog(t *testing.T) {
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	payload := validFailed()
	payload.ErrorMessage = "" // catalog should fill in

	if err := p.PublishStatus(context.Background(), payload); err != nil {
		t.Fatalf("PublishStatus: %v", err)
	}

	// Catalog lookup for the same code — wire MUST match spec.UserMessage.
	spec, ok := model.LookupErrorSpec(model.ErrCodeDMArtifactsTimeout)
	if !ok {
		t.Fatal("test fixture broken: ErrCodeDMArtifactsTimeout not in catalog")
	}
	if spec.UserMessage == "" {
		t.Fatal("test fixture broken: catalog UserMessage for DMArtifactsTimeout is empty")
	}

	var got struct {
		ErrorMessage string `json:"error_message"`
		ErrorCode    string `json:"error_code"`
		IsRetryable  *bool  `json:"is_retryable"`
	}
	if err := json.Unmarshal(pub.lastPayload, &got); err != nil {
		t.Fatalf("payload not valid JSON: %v", err)
	}
	if got.ErrorMessage != spec.UserMessage {
		t.Errorf("wire error_message: want %q (from catalog); got %q",
			spec.UserMessage, got.ErrorMessage)
	}
	if got.ErrorCode != string(model.ErrCodeDMArtifactsTimeout) {
		t.Errorf("wire error_code: want %q; got %q",
			model.ErrCodeDMArtifactsTimeout, got.ErrorCode)
	}
	if got.IsRetryable == nil || *got.IsRetryable != true {
		t.Errorf("wire is_retryable: want *true; got %v", got.IsRetryable)
	}

	rec := requireOneRecord(t, met)
	if rec.Outcome != PublishOutcomeSuccess {
		t.Errorf("metric outcome: want %q; got %q", PublishOutcomeSuccess, rec.Outcome)
	}
}

// -----------------------------------------------------------------------------
// T19: FAILED success path — caller-supplied junk ErrorMessage is OVERWRITTEN
// by catalog UserMessage; caller-side variable unchanged.
// -----------------------------------------------------------------------------

func TestPublishStatus_Failed_CallerErrorMessage_OverwrittenAndUnchanged(t *testing.T) {
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	const junk = "internal-stack-trace-do-not-publish"
	caller := validFailed()
	caller.ErrorMessage = junk

	if err := p.PublishStatus(context.Background(), caller); err != nil {
		t.Fatalf("PublishStatus: %v", err)
	}

	// Wire side carries catalog string, NOT the junk.
	spec, _ := model.LookupErrorSpec(model.ErrCodeDMArtifactsTimeout)
	var got struct {
		ErrorMessage string `json:"error_message"`
	}
	if err := json.Unmarshal(pub.lastPayload, &got); err != nil {
		t.Fatalf("payload not valid JSON: %v", err)
	}
	if got.ErrorMessage != spec.UserMessage {
		t.Errorf("wire error_message: want %q; got %q", spec.UserMessage, got.ErrorMessage)
	}
	if got.ErrorMessage == junk {
		t.Errorf("wire error_message must NOT echo caller junk; got %q", got.ErrorMessage)
	}

	// Caller-side variable unchanged (value semantics).
	if caller.ErrorMessage != junk {
		t.Errorf("caller-side ErrorMessage mutated: want %q; got %q",
			junk, caller.ErrorMessage)
	}
}

// -----------------------------------------------------------------------------
// T20: marshal-failure path — override marshalStatus seam → reason +
// metric=failure; broker NOT called.
// -----------------------------------------------------------------------------

func TestPublishStatus_MarshalFailure_EmitsFailureMetricAndPublishesNothing(t *testing.T) {
	originalMarshal := marshalStatus
	defer func() { marshalStatus = originalMarshal }()

	wantCause := errors.New("synthetic marshal failure")
	marshalStatus = func(_ any) ([]byte, error) { return nil, wantCause }

	pub := &fakePublisher{}
	met := &fakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	err := p.PublishStatus(context.Background(), validInProgress())

	var pe *PublishError
	if !errors.As(err, &pe) {
		t.Fatalf("want *PublishError; got %T (%v)", err, err)
	}
	if pe.Reason != reasonMarshalFailure {
		t.Errorf("Reason: want %q; got %q", reasonMarshalFailure, pe.Reason)
	}
	if !errors.Is(pe, wantCause) {
		t.Errorf("Cause: want errors.Is to reach %v; got false", wantCause)
	}
	if got := pub.Calls(); got != 0 {
		t.Errorf("publisher must NOT be called on marshal failure; got %d calls", got)
	}
	rec := requireOneRecord(t, met)
	if rec.Outcome != PublishOutcomeFailure {
		t.Errorf("metric outcome: want %q; got %q", PublishOutcomeFailure, rec.Outcome)
	}
	if rec.Topic != "lic.events.status-changed" {
		t.Errorf("metric topic: want lic.events.status-changed; got %q", rec.Topic)
	}
}

// -----------------------------------------------------------------------------
// T21: broker success → nil + metric=success.
// -----------------------------------------------------------------------------

func TestPublishStatus_BrokerSuccess(t *testing.T) {
	pub := &fakePublisher{returnErr: nil}
	met := &fakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	if err := p.PublishStatus(context.Background(), validInProgress()); err != nil {
		t.Fatalf("PublishStatus: want nil err; got %v", err)
	}
	rec := requireOneRecord(t, met)
	if rec.Outcome != PublishOutcomeSuccess {
		t.Errorf("metric outcome: want %q; got %q", PublishOutcomeSuccess, rec.Outcome)
	}
}

// -----------------------------------------------------------------------------
// T22: broker NACK → raw err + metric=nacked.
// -----------------------------------------------------------------------------

func TestPublishStatus_BrokerNack(t *testing.T) {
	nackErr := &broker.BrokerError{Op: "Publish", Retryable: true, Cause: broker.ErrPublishNack}
	pub := &fakePublisher{returnErr: nackErr}
	met := &fakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	err := p.PublishStatus(context.Background(), validInProgress())
	if !errors.Is(err, broker.ErrPublishNack) {
		t.Errorf("want errors.Is(err, broker.ErrPublishNack); got %v", err)
	}
	var be *broker.BrokerError
	if !errors.As(err, &be) {
		t.Errorf("want errors.As → *broker.BrokerError; got %T", err)
	}
	rec := requireOneRecord(t, met)
	if rec.Outcome != PublishOutcomeNacked {
		t.Errorf("metric outcome: want %q; got %q", PublishOutcomeNacked, rec.Outcome)
	}
}

// -----------------------------------------------------------------------------
// T23: broker ConfirmTimeout → raw err + metric=failure.
// -----------------------------------------------------------------------------

func TestPublishStatus_BrokerConfirmTimeout(t *testing.T) {
	cttErr := &broker.BrokerError{Op: "Publish", Retryable: true, Cause: broker.ErrConfirmTimeout}
	pub := &fakePublisher{returnErr: cttErr}
	met := &fakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	err := p.PublishStatus(context.Background(), validInProgress())
	if !errors.Is(err, broker.ErrConfirmTimeout) {
		t.Errorf("want errors.Is(err, broker.ErrConfirmTimeout); got %v", err)
	}
	rec := requireOneRecord(t, met)
	if rec.Outcome != PublishOutcomeFailure {
		t.Errorf("metric outcome: want %q; got %q", PublishOutcomeFailure, rec.Outcome)
	}
}

// -----------------------------------------------------------------------------
// T24: broker NotConnected / non-retryable AMQP / unknown → raw err +
// metric=failure.
// -----------------------------------------------------------------------------

func TestPublishStatus_BrokerOtherFailures(t *testing.T) {
	cases := []struct {
		name      string
		returnErr error
		wantIs    error // nil ⇒ no Is check
	}{
		{
			"not_connected",
			&broker.BrokerError{Op: "Publish", Retryable: true, Cause: broker.ErrNotConnected},
			broker.ErrNotConnected,
		},
		{
			"non_retryable_404",
			&broker.BrokerError{Op: "Publish", Retryable: false, Cause: errors.New("404 NotFound")},
			nil,
		},
		{
			"unknown_error",
			errors.New("some other transport error"),
			nil,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			pub := &fakePublisher{returnErr: tc.returnErr}
			met := &fakeMetrics{}
			p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

			err := p.PublishStatus(context.Background(), validInProgress())
			if err == nil {
				t.Fatal("want non-nil err; got nil")
			}
			if tc.wantIs != nil && !errors.Is(err, tc.wantIs) {
				t.Errorf("errors.Is(err, %v): want true; got false (err=%v)", tc.wantIs, err)
			}
			rec := requireOneRecord(t, met)
			if rec.Outcome != PublishOutcomeFailure {
				t.Errorf("metric outcome: want %q; got %q", PublishOutcomeFailure, rec.Outcome)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// T25: ctx.Canceled / ctx.DeadlineExceeded → raw ctx.Err() pass-through +
// metric=failure. NOT wrapped in *PublishError.
// -----------------------------------------------------------------------------

func TestPublishStatus_ContextErrors_RawPassthrough(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"context_canceled", context.Canceled},
		{"context_deadline_exceeded", context.DeadlineExceeded},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			pub := &fakePublisher{returnErr: tc.err}
			met := &fakeMetrics{}
			p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

			err := p.PublishStatus(context.Background(), validInProgress())
			if !errors.Is(err, tc.err) {
				t.Errorf("want errors.Is(err, %v); got %v", tc.err, err)
			}
			// Raw pass-through: NOT wrapped in *PublishError.
			var pe *PublishError
			if errors.As(err, &pe) {
				t.Errorf("ctx error must NOT be wrapped in *PublishError; got %v", err)
			}
			rec := requireOneRecord(t, met)
			if rec.Outcome != PublishOutcomeFailure {
				t.Errorf("metric outcome: want %q; got %q", PublishOutcomeFailure, rec.Outcome)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// T26: concurrent — 16 goroutines × distinct correlationIDs, -race clean.
// -----------------------------------------------------------------------------

func TestPublishStatus_Concurrent_DistinctCorrelationIDs(t *testing.T) {
	const N = 16
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	var wg sync.WaitGroup
	wg.Add(N)
	var errCount atomic.Int64
	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			payload := validInProgress()
			payload.CorrelationID = "corr-" + strconv.Itoa(i)
			if err := p.PublishStatus(context.Background(), payload); err != nil {
				errCount.Add(1)
				t.Errorf("g%d: PublishStatus: %v", i, err)
			}
		}()
	}
	wg.Wait()

	if errCount.Load() != 0 {
		t.Fatalf("concurrent publish errors: %d", errCount.Load())
	}
	if got := pub.Calls(); got != N {
		t.Errorf("publisher calls: want %d; got %d", N, got)
	}
	recs := met.Records()
	if len(recs) != N {
		t.Fatalf("metric records: want %d; got %d", N, len(recs))
	}
	for _, rec := range recs {
		if rec.Outcome != PublishOutcomeSuccess {
			t.Errorf("metric outcome: want all success; got %q", rec.Outcome)
		}
		if rec.Topic != "lic.events.status-changed" {
			t.Errorf("metric topic: want lic.events.status-changed; got %q", rec.Topic)
		}
	}
}

// -----------------------------------------------------------------------------
// T-CTOR-1: empty Exchange → wiring error contains "Exchange".
// -----------------------------------------------------------------------------

func TestNewStatusPublisher_EmptyExchange_ConstructorError(t *testing.T) {
	p, err := NewStatusPublisher(StatusPublisherConfig{Exchange: ""}, StatusPublisherDeps{
		Publisher: &fakePublisher{},
	})
	if err == nil {
		t.Fatal("want error on empty Exchange; got nil")
	}
	if p != nil {
		t.Errorf("want nil publisher on error; got %+v", p)
	}
	if got := err.Error(); !strings.Contains(got, "StatusPublisherConfig.Exchange") {
		t.Errorf("error must name the offending field (StatusPublisherConfig.Exchange); got %q", got)
	}
}

// -----------------------------------------------------------------------------
// T-CTOR-2: nil Publisher → wiring error contains "Publisher".
// -----------------------------------------------------------------------------

func TestNewStatusPublisher_NilPublisher_ConstructorError(t *testing.T) {
	p, err := NewStatusPublisher(StatusPublisherConfig{Exchange: testExchange}, StatusPublisherDeps{
		Publisher: nil,
		Metrics:   &fakeMetrics{},
		Clock:     fakeClock{now: fixedTime},
		Logger:    &fakeLogger{},
	})
	if err == nil {
		t.Fatal("want error on nil Publisher; got nil")
	}
	if p != nil {
		t.Errorf("want nil publisher on error; got %+v", p)
	}
	if got := err.Error(); !strings.Contains(got, "StatusPublisherDeps.Publisher") {
		t.Errorf("error must name the offending field (StatusPublisherDeps.Publisher); got %q", got)
	}
}

// -----------------------------------------------------------------------------
// T-CTOR-3: both defects (nil Publisher + empty Exchange) → errors.Join
// surfaces both substrings.
// -----------------------------------------------------------------------------

func TestNewStatusPublisher_BothDefects_ErrorsJoinSurfacesBoth(t *testing.T) {
	p, err := NewStatusPublisher(StatusPublisherConfig{Exchange: ""}, StatusPublisherDeps{
		Publisher: nil,
	})
	if err == nil {
		t.Fatal("want error on both defects; got nil")
	}
	if p != nil {
		t.Errorf("want nil publisher on error; got %+v", p)
	}
	got := err.Error()
	if !strings.Contains(got, "StatusPublisherDeps.Publisher") {
		t.Errorf("error must mention StatusPublisherDeps.Publisher defect; got %q", got)
	}
	if !strings.Contains(got, "StatusPublisherConfig.Exchange") {
		t.Errorf("error must mention StatusPublisherConfig.Exchange defect; got %q", got)
	}
}

// -----------------------------------------------------------------------------
// T-CTOR-EXTRA: nil optional seams (Metrics/Clock/Logger) → noop defaults;
// PublishStatus works without panic. Anchors the build-spec D2 contract
// (optional seams substituted by withDefaults; Publisher REQUIRED).
// -----------------------------------------------------------------------------

func TestNewStatusPublisher_NilOptionalSeams_NoopDefaults(t *testing.T) {
	pub := &fakePublisher{}
	p, err := NewStatusPublisher(StatusPublisherConfig{Exchange: testExchange}, StatusPublisherDeps{
		Publisher: pub,
		// Metrics / Clock / Logger intentionally nil — must get noop defaults.
	})
	if err != nil {
		t.Fatalf("NewStatusPublisher with nil optional seams: %v", err)
	}
	if p == nil {
		t.Fatal("want non-nil publisher; got nil")
	}
	// Hot path must work without panicking on the noop seams.
	if err := p.PublishStatus(context.Background(), validInProgress()); err != nil {
		t.Errorf("PublishStatus: %v", err)
	}
	if got := pub.Calls(); got != 1 {
		t.Errorf("publisher calls: want 1; got %d", got)
	}
}

// -----------------------------------------------------------------------------
// classifyOutcome — table-driven test over every documented branch.
// -----------------------------------------------------------------------------

func TestClassifyOutcome_AllBranches(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want PublishOutcome
	}{
		{"nil_success", nil, PublishOutcomeSuccess},
		{"context_canceled_raw", context.Canceled, PublishOutcomeFailure},
		{"context_deadline_exceeded_raw", context.DeadlineExceeded, PublishOutcomeFailure},
		{"broker_nack_sentinel", broker.ErrPublishNack, PublishOutcomeNacked},
		{
			"broker_nack_wrapped",
			&broker.BrokerError{Op: "Publish", Retryable: true, Cause: broker.ErrPublishNack},
			PublishOutcomeNacked,
		},
		{"broker_confirm_timeout_sentinel", broker.ErrConfirmTimeout, PublishOutcomeFailure},
		{
			"broker_confirm_timeout_wrapped",
			&broker.BrokerError{Op: "Publish", Retryable: true, Cause: broker.ErrConfirmTimeout},
			PublishOutcomeFailure,
		},
		{"broker_not_connected_sentinel", broker.ErrNotConnected, PublishOutcomeFailure},
		{
			"broker_not_connected_wrapped",
			&broker.BrokerError{Op: "Publish", Retryable: true, Cause: broker.ErrNotConnected},
			PublishOutcomeFailure,
		},
		{
			"broker_non_retryable_404",
			&broker.BrokerError{Op: "Publish", Retryable: false, Cause: errors.New("404 NotFound")},
			PublishOutcomeFailure,
		},
		{"unknown_error", errors.New("some other error"), PublishOutcomeFailure},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := classifyOutcome(tc.err)
			if got != tc.want {
				t.Errorf("classifyOutcome(%v): want %q; got %q", tc.err, tc.want, got)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// PublishError — Error/Unwrap behaviour.
// -----------------------------------------------------------------------------

func TestPublishError_ErrorAndUnwrap(t *testing.T) {
	// nil receiver guard.
	var nilErr *PublishError
	if got := nilErr.Error(); got != "<nil>" {
		t.Errorf("nil receiver Error: want <nil>; got %q", got)
	}
	if got := nilErr.Unwrap(); got != nil {
		t.Errorf("nil receiver Unwrap: want nil; got %v", got)
	}

	// With Cause: Error format includes Cause; Unwrap returns Cause.
	cause := errors.New("underlying")
	pe := &PublishError{Reason: reasonMarshalFailure, Cause: cause}
	if !errors.Is(pe, cause) {
		t.Errorf("errors.Is should reach Cause; got false")
	}
	wantSubstr := "marshal_failure"
	if got := pe.Error(); !strings.Contains(got, wantSubstr) {
		t.Errorf("Error: want substring %q; got %q", wantSubstr, got)
	}

	// Without Cause: Unwrap returns nil; Error has no `: <cause>` tail.
	pe2 := &PublishError{Reason: reasonMissingJobID}
	if got := pe2.Unwrap(); got != nil {
		t.Errorf("Unwrap: want nil; got %v", got)
	}
	if got := pe2.Error(); strings.Contains(got, ": <nil>") {
		t.Errorf("Error without Cause must not include ': <nil>' tail; got %q", got)
	}
}
