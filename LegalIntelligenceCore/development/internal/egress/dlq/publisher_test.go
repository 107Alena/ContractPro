package dlq

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
// Test fakes
// -----------------------------------------------------------------------------

// fakePublisher is a concurrency-safe in-memory Publisher seam.
type fakePublisher struct {
	mu             sync.Mutex
	calledCount    atomic.Int64
	lastExchange   string
	lastRoutingKey string
	lastPayload    []byte
	returnErr      error
	allCalls       []fakePublishCall
}

type fakePublishCall struct {
	Exchange   string
	RoutingKey string
	Payload    []byte
}

func (f *fakePublisher) Publish(_ context.Context, exchange, routingKey string, payload []byte) error {
	f.calledCount.Add(1)
	f.mu.Lock()
	f.lastExchange = exchange
	f.lastRoutingKey = routingKey
	cp := make([]byte, len(payload))
	copy(cp, payload)
	f.lastPayload = cp
	f.allCalls = append(f.allCalls, fakePublishCall{
		Exchange:   exchange,
		RoutingKey: routingKey,
		Payload:    cp,
	})
	f.mu.Unlock()
	return f.returnErr
}

func (f *fakePublisher) Calls() int64 { return f.calledCount.Load() }

// fakeMetrics captures IncPublish + IncDLQPublished records.
type fakeMetrics struct {
	mu      sync.Mutex
	publish []fakeMetricRecord
	dlq     []fakeDLQRecord
}

type fakeMetricRecord struct {
	Topic   string
	Outcome PublishOutcome
}

type fakeDLQRecord struct {
	Topic  string
	Reason string
}

func (f *fakeMetrics) IncPublish(topic string, outcome PublishOutcome) {
	f.mu.Lock()
	f.publish = append(f.publish, fakeMetricRecord{Topic: topic, Outcome: outcome})
	f.mu.Unlock()
}

func (f *fakeMetrics) IncDLQPublished(topic string, reason string) {
	f.mu.Lock()
	f.dlq = append(f.dlq, fakeDLQRecord{Topic: topic, Reason: reason})
	f.mu.Unlock()
}

func (f *fakeMetrics) PublishRecords() []fakeMetricRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeMetricRecord, len(f.publish))
	copy(out, f.publish)
	return out
}

func (f *fakeMetrics) DLQRecords() []fakeDLQRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeDLQRecord, len(f.dlq))
	copy(out, f.dlq)
	return out
}

type fakeClock struct{ now time.Time }

func (f fakeClock) Now() time.Time { return f.now }

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
// Test constants & helpers
// -----------------------------------------------------------------------------

const (
	testExchange       = "contractpro.dlx"
	testCorrelationID  = "corr-1"
	testJobID          = "job-1"
	testDocumentID     = "doc-1"
	testVersionID      = "ver-1"
	testOrganizationID = "org-1"
)

var fixedTime = time.Date(2026, 5, 22, 10, 30, 45, 123456789, time.UTC)

// validEnvelope returns a base envelope satisfying every required field;
// FailedAt is intentionally left empty so the auto-stamp branch is the
// default (callers that need a pre-set FailedAt override it).
func validEnvelope() port.LICDLQEnvelope {
	return port.LICDLQEnvelope{
		OriginalTopic:            "dm.events.version-artifacts-ready",
		OriginalMessageHash:      "abc123",
		OriginalMessageSizeBytes: 512,
		ErrorCode:                model.ErrorCode("INVALID_MESSAGE_SCHEMA"),
		ErrorMessage:             "schema validation failed at field X",
		RetryCount:               0,
		CorrelationID:            testCorrelationID,
		JobID:                    testJobID,
		DocumentID:               testDocumentID,
		VersionID:                testVersionID,
		OrganizationID:           testOrganizationID,
	}
}

func newTestPublisher(t *testing.T, pub Publisher, m Metrics, c Clock, l Logger) *DLQPublisher {
	t.Helper()
	if pub == nil {
		pub = &fakePublisher{}
	}
	p, err := NewDLQPublisher(Config{Exchange: testExchange}, Deps{
		Publisher: pub,
		Metrics:   m,
		Clock:     c,
		Logger:    l,
	})
	if err != nil {
		t.Fatalf("NewDLQPublisher: %v", err)
	}
	return p
}

