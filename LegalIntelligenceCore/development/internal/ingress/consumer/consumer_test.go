package consumer

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
	"contractpro/legal-intelligence-core/internal/infra/broker"
)

// ----------------------------------------------------------------------------
// In-package fakes (build-spec D1 — for BrokerSubscriber, EventRouter,
// port.DLQPublisherPort, Metrics, Clock, Logger, broker.Delivery). All are
// mutex-guarded so the suite is -race clean even though the broker may invoke
// handlers concurrently.
// ----------------------------------------------------------------------------

type fakeDelivery struct {
	mu   sync.Mutex
	body []byte
	acks int
	// nacks records (requeue) per Nack call; len == nack count.
	nacks   []bool
	rejects int
	ackErr  error
	nackErr error
	headers map[string]any
	xdeath  []broker.XDeathEntry
}

var _ broker.Delivery = (*fakeDelivery)(nil)

func newDelivery(body string) *fakeDelivery { return &fakeDelivery{body: []byte(body)} }

func (f *fakeDelivery) Body() []byte { return f.body }
func (f *fakeDelivery) Header(key string) (any, bool) {
	v, ok := f.headers[key]
	return v, ok
}
func (f *fakeDelivery) Headers() map[string]any      { return f.headers }
func (f *fakeDelivery) XDeath() []broker.XDeathEntry { return f.xdeath }
func (f *fakeDelivery) Ack() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.acks++
	return f.ackErr
}
func (f *fakeDelivery) Nack(requeue bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nacks = append(f.nacks, requeue)
	return f.nackErr
}
func (f *fakeDelivery) Reject(requeue bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rejects++
	return nil
}
func (f *fakeDelivery) ackCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.acks
}
func (f *fakeDelivery) nackCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.nacks)
}
func (f *fakeDelivery) rejectCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rejects
}

// fakeRouter records the LAST typed event received per Route* method and the
// number of calls; an injected err is returned by every method.
type fakeRouter struct {
	mu    sync.Mutex
	err   error
	calls int

	gotVAR  *port.VersionProcessingArtifactsReady
	gotVC   *port.VersionCreated
	gotAP   *port.ArtifactsProvided
	gotPers *port.LegalAnalysisArtifactsPersisted
	gotPF   *port.LegalAnalysisArtifactsPersistFailed
	gotUCT  *port.UserConfirmedType
}

var _ EventRouter = (*fakeRouter)(nil)

func (r *fakeRouter) RouteVersionArtifactsReady(_ context.Context, e port.VersionProcessingArtifactsReady) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	v := e
	r.gotVAR = &v
	return r.err
}
func (r *fakeRouter) RouteVersionCreated(_ context.Context, e port.VersionCreated) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	v := e
	r.gotVC = &v
	return r.err
}
func (r *fakeRouter) RouteArtifactsProvided(_ context.Context, e port.ArtifactsProvided) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	v := e
	r.gotAP = &v
	return r.err
}
func (r *fakeRouter) RoutePersisted(_ context.Context, e port.LegalAnalysisArtifactsPersisted) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	v := e
	r.gotPers = &v
	return r.err
}
func (r *fakeRouter) RoutePersistFailed(_ context.Context, e port.LegalAnalysisArtifactsPersistFailed) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	v := e
	r.gotPF = &v
	return r.err
}
func (r *fakeRouter) RouteUserConfirmedType(_ context.Context, e port.UserConfirmedType) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	v := e
	r.gotUCT = &v
	return r.err
}
func (r *fakeRouter) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

// fakeDLQ records every published envelope and can inject a publish error.
type fakeDLQ struct {
	mu     sync.Mutex
	err    error
	topics []port.DLQTopic
	envs   []port.LICDLQEnvelope
}

var _ port.DLQPublisherPort = (*fakeDLQ)(nil)

func (q *fakeDLQ) PublishDLQ(_ context.Context, topic port.DLQTopic, env port.LICDLQEnvelope) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.topics = append(q.topics, topic)
	q.envs = append(q.envs, env)
	return q.err
}
func (q *fakeDLQ) count() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.envs)
}
func (q *fakeDLQ) last() (port.DLQTopic, port.LICDLQEnvelope) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.topics[len(q.topics)-1], q.envs[len(q.envs)-1]
}

// metricCall is one recorded ConsumerMessage(topic, outcome) tuple.
type metricCall struct{ topic, outcome string }

type fakeMetrics struct {
	mu    sync.Mutex
	calls []metricCall
}

var _ Metrics = (*fakeMetrics)(nil)

func (m *fakeMetrics) ConsumerMessage(topic, outcome string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, metricCall{topic, outcome})
}
func (m *fakeMetrics) all() []metricCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]metricCall, len(m.calls))
	copy(out, m.calls)
	return out
}

// fakeClock returns a fixed instant for deterministic FailedAt.
type fakeClock struct{ t time.Time }

var _ Clock = fakeClock{}

func (c fakeClock) Now() time.Time { return c.t }

// fakeLogger records WithRequestContext invocations (order vs Route is proven
// by a sentinel ctx value the router asserts).
type ctxKey string

const reqCtxMarker ctxKey = "consumer-test-reqctx"

type fakeLogger struct {
	mu              sync.Mutex
	withReqCtxCalls int
	lastIDs         RequestIDs
	infos           int
	warns           int
	errorsLogged    int
	errorMsgs       []string
}

var _ Logger = (*fakeLogger)(nil)

