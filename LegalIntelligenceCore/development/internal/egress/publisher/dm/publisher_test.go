package dm

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
// Test fakes specific to the AnalysisArtifactsPublisher suite
//
// (fakePublisher / fakeClock / fakeLogger are reused from requester_test.go
// in the same package; pubFakeMetrics extends the requester suite's
// fakeMetrics with explicit ObservePublishedSize capture so the size-histogram
// expectations can be pinned per call.)
// -----------------------------------------------------------------------------

// pubFakeMetrics is a concurrency-safe in-memory Metrics seam that captures
// both IncPublish records AND ObservePublishedSize observations (the
// AnalysisArtifactsPublisher exercises both — the histogram is specific to
// this publisher, observability.md §3.5).
type pubFakeMetrics struct {
	mu       sync.Mutex
	records  []fakeMetricRecord
	observed []int
}

func (f *pubFakeMetrics) IncPublish(topic string, outcome PublishOutcome) {
	f.mu.Lock()
	f.records = append(f.records, fakeMetricRecord{Topic: topic, Outcome: outcome})
	f.mu.Unlock()
}

func (f *pubFakeMetrics) ObservePublishedSize(bytes int) {
	f.mu.Lock()
	f.observed = append(f.observed, bytes)
	f.mu.Unlock()
}

func (f *pubFakeMetrics) Records() []fakeMetricRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeMetricRecord, len(f.records))
	copy(out, f.records)
	return out
}

func (f *pubFakeMetrics) Observations() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]int, len(f.observed))
	copy(out, f.observed)
	return out
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

// validPayload builds a fully-populated LegalAnalysisArtifactsReady with all
// 5 IDs + 8 artifact pointers + RiskDelta=nil (the omitempty case). Used as
// the baseline for tests that vary one field at a time.
func validPayload() port.LegalAnalysisArtifactsReady {
	return port.LegalAnalysisArtifactsReady{
		CorrelationID:        testCorrelationID,
		Timestamp:            "STALE-WILL-BE-REWRITTEN",
		JobID:                testJobID,
		DocumentID:           testDocumentID,
		VersionID:            testVersionID,
		OrganizationID:       testOrganizationID,
		ClassificationResult: &model.ClassificationResult{},
		KeyParameters:        &model.KeyParameters{},
		RiskAnalysis:         &model.RiskAnalysis{},
		RiskProfile:          &model.RiskProfile{},
		Recommendations:      model.Recommendations{},
		Summary:              &model.Summary{},
		DetailedReport:       &model.DetailedReport{},
		AggregateScore:       &model.AggregateScore{},
		// RiskDelta intentionally nil (omitempty case).
	}
}

// newTestPublisher builds a fresh AnalysisArtifactsPublisher with the
// supplied seams. Optional publisher; nil ⇒ a fresh fakePublisher.
func newTestPublisher(t *testing.T, pub Publisher, m Metrics, c Clock, l Logger) *AnalysisArtifactsPublisher {
	t.Helper()
	if pub == nil {
		pub = &fakePublisher{}
	}
	p, err := NewAnalysisArtifactsPublisher(PublisherConfig{Exchange: testExchange}, PublisherDeps{
		Publisher: pub,
		Metrics:   m,
		Clock:     c,
		Logger:    l,
	})
	if err != nil {
		t.Fatalf("NewAnalysisArtifactsPublisher: %v", err)
	}
	return p
}

func requireOnePubRecord(t *testing.T, m *pubFakeMetrics) fakeMetricRecord {
	t.Helper()
	recs := m.Records()
	if len(recs) != 1 {
		t.Fatalf("want exactly 1 metric record; got %d (%+v)", len(recs), recs)
	}
	return recs[0]
}

// -----------------------------------------------------------------------------
// T1: success — publish on Exchange/topic, payload contains all 5 IDs +
// 8 artifacts + OrganizationID present + RiskDelta omitted; metric=success;
// size observed once.
// -----------------------------------------------------------------------------