func requireOnePublishRecord(t *testing.T, m *fakeMetrics) fakeMetricRecord {
	t.Helper()
	recs := m.PublishRecords()
	if len(recs) != 1 {
		t.Fatalf("want exactly 1 IncPublish record; got %d (%+v)", len(recs), recs)
	}
	return recs[0]
}

// -----------------------------------------------------------------------------
// T1: success — publish, IncPublish=success, IncDLQPublished bumped, payload
// well-formed, exchange + routing key correct.
// -----------------------------------------------------------------------------

func TestPublishDLQ_Success_PublishesAndBumpsBothCounters(t *testing.T) {
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	clk := fakeClock{now: fixedTime}
	log := &fakeLogger{}
	p := newTestPublisher(t, pub, met, clk, log)

	if err := p.PublishDLQ(context.Background(), port.DLQTopicInvalidMessage, validEnvelope()); err != nil {
		t.Fatalf("PublishDLQ: %v", err)
	}

	if got := pub.Calls(); got != 1 {
		t.Fatalf("publisher calls: want 1, got %d", got)
	}
	if pub.lastExchange != testExchange {
		t.Errorf("exchange: want %q, got %q", testExchange, pub.lastExchange)
	}
	if pub.lastRoutingKey != "lic.dlq.invalid-message" {
		t.Errorf("routingKey: want %q, got %q", "lic.dlq.invalid-message", pub.lastRoutingKey)
	}

	// IncPublish: 1 record, outcome=success.
	rec := requireOnePublishRecord(t, met)
	if rec.Outcome != PublishOutcomeSuccess {
		t.Errorf("IncPublish outcome: want success, got %q", rec.Outcome)
	}
	if rec.Topic != "lic.dlq.invalid-message" {
		t.Errorf("IncPublish topic: want %q, got %q", "lic.dlq.invalid-message", rec.Topic)
	}

	// IncDLQPublished: 1 record, reason=invalid_message.
	dlqs := met.DLQRecords()
	if len(dlqs) != 1 {
		t.Fatalf("want 1 IncDLQPublished record; got %d", len(dlqs))
	}
	if dlqs[0].Topic != "lic.dlq.invalid-message" || dlqs[0].Reason != reasonInvalidMessage {
		t.Errorf("IncDLQPublished: want {topic=lic.dlq.invalid-message, reason=invalid_message}; got %+v", dlqs[0])
	}

	// Payload is well-formed JSON containing all expected fields.
	var got map[string]json.RawMessage
	if err := json.Unmarshal(pub.lastPayload, &got); err != nil {
		t.Fatalf("payload not valid JSON: %v", err)
	}
	for _, field := range []string{"original_topic", "original_message_hash", "error_code", "error_message", "failed_at"} {
		if _, ok := got[field]; !ok {
			t.Errorf("payload missing required field %q", field)
		}
	}

	// Logger silent on the success path (architect Q5).
	if log.warnCount != 0 || log.errorCount != 0 {
		t.Errorf("logger should be silent on success; got warn=%d err=%d", log.warnCount, log.errorCount)
	}
}

// -----------------------------------------------------------------------------
// T2: FailedAt is auto-stamped when caller leaves it empty (architect Q6).
// -----------------------------------------------------------------------------