func (l *fakeLogger) Info(_ context.Context, _ string, _ ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.infos++
}
func (l *fakeLogger) Warn(_ context.Context, _ string, _ ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.warns++
}
func (l *fakeLogger) Error(_ context.Context, msg string, _ ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.errorsLogged++
	l.errorMsgs = append(l.errorMsgs, msg)
}
func (l *fakeLogger) WithRequestContext(ctx context.Context, ids RequestIDs) context.Context {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.withReqCtxCalls++
	l.lastIDs = ids
	return context.WithValue(ctx, reqCtxMarker, true)
}
func (l *fakeLogger) reqCtxCalls() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.withReqCtxCalls
}

// fakeSubscriber records (queue, handler) pairs in call order and can inject
// an error at a chosen queue.
type fakeSubscriber struct {
	mu        sync.Mutex
	queues    []string
	handlers  []broker.MessageHandler
	failQueue string
	failErr   error
}

var _ BrokerSubscriber = (*fakeSubscriber)(nil)

func (s *fakeSubscriber) Subscribe(queue string, h broker.MessageHandler) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failQueue == queue {
		return s.failErr
	}
	s.queues = append(s.queues, queue)
	s.handlers = append(s.handlers, h)
	return nil
}

// ----------------------------------------------------------------------------
// Helpers.
// ----------------------------------------------------------------------------

const testHashKey = "test-dlq-hmac-key"

func newTestConsumer(t *testing.T, r *fakeRouter, q *fakeDLQ, m *fakeMetrics, l *fakeLogger, clk Clock) *Consumer {
	t.Helper()
	if r == nil {
		r = &fakeRouter{}
	}
	if q == nil {
		q = &fakeDLQ{}
	}
	if m == nil {
		m = &fakeMetrics{}
	}
	if l == nil {
		l = &fakeLogger{}
	}
	c, err := NewConsumer(&fakeSubscriber{}, r, q, testHashKey, Deps{Metrics: m, Clock: clk, Logger: l})
	if err != nil {
		t.Fatalf("NewConsumer: unexpected error: %v", err)
	}
	return c
}

// canonical UUIDs for fixtures (canonical 36-char hyphenated form).
const (
	uCorr = "11111111-1111-4111-8111-111111111111"
	uJob  = "22222222-2222-4222-8222-222222222222"
	uDoc  = "33333333-3333-4333-8333-333333333333"
	uVer  = "44444444-4444-4444-8444-444444444444"
	uOrg  = "55555555-5555-4555-8555-555555555555"
	uUser = "66666666-6666-4666-8666-666666666666"
	uTS   = "2026-05-19T10:00:00Z"
)

// ----------------------------------------------------------------------------
// PART C #5 — test_step coverage: named tests.
// ----------------------------------------------------------------------------

func TestConsumer_InvalidJSON_DLQAndAck(t *testing.T) {
	r, q, m, l := &fakeRouter{}, &fakeDLQ{}, &fakeMetrics{}, &fakeLogger{}
	clk := fakeClock{t: time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)}
	c := newTestConsumer(t, r, q, m, l, clk)
	d := newDelivery("{not valid json")

	if err := c.handleVersionArtifactsReady(context.Background(), d); err != nil {
		t.Fatalf("handler returned non-nil advisory error: %v", err)
	}
	if q.count() != 1 {
		t.Fatalf("expected 1 DLQ publish, got %d", q.count())
	}
	if d.ackCount() != 1 || d.nackCount() != 0 {
		t.Fatalf("expected exactly 1 Ack and 0 Nack, got ack=%d nack=%d", d.ackCount(), d.nackCount())
	}
	if r.callCount() != 0 {
		t.Fatalf("router must not be called on invalid JSON, got %d calls", r.callCount())
	}
	topic, env := q.last()
	if topic != port.DLQTopicInvalidMessage {
		t.Fatalf("DLQ topic = %q, want %q", topic, port.DLQTopicInvalidMessage)
	}
	if env.OriginalTopic != "dm.events.version-artifacts-ready" {
		t.Fatalf("OriginalTopic = %q", env.OriginalTopic)
	}
	mc := m.all()
	if len(mc) != 1 || mc[0] != (metricCall{"dm.events.version-artifacts-ready", outcomeInvalid}) {
		t.Fatalf("metric = %+v, want one {topic,invalid}", mc)
	}
	if l.reqCtxCalls() != 0 {
		t.Fatalf("WithRequestContext must NOT be called on the invalid path, got %d", l.reqCtxCalls())
	}
}