func TestPublish_Success_PublishesEnvelope(t *testing.T) {
	pub := &fakePublisher{}
	met := &pubFakeMetrics{}
	clk := fakeClock{now: fixedTime}
	log := &fakeLogger{}
	p := newTestPublisher(t, pub, met, clk, log)

	if err := p.Publish(context.Background(), validPayload()); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// Publisher called once on configured exchange + analysis-ready topic.
	if got := pub.Calls(); got != 1 {
		t.Fatalf("publisher calls: want 1; got %d", got)
	}
	if pub.lastExchange != testExchange {
		t.Errorf("exchange: want %q; got %q", testExchange, pub.lastExchange)
	}
	if pub.lastRoutingKey != "lic.artifacts.analysis-ready" {
		t.Errorf("routingKey: want %q; got %q", "lic.artifacts.analysis-ready", pub.lastRoutingKey)
	}

	// Payload contains all required fields.
	var got map[string]json.RawMessage
	if err := json.Unmarshal(pub.lastPayload, &got); err != nil {
		t.Fatalf("payload not valid JSON: %v", err)
	}
	// 5 IDs + organization_id (non-empty, present) + 8 artifact slots +
	// timestamp.
	wantKeys := []string{
		"correlation_id", "timestamp", "job_id", "document_id",
		"version_id", "organization_id",
		"classification_result", "key_parameters", "risk_analysis",
		"risk_profile", "recommendations", "summary",
		"detailed_report", "aggregate_score",
	}
	for _, k := range wantKeys {
		if _, ok := got[k]; !ok {
			t.Errorf("payload missing field %q", k)
		}
	}
	// RiskDelta = nil ⇒ omitted via omitempty.
	if _, present := got["risk_delta"]; present {
		t.Errorf("payload must NOT contain risk_delta when nil (omitempty); got %s", string(pub.lastPayload))
	}

	// Metric: exactly one record, topic+outcome=success.
	rec := requireOnePubRecord(t, met)
	if rec.Topic != "lic.artifacts.analysis-ready" {
		t.Errorf("metric topic: want %q; got %q", "lic.artifacts.analysis-ready", rec.Topic)
	}
	if rec.Outcome != PublishOutcomeSuccess {
		t.Errorf("metric outcome: want %q; got %q", PublishOutcomeSuccess, rec.Outcome)
	}

	// Size observed exactly once; equals the wire payload size.
	obs := met.Observations()
	if len(obs) != 1 {
		t.Fatalf("ObservePublishedSize calls: want 1; got %d", len(obs))
	}
	if obs[0] != len(pub.lastPayload) {
		t.Errorf("observed size: want %d (len(payload)); got %d", len(pub.lastPayload), obs[0])
	}
}

// -----------------------------------------------------------------------------
// T2: timestamp = clock.Now().Format(RFC3339Nano), UTC. Caller-side payload
// variable MUST be unchanged (value semantics).
// -----------------------------------------------------------------------------

func TestPublish_Timestamp_RewrittenInWire_CallerUnchanged(t *testing.T) {
	pub := &fakePublisher{}
	met := &pubFakeMetrics{}
	clk := fakeClock{now: fixedTime}
	p := newTestPublisher(t, pub, met, clk, &fakeLogger{})

	caller := validPayload()
	caller.Timestamp = "STALE-CALLER-VALUE"
	originalTS := caller.Timestamp

	if err := p.Publish(context.Background(), caller); err != nil {
		t.Fatalf("Publish: %v", err)
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
	// Publish takes payload by value, build-spec D5).
	if caller.Timestamp != originalTS {
		t.Errorf("caller-side payload Timestamp mutated: want %q; got %q",
			originalTS, caller.Timestamp)
	}
}

// -----------------------------------------------------------------------------
// T3..T6: 4 ID validation failures (4 branches).
// -----------------------------------------------------------------------------

func TestPublish_IDValidation(t *testing.T) {
	cases := []struct {
		name       string
		mutate     func(p *port.LegalAnalysisArtifactsReady)
		wantReason string
	}{
		{"T3_empty_correlation_id", func(p *port.LegalAnalysisArtifactsReady) { p.CorrelationID = "" }, reasonMissingCorrelationID},
		{"T4_empty_job_id", func(p *port.LegalAnalysisArtifactsReady) { p.JobID = "" }, reasonMissingJobID},
		{"T5_empty_document_id", func(p *port.LegalAnalysisArtifactsReady) { p.DocumentID = "" }, reasonMissingDocumentID},
		{"T6_empty_version_id", func(p *port.LegalAnalysisArtifactsReady) { p.VersionID = "" }, reasonMissingVersionID},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			pub := &fakePublisher{}
			met := &pubFakeMetrics{}
			p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

			payload := validPayload()
			tc.mutate(&payload)

			err := p.Publish(context.Background(), payload)

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
			rec := requireOnePubRecord(t, met)
			if rec.Outcome != PublishOutcomeInvalid {
				t.Errorf("metric outcome: want %q; got %q", PublishOutcomeInvalid, rec.Outcome)
			}
			if len(met.Observations()) != 0 {
				t.Errorf("ObservePublishedSize must NOT be called on validation failure; got %d", len(met.Observations()))
			}
		})
	}
}