func TestPublishDLQ_FailedAtAutoStampedWhenEmpty(t *testing.T) {
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	clk := fakeClock{now: fixedTime}
	p := newTestPublisher(t, pub, met, clk, nil)

	env := validEnvelope()
	env.FailedAt = "" // explicit empty

	if err := p.PublishDLQ(context.Background(), port.DLQTopicConsumerFailed, env); err != nil {
		t.Fatalf("PublishDLQ: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(pub.lastPayload, &got); err != nil {
		t.Fatalf("payload JSON: %v", err)
	}
	want := fixedTime.Format(time.RFC3339Nano)
	if got["failed_at"] != want {
		t.Errorf("failed_at: want %q (clock.Now), got %v", want, got["failed_at"])
	}

	// Caller-side value MUST NOT be mutated (PublishDLQ takes envelope by value).
	if env.FailedAt != "" {
		t.Errorf("caller-side FailedAt was mutated: %q", env.FailedAt)
	}
}

// -----------------------------------------------------------------------------
// T3: FailedAt set by caller is PRESERVED — semantic difference vs dm/orch
// publishers (architect Q6).
// -----------------------------------------------------------------------------

func TestPublishDLQ_FailedAtPreservedWhenCallerSet(t *testing.T) {
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	clk := fakeClock{now: fixedTime}
	p := newTestPublisher(t, pub, met, clk, nil)

	callerStamp := "2026-05-22T08:00:00.000000000Z"
	env := validEnvelope()
	env.FailedAt = callerStamp

	if err := p.PublishDLQ(context.Background(), port.DLQTopicConsumerFailed, env); err != nil {
		t.Fatalf("PublishDLQ: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(pub.lastPayload, &got); err != nil {
		t.Fatalf("payload JSON: %v", err)
	}
	if got["failed_at"] != callerStamp {
		t.Errorf("failed_at: want caller-set %q, got %v (clock was %q — should NOT have overwritten)",
			callerStamp, got["failed_at"], fixedTime.Format(time.RFC3339Nano))
	}
}

// -----------------------------------------------------------------------------
// T4..T9: validation failures — invalid topic + 6 required-field branches.
// Each emits IncPublish(_, invalid); broker.Publish NOT called;
// IncDLQPublished NOT called.
// -----------------------------------------------------------------------------

func TestPublishDLQ_InvalidTopic_Rejected(t *testing.T) {
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, nil)

	err := p.PublishDLQ(context.Background(), port.DLQTopic("lic.dlq.bogus"), validEnvelope())
	assertValidationFailure(t, err, reasonInvalidTopic, pub, met, "lic.dlq.bogus")
}

func TestPublishDLQ_MissingOriginalTopic_Rejected(t *testing.T) {
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, nil)

	env := validEnvelope()
	env.OriginalTopic = ""
	err := p.PublishDLQ(context.Background(), port.DLQTopicInvalidMessage, env)
	assertValidationFailure(t, err, reasonMissingOriginalTopic, pub, met, "lic.dlq.invalid-message")
}

func TestPublishDLQ_MissingOriginalHash_Rejected(t *testing.T) {
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, nil)

	env := validEnvelope()
	env.OriginalMessageHash = ""
	err := p.PublishDLQ(context.Background(), port.DLQTopicInvalidMessage, env)
	assertValidationFailure(t, err, reasonMissingOriginalHash, pub, met, "lic.dlq.invalid-message")
}

func TestPublishDLQ_MissingErrorCode_Rejected(t *testing.T) {
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, nil)

	env := validEnvelope()
	env.ErrorCode = ""
	err := p.PublishDLQ(context.Background(), port.DLQTopicInvalidMessage, env)
	assertValidationFailure(t, err, reasonMissingErrorCode, pub, met, "lic.dlq.invalid-message")
}

func TestPublishDLQ_MissingErrorMessage_Rejected(t *testing.T) {
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, nil)

	env := validEnvelope()
	env.ErrorMessage = ""
	err := p.PublishDLQ(context.Background(), port.DLQTopicInvalidMessage, env)
	assertValidationFailure(t, err, reasonMissingErrorMessage, pub, met, "lic.dlq.invalid-message")
}

func TestPublishDLQ_NegativeRetryCount_Rejected(t *testing.T) {
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, nil)

	env := validEnvelope()
	env.RetryCount = -1
	err := p.PublishDLQ(context.Background(), port.DLQTopicInvalidMessage, env)
	assertValidationFailure(t, err, reasonNegativeRetryCount, pub, met, "lic.dlq.invalid-message")
}

func TestPublishDLQ_NegativeMessageSize_Rejected(t *testing.T) {
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, nil)

	env := validEnvelope()
	env.OriginalMessageSizeBytes = -1
	err := p.PublishDLQ(context.Background(), port.DLQTopicInvalidMessage, env)
	assertValidationFailure(t, err, reasonNegativeMessageSize, pub, met, "lic.dlq.invalid-message")
}

