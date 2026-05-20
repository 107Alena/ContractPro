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
	"contractpro/legal-intelligence-core/internal/infra/broker"
)

// -----------------------------------------------------------------------------
// Test fakes
// -----------------------------------------------------------------------------

// fakePublisher is a concurrency-safe in-memory Publisher seam. Captures the
// last invocation's exchange / routingKey / payload and an atomically
// counted call total so the success-on-concurrency test (T17) can pin the
// publish count without mutex contention on the assertion.
type fakePublisher struct {
	mu             sync.Mutex
	calledCount    atomic.Int64
	lastExchange   string
	lastRoutingKey string
	lastPayload    []byte
	// returnErr is returned from every Publish call; nil ⇒ success.
	returnErr error
	// allCalls captures every (exchange, routingKey, payload) tuple
	// across goroutines for the T17 concurrency assertion.
	allCalls []fakePublishCall
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
	// Defensive copy: the caller (RequestArtifacts) hands us a slice
	// from json.Marshal; we may need to inspect it after additional calls.
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

// fakeMetrics is a concurrency-safe in-memory Metrics seam.
type fakeMetrics struct {
	mu      sync.Mutex
	records []fakeMetricRecord
}

type fakeMetricRecord struct {
	Topic   string
	Outcome PublishOutcome
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

// fakeLogger captures Warn/Error call counts so tests can assert silence
// where required.
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
// Helpers
// -----------------------------------------------------------------------------

const (
	testExchange       = "lic.requests"
	testCorrelationID  = "corr-1"
	testJobID          = "job-1"
	testDocumentID     = "doc-1"
	testVersionID      = "ver-1"
	testOrganizationID = "org-1"
)

var testArtifactTypes = []model.ArtifactType{
	model.ArtifactSemanticTree,
	model.ArtifactExtractedText,
}

// fixedTime is the deterministic clock tick used across tests. UTC.
var fixedTime = time.Date(2026, 5, 20, 10, 30, 45, 123456789, time.UTC)

// newTestRequester builds a fresh requester with the supplied seams. Optional
// publisher; nil ⇒ a fresh fakePublisher.
func newTestRequester(t *testing.T, pub Publisher, m Metrics, c Clock, l Logger) *ArtifactRequester {
	t.Helper()
	if pub == nil {
		pub = &fakePublisher{}
	}
	r, err := NewArtifactRequester(Config{Exchange: testExchange}, Deps{
		Publisher: pub,
		Metrics:   m,
		Clock:     c,
		Logger:    l,
	})
	if err != nil {
		t.Fatalf("NewArtifactRequester: %v", err)
	}
	return r
}

// requireOneRecord asserts that exactly one metric record was emitted and
// returns it.
func requireOneRecord(t *testing.T, m *fakeMetrics) fakeMetricRecord {
	t.Helper()
	recs := m.Records()
	if len(recs) != 1 {
		t.Fatalf("want exactly 1 metric record; got %d (%+v)", len(recs), recs)
	}
	return recs[0]
}

// -----------------------------------------------------------------------------
// T1: success — publish on Exchange/topic, payload contains all 6 fields,
// metric={topic, outcome=success}
// -----------------------------------------------------------------------------

func TestRequestArtifacts_Success_PublishesEnvelope(t *testing.T) {
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	clk := fakeClock{now: fixedTime}
	log := &fakeLogger{}
	r := newTestRequester(t, pub, met, clk, log)

	err := r.RequestArtifacts(context.Background(),
		testCorrelationID, testJobID, testDocumentID, testVersionID, testOrganizationID,
		testArtifactTypes)
	if err != nil {
		t.Fatalf("RequestArtifacts: %v", err)
	}

	// Publisher was called once on the configured exchange + topic.
	if got := pub.Calls(); got != 1 {
		t.Fatalf("publisher calls: want 1; got %d", got)
	}
	if pub.lastExchange != testExchange {
		t.Errorf("exchange: want %q; got %q", testExchange, pub.lastExchange)
	}
	if pub.lastRoutingKey != "lic.requests.artifacts" {
		t.Errorf("routingKey: want %q; got %q", "lic.requests.artifacts", pub.lastRoutingKey)
	}

	// Payload contains all 6 fields with the expected values.
	var got map[string]json.RawMessage
	if err := json.Unmarshal(pub.lastPayload, &got); err != nil {
		t.Fatalf("payload not valid JSON: %v", err)
	}
	for _, k := range []string{
		"correlation_id", "timestamp", "job_id", "document_id",
		"version_id", "organization_id", "artifact_types",
	} {
		if _, ok := got[k]; !ok {
			t.Errorf("payload missing field %q", k)
		}
	}

	// Metric: exactly one record, topic+outcome=success.
	rec := requireOneRecord(t, met)
	if rec.Topic != "lic.requests.artifacts" {
		t.Errorf("metric topic: want %q; got %q", "lic.requests.artifacts", rec.Topic)
	}
	if rec.Outcome != PublishOutcomeSuccess {
		t.Errorf("metric outcome: want %q; got %q", PublishOutcomeSuccess, rec.Outcome)
	}
}

// -----------------------------------------------------------------------------
// T2: timestamp = clock.Now().Format(RFC3339Nano), UTC
// -----------------------------------------------------------------------------

func TestRequestArtifacts_Timestamp_RFC3339Nano_UTC(t *testing.T) {
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	clk := fakeClock{now: fixedTime}
	r := newTestRequester(t, pub, met, clk, &fakeLogger{})

	if err := r.RequestArtifacts(context.Background(),
		testCorrelationID, testJobID, testDocumentID, testVersionID, testOrganizationID,
		testArtifactTypes); err != nil {
		t.Fatalf("RequestArtifacts: %v", err)
	}

	var got struct {
		Timestamp string `json:"timestamp"`
	}
	if err := json.Unmarshal(pub.lastPayload, &got); err != nil {
		t.Fatalf("payload not valid JSON: %v", err)
	}
	wantTS := fixedTime.Format(time.RFC3339Nano)
	if got.Timestamp != wantTS {
		t.Errorf("timestamp: want %q; got %q", wantTS, got.Timestamp)
	}

	// Round-trip parse: must be RFC3339Nano-parseable and UTC.
	parsed, err := time.Parse(time.RFC3339Nano, got.Timestamp)
	if err != nil {
		t.Fatalf("timestamp not RFC3339Nano-parseable: %v", err)
	}
	if parsed.Location().String() != "UTC" {
		t.Errorf("timestamp not UTC: %s", parsed.Location().String())
	}
}

// -----------------------------------------------------------------------------
// T3..T7: validation failures — empty required field → PublishError, no
// publish call, metric=invalid
// -----------------------------------------------------------------------------

func TestRequestArtifacts_Validation(t *testing.T) {
	cases := []struct {
		name           string
		correlationID  string
		jobID          string
		documentID     string
		versionID      string
		organizationID string
		artifactTypes  []model.ArtifactType
		wantReason     string
	}{
		{
			name:           "T3_empty_correlation_id",
			correlationID:  "",
			jobID:          testJobID,
			documentID:     testDocumentID,
			versionID:      testVersionID,
			organizationID: testOrganizationID,
			artifactTypes:  testArtifactTypes,
			wantReason:     reasonMissingCorrelationID,
		},
		{
			name:           "T4_empty_job_id",
			correlationID:  testCorrelationID,
			jobID:          "",
			documentID:     testDocumentID,
			versionID:      testVersionID,
			organizationID: testOrganizationID,
			artifactTypes:  testArtifactTypes,
			wantReason:     reasonMissingJobID,
		},
		{
			name:           "T5_empty_document_id",
			correlationID:  testCorrelationID,
			jobID:          testJobID,
			documentID:     "",
			versionID:      testVersionID,
			organizationID: testOrganizationID,
			artifactTypes:  testArtifactTypes,
			wantReason:     reasonMissingDocumentID,
		},
		{
			name:           "T6_empty_version_id",
			correlationID:  testCorrelationID,
			jobID:          testJobID,
			documentID:     testDocumentID,
			versionID:      "",
			organizationID: testOrganizationID,
			artifactTypes:  testArtifactTypes,
			wantReason:     reasonMissingVersionID,
		},
		{
			name:           "T7_empty_artifact_types",
			correlationID:  testCorrelationID,
			jobID:          testJobID,
			documentID:     testDocumentID,
			versionID:      testVersionID,
			organizationID: testOrganizationID,
			artifactTypes:  nil,
			wantReason:     reasonMissingArtifactTypes,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			pub := &fakePublisher{}
			met := &fakeMetrics{}
			r := newTestRequester(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

			err := r.RequestArtifacts(context.Background(),
				tc.correlationID, tc.jobID, tc.documentID, tc.versionID,
				tc.organizationID, tc.artifactTypes)

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
// T8: invalid artifact type ("BOGUS") → PublishError, no publish, metric=invalid
// -----------------------------------------------------------------------------

func TestRequestArtifacts_InvalidArtifactType(t *testing.T) {
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	r := newTestRequester(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	err := r.RequestArtifacts(context.Background(),
		testCorrelationID, testJobID, testDocumentID, testVersionID, testOrganizationID,
		[]model.ArtifactType{model.ArtifactSemanticTree, model.ArtifactType("BOGUS")})

	var pe *PublishError
	if !errors.As(err, &pe) {
		t.Fatalf("want *PublishError; got %T %v", err, err)
	}
	if pe.Reason != reasonInvalidArtifactType {
		t.Errorf("reason: want %q; got %q", reasonInvalidArtifactType, pe.Reason)
	}
	if got := pub.Calls(); got != 0 {
		t.Errorf("publisher must NOT be called on invalid artifact type; got %d calls", got)
	}
	rec := requireOneRecord(t, met)
	if rec.Outcome != PublishOutcomeInvalid {
		t.Errorf("metric outcome: want %q; got %q", PublishOutcomeInvalid, rec.Outcome)
	}
}

// -----------------------------------------------------------------------------
// T9: organizationID="" → success, payload JSON does NOT contain
// "organization_id" (omitempty)
// -----------------------------------------------------------------------------

func TestRequestArtifacts_EmptyOrganizationID_OmittedFromPayload(t *testing.T) {
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	r := newTestRequester(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	if err := r.RequestArtifacts(context.Background(),
		testCorrelationID, testJobID, testDocumentID, testVersionID, "",
		testArtifactTypes); err != nil {
		t.Fatalf("RequestArtifacts: %v", err)
	}

	var got map[string]json.RawMessage
	if err := json.Unmarshal(pub.lastPayload, &got); err != nil {
		t.Fatalf("payload not valid JSON: %v", err)
	}
	if _, present := got["organization_id"]; present {
		t.Errorf("payload must NOT contain organization_id when empty (omitempty); got payload=%s", string(pub.lastPayload))
	}
	rec := requireOneRecord(t, met)
	if rec.Outcome != PublishOutcomeSuccess {
		t.Errorf("metric outcome: want %q; got %q", PublishOutcomeSuccess, rec.Outcome)
	}
}

// -----------------------------------------------------------------------------
// T10: broker success (nil) → method nil, metric=success
// -----------------------------------------------------------------------------

func TestRequestArtifacts_BrokerSuccess(t *testing.T) {
	pub := &fakePublisher{returnErr: nil}
	met := &fakeMetrics{}
	r := newTestRequester(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	if err := r.RequestArtifacts(context.Background(),
		testCorrelationID, testJobID, testDocumentID, testVersionID, testOrganizationID,
		testArtifactTypes); err != nil {
		t.Fatalf("RequestArtifacts: want nil err; got %v", err)
	}
	rec := requireOneRecord(t, met)
	if rec.Outcome != PublishOutcomeSuccess {
		t.Errorf("metric outcome: want %q; got %q", PublishOutcomeSuccess, rec.Outcome)
	}
}

// -----------------------------------------------------------------------------
// T11: broker NACK → method returns err raw, metric=nacked
// -----------------------------------------------------------------------------

func TestRequestArtifacts_BrokerNack(t *testing.T) {
	nackErr := &broker.BrokerError{Op: "Publish", Retryable: true, Cause: broker.ErrPublishNack}
	pub := &fakePublisher{returnErr: nackErr}
	met := &fakeMetrics{}
	r := newTestRequester(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	err := r.RequestArtifacts(context.Background(),
		testCorrelationID, testJobID, testDocumentID, testVersionID, testOrganizationID,
		testArtifactTypes)
	if !errors.Is(err, broker.ErrPublishNack) {
		t.Errorf("want errors.Is(err, broker.ErrPublishNack); got %v", err)
	}
	// errors.Is(err, BrokerError) chain must remain intact (raw pass-through).
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
// T12: broker ConfirmTimeout → metric=failure
// -----------------------------------------------------------------------------

func TestRequestArtifacts_BrokerConfirmTimeout(t *testing.T) {
	cttErr := &broker.BrokerError{Op: "Publish", Retryable: true, Cause: broker.ErrConfirmTimeout}
	pub := &fakePublisher{returnErr: cttErr}
	met := &fakeMetrics{}
	r := newTestRequester(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	err := r.RequestArtifacts(context.Background(),
		testCorrelationID, testJobID, testDocumentID, testVersionID, testOrganizationID,
		testArtifactTypes)
	if !errors.Is(err, broker.ErrConfirmTimeout) {
		t.Errorf("want errors.Is(err, broker.ErrConfirmTimeout); got %v", err)
	}
	rec := requireOneRecord(t, met)
	if rec.Outcome != PublishOutcomeFailure {
		t.Errorf("metric outcome: want %q; got %q", PublishOutcomeFailure, rec.Outcome)
	}
}

// -----------------------------------------------------------------------------
// T13: broker NotConnected → metric=failure
// -----------------------------------------------------------------------------

func TestRequestArtifacts_BrokerNotConnected(t *testing.T) {
	ncErr := &broker.BrokerError{Op: "Publish", Retryable: true, Cause: broker.ErrNotConnected}
	pub := &fakePublisher{returnErr: ncErr}
	met := &fakeMetrics{}
	r := newTestRequester(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	err := r.RequestArtifacts(context.Background(),
		testCorrelationID, testJobID, testDocumentID, testVersionID, testOrganizationID,
		testArtifactTypes)
	if !errors.Is(err, broker.ErrNotConnected) {
		t.Errorf("want errors.Is(err, broker.ErrNotConnected); got %v", err)
	}
	rec := requireOneRecord(t, met)
	if rec.Outcome != PublishOutcomeFailure {
		t.Errorf("metric outcome: want %q; got %q", PublishOutcomeFailure, rec.Outcome)
	}
}

// -----------------------------------------------------------------------------
// T14: broker non-retryable (404-style) → metric=failure
// -----------------------------------------------------------------------------

func TestRequestArtifacts_BrokerNonRetryable(t *testing.T) {
	nrErr := &broker.BrokerError{Op: "Publish", Retryable: false, Cause: errors.New("404 NotFound")}
	pub := &fakePublisher{returnErr: nrErr}
	met := &fakeMetrics{}
	r := newTestRequester(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	err := r.RequestArtifacts(context.Background(),
		testCorrelationID, testJobID, testDocumentID, testVersionID, testOrganizationID,
		testArtifactTypes)
	if err == nil {
		t.Fatal("want non-nil error; got nil")
	}
	var be *broker.BrokerError
	if !errors.As(err, &be) {
		t.Errorf("want errors.As → *broker.BrokerError; got %T", err)
	}
	rec := requireOneRecord(t, met)
	if rec.Outcome != PublishOutcomeFailure {
		t.Errorf("metric outcome: want %q; got %q", PublishOutcomeFailure, rec.Outcome)
	}
}

// -----------------------------------------------------------------------------
// T15: ctx.Canceled raw → method returns context.Canceled raw, metric=failure
// -----------------------------------------------------------------------------

func TestRequestArtifacts_ContextCanceled(t *testing.T) {
	pub := &fakePublisher{returnErr: context.Canceled}
	met := &fakeMetrics{}
	r := newTestRequester(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	err := r.RequestArtifacts(context.Background(),
		testCorrelationID, testJobID, testDocumentID, testVersionID, testOrganizationID,
		testArtifactTypes)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("want errors.Is(err, context.Canceled); got %v", err)
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
}

// -----------------------------------------------------------------------------
// T16: ctx.DeadlineExceeded raw → method returns context.DeadlineExceeded raw,
// metric=failure
// -----------------------------------------------------------------------------

func TestRequestArtifacts_ContextDeadlineExceeded(t *testing.T) {
	pub := &fakePublisher{returnErr: context.DeadlineExceeded}
	met := &fakeMetrics{}
	r := newTestRequester(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	err := r.RequestArtifacts(context.Background(),
		testCorrelationID, testJobID, testDocumentID, testVersionID, testOrganizationID,
		testArtifactTypes)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("want errors.Is(err, context.DeadlineExceeded); got %v", err)
	}
	var pe *PublishError
	if errors.As(err, &pe) {
		t.Errorf("ctx error must NOT be wrapped in *PublishError; got %v", err)
	}
	rec := requireOneRecord(t, met)
	if rec.Outcome != PublishOutcomeFailure {
		t.Errorf("metric outcome: want %q; got %q", PublishOutcomeFailure, rec.Outcome)
	}
}

// -----------------------------------------------------------------------------
// T17: concurrent 16 goroutines × distinct correlationIDs → 16 successful
// publishes, metric counter check, -race clean
// -----------------------------------------------------------------------------

func TestRequestArtifacts_Concurrent_DistinctCorrelationIDs(t *testing.T) {
	const N = 16
	pub := &fakePublisher{}
	met := &fakeMetrics{}
	r := newTestRequester(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			corr := "corr-" + strconv.Itoa(i)
			if err := r.RequestArtifacts(context.Background(),
				corr, testJobID, testDocumentID, testVersionID, testOrganizationID,
				testArtifactTypes); err != nil {
				t.Errorf("g%d: RequestArtifacts: %v", i, err)
			}
		}()
	}
	wg.Wait()

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
		if rec.Topic != "lic.requests.artifacts" {
			t.Errorf("metric topic: want lic.requests.artifacts; got %q", rec.Topic)
		}
	}
}

// -----------------------------------------------------------------------------
// T-CTOR-1: Deps.Publisher=nil → constructor error
// -----------------------------------------------------------------------------

func TestNewArtifactRequester_NilPublisher_ConstructorError(t *testing.T) {
	r, err := NewArtifactRequester(Config{Exchange: testExchange}, Deps{
		Publisher: nil,
		Metrics:   &fakeMetrics{},
		Clock:     fakeClock{now: fixedTime},
		Logger:    &fakeLogger{},
	})
	if err == nil {
		t.Fatal("want error on nil Publisher; got nil")
	}
	if r != nil {
		t.Errorf("want nil requester on error; got %+v", r)
	}
	if got := err.Error(); !strings.Contains(got, "Publisher") {
		t.Errorf("error must name the offending field (Publisher); got %q", got)
	}
}

// -----------------------------------------------------------------------------
// T-CTOR-2: Config.Exchange="" → constructor error
// -----------------------------------------------------------------------------

func TestNewArtifactRequester_EmptyExchange_ConstructorError(t *testing.T) {
	r, err := NewArtifactRequester(Config{Exchange: ""}, Deps{
		Publisher: &fakePublisher{},
	})
	if err == nil {
		t.Fatal("want error on empty Exchange; got nil")
	}
	if r != nil {
		t.Errorf("want nil requester on error; got %+v", r)
	}
	if got := err.Error(); !strings.Contains(got, "Exchange") {
		t.Errorf("error must name the offending field (Exchange); got %q", got)
	}
}

// -----------------------------------------------------------------------------
// T-CTOR-3: Deps.Metrics/Clock/Logger=nil → success (noop defaults),
// RequestArtifacts works
// -----------------------------------------------------------------------------

func TestNewArtifactRequester_NilOptionalSeams_NoopDefaults(t *testing.T) {
	pub := &fakePublisher{}
	r, err := NewArtifactRequester(Config{Exchange: testExchange}, Deps{
		Publisher: pub,
		// Metrics / Clock / Logger intentionally nil — must get noop defaults.
	})
	if err != nil {
		t.Fatalf("NewArtifactRequester with nil optional seams: %v", err)
	}
	if r == nil {
		t.Fatal("want non-nil requester; got nil")
	}
	// Hot path must work without panicking on the noop seams.
	if err := r.RequestArtifacts(context.Background(),
		testCorrelationID, testJobID, testDocumentID, testVersionID, testOrganizationID,
		testArtifactTypes); err != nil {
		t.Errorf("RequestArtifacts: %v", err)
	}
	if got := pub.Calls(); got != 1 {
		t.Errorf("publisher calls: want 1; got %d", got)
	}
}

// -----------------------------------------------------------------------------
// classifyOutcome — table-driven test over every documented branch
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
// PublishError — Error/Unwrap behaviour
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
}

// -----------------------------------------------------------------------------
// M1 / build-spec D6 — marshal_failure path coverage
//
// The reasonMarshalFailure branch is defensive (unreachable for compliant
// inputs — port.GetArtifactsRequest has no exotic types) but the call site
// emits PublishOutcomeFailure DIRECTLY and returns *PublishError without
// invoking broker.Publish. The seam is the package-level `marshalRequest`
// var in requester.go; tests swap it for the duration of the test.
// -----------------------------------------------------------------------------

func TestRequestArtifacts_MarshalFailure_EmitsFailureMetricAndPublishesNothing(t *testing.T) {
	originalMarshal := marshalRequest
	defer func() { marshalRequest = originalMarshal }()

	wantCause := errors.New("synthetic marshal failure")
	marshalRequest = func(_ any) ([]byte, error) { return nil, wantCause }

	pub := &fakePublisher{}
	met := &fakeMetrics{}
	r := newTestRequester(t, pub, met, fakeClock{now: fixedTime}, &fakeLogger{})

	err := r.RequestArtifacts(context.Background(),
		testCorrelationID, testJobID, testDocumentID, testVersionID, testOrganizationID,
		testArtifactTypes)

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
	recs := met.Records()
	if len(recs) != 1 {
		t.Fatalf("metric records: want 1; got %d", len(recs))
	}
	if recs[0].Outcome != PublishOutcomeFailure {
		t.Errorf("metric outcome: want %q; got %q", PublishOutcomeFailure, recs[0].Outcome)
	}
	if recs[0].Topic != "lic.requests.artifacts" {
		t.Errorf("metric topic: want lic.requests.artifacts; got %q", recs[0].Topic)
	}
}