// -----------------------------------------------------------------------------
// T7..T14: 8 artifact-nil validation failures (8 branches).
// -----------------------------------------------------------------------------

func TestPublish_ArtifactValidation(t *testing.T) {
	cases := []struct {
		name       string
		mutate     func(p *port.LegalAnalysisArtifactsReady)
		wantReason string
	}{
		{"T7_nil_classification_result", func(p *port.LegalAnalysisArtifactsReady) { p.ClassificationResult = nil }, reasonMissingClassificationResult},
		{"T8_nil_key_parameters", func(p *port.LegalAnalysisArtifactsReady) { p.KeyParameters = nil }, reasonMissingKeyParameters},
		{"T9_nil_risk_analysis", func(p *port.LegalAnalysisArtifactsReady) { p.RiskAnalysis = nil }, reasonMissingRiskAnalysis},
		{"T10_nil_risk_profile", func(p *port.LegalAnalysisArtifactsReady) { p.RiskProfile = nil }, reasonMissingRiskProfile},
		{"T11_nil_recommendations", func(p *port.LegalAnalysisArtifactsReady) { p.Recommendations = nil }, reasonMissingRecommendations},
		{"T12_nil_summary", func(p *port.LegalAnalysisArtifactsReady) { p.Summary = nil }, reasonMissingSummary},
		{"T13_nil_detailed_report", func(p *port.LegalAnalysisArtifactsReady) { p.DetailedReport = nil }, reasonMissingDetailedReport},
		{"T14_nil_aggregate_score", func(p *port.LegalAnalysisArtifactsReady) { p.AggregateScore = nil }, reasonMissingAggregateScore},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			pub := &fakePublisher{}
			met := &pubFakeMetrics{}
			p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

			payload := validPayload()
			tc.mutate(&payload)

			err := p.Publish(context.Background(), payload)

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
				t.Errorf("publisher must NOT be called on artifact validation failure; got %d", got)
			}
			rec := requireOnePubRecord(t, met)
			if rec.Outcome != PublishOutcomeInvalid {
				t.Errorf("metric outcome: want %q; got %q", PublishOutcomeInvalid, rec.Outcome)
			}
			if len(met.Observations()) != 0 {
				t.Errorf("ObservePublishedSize must NOT be called on validation failure; got %d", len(met.Observations()))
			}
		})
	}
}

// -----------------------------------------------------------------------------
// T15: Recommendations nil → invalid (covered above in T11; this anchors the
// nil-vs-empty distinction explicitly per build-spec D4).
// -----------------------------------------------------------------------------

func TestPublish_Recommendations_NilSlice_Invalid(t *testing.T) {
	pub := &fakePublisher{}
	met := &pubFakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	payload := validPayload()
	payload.Recommendations = nil

	err := p.Publish(context.Background(), payload)
	var pe *PublishError
	if !errors.As(err, &pe) {
		t.Fatalf("want *PublishError; got %T %v", err, err)
	}
	if pe.Reason != reasonMissingRecommendations {
		t.Errorf("reason: want %q; got %q", reasonMissingRecommendations, pe.Reason)
	}
	if got := pub.Calls(); got != 0 {
		t.Errorf("publisher must NOT be called; got %d", got)
	}
}

// -----------------------------------------------------------------------------
// T16: Recommendations empty (len==0, non-nil) → SUCCESS, not invalid.
// -----------------------------------------------------------------------------