// assertValidationFailure pins the shared contract: returned err is a
// *PublishError with the expected reason, broker.Publish NOT called,
// IncPublish=invalid emitted exactly once with the topic-string, and
// IncDLQPublished is silent.
func assertValidationFailure(t *testing.T, err error, wantReason string, pub *fakePublisher, met *fakeMetrics, topicStr string) {
	t.Helper()
	var pe *PublishError
	if !errors.As(err, &pe) {
		t.Fatalf("want *PublishError, got %T (%v)", err, err)
	}
	if pe.Reason != wantReason {
		t.Errorf("Reason: want %q, got %q", wantReason, pe.Reason)
	}
	if pe.Cause != nil {
		t.Errorf("Cause: want nil on validation failure, got %v", pe.Cause)
	}
	if got := pub.Calls(); got != 0 {
		t.Errorf("broker.Publish should NOT be called on validation failure; got %d", got)
	}
	rec := requireOnePublishRecord(t, met)
	if rec.Outcome != PublishOutcomeInvalid {
		t.Errorf("IncPublish outcome: want invalid, got %q", rec.Outcome)
	}
	if rec.Topic != topicStr {
		t.Errorf("IncPublish topic: want %q, got %q", topicStr, rec.Topic)
	}
	if got := met.DLQRecords(); len(got) != 0 {
		t.Errorf("IncDLQPublished should NOT be called on validation failure; got %+v", got)
	}
}

// -----------------------------------------------------------------------------
// T10: best-effort correlation IDs — invalid-message envelope with EVERY ID
// empty is accepted (per integration-contracts §10.1).
// -----------------------------------------------------------------------------

func TestPublishDLQ_BestEffortCorrelationIDs_AllEmptyAccepted(t *testing.T) {
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, nil)

	env := port.LICDLQEnvelope{
		OriginalTopic:            "dm.events.version-artifacts-ready",
		OriginalMessageHash:      "abc",
		OriginalMessageSizeBytes: 512,
		ErrorCode:                model.ErrorCode("INVALID_MESSAGE_SCHEMA"),
		ErrorMessage:             "could not parse JSON",
		// ALL correlation IDs empty — best-effort per §10.1.
	}
	if err := p.PublishDLQ(context.Background(), port.DLQTopicInvalidMessage, env); err != nil {
		t.Fatalf("PublishDLQ: want success on best-effort empty IDs, got %v", err)
	}

	// Payload has NO correlation_id / job_id / document_id / version_id /
	// organization_id (omitempty).
	var got map[string]any
	if err := json.Unmarshal(pub.lastPayload, &got); err != nil {
		t.Fatalf("payload JSON: %v", err)
	}
	for _, field := range []string{"correlation_id", "job_id", "document_id", "version_id", "organization_id"} {
		if _, present := got[field]; present {
			t.Errorf("field %q should be omitted from JSON when empty (omitempty); got %v", field, got[field])
		}
	}
}

// -----------------------------------------------------------------------------
// T11: ErrorCode NOT validated against catalog — non-publishable codes (like
// INVALID_MESSAGE_SCHEMA, INVALID_ORG_ID_MISMATCH) reach the DLQ envelope
// (architect Q3, DLQ catches ALL terminal errors).
// -----------------------------------------------------------------------------

func TestPublishDLQ_NonPublishableErrorCodeAccepted(t *testing.T) {
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, nil)

	env := validEnvelope()
	env.ErrorCode = model.ErrorCode("INVALID_ORG_ID_MISMATCH") // non-publishable per error catalog
	if err := p.PublishDLQ(context.Background(), port.DLQTopicInvalidMessage, env); err != nil {
		t.Fatalf("PublishDLQ: want success on non-publishable code, got %v", err)
	}

	// Same for an arbitrary made-up code — the publisher trusts the caller.
	env2 := validEnvelope()
	env2.ErrorCode = model.ErrorCode("ARBITRARY_NEW_FAILURE_KIND")
	if err := p.PublishDLQ(context.Background(), port.DLQTopicInvalidMessage, env2); err != nil {
		t.Fatalf("PublishDLQ: want success on arbitrary code, got %v", err)
	}

	if got := pub.Calls(); got != 2 {
		t.Errorf("want 2 successful publishes, got %d", got)
	}
}