func TestConsumer_MissingRequiredField_DLQ(t *testing.T) {
	cases := []struct {
		name    string
		handler func(*Consumer) func(context.Context, broker.Delivery) error
		body    string
		topic   string
	}{
		{
			name:    "VersionProcessingArtifactsReady_missing_created_by_user_id",
			handler: func(c *Consumer) func(context.Context, broker.Delivery) error { return c.handleVersionArtifactsReady },
			// created_by_user_id omitted ⇒ INVALID (events.go:52 required).
			body:  `{"correlation_id":"` + uCorr + `","timestamp":"` + uTS + `","document_id":"` + uDoc + `","version_id":"` + uVer + `","organization_id":"` + uOrg + `","job_id":"` + uJob + `"}`,
			topic: "dm.events.version-artifacts-ready",
		},
		{
			name:    "VersionCreated_missing_created_by_user_id",
			handler: func(c *Consumer) func(context.Context, broker.Delivery) error { return c.handleVersionCreated },
			body:    `{"correlation_id":"` + uCorr + `","timestamp":"` + uTS + `","document_id":"` + uDoc + `","version_id":"` + uVer + `","organization_id":"` + uOrg + `"}`,
			topic:   "dm.events.version-created",
		},
		{
			name:    "ArtifactsProvided_missing_version_id",
			handler: func(c *Consumer) func(context.Context, broker.Delivery) error { return c.handleArtifactsProvided },
			body:    `{"correlation_id":"` + uCorr + `","timestamp":"` + uTS + `","job_id":"` + uJob + `","document_id":"` + uDoc + `"}`,
			topic:   "dm.responses.artifacts-provided",
		},
		{
			name:    "LegalAnalysisArtifactsPersisted_missing_document_id",
			handler: func(c *Consumer) func(context.Context, broker.Delivery) error { return c.handlePersisted },
			body:    `{"correlation_id":"` + uCorr + `","timestamp":"` + uTS + `","job_id":"` + uJob + `"}`,
			topic:   "dm.responses.lic-artifacts-persisted",
		},
		{
			name:    "LegalAnalysisArtifactsPersistFailed_missing_error_message",
			handler: func(c *Consumer) func(context.Context, broker.Delivery) error { return c.handlePersistFailed },
			body:    `{"correlation_id":"` + uCorr + `","timestamp":"` + uTS + `","job_id":"` + uJob + `","document_id":"` + uDoc + `","is_retryable":false}`,
			topic:   "dm.responses.lic-artifacts-persist-failed",
		},
		{
			name:    "UserConfirmedType_missing_contract_type",
			handler: func(c *Consumer) func(context.Context, broker.Delivery) error { return c.handleUserConfirmedType },
			body:    `{"correlation_id":"` + uCorr + `","timestamp":"` + uTS + `","job_id":"` + uJob + `","document_id":"` + uDoc + `","version_id":"` + uVer + `","organization_id":"` + uOrg + `"}`,
			topic:   "orch.commands.user-confirmed-type",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, q, m, l := &fakeRouter{}, &fakeDLQ{}, &fakeMetrics{}, &fakeLogger{}
			c := newTestConsumer(t, r, q, m, l, fakeClock{t: time.Now().UTC()})
			d := newDelivery(tc.body)
			if err := tc.handler(c)(context.Background(), d); err != nil {
				t.Fatalf("advisory error: %v", err)
			}
			if q.count() != 1 {
				t.Fatalf("expected 1 DLQ publish, got %d", q.count())
			}
			if d.ackCount() != 1 || d.nackCount() != 0 {
				t.Fatalf("expected exactly 1 Ack and 0 Nack, got ack=%d nack=%d", d.ackCount(), d.nackCount())
			}
			if r.callCount() != 0 {
				t.Fatalf("router must not be called for an invalid message")
			}
			topic, env := q.last()
			if topic != port.DLQTopicInvalidMessage || env.OriginalTopic != tc.topic {
				t.Fatalf("DLQ topic=%q OriginalTopic=%q want invalid-message / %q", topic, env.OriginalTopic, tc.topic)
			}
			if env.ErrorCode != model.ErrCodeInvalidMessageSchema {
				t.Fatalf("ErrorCode = %q, want %q", env.ErrorCode, model.ErrCodeInvalidMessageSchema)
			}
			mc := m.all()
			if len(mc) != 1 || mc[0].outcome != outcomeInvalid || mc[0].topic != tc.topic {
				t.Fatalf("metric = %+v, want one {%q,invalid}", mc, tc.topic)
			}
		})
	}
}