func TestPublish_Recommendations_EmptySlice_Success(t *testing.T) {
	pub := &fakePublisher{}
	met := &pubFakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	payload := validPayload()
	payload.Recommendations = model.Recommendations{} // non-nil, len==0

	if err := p.Publish(context.Background(), payload); err != nil {
		t.Fatalf("Publish on empty (non-nil) Recommendations: %v", err)
	}
	if got := pub.Calls(); got != 1 {
		t.Errorf("publisher calls: want 1; got %d", got)
	}
	rec := requireOnePubRecord(t, met)
	if rec.Outcome != PublishOutcomeSuccess {
		t.Errorf("metric outcome: want %q; got %q", PublishOutcomeSuccess, rec.Outcome)
	}

	// Wire payload still contains recommendations key (empty slice — no
	// omitempty on Recommendations).
	var got map[string]json.RawMessage
	if err := json.Unmarshal(pub.lastPayload, &got); err != nil {
		t.Fatalf("payload not valid JSON: %v", err)
	}
	if _, present := got["recommendations"]; !present {
		t.Errorf("payload missing recommendations key for empty slice")
	}
}

// -----------------------------------------------------------------------------
// T17: OrganizationID omitempty — empty → omitted; non-empty → present.
// -----------------------------------------------------------------------------

func TestPublish_OrganizationID_OmitemptyContract(t *testing.T) {
	t.Run("empty_omitted", func(t *testing.T) {
		pub := &fakePublisher{}
		met := &pubFakeMetrics{}
		p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

		payload := validPayload()
		payload.OrganizationID = ""

		if err := p.Publish(context.Background(), payload); err != nil {
			t.Fatalf("Publish: %v", err)
		}
		var got map[string]json.RawMessage
		if err := json.Unmarshal(pub.lastPayload, &got); err != nil {
			t.Fatalf("payload not valid JSON: %v", err)
		}
		if _, present := got["organization_id"]; present {
			t.Errorf("payload must NOT contain organization_id when empty (omitempty); got %s", string(pub.lastPayload))
		}
	})
	t.Run("non_empty_present", func(t *testing.T) {
		pub := &fakePublisher{}
		met := &pubFakeMetrics{}
		p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

		payload := validPayload()
		payload.OrganizationID = "org-XYZ"

		if err := p.Publish(context.Background(), payload); err != nil {
			t.Fatalf("Publish: %v", err)
		}
		var got struct {
			OrganizationID string `json:"organization_id"`
		}
		if err := json.Unmarshal(pub.lastPayload, &got); err != nil {
			t.Fatalf("payload not valid JSON: %v", err)
		}
		if got.OrganizationID != "org-XYZ" {
			t.Errorf("organization_id: want %q; got %q", "org-XYZ", got.OrganizationID)
		}
	})
}

// -----------------------------------------------------------------------------
// T18: RiskDelta omitempty — nil → omitted; non-nil → present.
// -----------------------------------------------------------------------------

func TestPublish_RiskDelta_OmitemptyContract(t *testing.T) {
	t.Run("nil_omitted", func(t *testing.T) {
		pub := &fakePublisher{}
		met := &pubFakeMetrics{}
		p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

		payload := validPayload() // RiskDelta nil by default

		if err := p.Publish(context.Background(), payload); err != nil {
			t.Fatalf("Publish: %v", err)
		}
		var got map[string]json.RawMessage
		if err := json.Unmarshal(pub.lastPayload, &got); err != nil {
			t.Fatalf("payload not valid JSON: %v", err)
		}
		if _, present := got["risk_delta"]; present {
			t.Errorf("payload must NOT contain risk_delta when nil (omitempty); got %s", string(pub.lastPayload))
		}
	})
	t.Run("non_nil_present", func(t *testing.T) {
		pub := &fakePublisher{}
		met := &pubFakeMetrics{}
		p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

		payload := validPayload()
		payload.RiskDelta = &model.RiskDelta{
			BaseVersionID:   "v-base",
			TargetVersionID: "v-target",
		}

		if err := p.Publish(context.Background(), payload); err != nil {
			t.Fatalf("Publish: %v", err)
		}
		var got map[string]json.RawMessage
		if err := json.Unmarshal(pub.lastPayload, &got); err != nil {
			t.Fatalf("payload not valid JSON: %v", err)
		}
		if _, present := got["risk_delta"]; !present {
			t.Errorf("payload must contain risk_delta when non-nil; got %s", string(pub.lastPayload))
		}
	})
}