// -----------------------------------------------------------------------------
// T12: marshal failure exercises the package-level marshalEnvelope seam.
// -----------------------------------------------------------------------------

func TestPublishDLQ_MarshalFailure(t *testing.T) {
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, nil)

	boom := errors.New("synthetic marshal failure")
	prev := marshalEnvelope
	marshalEnvelope = func(any) ([]byte, error) { return nil, boom }
	defer func() { marshalEnvelope = prev }()

	err := p.PublishDLQ(context.Background(), port.DLQTopicInvalidMessage, validEnvelope())

	var pe *PublishError
	if !errors.As(err, &pe) {
		t.Fatalf("want *PublishError, got %T (%v)", err, err)
	}
	if pe.Reason != reasonMarshalFailure {
		t.Errorf("Reason: want %q, got %q", reasonMarshalFailure, pe.Reason)
	}
	if !errors.Is(err, boom) {
		t.Errorf("errors.Is should traverse to the underlying marshal error")
	}
	if got := pub.Calls(); got != 0 {
		t.Errorf("broker.Publish must NOT be called on marshal failure; got %d", got)
	}
	rec := requireOnePublishRecord(t, met)
	if rec.Outcome != PublishOutcomeFailure {
		t.Errorf("IncPublish outcome: want failure, got %q", rec.Outcome)
	}
	if got := met.DLQRecords(); len(got) != 0 {
		t.Errorf("IncDLQPublished should NOT be called on marshal failure; got %+v", got)
	}
}

// -----------------------------------------------------------------------------
// T13..T17: broker outcomes — nack / confirm-timeout / not-connected /
// non-retryable AMQP / context errors. Each must:
//   - return the raw error (errors.Is preserves the sentinel chain)
//   - IncPublish with the right outcome label
//   - NOT bump IncDLQPublished
// -----------------------------------------------------------------------------

func TestPublishDLQ_BrokerNack_Nacked(t *testing.T) {
	pub := &fakePublisher{returnErr: broker.ErrPublishNack}
	met := &fakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, nil)

	err := p.PublishDLQ(context.Background(), port.DLQTopicPublishFailed, validEnvelope())
	if !errors.Is(err, broker.ErrPublishNack) {
		t.Fatalf("errors.Is(err, broker.ErrPublishNack) should hold; got %v", err)
	}
	rec := requireOnePublishRecord(t, met)
	if rec.Outcome != PublishOutcomeNacked {
		t.Errorf("outcome: want nacked, got %q", rec.Outcome)
	}
	if got := met.DLQRecords(); len(got) != 0 {
		t.Errorf("IncDLQPublished should NOT bump on nack; got %+v", got)
	}
}

func TestPublishDLQ_BrokerConfirmTimeout_Failure(t *testing.T) {
	pub := &fakePublisher{returnErr: broker.ErrConfirmTimeout}
	met := &fakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, nil)

	err := p.PublishDLQ(context.Background(), port.DLQTopicPublishFailed, validEnvelope())
	if !errors.Is(err, broker.ErrConfirmTimeout) {
		t.Fatalf("errors.Is(err, broker.ErrConfirmTimeout) should hold; got %v", err)
	}
	rec := requireOnePublishRecord(t, met)
	if rec.Outcome != PublishOutcomeFailure {
		t.Errorf("outcome: want failure, got %q", rec.Outcome)
	}
}

func TestPublishDLQ_BrokerNotConnected_Failure(t *testing.T) {
	pub := &fakePublisher{returnErr: broker.ErrNotConnected}
	met := &fakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, nil)

	err := p.PublishDLQ(context.Background(), port.DLQTopicAgentOutputInvalid, validEnvelope())
	if !errors.Is(err, broker.ErrNotConnected) {
		t.Fatalf("errors.Is(err, broker.ErrNotConnected) should hold; got %v", err)
	}
	rec := requireOnePublishRecord(t, met)
	if rec.Outcome != PublishOutcomeFailure {
		t.Errorf("outcome: want failure, got %q", rec.Outcome)
	}
}

