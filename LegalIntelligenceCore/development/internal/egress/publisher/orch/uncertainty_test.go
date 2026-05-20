package orch

import (
	"context"
	"encoding/json"
	"errors"
	"math"
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
// Test fixtures (UncertaintyPublisher).
//
// Reuse the in-package fakes (fakePublisher / fakeMetrics / fakeClock /
// fakeLogger) and shared test constants (testExchange, testCorrelationID,
// testJobID, testDocumentID, testVersionID, testOrganizationID, fixedTime)
// defined in publisher_test.go.
// -----------------------------------------------------------------------------

// validUncertain builds a baseline valid ClassificationUncertain envelope
// with two valid alternatives. Used as the baseline for tests that vary one
// field at a time.
func validUncertain() port.ClassificationUncertain {
	return port.ClassificationUncertain{
		CorrelationID:  testCorrelationID,
		Timestamp:      "STALE-WILL-BE-REWRITTEN",
		JobID:          testJobID,
		DocumentID:     testDocumentID,
		VersionID:      testVersionID,
		OrganizationID: testOrganizationID,
		SuggestedType:  model.ContractTypeServices,
		Confidence:     0.42,
		Threshold:      0.7,
		Alternatives: []model.ClassificationAlternative{
			{ContractType: model.ContractTypeSupply, Confidence: 0.35},
			{ContractType: model.ContractTypeWorkContract, Confidence: 0.18},
		},
	}
}

// newTestUncertaintyPublisher builds a fresh UncertaintyPublisher with the
// supplied seams. Optional publisher; nil ⇒ a fresh fakePublisher.
func newTestUncertaintyPublisher(t *testing.T, pub Publisher, m Metrics, c Clock, l Logger) *UncertaintyPublisher {
	t.Helper()
	if pub == nil {
		pub = &fakePublisher{}
	}
	p, err := NewUncertaintyPublisher(UncertaintyPublisherConfig{Exchange: testExchange}, UncertaintyPublisherDeps{
		Publisher: pub,
		Metrics:   m,
		Clock:     c,
		Logger:    l,
	})
	if err != nil {
		t.Fatalf("NewUncertaintyPublisher: %v", err)
	}
	return p
}

// -----------------------------------------------------------------------------
// T1: success path — publish on Exchange/topic; payload contains all 6 IDs +
// SuggestedType + Confidence + Threshold + rewritten Timestamp + 2
// alternatives; metric=success.
// -----------------------------------------------------------------------------

func TestPublishClassificationUncertain_Success_PublishesEnvelope(t *testing.T) {
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	clk := fakeClock{now: fixedTime}
	p := newTestUncertaintyPublisher(t, pub, met, clk, &fakeLogger{})

	if err := p.PublishClassificationUncertain(context.Background(), validUncertain()); err != nil {
		t.Fatalf("PublishClassificationUncertain: %v", err)
	}

	// Publisher called once on configured exchange + classification-uncertain topic.
	if got := pub.Calls(); got != 1 {
		t.Fatalf("publisher calls: want 1; got %d", got)
	}
	if pub.lastExchange != testExchange {
		t.Errorf("exchange: want %q; got %q", testExchange, pub.lastExchange)
	}
	if pub.lastRoutingKey != topicClassificationUncertain {
		t.Errorf("routingKey: want %q; got %q", topicClassificationUncertain, pub.lastRoutingKey)
	}
	if pub.lastRoutingKey != "lic.events.classification-uncertain" {
		t.Errorf("routingKey FROZEN wire constant drift: got %q", pub.lastRoutingKey)
	}

	// Payload shape — all required fields present, Alternatives carries 2.
	var got map[string]json.RawMessage
	if err := json.Unmarshal(pub.lastPayload, &got); err != nil {
		t.Fatalf("payload not valid JSON: %v", err)
	}
	wantKeys := []string{
		"correlation_id", "timestamp", "job_id", "document_id",
		"version_id", "organization_id", "suggested_type",
		"confidence", "threshold", "alternatives",
	}
	for _, k := range wantKeys {
		if _, ok := got[k]; !ok {
			t.Errorf("payload missing field %q; payload=%s", k, string(pub.lastPayload))
		}
	}

	// Typed round-trip on the value fields.
	var typed struct {
		SuggestedType  string  `json:"suggested_type"`
		Confidence     float64 `json:"confidence"`
		Threshold      float64 `json:"threshold"`
		OrganizationID string  `json:"organization_id"`
		Alternatives   []struct {
			ContractType string  `json:"contract_type"`
			Confidence   float64 `json:"confidence"`
		} `json:"alternatives"`
	}
	if err := json.Unmarshal(pub.lastPayload, &typed); err != nil {
		t.Fatalf("payload typed unmarshal: %v", err)
	}
	if typed.SuggestedType != string(model.ContractTypeServices) {
		t.Errorf("suggested_type: want %q; got %q", model.ContractTypeServices, typed.SuggestedType)
	}
	if typed.Confidence != 0.42 {
		t.Errorf("confidence: want 0.42; got %v", typed.Confidence)
	}
	if typed.Threshold != 0.7 {
		t.Errorf("threshold: want 0.7; got %v", typed.Threshold)
	}
	if typed.OrganizationID != testOrganizationID {
		t.Errorf("organization_id: want %q; got %q", testOrganizationID, typed.OrganizationID)
	}
	if len(typed.Alternatives) != 2 {
		t.Fatalf("alternatives: want 2 entries; got %d", len(typed.Alternatives))
	}
	if typed.Alternatives[0].ContractType != string(model.ContractTypeSupply) ||
		typed.Alternatives[0].Confidence != 0.35 {
		t.Errorf("alternatives[0]: want {SUPPLY, 0.35}; got %+v", typed.Alternatives[0])
	}
	if typed.Alternatives[1].ContractType != string(model.ContractTypeWorkContract) ||
		typed.Alternatives[1].Confidence != 0.18 {
		t.Errorf("alternatives[1]: want {WORK_CONTRACT, 0.18}; got %+v", typed.Alternatives[1])
	}

	// Timestamp on the wire equals the clock tick.
	var ts struct {
		Timestamp string `json:"timestamp"`
	}
	if err := json.Unmarshal(pub.lastPayload, &ts); err != nil {
		t.Fatalf("payload timestamp unmarshal: %v", err)
	}
	if ts.Timestamp != fixedTime.Format(time.RFC3339Nano) {
		t.Errorf("timestamp: want %q; got %q", fixedTime.Format(time.RFC3339Nano), ts.Timestamp)
	}

	// Metric: exactly one record, topic+outcome=success.
	rec := requireOneRecord(t, met)
	if rec.Topic != topicClassificationUncertain {
		t.Errorf("metric topic: want %q; got %q", topicClassificationUncertain, rec.Topic)
	}
	if rec.Outcome != PublishOutcomeSuccess {
		t.Errorf("metric outcome: want %q; got %q", PublishOutcomeSuccess, rec.Outcome)
	}
}

// -----------------------------------------------------------------------------
// T2: Timestamp rewrite = clock.Now().Format(RFC3339Nano) UTC; caller-side
// variable unchanged (value semantics).
// -----------------------------------------------------------------------------

func TestPublishClassificationUncertain_Timestamp_RewrittenInWire_CallerUnchanged(t *testing.T) {
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	clk := fakeClock{now: fixedTime}
	p := newTestUncertaintyPublisher(t, pub, met, clk, &fakeLogger{})

	caller := validUncertain()
	const staleTS = "STALE-WILL-BE-REWRITTEN"
	caller.Timestamp = staleTS

	if err := p.PublishClassificationUncertain(context.Background(), caller); err != nil {
		t.Fatalf("PublishClassificationUncertain: %v", err)
	}

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

	// Caller-side variable MUST be unchanged (value semantics — the publisher
	// takes the payload by value, so the in-method Timestamp rewrite operates
	// on the local copy only).
	if caller.Timestamp != staleTS {
		t.Errorf("caller-side payload Timestamp mutated: want %q; got %q",
			staleTS, caller.Timestamp)
	}
}

// -----------------------------------------------------------------------------
// T3..T7: 5 envelope-ID validation failures.
// -----------------------------------------------------------------------------

func TestPublishClassificationUncertain_IDValidation(t *testing.T) {
	cases := []struct {
		name       string
		mutate     func(p *port.ClassificationUncertain)
		wantReason string
	}{
		{"T3_empty_correlation_id", func(p *port.ClassificationUncertain) { p.CorrelationID = "" }, reasonMissingCorrelationID},
		{"T4_empty_job_id", func(p *port.ClassificationUncertain) { p.JobID = "" }, reasonMissingJobID},
		{"T5_empty_document_id", func(p *port.ClassificationUncertain) { p.DocumentID = "" }, reasonMissingDocumentID},
		{"T6_empty_version_id", func(p *port.ClassificationUncertain) { p.VersionID = "" }, reasonMissingVersionID},
		{"T7_empty_organization_id", func(p *port.ClassificationUncertain) { p.OrganizationID = "" }, reasonMissingOrganizationID},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			pub := &fakePublisher{}
			met := &fakeMetrics{}
			p := newTestUncertaintyPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

			payload := validUncertain()
			tc.mutate(&payload)

			err := p.PublishClassificationUncertain(context.Background(), payload)

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
			if rec.Topic != topicClassificationUncertain {
				t.Errorf("metric topic: want %q; got %q", topicClassificationUncertain, rec.Topic)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// T8/T9: invalid SuggestedType — empty + "BOGUS" both rejected via IsValid.
// -----------------------------------------------------------------------------

func TestPublishClassificationUncertain_InvalidSuggestedType(t *testing.T) {
	cases := []struct {
		name string
		typ  model.ContractType
	}{
		{"T8_empty_suggested_type", model.ContractType("")},
		{"T9_unknown_suggested_type", model.ContractType("BOGUS")},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			pub := &fakePublisher{}
			met := &fakeMetrics{}
			p := newTestUncertaintyPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

			payload := validUncertain()
			payload.SuggestedType = tc.typ

			err := p.PublishClassificationUncertain(context.Background(), payload)
			var pe *PublishError
			if !errors.As(err, &pe) {
				t.Fatalf("want *PublishError; got %T %v", err, err)
			}
			if pe.Reason != reasonInvalidSuggestedType {
				t.Errorf("reason: want %q; got %q", reasonInvalidSuggestedType, pe.Reason)
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
// T10..T12: Confidence ∈ [0, 1] — boundary and NaN-explicit checks.
// -----------------------------------------------------------------------------

func TestPublishClassificationUncertain_InvalidConfidence(t *testing.T) {
	cases := []struct {
		name  string
		value float64
	}{
		{"T10_confidence_negative", -0.0001},
		{"T11_confidence_above_one", 1.0001},
		{"T12_confidence_nan", math.NaN()},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			pub := &fakePublisher{}
			met := &fakeMetrics{}
			p := newTestUncertaintyPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

			payload := validUncertain()
			payload.Confidence = tc.value

			err := p.PublishClassificationUncertain(context.Background(), payload)
			var pe *PublishError
			if !errors.As(err, &pe) {
				t.Fatalf("want *PublishError; got %T %v", err, err)
			}
			if pe.Reason != reasonInvalidConfidence {
				t.Errorf("reason: want %q; got %q", reasonInvalidConfidence, pe.Reason)
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
// T13: Confidence boundaries 0.0 and 1.0 are ACCEPTED (inclusive range).
// -----------------------------------------------------------------------------

func TestPublishClassificationUncertain_ConfidenceBoundariesAccepted(t *testing.T) {
	for _, v := range []float64{0.0, 1.0} {
		v := v
		t.Run("boundary_"+strconv.FormatFloat(v, 'f', -1, 64), func(t *testing.T) {
			pub := &fakePublisher{}
			met := &fakeMetrics{}
			p := newTestUncertaintyPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

			payload := validUncertain()
			payload.Confidence = v

			if err := p.PublishClassificationUncertain(context.Background(), payload); err != nil {
				t.Fatalf("PublishClassificationUncertain: %v", err)
			}
			if got := pub.Calls(); got != 1 {
				t.Errorf("publisher calls: want 1; got %d", got)
			}
			rec := requireOneRecord(t, met)
			if rec.Outcome != PublishOutcomeSuccess {
				t.Errorf("metric outcome: want %q; got %q", PublishOutcomeSuccess, rec.Outcome)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// T14: Threshold ∈ [0, 1] — boundary and NaN-explicit checks.
// -----------------------------------------------------------------------------

func TestPublishClassificationUncertain_InvalidThreshold(t *testing.T) {
	cases := []struct {
		name  string
		value float64
	}{
		{"threshold_negative", -0.5},
		{"threshold_above_one", 1.5},
		{"threshold_nan", math.NaN()},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			pub := &fakePublisher{}
			met := &fakeMetrics{}
			p := newTestUncertaintyPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

			payload := validUncertain()
			payload.Threshold = tc.value

			err := p.PublishClassificationUncertain(context.Background(), payload)
			var pe *PublishError
			if !errors.As(err, &pe) {
				t.Fatalf("want *PublishError; got %T %v", err, err)
			}
			if pe.Reason != reasonInvalidThreshold {
				t.Errorf("reason: want %q; got %q", reasonInvalidThreshold, pe.Reason)
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
// T15: Threshold boundaries 0.0 and 1.0 ACCEPTED.
// -----------------------------------------------------------------------------

func TestPublishClassificationUncertain_ThresholdBoundariesAccepted(t *testing.T) {
	for _, v := range []float64{0.0, 1.0} {
		v := v
		t.Run("boundary_"+strconv.FormatFloat(v, 'f', -1, 64), func(t *testing.T) {
			pub := &fakePublisher{}
			met := &fakeMetrics{}
			p := newTestUncertaintyPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

			payload := validUncertain()
			payload.Threshold = v

			if err := p.PublishClassificationUncertain(context.Background(), payload); err != nil {
				t.Fatalf("PublishClassificationUncertain: %v", err)
			}
			rec := requireOneRecord(t, met)
			if rec.Outcome != PublishOutcomeSuccess {
				t.Errorf("metric outcome: want %q; got %q", PublishOutcomeSuccess, rec.Outcome)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// T16+T17 (table-driven NTH-3): Alternatives nil and empty are EQUIVALENT —
// both publish successfully and the wire payload OMITS the `alternatives`
// field (omitempty on the DTO).
// -----------------------------------------------------------------------------

func TestPublishClassificationUncertain_Alternatives_NilAndEmpty(t *testing.T) {
	for _, tc := range []struct {
		name string
		alts []model.ClassificationAlternative
	}{
		{"nil_alternatives", nil},
		{"empty_alternatives", []model.ClassificationAlternative{}},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			pub := &fakePublisher{}
			met := &fakeMetrics{}
			p := newTestUncertaintyPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

			payload := validUncertain()
			payload.Alternatives = tc.alts

			if err := p.PublishClassificationUncertain(context.Background(), payload); err != nil {
				t.Fatalf("PublishClassificationUncertain: %v", err)
			}
			if got := pub.Calls(); got != 1 {
				t.Errorf("publisher calls: want 1; got %d", got)
			}

			var got map[string]json.RawMessage
			if err := json.Unmarshal(pub.lastPayload, &got); err != nil {
				t.Fatalf("payload not valid JSON: %v", err)
			}
			if _, present := got["alternatives"]; present {
				t.Errorf("payload must NOT contain alternatives when nil/empty (omitempty); got %s",
					string(pub.lastPayload))
			}

			rec := requireOneRecord(t, met)
			if rec.Outcome != PublishOutcomeSuccess {
				t.Errorf("metric outcome: want %q; got %q", PublishOutcomeSuccess, rec.Outcome)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// T18: alternative.ContractType invalid → reasonInvalidAlternativeType.
// -----------------------------------------------------------------------------

func TestPublishClassificationUncertain_InvalidAlternativeType(t *testing.T) {
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	p := newTestUncertaintyPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	payload := validUncertain()
	payload.Alternatives = []model.ClassificationAlternative{
		{ContractType: model.ContractTypeSupply, Confidence: 0.3},
		{ContractType: model.ContractType("BOGUS"), Confidence: 0.2},
	}

	err := p.PublishClassificationUncertain(context.Background(), payload)
	var pe *PublishError
	if !errors.As(err, &pe) {
		t.Fatalf("want *PublishError; got %T %v", err, err)
	}
	if pe.Reason != reasonInvalidAlternativeType {
		t.Errorf("reason: want %q; got %q", reasonInvalidAlternativeType, pe.Reason)
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
// T19: alternative.Confidence out of [0,1] or NaN → reasonInvalidAlternativeConfidence.
// -----------------------------------------------------------------------------

func TestPublishClassificationUncertain_InvalidAlternativeConfidence(t *testing.T) {
	cases := []struct {
		name  string
		value float64
	}{
		{"alt_confidence_negative", -0.1},
		{"alt_confidence_above_one", 1.1},
		{"alt_confidence_nan", math.NaN()},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			pub := &fakePublisher{}
			met := &fakeMetrics{}
			p := newTestUncertaintyPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

			payload := validUncertain()
			payload.Alternatives = []model.ClassificationAlternative{
				{ContractType: model.ContractTypeSupply, Confidence: tc.value},
			}

			err := p.PublishClassificationUncertain(context.Background(), payload)
			var pe *PublishError
			if !errors.As(err, &pe) {
				t.Fatalf("want *PublishError; got %T %v", err, err)
			}
			if pe.Reason != reasonInvalidAlternativeConfidence {
				t.Errorf("reason: want %q; got %q", reasonInvalidAlternativeConfidence, pe.Reason)
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
// T20: alternative.ContractType invalid takes precedence over an out-of-range
// alt.Confidence on the SAME alternative — Block E checks type FIRST.
// -----------------------------------------------------------------------------

func TestPublishClassificationUncertain_AlternativeType_PrecedesConfidence(t *testing.T) {
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	p := newTestUncertaintyPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	payload := validUncertain()
	payload.Alternatives = []model.ClassificationAlternative{
		{ContractType: model.ContractType("BAD"), Confidence: math.NaN()},
	}

	err := p.PublishClassificationUncertain(context.Background(), payload)
	var pe *PublishError
	if !errors.As(err, &pe) {
		t.Fatalf("want *PublishError; got %T %v", err, err)
	}
	if pe.Reason != reasonInvalidAlternativeType {
		t.Errorf("reason: want %q (type checked first); got %q",
			reasonInvalidAlternativeType, pe.Reason)
	}
}

// -----------------------------------------------------------------------------
// T21: marshal-failure path — override marshalUncertain → reason +
// metric=failure; broker NOT called.
// -----------------------------------------------------------------------------

func TestPublishClassificationUncertain_MarshalFailure_EmitsFailureMetricAndPublishesNothing(t *testing.T) {
	originalMarshal := marshalUncertain
	t.Cleanup(func() { marshalUncertain = originalMarshal })

	wantCause := errors.New("synthetic marshal failure")
	marshalUncertain = func(_ any) ([]byte, error) { return nil, wantCause }

	pub := &fakePublisher{}
	met := &fakeMetrics{}
	p := newTestUncertaintyPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	err := p.PublishClassificationUncertain(context.Background(), validUncertain())

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
	if rec.Topic != topicClassificationUncertain {
		t.Errorf("metric topic: want %q; got %q", topicClassificationUncertain, rec.Topic)
	}
}

// -----------------------------------------------------------------------------
// T22: broker success → nil + metric=success.
// -----------------------------------------------------------------------------

func TestPublishClassificationUncertain_BrokerSuccess(t *testing.T) {
	pub := &fakePublisher{returnErr: nil}
	met := &fakeMetrics{}
	p := newTestUncertaintyPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	if err := p.PublishClassificationUncertain(context.Background(), validUncertain()); err != nil {
		t.Fatalf("PublishClassificationUncertain: want nil err; got %v", err)
	}
	rec := requireOneRecord(t, met)
	if rec.Outcome != PublishOutcomeSuccess {
		t.Errorf("metric outcome: want %q; got %q", PublishOutcomeSuccess, rec.Outcome)
	}
}

// -----------------------------------------------------------------------------
// T23: broker NACK → raw err + metric=nacked.
// -----------------------------------------------------------------------------

func TestPublishClassificationUncertain_BrokerNack(t *testing.T) {
	nackErr := &broker.BrokerError{Op: "Publish", Retryable: true, Cause: broker.ErrPublishNack}
	pub := &fakePublisher{returnErr: nackErr}
	met := &fakeMetrics{}
	p := newTestUncertaintyPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	err := p.PublishClassificationUncertain(context.Background(), validUncertain())
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
// T24: broker ConfirmTimeout → raw err + metric=failure.
// -----------------------------------------------------------------------------

func TestPublishClassificationUncertain_BrokerConfirmTimeout(t *testing.T) {
	cttErr := &broker.BrokerError{Op: "Publish", Retryable: true, Cause: broker.ErrConfirmTimeout}
	pub := &fakePublisher{returnErr: cttErr}
	met := &fakeMetrics{}
	p := newTestUncertaintyPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	err := p.PublishClassificationUncertain(context.Background(), validUncertain())
	if !errors.Is(err, broker.ErrConfirmTimeout) {
		t.Errorf("want errors.Is(err, broker.ErrConfirmTimeout); got %v", err)
	}
	rec := requireOneRecord(t, met)
	if rec.Outcome != PublishOutcomeFailure {
		t.Errorf("metric outcome: want %q; got %q", PublishOutcomeFailure, rec.Outcome)
	}
}

// -----------------------------------------------------------------------------
// T25: broker NotConnected / non-retryable AMQP / unknown → raw err +
// metric=failure.
// -----------------------------------------------------------------------------

func TestPublishClassificationUncertain_BrokerOtherFailures(t *testing.T) {
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
			p := newTestUncertaintyPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

			err := p.PublishClassificationUncertain(context.Background(), validUncertain())
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
// T26: ctx.Canceled / ctx.DeadlineExceeded → raw ctx.Err() pass-through +
// metric=failure. NOT wrapped in *PublishError.
// -----------------------------------------------------------------------------

func TestPublishClassificationUncertain_ContextErrors_RawPassthrough(t *testing.T) {
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
			p := newTestUncertaintyPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

			err := p.PublishClassificationUncertain(context.Background(), validUncertain())
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
// T27: success path WITHOUT Alternatives — wire shape omits the optional
// `alternatives` field entirely (omitempty contract pin, sibling of T16+T17).
// -----------------------------------------------------------------------------

func TestPublishClassificationUncertain_NoAlternatives_OmittedFromWire(t *testing.T) {
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	p := newTestUncertaintyPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	payload := validUncertain()
	payload.Alternatives = nil

	if err := p.PublishClassificationUncertain(context.Background(), payload); err != nil {
		t.Fatalf("PublishClassificationUncertain: %v", err)
	}
	if !strings.Contains(string(pub.lastPayload), `"suggested_type"`) {
		t.Errorf("payload missing suggested_type: %s", string(pub.lastPayload))
	}
	if strings.Contains(string(pub.lastPayload), `"alternatives"`) {
		t.Errorf("payload must NOT contain alternatives when nil (omitempty); got %s",
			string(pub.lastPayload))
	}
}

// -----------------------------------------------------------------------------
// T28: marshal succeeds AFTER an alternative slice with float64 → no panic on
// JSON encoding of regular doubles (defensive smoke for the Block-E happy
// path).
// -----------------------------------------------------------------------------

func TestPublishClassificationUncertain_MarshalsAlternativesCleanly(t *testing.T) {
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	p := newTestUncertaintyPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	payload := validUncertain()
	payload.Alternatives = []model.ClassificationAlternative{
		{ContractType: model.ContractTypeNDA, Confidence: 0.51},
		{ContractType: model.ContractTypeLease, Confidence: 0.49},
		{ContractType: model.ContractTypeLoan, Confidence: 0.01},
	}

	if err := p.PublishClassificationUncertain(context.Background(), payload); err != nil {
		t.Fatalf("PublishClassificationUncertain: %v", err)
	}
	var got struct {
		Alternatives []struct {
			ContractType string  `json:"contract_type"`
			Confidence   float64 `json:"confidence"`
		} `json:"alternatives"`
	}
	if err := json.Unmarshal(pub.lastPayload, &got); err != nil {
		t.Fatalf("payload typed unmarshal: %v", err)
	}
	if len(got.Alternatives) != 3 {
		t.Fatalf("alternatives len: want 3; got %d", len(got.Alternatives))
	}
}

// -----------------------------------------------------------------------------
// T29: concurrent — 16 goroutines × 64 iterations each over distinct
// correlationIDs; -race clean (mirrors T26 of StatusPublisher).
// -----------------------------------------------------------------------------

func TestPublishClassificationUncertain_Concurrent_DistinctCorrelationIDs(t *testing.T) {
	const (
		goroutines = 16
		iterations = 64
	)
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	p := newTestUncertaintyPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	var wg sync.WaitGroup
	wg.Add(goroutines)
	var errCount atomic.Int64
	for g := 0; g < goroutines; g++ {
		g := g
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				payload := validUncertain()
				payload.CorrelationID = "corr-" + strconv.Itoa(g) + "-" + strconv.Itoa(i)
				if err := p.PublishClassificationUncertain(context.Background(), payload); err != nil {
					errCount.Add(1)
					t.Errorf("g%d/i%d: PublishClassificationUncertain: %v", g, i, err)
				}
			}
		}()
	}
	wg.Wait()

	if errCount.Load() != 0 {
		t.Fatalf("concurrent publish errors: %d", errCount.Load())
	}
	wantCalls := int64(goroutines * iterations)
	if got := pub.Calls(); got != wantCalls {
		t.Errorf("publisher calls: want %d; got %d", wantCalls, got)
	}
	recs := met.Records()
	if int64(len(recs)) != wantCalls {
		t.Fatalf("metric records: want %d; got %d", wantCalls, len(recs))
	}
	for _, rec := range recs {
		if rec.Outcome != PublishOutcomeSuccess {
			t.Errorf("metric outcome: want all success; got %q", rec.Outcome)
		}
		if rec.Topic != topicClassificationUncertain {
			t.Errorf("metric topic: want %q; got %q", topicClassificationUncertain, rec.Topic)
		}
	}
}

// -----------------------------------------------------------------------------
// T-CTOR-1: empty Exchange → wiring error names UncertaintyPublisherConfig.Exchange.
// -----------------------------------------------------------------------------

func TestNewUncertaintyPublisher_EmptyExchange_ConstructorError(t *testing.T) {
	p, err := NewUncertaintyPublisher(UncertaintyPublisherConfig{Exchange: ""}, UncertaintyPublisherDeps{
		Publisher: &fakePublisher{},
	})
	if err == nil {
		t.Fatal("want error on empty Exchange; got nil")
	}
	if p != nil {
		t.Errorf("want nil publisher on error; got %+v", p)
	}
	if got := err.Error(); !strings.Contains(got, "UncertaintyPublisherConfig.Exchange") {
		t.Errorf("error must name the offending field (UncertaintyPublisherConfig.Exchange); got %q", got)
	}
}

// -----------------------------------------------------------------------------
// T-CTOR-2: nil Publisher → wiring error names UncertaintyPublisherDeps.Publisher.
// -----------------------------------------------------------------------------

func TestNewUncertaintyPublisher_NilPublisher_ConstructorError(t *testing.T) {
	p, err := NewUncertaintyPublisher(UncertaintyPublisherConfig{Exchange: testExchange}, UncertaintyPublisherDeps{
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
	if got := err.Error(); !strings.Contains(got, "UncertaintyPublisherDeps.Publisher") {
		t.Errorf("error must name the offending field (UncertaintyPublisherDeps.Publisher); got %q", got)
	}
}

// -----------------------------------------------------------------------------
// T-CTOR-3: both defects (nil Publisher + empty Exchange) → errors.Join
// surfaces both substrings.
// -----------------------------------------------------------------------------

func TestNewUncertaintyPublisher_BothDefects_ErrorsJoinSurfacesBoth(t *testing.T) {
	p, err := NewUncertaintyPublisher(UncertaintyPublisherConfig{Exchange: ""}, UncertaintyPublisherDeps{
		Publisher: nil,
	})
	if err == nil {
		t.Fatal("want error on both defects; got nil")
	}
	if p != nil {
		t.Errorf("want nil publisher on error; got %+v", p)
	}
	got := err.Error()
	if !strings.Contains(got, "UncertaintyPublisherConfig.Exchange") {
		t.Errorf("error must mention UncertaintyPublisherConfig.Exchange; got %q", got)
	}
	if !strings.Contains(got, "UncertaintyPublisherDeps.Publisher") {
		t.Errorf("error must mention UncertaintyPublisherDeps.Publisher; got %q", got)
	}
}

// -----------------------------------------------------------------------------
// T-CTOR-4: nil optional seams (Metrics / Clock / Logger) → noop defaults;
// hot path works without panic.
// -----------------------------------------------------------------------------

func TestNewUncertaintyPublisher_NilOptionalSeams_NoopDefaults(t *testing.T) {
	pub := &fakePublisher{}
	p, err := NewUncertaintyPublisher(UncertaintyPublisherConfig{Exchange: testExchange}, UncertaintyPublisherDeps{
		Publisher: pub,
		// Metrics / Clock / Logger intentionally nil — must get noop defaults.
	})
	if err != nil {
		t.Fatalf("NewUncertaintyPublisher with nil optional seams: %v", err)
	}
	if p == nil {
		t.Fatal("want non-nil publisher; got nil")
	}
	if err := p.PublishClassificationUncertain(context.Background(), validUncertain()); err != nil {
		t.Errorf("PublishClassificationUncertain: %v", err)
	}
	if got := pub.Calls(); got != 1 {
		t.Errorf("publisher calls: want 1; got %d", got)
	}
}