// -----------------------------------------------------------------------------
// T19: marshal-failure path coverage. Override marshalArtifacts via the
// package-level seam; verify reason+Cause+metric+publisher-not-called+
// size-not-observed.
// -----------------------------------------------------------------------------

func TestPublish_MarshalFailure_EmitsFailureMetricAndPublishesNothing(t *testing.T) {
	originalMarshal := marshalArtifacts
	defer func() { marshalArtifacts = originalMarshal }()

	wantCause := errors.New("synthetic marshal failure")
	marshalArtifacts = func(_ any) ([]byte, error) { return nil, wantCause }

	pub := &fakePublisher{}
	met := &pubFakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	err := p.Publish(context.Background(), validPayload())

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
	rec := requireOnePubRecord(t, met)
	if rec.Outcome != PublishOutcomeFailure {
		t.Errorf("metric outcome: want %q; got %q", PublishOutcomeFailure, rec.Outcome)
	}
	if rec.Topic != "lic.artifacts.analysis-ready" {
		t.Errorf("metric topic: want lic.artifacts.analysis-ready; got %q", rec.Topic)
	}
	if got := len(met.Observations()); got != 0 {
		t.Errorf("ObservePublishedSize must NOT be called on marshal failure; got %d", got)
	}
}

// -----------------------------------------------------------------------------
// T20..T26: 7 broker outcomes — success, ctx.Canceled, ctx.DeadlineExceeded,
// broker.ErrPublishNack, broker.ErrConfirmTimeout, broker.ErrNotConnected,
// generic *broker.BrokerError. Raw err passthrough + correct PublishOutcome
// label + size observed exactly once.
// -----------------------------------------------------------------------------

func TestPublish_BrokerOutcomes(t *testing.T) {
	cases := []struct {
		name        string
		returnErr   error
		wantOutcome PublishOutcome
		wantIs      error // errors.Is target; nil ⇒ no Is check
	}{
		{"T20_success", nil, PublishOutcomeSuccess, nil},
		{"T21_ctx_canceled", context.Canceled, PublishOutcomeFailure, context.Canceled},
		{"T22_ctx_deadline_exceeded", context.DeadlineExceeded, PublishOutcomeFailure, context.DeadlineExceeded},
		{
			"T23_broker_nack",
			&broker.BrokerError{Op: "Publish", Retryable: true, Cause: broker.ErrPublishNack},
			PublishOutcomeNacked,
			broker.ErrPublishNack,
		},
		{
			"T24_broker_confirm_timeout",
			&broker.BrokerError{Op: "Publish", Retryable: true, Cause: broker.ErrConfirmTimeout},
			PublishOutcomeFailure,
			broker.ErrConfirmTimeout,
		},
		{
			"T25_broker_not_connected",
			&broker.BrokerError{Op: "Publish", Retryable: true, Cause: broker.ErrNotConnected},
			PublishOutcomeFailure,
			broker.ErrNotConnected,
		},
		{
			"T26_broker_non_retryable_404",
			&broker.BrokerError{Op: "Publish", Retryable: false, Cause: errors.New("404 NotFound")},
			PublishOutcomeFailure,
			nil, // no sentinel — just *BrokerError
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			pub := &fakePublisher{returnErr: tc.returnErr}
			met := &pubFakeMetrics{}
			p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

			err := p.Publish(context.Background(), validPayload())

			if tc.returnErr == nil {
				if err != nil {
					t.Errorf("Publish: want nil; got %v", err)
				}
			} else {
				if err == nil {
					t.Fatal("Publish: want non-nil err; got nil")
				}
				if tc.wantIs != nil && !errors.Is(err, tc.wantIs) {
					t.Errorf("errors.Is(err, %v): want true; got false (err=%v)", tc.wantIs, err)
				}
				// Raw pass-through: ctx errors NOT wrapped in *PublishError.
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					var pe *PublishError
					if errors.As(err, &pe) {
						t.Errorf("ctx error must NOT be wrapped in *PublishError; got %v", err)
					}
				}
			}

			rec := requireOnePubRecord(t, met)
			if rec.Outcome != tc.wantOutcome {
				t.Errorf("metric outcome: want %q; got %q", tc.wantOutcome, rec.Outcome)
			}
			if rec.Topic != "lic.artifacts.analysis-ready" {
				t.Errorf("metric topic: want lic.artifacts.analysis-ready; got %q", rec.Topic)
			}

			// Size observed exactly once — even on broker failure (the
			// payload reached the wire boundary).
			obs := met.Observations()
			if len(obs) != 1 {
				t.Errorf("ObservePublishedSize calls: want 1; got %d", len(obs))
			}
		})
	}
}