func TestPublishDLQ_NonRetryableBrokerError_Failure(t *testing.T) {
	pub := &fakePublisher{returnErr: &broker.BrokerError{Op: "Publish: 404 NOT_FOUND", Retryable: false}}
	met := &fakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, nil)

	err := p.PublishDLQ(context.Background(), port.DLQTopicInvalidMessage, validEnvelope())
	if err == nil {
		t.Fatal("expected error")
	}
	var be *broker.BrokerError
	if !errors.As(err, &be) {
		t.Fatalf("errors.As(err, *broker.BrokerError) should hold; got %T", err)
	}
	rec := requireOnePublishRecord(t, met)
	if rec.Outcome != PublishOutcomeFailure {
		t.Errorf("outcome: want failure, got %q", rec.Outcome)
	}
}

func TestPublishDLQ_ContextCanceled_FailureRaw(t *testing.T) {
	pub := &fakePublisher{returnErr: context.Canceled}
	met := &fakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, nil)

	err := p.PublishDLQ(context.Background(), port.DLQTopicConsumerFailed, validEnvelope())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ctx errors must pass through raw (codebase convention); got %v", err)
	}
	rec := requireOnePublishRecord(t, met)
	if rec.Outcome != PublishOutcomeFailure {
		t.Errorf("outcome: want failure, got %q", rec.Outcome)
	}
}

func TestPublishDLQ_ContextDeadlineExceeded_FailureRaw(t *testing.T) {
	pub := &fakePublisher{returnErr: context.DeadlineExceeded}
	met := &fakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, nil)

	err := p.PublishDLQ(context.Background(), port.DLQTopicConsumerFailed, validEnvelope())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ctx errors must pass through raw; got %v", err)
	}
	rec := requireOnePublishRecord(t, met)
	if rec.Outcome != PublishOutcomeFailure {
		t.Errorf("outcome: want failure, got %q", rec.Outcome)
	}
}

// -----------------------------------------------------------------------------
// T18: all four topic→reason mappings produce the right reason on the DLQ
// counter (exhaustive coverage of topicToReason).
// -----------------------------------------------------------------------------

func TestPublishDLQ_TopicToReason_AllFourTopics(t *testing.T) {
	cases := []struct {
		topic      port.DLQTopic
		wantReason string
	}{
		{port.DLQTopicInvalidMessage, reasonInvalidMessage},
		{port.DLQTopicConsumerFailed, reasonConsumerFailed},
		{port.DLQTopicPublishFailed, reasonPublishFailed},
		{port.DLQTopicAgentOutputInvalid, reasonAgentOutputInvalid},
	}
	for _, c := range cases {
		c := c
		t.Run(string(c.topic), func(t *testing.T) {
			pub := &fakePublisher{}
			met := &fakeMetrics{}
			p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, nil)

			if err := p.PublishDLQ(context.Background(), c.topic, validEnvelope()); err != nil {
				t.Fatalf("PublishDLQ: %v", err)
			}
			dlqs := met.DLQRecords()
			if len(dlqs) != 1 {
				t.Fatalf("want 1 DLQ record; got %d", len(dlqs))
			}
			if dlqs[0].Topic != string(c.topic) || dlqs[0].Reason != c.wantReason {
				t.Errorf("DLQ record: want {topic=%q, reason=%q}; got %+v", c.topic, c.wantReason, dlqs[0])
			}
		})
	}
}

// -----------------------------------------------------------------------------
// T19: PublishDLQ takes envelope by value — agent_id / stage / raw_llm_response_hash
// / payload_storage_key correctly serialize when set.
// -----------------------------------------------------------------------------