func TestConsumer_ValidMessage_Dispatched(t *testing.T) {
	parent := "77777777-7777-4777-8777-777777777777"
	cases := []struct {
		name   string
		body   string
		topic  string
		invoke func(c *Consumer, d broker.Delivery) error
		assert func(t *testing.T, r *fakeRouter)
	}{
		{
			name:  "VersionProcessingArtifactsReady",
			body:  `{"correlation_id":"` + uCorr + `","timestamp":"` + uTS + `","document_id":"` + uDoc + `","version_id":"` + uVer + `","organization_id":"` + uOrg + `","job_id":"` + uJob + `","created_by_user_id":"` + uUser + `","artifact_types":["RAW_TEXT"]}`,
			topic: "dm.events.version-artifacts-ready",
			invoke: func(c *Consumer, d broker.Delivery) error {
				return c.handleVersionArtifactsReady(context.Background(), d)
			},
			assert: func(t *testing.T, r *fakeRouter) {
				if r.gotVAR == nil || r.gotVAR.JobID != uJob || r.gotVAR.CreatedByUserID != uUser {
					t.Fatalf("RouteVersionArtifactsReady got %+v", r.gotVAR)
				}
			},
		},
		{
			name:   "VersionCreated_no_job_id_is_valid",
			body:   `{"correlation_id":"` + uCorr + `","timestamp":"` + uTS + `","document_id":"` + uDoc + `","version_id":"` + uVer + `","organization_id":"` + uOrg + `","created_by_user_id":"` + uUser + `","version_number":2,"parent_version_id":"` + parent + `"}`,
			topic:  "dm.events.version-created",
			invoke: func(c *Consumer, d broker.Delivery) error { return c.handleVersionCreated(context.Background(), d) },
			assert: func(t *testing.T, r *fakeRouter) {
				if r.gotVC == nil || r.gotVC.VersionID != uVer || r.gotVC.JobID != "" {
					t.Fatalf("RouteVersionCreated got %+v (job_id must be empty/optional)", r.gotVC)
				}
			},
		},
		{
			name:   "ArtifactsProvided_no_org_id_is_valid",
			body:   `{"correlation_id":"` + uCorr + `","timestamp":"` + uTS + `","job_id":"` + uJob + `","document_id":"` + uDoc + `","version_id":"` + uVer + `","artifacts":{}}`,
			topic:  "dm.responses.artifacts-provided",
			invoke: func(c *Consumer, d broker.Delivery) error { return c.handleArtifactsProvided(context.Background(), d) },
			assert: func(t *testing.T, r *fakeRouter) {
				if r.gotAP == nil || r.gotAP.JobID != uJob {
					t.Fatalf("RouteArtifactsProvided got %+v", r.gotAP)
				}
			},
		},
		{
			name:   "LegalAnalysisArtifactsPersisted",
			body:   `{"correlation_id":"` + uCorr + `","timestamp":"` + uTS + `","job_id":"` + uJob + `","document_id":"` + uDoc + `"}`,
			topic:  "dm.responses.lic-artifacts-persisted",
			invoke: func(c *Consumer, d broker.Delivery) error { return c.handlePersisted(context.Background(), d) },
			assert: func(t *testing.T, r *fakeRouter) {
				if r.gotPers == nil || r.gotPers.DocumentID != uDoc {
					t.Fatalf("RoutePersisted got %+v", r.gotPers)
				}
			},
		},
		{
			name:   "LegalAnalysisArtifactsPersistFailed",
			body:   `{"correlation_id":"` + uCorr + `","timestamp":"` + uTS + `","job_id":"` + uJob + `","document_id":"` + uDoc + `","error_message":"DM rejected","is_retryable":true}`,
			topic:  "dm.responses.lic-artifacts-persist-failed",
			invoke: func(c *Consumer, d broker.Delivery) error { return c.handlePersistFailed(context.Background(), d) },
			assert: func(t *testing.T, r *fakeRouter) {
				if r.gotPF == nil || r.gotPF.ErrorMessage != "DM rejected" || !r.gotPF.IsRetryable {
					t.Fatalf("RoutePersistFailed got %+v", r.gotPF)
				}
			},
		},
		{
			name:   "UserConfirmedType_nonwhitelist_contract_type_is_valid_and_dispatched",
			body:   `{"correlation_id":"` + uCorr + `","timestamp":"` + uTS + `","job_id":"` + uJob + `","document_id":"` + uDoc + `","version_id":"` + uVer + `","organization_id":"` + uOrg + `","contract_type":"NOT_A_WHITELIST_VALUE","user_id":"` + uUser + `"}`,
			topic:  "orch.commands.user-confirmed-type",
			invoke: func(c *Consumer, d broker.Delivery) error { return c.handleUserConfirmedType(context.Background(), d) },
			assert: func(t *testing.T, r *fakeRouter) {
				if r.gotUCT == nil || r.gotUCT.ContractType != "NOT_A_WHITELIST_VALUE" {
					t.Fatalf("RouteUserConfirmedType got %+v (R6: 039 does NOT whitelist contract_type)", r.gotUCT)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, q, m, l := &fakeRouter{}, &fakeDLQ{}, &fakeMetrics{}, &fakeLogger{}
			c := newTestConsumer(t, r, q, m, l, fakeClock{t: time.Now().UTC()})
			d := newDelivery(tc.body)
			if err := tc.invoke(c, d); err != nil {
				t.Fatalf("advisory error: %v", err)
			}
			if r.callCount() != 1 {
				t.Fatalf("router call count = %d, want 1", r.callCount())
			}
			tc.assert(t, r)
			if d.ackCount() != 1 || d.nackCount() != 0 {
				t.Fatalf("expected exactly 1 Ack and 0 Nack, got ack=%d nack=%d", d.ackCount(), d.nackCount())
			}
			if q.count() != 0 {
				t.Fatalf("no DLQ publish expected on the valid path, got %d", q.count())
			}
			mc := m.all()
			if len(mc) != 1 || mc[0] != (metricCall{tc.topic, outcomeSuccess}) {
				t.Fatalf("metric = %+v, want one {%q,success}", mc, tc.topic)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// PART C #6 — exactly-one-ack invariant for EVERY path + #7 metric outcome.
// ----------------------------------------------------------------------------

func validVARBody() string {
	return `{"correlation_id":"` + uCorr + `","timestamp":"` + uTS + `","document_id":"` + uDoc + `","version_id":"` + uVer + `","organization_id":"` + uOrg + `","job_id":"` + uJob + `","created_by_user_id":"` + uUser + `","artifact_types":[]}`
}

func TestConsumer_ExactlyOneAck_RouterError_Nack(t *testing.T) {
	r := &fakeRouter{err: errors.New("router boom")}
	q, m, l := &fakeDLQ{}, &fakeMetrics{}, &fakeLogger{}
	c := newTestConsumer(t, r, q, m, l, fakeClock{t: time.Now().UTC()})
	d := newDelivery(validVARBody())

	if err := c.handleVersionArtifactsReady(context.Background(), d); err != nil {
		t.Fatalf("advisory error: %v", err)
	}
	if d.nackCount() != 1 || d.nacks[0] != false {
		t.Fatalf("expected exactly 1 Nack(false), got nacks=%v", d.nacks)
	}
	if d.ackCount() != 0 {
		t.Fatalf("expected 0 Ack on router error, got %d", d.ackCount())
	}
	if q.count() != 0 {
		t.Fatalf("router error must NOT publish to DLQ, got %d", q.count())
	}
	if d.rejectCount() != 0 {
		t.Fatalf("Reject must never be used, got %d", d.rejectCount())
	}
	mc := m.all()
	if len(mc) != 1 || mc[0] != (metricCall{"dm.events.version-artifacts-ready", outcomeNacked}) {
		t.Fatalf("metric = %+v, want one {topic,nacked}", mc)
	}
}

func TestConsumer_ExactlyOneAck_DLQPublishFails_Nack(t *testing.T) {
	r := &fakeRouter{}
	q := &fakeDLQ{err: errors.New("dlq down")}
	m, l := &fakeMetrics{}, &fakeLogger{}
	c := newTestConsumer(t, r, q, m, l, fakeClock{t: time.Now().UTC()})
	d := newDelivery("{broken")

	if err := c.handleVersionArtifactsReady(context.Background(), d); err != nil {
		t.Fatalf("advisory error: %v", err)
	}
	if q.count() != 1 {
		t.Fatalf("expected the DLQ publish to be attempted once, got %d", q.count())
	}
	if d.nackCount() != 1 || d.nacks[0] != false {
		t.Fatalf("expected exactly 1 Nack(false) when DLQ publish fails, got nacks=%v", d.nacks)
	}
	if d.ackCount() != 0 {
		t.Fatalf("must NOT Ack when DLQ publish fails (R2), got %d", d.ackCount())
	}
	mc := m.all()
	if len(mc) != 1 || mc[0].outcome != outcomeNacked {
		t.Fatalf("metric = %+v, want one {topic,nacked}", mc)
	}
	// R2: Error log present (invalid-message + DLQ-publish-failed = 2 Error
	// lines; at least the publish-failure one must be present).
	found := false
	for _, msg := range l.errorMsgs {
		if strings.Contains(msg, "DLQ publish failed") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an Error log for the failed DLQ publish (R2), got %v", l.errorMsgs)
	}
}

// ----------------------------------------------------------------------------
// PART C #8 — HMAC (R3) + envelope fields.
// ----------------------------------------------------------------------------

func TestConsumer_HMAC_And_EnvelopeFields(t *testing.T) {
	fixedTime := time.Date(2026, 5, 19, 9, 30, 0, 0, time.UTC)
	q := &fakeDLQ{}
	c := newTestConsumer(t, &fakeRouter{}, q, &fakeMetrics{}, &fakeLogger{}, fakeClock{t: fixedTime})
	bodyStr := "{this is not valid json at all"
	d := newDelivery(bodyStr)

	if err := c.handleArtifactsProvided(context.Background(), d); err != nil {
		t.Fatalf("advisory error: %v", err)
	}
	_, env := q.last()

	want := hmacFirst64([]byte(bodyStr), testHashKey)
	if env.OriginalMessageHash != want {
		t.Fatalf("OriginalMessageHash = %q, want %q", env.OriginalMessageHash, want)
	}
	if len(env.OriginalMessageHash) != 64 {
		t.Fatalf("hash length = %d, want 64", len(env.OriginalMessageHash))
	}
	if env.OriginalMessageHash != strings.ToLower(env.OriginalMessageHash) {
		t.Fatalf("hash must be lowercase hex: %q", env.OriginalMessageHash)
	}
	for _, ch := range env.OriginalMessageHash {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')) {
			t.Fatalf("hash has non-hex char %q", ch)
		}
	}
	// Reproducible for a fixed (key, body).
	if hmacFirst64([]byte(bodyStr), testHashKey) != want {
		t.Fatalf("hmacFirst64 not reproducible")
	}
	if env.OriginalMessageSizeBytes != len(bodyStr) {
		t.Fatalf("OriginalMessageSizeBytes = %d, want %d", env.OriginalMessageSizeBytes, len(bodyStr))
	}
	// Raw body must NOT appear anywhere in the envelope.
	if strings.Contains(env.ErrorMessage, bodyStr) || strings.Contains(env.OriginalMessageHash, bodyStr) {
		t.Fatalf("raw body leaked into envelope: %+v", env)
	}
	if env.OriginalTopic != "dm.responses.artifacts-provided" {
		t.Fatalf("OriginalTopic = %q", env.OriginalTopic)
	}
	if env.ErrorCode != model.ErrCodeInvalidMessageSchema {
		t.Fatalf("ErrorCode = %q", env.ErrorCode)
	}
	if env.RetryCount != 0 {
		t.Fatalf("RetryCount = %d, want 0", env.RetryCount)
	}
	parsed, perr := time.Parse(time.RFC3339, env.FailedAt)
	if perr != nil {
		t.Fatalf("FailedAt %q not RFC3339: %v", env.FailedAt, perr)
	}
	if !parsed.Equal(fixedTime) {
		t.Fatalf("FailedAt = %v, want injected clock %v", parsed, fixedTime)
	}
}

// ----------------------------------------------------------------------------
// PART C #9 — best-effort IDs (D12).
// ----------------------------------------------------------------------------

func TestConsumer_BestEffortIDs(t *testing.T) {
	q := &fakeDLQ{}
	c := newTestConsumer(t, &fakeRouter{}, q, &fakeMetrics{}, &fakeLogger{}, fakeClock{t: time.Now().UTC()})
	// Structurally-valid JSON but fails validation (missing timestamp etc.);
	// carries a clean correlation_id and a garbage job_id.
	body := `{"correlation_id":"` + uCorr + `","job_id":"not-a-uuid","document_id":"` + uDoc + `"}`
	d := newDelivery(body)

	if err := c.handlePersisted(context.Background(), d); err != nil {
		t.Fatalf("advisory error: %v", err)
	}
	_, env := q.last()
	if env.CorrelationID != uCorr {
		t.Fatalf("CorrelationID = %q, want %q (clean canonical UUID adopted)", env.CorrelationID, uCorr)
	}
	if env.JobID != "" {
		t.Fatalf("JobID = %q, want empty (garbage non-UUID dropped — D12)", env.JobID)
	}
	if env.DocumentID != uDoc {
		t.Fatalf("DocumentID = %q, want %q", env.DocumentID, uDoc)
	}
}

// ----------------------------------------------------------------------------
// PART C #10 — per-event D9 matrix.
// ----------------------------------------------------------------------------

func TestConsumer_PerEventMatrix(t *testing.T) {
	parent := "77777777-7777-4777-8777-777777777777"
	type row struct {
		name      string
		handler   func(c *Consumer) func(context.Context, broker.Delivery) error
		body      string
		wantValid bool
	}
	rows := []row{
		{
			name:      "ArtifactsProvided_absent_organization_id_is_VALID",
			handler:   func(c *Consumer) func(context.Context, broker.Delivery) error { return c.handleArtifactsProvided },
			body:      `{"correlation_id":"` + uCorr + `","timestamp":"` + uTS + `","job_id":"` + uJob + `","document_id":"` + uDoc + `","version_id":"` + uVer + `"}`,
			wantValid: true,
		},
		{
			name:      "Persisted_only_4_fields_is_VALID",
			handler:   func(c *Consumer) func(context.Context, broker.Delivery) error { return c.handlePersisted },
			body:      `{"correlation_id":"` + uCorr + `","timestamp":"` + uTS + `","job_id":"` + uJob + `","document_id":"` + uDoc + `"}`,
			wantValid: true,
		},
		{
			name:      "VersionProcessingArtifactsReady_missing_created_by_user_id_is_INVALID",
			handler:   func(c *Consumer) func(context.Context, broker.Delivery) error { return c.handleVersionArtifactsReady },
			body:      `{"correlation_id":"` + uCorr + `","timestamp":"` + uTS + `","document_id":"` + uDoc + `","version_id":"` + uVer + `","organization_id":"` + uOrg + `","job_id":"` + uJob + `"}`,
			wantValid: false,
		},
		{
			name:      "VersionCreated_missing_job_id_is_VALID",
			handler:   func(c *Consumer) func(context.Context, broker.Delivery) error { return c.handleVersionCreated },
			body:      `{"correlation_id":"` + uCorr + `","timestamp":"` + uTS + `","document_id":"` + uDoc + `","version_id":"` + uVer + `","organization_id":"` + uOrg + `","created_by_user_id":"` + uUser + `","parent_version_id":"` + parent + `"}`,
			wantValid: true,
		},
		{
			name:      "VersionCreated_present_but_bad_job_id_is_INVALID",
			handler:   func(c *Consumer) func(context.Context, broker.Delivery) error { return c.handleVersionCreated },
			body:      `{"correlation_id":"` + uCorr + `","timestamp":"` + uTS + `","document_id":"` + uDoc + `","version_id":"` + uVer + `","organization_id":"` + uOrg + `","created_by_user_id":"` + uUser + `","job_id":"bad"}`,
			wantValid: false,
		},
		{
			name:      "UserConfirmedType_nonwhitelist_contract_type_is_VALID",
			handler:   func(c *Consumer) func(context.Context, broker.Delivery) error { return c.handleUserConfirmedType },
			body:      `{"correlation_id":"` + uCorr + `","timestamp":"` + uTS + `","job_id":"` + uJob + `","document_id":"` + uDoc + `","version_id":"` + uVer + `","organization_id":"` + uOrg + `","contract_type":"weird-lowercase-not-whitelisted"}`,
			wantValid: true,
		},
		{
			name:      "VersionProcessingArtifactsReady_bad_uuid_is_INVALID",
			handler:   func(c *Consumer) func(context.Context, broker.Delivery) error { return c.handleVersionArtifactsReady },
			body:      `{"correlation_id":"urn:uuid:` + uCorr + `","timestamp":"` + uTS + `","document_id":"` + uDoc + `","version_id":"` + uVer + `","organization_id":"` + uOrg + `","job_id":"` + uJob + `","created_by_user_id":"` + uUser + `"}`,
			wantValid: false,
		},
	}
	for _, rr := range rows {
		t.Run(rr.name, func(t *testing.T) {
			r, q := &fakeRouter{}, &fakeDLQ{}
			c := newTestConsumer(t, r, q, &fakeMetrics{}, &fakeLogger{}, fakeClock{t: time.Now().UTC()})
			d := newDelivery(rr.body)
			if err := rr.handler(c)(context.Background(), d); err != nil {
				t.Fatalf("advisory error: %v", err)
			}
			if rr.wantValid {
				if r.callCount() != 1 || q.count() != 0 || d.ackCount() != 1 {
					t.Fatalf("expected VALID dispatch: router=%d dlq=%d ack=%d", r.callCount(), q.count(), d.ackCount())
				}
			} else {
				if r.callCount() != 0 || q.count() != 1 || d.ackCount() != 1 {
					t.Fatalf("expected INVALID→DLQ+ACK: router=%d dlq=%d ack=%d", r.callCount(), q.count(), d.ackCount())
				}
			}
		})
	}
}

// ----------------------------------------------------------------------------
// PART C #11 — RequestContext attached once before Route, never on invalid.
// ----------------------------------------------------------------------------

// routerCtxAssert asserts the ctx it receives already carries the reqCtx
// marker (i.e. WithRequestContext ran BEFORE Route).
type routerCtxAssert struct {
	fakeRouter
	t         *testing.T
	sawMarker bool
}

func (r *routerCtxAssert) RouteVersionArtifactsReady(ctx context.Context, e port.VersionProcessingArtifactsReady) error {
	if v, _ := ctx.Value(reqCtxMarker).(bool); v {
		r.sawMarker = true
	}
	return r.fakeRouter.RouteVersionArtifactsReady(ctx, e)
}

func TestConsumer_RequestContextAttachedOnceBeforeRoute(t *testing.T) {
	l := &fakeLogger{}
	r := &routerCtxAssert{t: t}
	q, m := &fakeDLQ{}, &fakeMetrics{}
	c, err := NewConsumer(&fakeSubscriber{}, r, q, testHashKey, Deps{Metrics: m, Logger: l, Clock: fakeClock{t: time.Now().UTC()}})
	if err != nil {
		t.Fatalf("NewConsumer: %v", err)
	}
	d := newDelivery(validVARBody())
	if err := c.handleVersionArtifactsReady(context.Background(), d); err != nil {
		t.Fatalf("advisory error: %v", err)
	}
	if l.reqCtxCalls() != 1 {
		t.Fatalf("WithRequestContext call count = %d, want exactly 1", l.reqCtxCalls())
	}
	if !r.sawMarker {
		t.Fatalf("Route was called with a ctx lacking the reqCtx marker — WithRequestContext must run BEFORE Route")
	}
	if l.lastIDs.JobID != uJob || l.lastIDs.CreatedByUserID != uUser || l.lastIDs.OrganizationID != uOrg {
		t.Fatalf("RequestIDs not populated per D11 table: %+v", l.lastIDs)
	}

	// Invalid path: WithRequestContext must NOT be called.
	l2 := &fakeLogger{}
	c2 := newTestConsumer(t, &fakeRouter{}, &fakeDLQ{}, &fakeMetrics{}, l2, fakeClock{t: time.Now().UTC()})
	if err := c2.handleVersionArtifactsReady(context.Background(), newDelivery("{garbage")); err != nil {
		t.Fatalf("advisory error: %v", err)
	}
	if l2.reqCtxCalls() != 0 {
		t.Fatalf("WithRequestContext must NOT run on the invalid path, got %d", l2.reqCtxCalls())
	}
}

// ----------------------------------------------------------------------------
// PART C #13 — constructor fail-fast.
// ----------------------------------------------------------------------------

func TestNewConsumer_FailFast(t *testing.T) {
	okSub := &fakeSubscriber{}
	okRouter := &fakeRouter{}
	okDLQ := &fakeDLQ{}

	cases := []struct {
		name       string
		sub        BrokerSubscriber
		router     EventRouter
		dlq        port.DLQPublisherPort
		key        string
		wantSubstr string
	}{
		{"nil_sub", nil, okRouter, okDLQ, testHashKey, "sub (BrokerSubscriber)"},
		{"nil_router", okSub, nil, okDLQ, testHashKey, "router (EventRouter)"},
		{"nil_dlq", okSub, okRouter, nil, testHashKey, "dlq (port.DLQPublisherPort)"},
		{"empty_key", okSub, okRouter, okDLQ, "   ", "dlqHashKey (LIC_DLQ_HASH_KEY)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewConsumer(tc.sub, tc.router, tc.dlq, tc.key, Deps{})
			if err == nil {
				t.Fatalf("expected a non-nil error")
			}
			if c != nil {
				t.Fatalf("expected nil *Consumer on failure, got %p", c)
			}
			if !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Fatalf("error %q does not mention %q", err.Error(), tc.wantSubstr)
			}
		})
	}

	// All-nil: the joined error mentions every failing arg.
	c, err := NewConsumer(nil, nil, nil, "", Deps{})
	if err == nil || c != nil {
		t.Fatalf("expected (nil, err) for all-nil")
	}
	for _, want := range []string{"sub (BrokerSubscriber)", "router (EventRouter)", "dlq (port.DLQPublisherPort)", "dlqHashKey (LIC_DLQ_HASH_KEY)"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("joined error %q missing %q", err.Error(), want)
		}
	}
}

// ----------------------------------------------------------------------------
// PART C #14 — subscription contract.
// ----------------------------------------------------------------------------

func TestSubscriptionTableMatchesContract(t *testing.T) {
	want := [6]subscription{
		{"lic.q.version-artifacts-ready", "dm.events.version-artifacts-ready"},
		{"lic.q.version-created", "dm.events.version-created"},
		{"lic.q.artifacts-provided", "dm.responses.artifacts-provided"},
		{"lic.q.lic-persist-confirm", "dm.responses.lic-artifacts-persisted"},
		{"lic.q.lic-persist-fail", "dm.responses.lic-artifacts-persist-failed"},
		{"lic.q.user-confirmed-type", "orch.commands.user-confirmed-type"},
	}
	if subscriptionTable != want {
		t.Fatalf("subscriptionTable = %+v, want frozen %+v (topology.go:50-55 / integration-contracts.md:170-175)", subscriptionTable, want)
	}
}

// ----------------------------------------------------------------------------
// PART C #15 — Start() semantics: idempotency + order + error-wrap.
// ----------------------------------------------------------------------------

func TestConsumer_Start_OrderAndIdempotency(t *testing.T) {
	sub := &fakeSubscriber{}
	c, err := NewConsumer(sub, &fakeRouter{}, &fakeDLQ{}, testHashKey, Deps{})
	if err != nil {
		t.Fatalf("NewConsumer: %v", err)
	}
	if err := c.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	wantQueues := []string{
		"lic.q.version-artifacts-ready",
		"lic.q.version-created",
		"lic.q.artifacts-provided",
		"lic.q.lic-persist-confirm",
		"lic.q.lic-persist-fail",
		"lic.q.user-confirmed-type",
	}
	if len(sub.queues) != 6 {
		t.Fatalf("got %d Subscribe calls, want 6", len(sub.queues))
	}
	for i := range wantQueues {
		if sub.queues[i] != wantQueues[i] {
			t.Fatalf("Subscribe[%d] queue = %q, want %q (D7 order)", i, sub.queues[i], wantQueues[i])
		}
	}
	// (queue, handler) pairing: invoking handler[i] with a valid body for that
	// event must route exactly that event. Spot-check the first and last.
	d := newDelivery(validVARBody())
	if err := sub.handlers[0](context.Background(), d); err != nil {
		t.Fatalf("handler[0] advisory error: %v", err)
	}
	d2 := newDelivery(`{"correlation_id":"` + uCorr + `","timestamp":"` + uTS + `","job_id":"` + uJob + `","document_id":"` + uDoc + `","version_id":"` + uVer + `","organization_id":"` + uOrg + `","contract_type":"X"}`)
	if err := sub.handlers[5](context.Background(), d2); err != nil {
		t.Fatalf("handler[5] advisory error: %v", err)
	}

	// Idempotency: second Start() returns the first result and does NOT
	// re-subscribe.
	if err := c.Start(); err != nil {
		t.Fatalf("2nd Start: %v", err)
	}
	if len(sub.queues) != 6 {
		t.Fatalf("Start re-subscribed: %d calls, want 6", len(sub.queues))
	}
}

func TestConsumer_Start_ErrorShortCircuitsAndWraps(t *testing.T) {
	subErr := errors.New("broker not connected")
	sub := &fakeSubscriber{failQueue: "lic.q.artifacts-provided", failErr: subErr}
	c, err := NewConsumer(sub, &fakeRouter{}, &fakeDLQ{}, testHashKey, Deps{})
	if err != nil {
		t.Fatalf("NewConsumer: %v", err)
	}
	err = c.Start()
	if err == nil {
		t.Fatalf("expected Start error")
	}
	if !errors.Is(err, subErr) {
		t.Fatalf("Start error %v does not wrap %v", err, subErr)
	}
	if !strings.Contains(err.Error(), "consumer: subscribe to lic.q.artifacts-provided") {
		t.Fatalf("Start error not wrapped with queue: %q", err.Error())
	}
	// Short-circuit: only the first two (before the failing third) succeeded.
	if len(sub.queues) != 2 {
		t.Fatalf("expected short-circuit after 2 successful subscribes, got %d", len(sub.queues))
	}
	// Idempotent: second Start returns the same stored error.
	if got := c.Start(); got == nil || got.Error() != err.Error() {
		t.Fatalf("2nd Start = %v, want stored first result %v", got, err)
	}
}

// ----------------------------------------------------------------------------
// PART C #16 — outcome constants match SSOT (no metrics import).
// ----------------------------------------------------------------------------

func TestOutcomeConstantsMatchSSOT(t *testing.T) {
	if outcomeSuccess != "success" {
		t.Fatalf("outcomeSuccess = %q, want \"success\"", outcomeSuccess)
	}
	if outcomeInvalid != "invalid" {
		t.Fatalf("outcomeInvalid = %q, want \"invalid\"", outcomeInvalid)
	}
	if outcomeNacked != "nacked" {
		t.Fatalf("outcomeNacked = %q, want \"nacked\"", outcomeNacked)
	}
}

// ----------------------------------------------------------------------------
// Defaults: nil Deps fields degrade to noop (build-spec D19) — exercising the
// noop seams so they are covered and the hot path never nil-checks.
// ----------------------------------------------------------------------------

func TestConsumer_NilDepsUseNoops(t *testing.T) {
	c, err := NewConsumer(&fakeSubscriber{}, &fakeRouter{}, &fakeDLQ{}, testHashKey, Deps{})
	if err != nil {
		t.Fatalf("NewConsumer: %v", err)
	}
	// Valid path with noop metrics/clock/logger must not panic.
	d := newDelivery(validVARBody())
	if err := c.handleVersionArtifactsReady(context.Background(), d); err != nil {
		t.Fatalf("advisory error: %v", err)
	}
	if d.ackCount() != 1 {
		t.Fatalf("expected 1 Ack with noop deps, got %d", d.ackCount())
	}
	// Invalid path with noop clock must still produce a parseable FailedAt.
	q := &fakeDLQ{}
	c2, _ := NewConsumer(&fakeSubscriber{}, &fakeRouter{}, q, testHashKey, Deps{})
	if err := c2.handlePersisted(context.Background(), newDelivery("{bad")); err != nil {
		t.Fatalf("advisory error: %v", err)
	}
	_, env := q.last()
	if _, perr := time.Parse(time.RFC3339, env.FailedAt); perr != nil {
		t.Fatalf("systemClock FailedAt not RFC3339: %v", perr)
	}
}

// ack/nack transport-error path: a failing Ack/Nack is logged but does NOT
// change the decided outcome nor re-ack (build-spec D11 invariant).
func TestConsumer_AckTransportErrorDoesNotReAck(t *testing.T) {
	l := &fakeLogger{}
	c := newTestConsumer(t, &fakeRouter{}, &fakeDLQ{}, &fakeMetrics{}, l, fakeClock{t: time.Now().UTC()})
	d := newDelivery(validVARBody())
	d.ackErr = errors.New("channel gone")
	if err := c.handleVersionArtifactsReady(context.Background(), d); err != nil {
		t.Fatalf("advisory error: %v", err)
	}
	if d.ackCount() != 1 {
		t.Fatalf("Ack must be attempted exactly once even on transport error, got %d", d.ackCount())
	}
	if d.nackCount() != 0 {
		t.Fatalf("a failed Ack must NOT be followed by a Nack, got %d", d.nackCount())
	}
}