// -----------------------------------------------------------------------------
// T27: concurrency — 16 goroutines × distinct correlationIDs, -race clean.
// -----------------------------------------------------------------------------

func TestPublish_Concurrent_DistinctCorrelationIDs(t *testing.T) {
	const N = 16
	pub := &fakePublisher{}
	met := &pubFakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	var wg sync.WaitGroup
	wg.Add(N)
	var errCount atomic.Int64
	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			payload := validPayload()
			payload.CorrelationID = "corr-" + strconv.Itoa(i)
			if err := p.Publish(context.Background(), payload); err != nil {
				errCount.Add(1)
				t.Errorf("g%d: Publish: %v", i, err)
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
		if rec.Topic != "lic.artifacts.analysis-ready" {
			t.Errorf("metric topic: want lic.artifacts.analysis-ready; got %q", rec.Topic)
		}
	}
	if got := len(met.Observations()); got != N {
		t.Errorf("ObservePublishedSize calls: want %d; got %d", N, got)
	}
}

// -----------------------------------------------------------------------------
// T28: ObservePublishedSize called exactly once per call even when
// broker.Publish fails (nack / failure-after-marshal). Covered table-wise by
// T20..T26 above, but pinned here explicitly with a NACK to anchor the
// "size observed even on broker fail" invariant per build-spec D4.
// -----------------------------------------------------------------------------

func TestPublish_ObservePublishedSize_OnceEvenOnBrokerFail(t *testing.T) {
	nackErr := &broker.BrokerError{Op: "Publish", Retryable: true, Cause: broker.ErrPublishNack}
	pub := &fakePublisher{returnErr: nackErr}
	met := &pubFakeMetrics{}
	p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	err := p.Publish(context.Background(), validPayload())
	if !errors.Is(err, broker.ErrPublishNack) {
		t.Errorf("want errors.Is(err, broker.ErrPublishNack); got %v", err)
	}
	obs := met.Observations()
	if len(obs) != 1 {
		t.Fatalf("ObservePublishedSize calls on broker NACK: want 1; got %d", len(obs))
	}
	if obs[0] <= 0 {
		t.Errorf("observed size: want > 0; got %d", obs[0])
	}
}

// -----------------------------------------------------------------------------
// T29: ObservePublishedSize NOT called on validation-fail AND marshal-fail.
// Anchors the contract from the §3.5 histogram godoc ("NOT called on
// validation-fail or marshal-fail — no bytes were produced"). Validation-fail
// is covered table-wise by T3..T14; this test pins the marshal-fail leg.
// -----------------------------------------------------------------------------