func TestPublishDLQ_AgentOutputInvalid_OptionalFieldsSerialized(t *testing.T) {
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, nil)

	env := validEnvelope()
	env.AgentID = model.AgentID("AGENT_RISK_DETECTION")
	env.Stage = model.Stage("STAGE_RISK_DETECTION")
	env.RawLLMResponseHash = "0123456789abcdef"

	if err := p.PublishDLQ(context.Background(), port.DLQTopicAgentOutputInvalid, env); err != nil {
		t.Fatalf("PublishDLQ: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(pub.lastPayload, &got); err != nil {
		t.Fatalf("payload JSON: %v", err)
	}
	if got["agent_id"] != "AGENT_RISK_DETECTION" {
		t.Errorf("agent_id: got %v", got["agent_id"])
	}
	if got["stage"] != "STAGE_RISK_DETECTION" {
		t.Errorf("stage: got %v", got["stage"])
	}
	if got["raw_llm_response_hash"] != "0123456789abcdef" {
		t.Errorf("raw_llm_response_hash: got %v", got["raw_llm_response_hash"])
	}
}

// -----------------------------------------------------------------------------
// T20: PayloadStorageKey serializes when present (publish-failed v1-optional
// retention path).
// -----------------------------------------------------------------------------

func TestPublishDLQ_PublishFailed_PayloadStorageKeySerialized(t *testing.T) {
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, nil)

	env := validEnvelope()
	env.PayloadStorageKey = "lic-dlq-payloads-prod/2026-05-22/abc.json.gz"

	if err := p.PublishDLQ(context.Background(), port.DLQTopicPublishFailed, env); err != nil {
		t.Fatalf("PublishDLQ: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(pub.lastPayload, &got); err != nil {
		t.Fatalf("payload JSON: %v", err)
	}
	if got["payload_storage_key"] != env.PayloadStorageKey {
		t.Errorf("payload_storage_key: want %q, got %v", env.PayloadStorageKey, got["payload_storage_key"])
	}
}

// -----------------------------------------------------------------------------
// T21: omitempty on optional fields when NOT set (PayloadStorageKey absent
// for non-publish-failed; AgentID absent for non-agent-output-invalid).
// -----------------------------------------------------------------------------

func TestPublishDLQ_OmitemptyOnUnsetOptionalFields(t *testing.T) {
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, nil)

	if err := p.PublishDLQ(context.Background(), port.DLQTopicInvalidMessage, validEnvelope()); err != nil {
		t.Fatalf("PublishDLQ: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(pub.lastPayload, &got); err != nil {
		t.Fatalf("payload JSON: %v", err)
	}
	for _, field := range []string{"agent_id", "stage", "raw_llm_response_hash", "payload_storage_key"} {
		if _, present := got[field]; present {
			t.Errorf("field %q should be omitted when empty (omitempty); got %v", field, got[field])
		}
	}
}

// -----------------------------------------------------------------------------
// T22: PublishDLQ is goroutine-safe across distinct envelopes — 32 goroutines
// × 16 publishes, race-clean (-race), all succeed, all counters intact.
// -----------------------------------------------------------------------------

func TestPublishDLQ_ConcurrentInvocationsRaceClean(t *testing.T) {
	const goroutines = 32
	const perGoroutine = 16
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, nil)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				env := validEnvelope()
				env.CorrelationID = "corr-" + strconv.Itoa(i) + "-" + strconv.Itoa(j)
				if err := p.PublishDLQ(context.Background(), port.DLQTopicInvalidMessage, env); err != nil {
					t.Errorf("goroutine %d iter %d: %v", i, j, err)
					return
				}
			}
		}()
	}
	wg.Wait()

	want := int64(goroutines * perGoroutine)
	if got := pub.Calls(); got != want {
		t.Errorf("publisher.Calls: want %d, got %d", want, got)
	}
	if got := len(met.PublishRecords()); got != int(want) {
		t.Errorf("IncPublish records: want %d, got %d", want, got)
	}
	if got := len(met.DLQRecords()); got != int(want) {
		t.Errorf("IncDLQPublished records: want %d, got %d", want, got)
	}
}

// -----------------------------------------------------------------------------
// T-CTOR-1..4: constructor validation — empty Exchange, nil Publisher,
// errors.Join surfaces BOTH defects at once, nil optional seams accepted.
// -----------------------------------------------------------------------------

func TestNewDLQPublisher_EmptyExchange_Error(t *testing.T) {
	_, err := NewDLQPublisher(Config{Exchange: ""}, Deps{Publisher: &fakePublisher{}})
	if err == nil {
		t.Fatal("want error on empty Exchange")
	}
	if !strings.Contains(err.Error(), "Exchange") {
		t.Errorf("error should mention Exchange; got %v", err)
	}
}