func TestPublish_ObservePublishedSize_NotCalledOnValidationOrMarshalFail(t *testing.T) {
	t.Run("validation_fail_no_size_observed", func(t *testing.T) {
		pub := &fakePublisher{}
		met := &pubFakeMetrics{}
		p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

		payload := validPayload()
		payload.JobID = "" // trigger validation failure
		_ = p.Publish(context.Background(), payload)

		if got := len(met.Observations()); got != 0 {
			t.Errorf("ObservePublishedSize on validation fail: want 0; got %d", got)
		}
	})
	t.Run("marshal_fail_no_size_observed", func(t *testing.T) {
		originalMarshal := marshalArtifacts
		defer func() { marshalArtifacts = originalMarshal }()
		marshalArtifacts = func(_ any) ([]byte, error) { return nil, errors.New("synth") }

		pub := &fakePublisher{}
		met := &pubFakeMetrics{}
		p := newTestPublisher(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

		_ = p.Publish(context.Background(), validPayload())

		if got := len(met.Observations()); got != 0 {
			t.Errorf("ObservePublishedSize on marshal fail: want 0; got %d", got)
		}
	})
}

// -----------------------------------------------------------------------------
// T-CTOR-1: PublisherDeps.Publisher=nil → constructor error contains "Publisher"
// -----------------------------------------------------------------------------

func TestNewAnalysisArtifactsPublisher_NilPublisher_ConstructorError(t *testing.T) {
	p, err := NewAnalysisArtifactsPublisher(PublisherConfig{Exchange: testExchange}, PublisherDeps{
		Publisher: nil,
		Metrics:   &pubFakeMetrics{},
		Clock:     fakeClock{now: fixedTime},
		Logger:    &fakeLogger{},
	})
	if err == nil {
		t.Fatal("want error on nil Publisher; got nil")
	}
	if p != nil {
		t.Errorf("want nil publisher on error; got %+v", p)
	}
	if got := err.Error(); !strings.Contains(got, "Publisher") {
		t.Errorf("error must name the offending field (Publisher); got %q", got)
	}
}

// -----------------------------------------------------------------------------
// T-CTOR-2: PublisherConfig.Exchange="" → constructor error contains "Exchange"
// -----------------------------------------------------------------------------

func TestNewAnalysisArtifactsPublisher_EmptyExchange_ConstructorError(t *testing.T) {
	p, err := NewAnalysisArtifactsPublisher(PublisherConfig{Exchange: ""}, PublisherDeps{
		Publisher: &fakePublisher{},
	})
	if err == nil {
		t.Fatal("want error on empty Exchange; got nil")
	}
	if p != nil {
		t.Errorf("want nil publisher on error; got %+v", p)
	}
	if got := err.Error(); !strings.Contains(got, "Exchange") {
		t.Errorf("error must name the offending field (Exchange); got %q", got)
	}
}

// -----------------------------------------------------------------------------
// T-CTOR-3: nil optional seams (Metrics/Clock/Logger) → noop defaults;
// Publish works without panic.
// -----------------------------------------------------------------------------

func TestNewAnalysisArtifactsPublisher_NilOptionalSeams_NoopDefaults(t *testing.T) {
	pub := &fakePublisher{}
	p, err := NewAnalysisArtifactsPublisher(PublisherConfig{Exchange: testExchange}, PublisherDeps{
		Publisher: pub,
		// Metrics / Clock / Logger intentionally nil — must get noop defaults.
	})
	if err != nil {
		t.Fatalf("NewAnalysisArtifactsPublisher with nil optional seams: %v", err)
	}
	if p == nil {
		t.Fatal("want non-nil publisher; got nil")
	}
	// Hot path must work without panicking on the noop seams.
	if err := p.Publish(context.Background(), validPayload()); err != nil {
		t.Errorf("Publish: %v", err)
	}
	if got := pub.Calls(); got != 1 {
		t.Errorf("publisher calls: want 1; got %d", got)
	}
}

// -----------------------------------------------------------------------------
// T-CTOR-4: both defects (nil Publisher + empty Exchange) → errors.Join
// surfaces both substrings.
// -----------------------------------------------------------------------------

func TestNewAnalysisArtifactsPublisher_BothDefects_ErrorsJoinSurfacesBoth(t *testing.T) {
	p, err := NewAnalysisArtifactsPublisher(PublisherConfig{Exchange: ""}, PublisherDeps{
		Publisher: nil,
	})
	if err == nil {
		t.Fatal("want error on both defects; got nil")
	}
	if p != nil {
		t.Errorf("want nil publisher on error; got %+v", p)
	}
	got := err.Error()
	if !strings.Contains(got, "Publisher") {
		t.Errorf("error must mention Publisher defect; got %q", got)
	}
	if !strings.Contains(got, "Exchange") {
		t.Errorf("error must mention Exchange defect; got %q", got)
	}
}