func TestNewDLQPublisher_NilPublisher_Error(t *testing.T) {
	_, err := NewDLQPublisher(Config{Exchange: testExchange}, Deps{Publisher: nil})
	if err == nil {
		t.Fatal("want error on nil Publisher")
	}
	if !strings.Contains(err.Error(), "Publisher") {
		t.Errorf("error should mention Publisher; got %v", err)
	}
}

func TestNewDLQPublisher_BothDefects_ErrorsJoined(t *testing.T) {
	_, err := NewDLQPublisher(Config{Exchange: ""}, Deps{Publisher: nil})
	if err == nil {
		t.Fatal("want error when both defects present")
	}
	msg := err.Error()
	if !strings.Contains(msg, "Exchange") {
		t.Errorf("joined error should mention Exchange; got %v", err)
	}
	if !strings.Contains(msg, "Publisher") {
		t.Errorf("joined error should mention Publisher; got %v", err)
	}
}

func TestNewDLQPublisher_NilOptionalSeamsAccepted(t *testing.T) {
	// Only Publisher provided — Metrics, Clock, Logger left nil. Deps.withDefaults
	// must substitute noops; constructor must NOT error.
	_, err := NewDLQPublisher(Config{Exchange: testExchange}, Deps{Publisher: &fakePublisher{}})
	if err != nil {
		t.Fatalf("NewDLQPublisher with nil optional seams: %v", err)
	}
}

// -----------------------------------------------------------------------------
// T-CLASSIFIER: classifyOutcome covers all five broker-return shapes
// -----------------------------------------------------------------------------

func TestClassifyOutcome_AllBranches(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want PublishOutcome
	}{
		{"nil → success", nil, PublishOutcomeSuccess},
		{"ctx.Canceled → failure", context.Canceled, PublishOutcomeFailure},
		{"ctx.DeadlineExceeded → failure", context.DeadlineExceeded, PublishOutcomeFailure},
		{"ErrPublishNack → nacked", broker.ErrPublishNack, PublishOutcomeNacked},
		{"ErrConfirmTimeout → failure", broker.ErrConfirmTimeout, PublishOutcomeFailure},
		{"ErrNotConnected → failure", broker.ErrNotConnected, PublishOutcomeFailure},
		{"unknown error → failure", errors.New("synthetic"), PublishOutcomeFailure},
		{"non-retryable BrokerError → failure", &broker.BrokerError{Op: "Publish: 404", Retryable: false}, PublishOutcomeFailure},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if got := classifyOutcome(c.err); got != c.want {
				t.Errorf("classifyOutcome(%v) = %q, want %q", c.err, got, c.want)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// T-ERR: PublishError.Error / Unwrap pin behaviour
// -----------------------------------------------------------------------------

func TestPublishError_ErrorAndUnwrap(t *testing.T) {
	t.Run("nil receiver", func(t *testing.T) {
		var pe *PublishError
		if pe.Error() != "<nil>" {
			t.Errorf("nil.Error() = %q, want <nil>", pe.Error())
		}
		if pe.Unwrap() != nil {
			t.Errorf("nil.Unwrap() != nil")
		}
	})
	t.Run("reason without cause", func(t *testing.T) {
		pe := &PublishError{Reason: "demo"}
		if !strings.Contains(pe.Error(), "demo") {
			t.Errorf("Error() should contain reason; got %q", pe.Error())
		}
		if pe.Unwrap() != nil {
			t.Errorf("Unwrap() with nil Cause should return nil")
		}
	})
	t.Run("reason with cause", func(t *testing.T) {
		cause := errors.New("root")
		pe := &PublishError{Reason: "demo", Cause: cause}
		if !strings.Contains(pe.Error(), "demo") || !strings.Contains(pe.Error(), "root") {
			t.Errorf("Error() should contain reason+cause; got %q", pe.Error())
		}
		if pe.Unwrap() != cause {
			t.Errorf("Unwrap() should return Cause; got %v", pe.Unwrap())
		}
	})
}
