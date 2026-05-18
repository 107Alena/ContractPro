package pendingconfirmation

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// ============================================================================
// fakes — every port + every seam (the orchestrator_test fakes precedent)
// ============================================================================

// callRecorder records an ordered global sequence of operation labels so
// Pin 1 can assert the §6.5 strict order (Save→uncertain→IN_PROGRESS→
// SetPaused) end-to-end.
type callRecorder struct {
	mu  sync.Mutex
	seq []string
}

func (r *callRecorder) add(label string) {
	r.mu.Lock()
	r.seq = append(r.seq, label)
	r.mu.Unlock()
}
func (r *callRecorder) order() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.seq...)
}

// --- port: PendingStatePort -------------------------------------------------

type fakePending struct {
	rec       *callRecorder
	saveErr   error
	loadRet   *model.PendingTypeConfirmation
	loadErr   error
	deleteErr error

	mu       sync.Mutex
	saved    *model.PendingTypeConfirmation
	savedTTL time.Duration
	deletes  int
}

func (p *fakePending) Save(_ context.Context, _ string, st *model.PendingTypeConfirmation, ttl time.Duration) error {
	p.rec.add("Save")
	if p.saveErr != nil {
		return p.saveErr
	}
	p.mu.Lock()
	p.saved = st
	p.savedTTL = ttl
	p.mu.Unlock()
	return nil
}
func (p *fakePending) Load(context.Context, string) (*model.PendingTypeConfirmation, error) {
	p.rec.add("Load")
	if p.loadErr != nil {
		return nil, p.loadErr
	}
	return p.loadRet, nil
}
func (p *fakePending) Delete(context.Context, string) error {
	p.rec.add("Delete")
	p.mu.Lock()
	p.deletes++
	p.mu.Unlock()
	return p.deleteErr
}
func (p *fakePending) deleteCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.deletes
}

// --- port: IdempotencyStorePort --------------------------------------------

type fakeIdem struct {
	rec *callRecorder

	setnxStatus port.IdempotencyStatus
	setnxErr    error

	setPausedErr error

	mu              sync.Mutex
	setCompletedKey []string
	setPausedKeys   []string
}

func (i *fakeIdem) SetNX(_ context.Context, _ string, _ time.Duration) (port.IdempotencyStatus, error) {
	i.rec.add("SetNX")
	if i.setnxErr != nil {
		return i.setnxStatus, i.setnxErr
	}
	return port.IdempotencyAbsent, nil
}
func (i *fakeIdem) Get(context.Context, string) (port.IdempotencyStatus, error) {
	return port.IdempotencyAbsent, nil
}
func (i *fakeIdem) ExtendTTL(context.Context, string, time.Duration) error { return nil }
func (i *fakeIdem) SetCompleted(_ context.Context, key string, _ time.Duration) error {
	i.rec.add("SetCompleted:" + key)
	i.mu.Lock()
	i.setCompletedKey = append(i.setCompletedKey, key)
	i.mu.Unlock()
	return nil
}
func (i *fakeIdem) SetPaused(_ context.Context, key string, _ time.Duration) error {
	i.rec.add("SetPaused")
	if i.setPausedErr != nil {
		return i.setPausedErr
	}
	i.mu.Lock()
	i.setPausedKeys = append(i.setPausedKeys, key)
	i.mu.Unlock()
	return nil
}
func (i *fakeIdem) completedKeys() []string {
	i.mu.Lock()
	defer i.mu.Unlock()
	return append([]string(nil), i.setCompletedKey...)
}
func (i *fakeIdem) pausedKeys() []string {
	i.mu.Lock()
	defer i.mu.Unlock()
	return append([]string(nil), i.setPausedKeys...)
}

// --- port: UncertaintyPublisherPort ----------------------------------------

type fakeUncert struct {
	rec *callRecorder
	err error

	mu     sync.Mutex
	events []port.ClassificationUncertain
}

func (u *fakeUncert) PublishClassificationUncertain(_ context.Context, evt port.ClassificationUncertain) error {
	u.rec.add("Uncertain")
	if u.err != nil {
		return u.err
	}
	u.mu.Lock()
	u.events = append(u.events, evt)
	u.mu.Unlock()
	return nil
}
func (u *fakeUncert) last() (port.ClassificationUncertain, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.events) == 0 {
		return port.ClassificationUncertain{}, false
	}
	return u.events[len(u.events)-1], true
}

// --- port: StatusPublisherPort ---------------------------------------------

type fakeStatus struct {
	rec *callRecorder
	err error

	mu     sync.Mutex
	events []port.LICStatusChangedEvent
}

func (s *fakeStatus) PublishStatus(_ context.Context, evt port.LICStatusChangedEvent) error {
	s.rec.add("Status:" + string(evt.Status))
	if s.err != nil {
		return s.err
	}
	s.mu.Lock()
	s.events = append(s.events, evt)
	s.mu.Unlock()
	return nil
}
func (s *fakeStatus) byStatus(st model.ExternalStatus) []port.LICStatusChangedEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []port.LICStatusChangedEvent
	for _, e := range s.events {
		if e.Status == st {
			out = append(out, e)
		}
	}
	return out
}

// --- port: DLQPublisherPort ------------------------------------------------

type fakeDLQ struct {
	mu        sync.Mutex
	topics    []port.DLQTopic
	envelopes []port.LICDLQEnvelope
}

func (d *fakeDLQ) PublishDLQ(_ context.Context, topic port.DLQTopic, env port.LICDLQEnvelope) error {
	d.mu.Lock()
	d.topics = append(d.topics, topic)
	d.envelopes = append(d.envelopes, env)
	d.mu.Unlock()
	return nil
}
func (d *fakeDLQ) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.topics)
}
func (d *fakeDLQ) last() (port.DLQTopic, port.LICDLQEnvelope, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.topics) == 0 {
		return "", port.LICDLQEnvelope{}, false
	}
	return d.topics[len(d.topics)-1], d.envelopes[len(d.envelopes)-1], true
}

// --- seam: PipelineResumer --------------------------------------------------

type fakeResumer struct {
	rec *callRecorder
	err error

	mu    sync.Mutex
	calls int
	gotSt *model.PipelineState
}

func (r *fakeResumer) ResumeAfterConfirmation(_ context.Context, st *model.PipelineState) error {
	r.rec.add("Resume")
	r.mu.Lock()
	r.calls++
	r.gotSt = st
	r.mu.Unlock()
	return r.err
}
func (r *fakeResumer) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}
func (r *fakeResumer) state() *model.PipelineState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.gotSt
}

// --- seam: Metrics ----------------------------------------------------------

type fakeMetrics struct {
	mu          sync.Mutex
	incs        int
	decs        int
	ages        int
	userOutcome []string
}

func (m *fakeMetrics) PendingStateInc() {
	m.mu.Lock()
	m.incs++
	m.mu.Unlock()
}
func (m *fakeMetrics) PendingStateDec() {
	m.mu.Lock()
	m.decs++
	m.mu.Unlock()
}
func (m *fakeMetrics) PendingStateAgeMaxSeconds(float64) {
	m.mu.Lock()
	m.ages++
	m.mu.Unlock()
}
func (m *fakeMetrics) UserConfirmation(outcome string) {
	m.mu.Lock()
	m.userOutcome = append(m.userOutcome, outcome)
	m.mu.Unlock()
}
func (m *fakeMetrics) snap() (inc, dec, age int, outcomes []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.incs, m.decs, m.ages, append([]string(nil), m.userOutcome...)
}

// --- seam: Clock ------------------------------------------------------------

type fakeClock struct{ t time.Time }

func (c fakeClock) Now() time.Time { return c.t }

// --- seam: Logger -----------------------------------------------------------

type auditLine struct {
	msg string
	kv  []any
}

type fakeLogger struct {
	mu     sync.Mutex
	infos  []auditLine
	warns  int
	errors int
}

func (l *fakeLogger) Info(_ context.Context, msg string, kv ...any) {
	l.mu.Lock()
	l.infos = append(l.infos, auditLine{msg: msg, kv: kv})
	l.mu.Unlock()
}
func (l *fakeLogger) Warn(context.Context, string, ...any) {
	l.mu.Lock()
	l.warns++
	l.mu.Unlock()
}
func (l *fakeLogger) Error(context.Context, string, ...any) {
	l.mu.Lock()
	l.errors++
	l.mu.Unlock()
}
func (l *fakeLogger) audits() []auditLine {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]auditLine(nil), l.infos...)
}

// auditOutcome extracts the validation_outcome kv value from an audit line.
func auditOutcome(t *testing.T, a auditLine) string {
	t.Helper()
	for i := 0; i+1 < len(a.kv); i += 2 {
		if a.kv[i] == "validation_outcome" {
			s, _ := a.kv[i+1].(string)
			return s
		}
	}
	t.Fatalf("audit line %q has no validation_outcome kv", a.msg)
	return ""
}

// --- seam: TraceRestorer ----------------------------------------------------

type fakeTrace struct {
	mu    sync.Mutex
	calls int
	gotTC model.TraceContext
}

func (f *fakeTrace) Restore(ctx context.Context, tc model.TraceContext) context.Context {
	f.mu.Lock()
	f.calls++
	f.gotTC = tc
	f.mu.Unlock()
	return ctx
}

// ============================================================================
// harness
// ============================================================================

// testPaused is a LOCAL sentinel injected as Config.PausedSentinel; Pin 1
// asserts Pause returns exactly it (identity-equal — build-spec D5).
var testPaused = errors.New("test: paused")

type harness struct {
	t       *testing.T
	cfg     Config
	rec     *callRecorder
	pending *fakePending
	idem    *fakeIdem
	uncert  *fakeUncert
	status  *fakeStatus
	dlq     *fakeDLQ
	resumer *fakeResumer
	metrics *fakeMetrics
	clock   fakeClock
	log     *fakeLogger
	trace   *fakeTrace
}

func newHarness(t *testing.T) *harness {
	rec := &callRecorder{}
	return &harness{
		t: t,
		cfg: Config{
			PendingStateTTL:            25 * time.Hour,
			UserConfirmedProcessingTTL: 90 * time.Second,
			CompletedTTL:               24 * time.Hour,
			ConfidenceThreshold:        0.7,
			PausedSentinel:             testPaused,
		},
		rec:     rec,
		pending: &fakePending{rec: rec},
		idem:    &fakeIdem{rec: rec},
		uncert:  &fakeUncert{rec: rec},
		status:  &fakeStatus{rec: rec},
		dlq:     &fakeDLQ{},
		resumer: &fakeResumer{rec: rec},
		metrics: &fakeMetrics{},
		clock:   fakeClock{t: time.Unix(1_700_000_000, 0).UTC()},
		log:     &fakeLogger{},
		trace:   &fakeTrace{},
	}
}

func (h *harness) mgr() *Manager {
	m, err := NewManager(
		h.cfg, h.pending, h.idem, h.uncert, h.status, h.dlq, h.resumer,
		Deps{Metrics: h.metrics, Clock: h.clock, Logger: h.log, TraceRestorer: h.trace},
	)
	if err != nil {
		h.t.Fatalf("NewManager: %v", err)
	}
	return m
}

func liveState() *model.PipelineState {
	st := model.NewPipelineState("corr-1", "job", "doc", "ver", "org")
	st.CreatedByUserID = "user-1"
	st.OriginType = "UPLOAD"
	st.Classification = &model.ClassificationResult{
		ContractType: model.ContractTypeServices,
		Confidence:   0.42,
		Alternatives: []model.ClassificationAlternative{
			{ContractType: model.ContractTypeSupply, Confidence: 0.30},
		},
	}
	return st
}

func pendingBlob() *model.PendingTypeConfirmation {
	return &model.PendingTypeConfirmation{
		JobID:          "job",
		DocumentID:     "doc",
		VersionID:      "ver",
		OrganizationID: "org",
		CorrelationID:  "corr-1",
		TraceContext:   model.TraceContext{TraceParent: "00-trace-span-01"},
		ClassificationResult: &model.ClassificationResult{
			ContractType: model.ContractTypeServices,
			Confidence:   0.42,
		},
	}
}

func userCmd(ct string) port.UserConfirmedType {
	return port.UserConfirmedType{
		CorrelationID:  "corr-1",
		JobID:          "job",
		DocumentID:     "doc",
		VersionID:      "ver",
		OrganizationID: "org",
		ContractType:   ct,
		UserID:         "user-1",
	}
}

// ============================================================================
// D.A pins
// ============================================================================

// Pin 1 — Pause happy path: strict order + sentinel identity + Inc-after-Save.
func TestPause_HappyPath_StrictOrder(t *testing.T) {
	h := newHarness(t)
	m := h.mgr()
	st := liveState()

	err := m.Pause(context.Background(), st)
	if err != testPaused {
		t.Fatalf("Pause must return the injected sentinel (identity-equal), got %v", err)
	}
	want := []string{"Save", "Uncertain", "Status:IN_PROGRESS", "SetPaused"}
	got := h.rec.order()
	if len(got) != len(want) {
		t.Fatalf("call order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("call order[%d] = %q, want %q (full %v)", i, got[i], want[i], got)
		}
	}
	inc, _, _, _ := h.metrics.snap()
	if inc != 1 {
		t.Fatalf("PendingStateInc must be called exactly once after Save, got %d", inc)
	}
	if keys := h.idem.pausedKeys(); len(keys) != 1 || keys[0] != "lic-trigger:ver" {
		t.Fatalf("SetPaused must use lic-trigger:ver, got %v", keys)
	}
}

// Pin 2 — Pause ClassificationUncertain payload.
func TestPause_UncertainPayload(t *testing.T) {
	h := newHarness(t)
	m := h.mgr()
	st := liveState()

	if err := m.Pause(context.Background(), st); err != testPaused {
		t.Fatalf("Pause: %v", err)
	}
	evt, ok := h.uncert.last()
	if !ok {
		t.Fatal("classification-uncertain not published")
	}
	if evt.SuggestedType != st.Classification.ContractType {
		t.Fatalf("SuggestedType = %q, want %q", evt.SuggestedType, st.Classification.ContractType)
	}
	if evt.Confidence != st.Classification.Confidence {
		t.Fatalf("Confidence = %v, want %v", evt.Confidence, st.Classification.Confidence)
	}
	if evt.Threshold != h.cfg.ConfidenceThreshold {
		t.Fatalf("Threshold = %v, want %v", evt.Threshold, h.cfg.ConfidenceThreshold)
	}
	if len(evt.Alternatives) != 1 || evt.Alternatives[0].ContractType != model.ContractTypeSupply {
		t.Fatalf("Alternatives not carried verbatim, got %+v", evt.Alternatives)
	}
}

// Pin 3 — Pause step failures.
func TestPause_SaveFails_NonRetryable_NoPublishes(t *testing.T) {
	h := newHarness(t)
	h.pending.saveErr = errors.New("redis down")
	m := h.mgr()

	err := m.Pause(context.Background(), liveState())
	de, ok := model.AsDomainError(err)
	if !ok {
		t.Fatalf("want *model.DomainError, got %T", err)
	}
	if de.Code != model.ErrCodeInternal || de.Retryable || de.Stage != model.StageAwaitingUserConfirmation {
		t.Fatalf("Save-fail: want INTERNAL_ERROR/non-retryable/STAGE_AWAITING_USER_CONFIRMATION, got code=%s retry=%v stage=%s",
			de.Code, de.Retryable, de.Stage)
	}
	if err == testPaused {
		t.Fatal("must NOT return the sentinel on Save failure")
	}
	if _, published := h.uncert.last(); published {
		t.Fatal("no classification-uncertain may be published when Save fails")
	}
	inc, _, _, _ := h.metrics.snap()
	if inc != 0 {
		t.Fatalf("no PendingStateInc when Save fails, got %d", inc)
	}
}

func TestPause_UncertainFails_Retryable_SaveCalled_IncCalled(t *testing.T) {
	h := newHarness(t)
	h.uncert.err = errors.New("broker NACK")
	m := h.mgr()

	err := m.Pause(context.Background(), liveState())
	de, ok := model.AsDomainError(err)
	if !ok {
		t.Fatalf("want *model.DomainError, got %T", err)
	}
	if de.Code != model.ErrCodeInternal || !de.Retryable {
		t.Fatalf("uncertain-fail: want INTERNAL_ERROR/retryable, got code=%s retry=%v", de.Code, de.Retryable)
	}
	if order := h.rec.order(); len(order) < 1 || order[0] != "Save" {
		t.Fatalf("Save must have been called before the failed publish, order=%v", order)
	}
	inc, _, _, _ := h.metrics.snap()
	if inc != 1 {
		t.Fatalf("PendingStateInc must be called (Save succeeded), got %d", inc)
	}
}

func TestPause_SetPausedFails_Retryable(t *testing.T) {
	h := newHarness(t)
	h.idem.setPausedErr = errors.New("redis down")
	m := h.mgr()

	err := m.Pause(context.Background(), liveState())
	de, ok := model.AsDomainError(err)
	if !ok {
		t.Fatalf("want *model.DomainError, got %T", err)
	}
	if de.Code != model.ErrCodeInternal || !de.Retryable {
		t.Fatalf("SetPaused-fail: want INTERNAL_ERROR/retryable, got code=%s retry=%v", de.Code, de.Retryable)
	}
}

// Pin 4 — Resume happy → COMPLETED.
func TestHandleUserConfirmedType_Happy_Completed(t *testing.T) {
	h := newHarness(t)
	h.pending.loadRet = pendingBlob()
	m := h.mgr()

	err := m.HandleUserConfirmedType(context.Background(), userCmd("SUPPLY"))
	if err != nil {
		t.Fatalf("happy resume must return nil, got %v", err)
	}
	if h.resumer.callCount() != 1 {
		t.Fatalf("ResumeAfterConfirmation must be called once, got %d", h.resumer.callCount())
	}
	got := h.resumer.state()
	if got == nil {
		t.Fatal("resumer received nil state")
	}
	if got.Classification == nil || got.Classification.ContractType != model.ContractType("SUPPLY") {
		t.Fatalf("classification override: want ContractType=SUPPLY, got %+v", got.Classification)
	}
	if got.Classification.Confidence != 1.0 {
		t.Fatalf("classification override: want Confidence=1.0, got %v", got.Classification.Confidence)
	}
	if h.pending.deleteCount() != 1 {
		t.Fatalf("pending.Delete must be called once on COMPLETED, got %d", h.pending.deleteCount())
	}
	ck := h.idem.completedKeys()
	if len(ck) != 2 || ck[0] != "lic-trigger:ver" || ck[1] != "lic-user-confirmed:ver" {
		t.Fatalf("SetCompleted must hit lic-trigger:ver then lic-user-confirmed:ver, got %v", ck)
	}
	_, dec, _, outcomes := h.metrics.snap()
	if dec != 1 {
		t.Fatalf("PendingStateDec must be called once on COMPLETED, got %d", dec)
	}
	if len(outcomes) != 1 || outcomes[0] != "resumed" {
		t.Fatalf("UserConfirmation must be 'resumed' once, got %v", outcomes)
	}
}

// Pin 5 — Resume RE_CHECK restore (state restoration; D8 wiring is pinned in
// the pipeline tests).
func TestHandleUserConfirmedType_ReCheckRestore(t *testing.T) {
	h := newHarness(t)
	blob := pendingBlob()
	parent := "parent-ver"
	blob.ParentVersionID = &parent
	h.pending.loadRet = blob
	m := h.mgr()

	if err := m.HandleUserConfirmedType(context.Background(), userCmd("SUPPLY")); err != nil {
		t.Fatalf("resume: %v", err)
	}
	got := h.resumer.state()
	if got.Mode != model.PipelineModeReCheck {
		t.Fatalf("ParentVersionID set ⇒ restored Mode must be RE_CHECK, got %q", got.Mode)
	}
	if got.ParentVersionID == nil || *got.ParentVersionID != "parent-ver" {
		t.Fatalf("ParentVersionID must be restored, got %v", got.ParentVersionID)
	}
}

// Pin 6 — duplicate delivery.
func TestHandleUserConfirmedType_Duplicate_Processing_Retryable(t *testing.T) {
	h := newHarness(t)
	h.idem.setnxErr = port.ErrIdempotencyKeyExists
	h.idem.setnxStatus = port.IdempotencyProcessing
	m := h.mgr()

	err := m.HandleUserConfirmedType(context.Background(), userCmd("SUPPLY"))
	de, ok := model.AsDomainError(err)
	if !ok || !de.Retryable {
		t.Fatalf("PROCESSING duplicate ⇒ retryable *DomainError, got %v", err)
	}
	if h.resumer.callCount() != 0 {
		t.Fatal("no resumer call on a PROCESSING duplicate")
	}
	for _, c := range h.rec.order() {
		if c == "Load" {
			t.Fatal("no Load on a PROCESSING duplicate")
		}
	}
	_, _, _, outcomes := h.metrics.snap()
	if len(outcomes) != 0 {
		t.Fatalf("no UserConfirmation increment on a duplicate, got %v", outcomes)
	}
}

func TestHandleUserConfirmedType_Duplicate_Completed_Nil(t *testing.T) {
	h := newHarness(t)
	h.idem.setnxErr = port.ErrIdempotencyKeyExists
	h.idem.setnxStatus = port.IdempotencyCompleted
	m := h.mgr()

	if err := m.HandleUserConfirmedType(context.Background(), userCmd("SUPPLY")); err != nil {
		t.Fatalf("COMPLETED duplicate ⇒ nil, got %v", err)
	}
	if h.resumer.callCount() != 0 {
		t.Fatal("no resumer call on a COMPLETED duplicate")
	}
	_, _, _, outcomes := h.metrics.snap()
	if len(outcomes) != 0 {
		t.Fatalf("no UserConfirmation increment on a COMPLETED duplicate, got %v", outcomes)
	}
}

// Pin 7 — USER_CONFIRMATION_EXPIRED.
func TestHandleUserConfirmedType_Expired(t *testing.T) {
	h := newHarness(t)
	h.pending.loadErr = port.ErrPendingStateNotFound
	m := h.mgr()

	if err := m.HandleUserConfirmedType(context.Background(), userCmd("SUPPLY")); err != nil {
		t.Fatalf("expired ⇒ nil (ACK), got %v", err)
	}
	failed := h.status.byStatus(model.StatusFailed)
	if len(failed) != 1 {
		t.Fatalf("exactly one FAILED expected, got %d", len(failed))
	}
	f := failed[0]
	if f.ErrorCode != model.ErrCodeUserConfirmationExpired {
		t.Fatalf("FAILED code = %s, want USER_CONFIRMATION_EXPIRED", f.ErrorCode)
	}
	if f.IsRetryable == nil || *f.IsRetryable {
		t.Fatal("USER_CONFIRMATION_EXPIRED must be is_retryable=false")
	}
	spec, _ := model.LookupErrorSpec(model.ErrCodeUserConfirmationExpired)
	if f.ErrorMessage != spec.UserMessage || f.ErrorMessage == "" {
		t.Fatalf("FAILED message must be the RU catalog string, got %q", f.ErrorMessage)
	}
	if ck := h.idem.completedKeys(); len(ck) != 1 || ck[0] != "lic-user-confirmed:ver" {
		t.Fatalf("SetCompleted must hit lic-user-confirmed:ver only, got %v", ck)
	}
	if h.dlq.count() != 0 {
		t.Fatal("expired pause must NOT DLQ")
	}
	if h.resumer.callCount() != 0 {
		t.Fatal("no resumer call on expired")
	}
	_, _, _, outcomes := h.metrics.snap()
	if len(outcomes) != 1 || outcomes[0] != "expired" {
		t.Fatalf("UserConfirmation must be 'expired' once, got %v", outcomes)
	}
}

// Pin 8 — INVALID_CONTRACT_TYPE (both regex-reject and whitelist-reject).
func TestHandleUserConfirmedType_InvalidContractType_FormatReject(t *testing.T) {
	h := newHarness(t)
	m := h.mgr()

	if err := m.HandleUserConfirmedType(context.Background(), userCmd("other")); err != nil {
		t.Fatalf("invalid ⇒ nil (ACK), got %v", err)
	}
	if h.dlq.count() != 1 {
		t.Fatalf("DLQ once, got %d", h.dlq.count())
	}
	topic, env, _ := h.dlq.last()
	if topic != port.DLQTopicInvalidMessage || env.ErrorCode != model.ErrCodeInvalidContractType {
		t.Fatalf("DLQ must be invalid-message/INVALID_CONTRACT_TYPE, got %s/%s", topic, env.ErrorCode)
	}
	failed := h.status.byStatus(model.StatusFailed)
	if len(failed) != 1 || failed[0].ErrorCode != model.ErrCodeInvalidContractType {
		t.Fatalf("one FAILED INVALID_CONTRACT_TYPE expected, got %+v", failed)
	}
	if failed[0].IsRetryable == nil || *failed[0].IsRetryable {
		t.Fatal("INVALID_CONTRACT_TYPE must be non-retryable")
	}
	if ck := h.idem.completedKeys(); len(ck) != 1 || ck[0] != "lic-user-confirmed:ver" {
		t.Fatalf("SetCompleted(lic-user-confirmed:ver) expected, got %v", ck)
	}
	_, _, _, outcomes := h.metrics.snap()
	if len(outcomes) != 1 || outcomes[0] != "invalid" {
		t.Fatalf("UserConfirmation 'invalid' once expected, got %v", outcomes)
	}
	a := h.log.audits()
	if len(a) != 1 || auditOutcome(t, a[0]) != auditRejectedFormat {
		t.Fatalf("audit must be rejected_format, got %+v", a)
	}
}

func TestHandleUserConfirmedType_InvalidContractType_WhitelistReject(t *testing.T) {
	h := newHarness(t)
	m := h.mgr()

	if err := m.HandleUserConfirmedType(context.Background(), userCmd("ZZZ_NOT_WHITELISTED")); err != nil {
		t.Fatalf("invalid ⇒ nil (ACK), got %v", err)
	}
	a := h.log.audits()
	if len(a) != 1 || auditOutcome(t, a[0]) != auditRejectedWhitelist {
		t.Fatalf("audit must be rejected_whitelist (regex passes, whitelist fails), got %+v", a)
	}
	if h.dlq.count() != 1 {
		t.Fatalf("DLQ once, got %d", h.dlq.count())
	}
	if h.resumer.callCount() != 0 {
		t.Fatal("no resumer call on invalid contract type")
	}
}

// Pin 9 — tenant mismatch (§11.2 R5).
func TestHandleUserConfirmedType_TenantMismatch(t *testing.T) {
	h := newHarness(t)
	blob := pendingBlob()
	blob.OrganizationID = "org-OTHER"
	h.pending.loadRet = blob
	m := h.mgr()

	if err := m.HandleUserConfirmedType(context.Background(), userCmd("SUPPLY")); err != nil {
		t.Fatalf("tenant mismatch ⇒ nil (ACK poison), got %v", err)
	}
	if h.dlq.count() != 1 {
		t.Fatalf("DLQ once, got %d", h.dlq.count())
	}
	_, env, _ := h.dlq.last()
	if env.ErrorCode != model.ErrCodeInvalidOrgIDMismatch {
		t.Fatalf("DLQ code = %s, want INVALID_ORG_ID_MISMATCH", env.ErrorCode)
	}
	if got := len(h.status.byStatus(model.StatusFailed)); got != 0 {
		t.Fatalf("non-publishable code ⇒ NO FAILED published, got %d", got)
	}
	if h.pending.deleteCount() != 0 {
		t.Fatal("pending-state must NOT be consumed on tenant mismatch (§11.2:494)")
	}
	if h.resumer.callCount() != 0 {
		t.Fatal("no resumer call on tenant mismatch")
	}
	_, _, _, outcomes := h.metrics.snap()
	if len(outcomes) != 1 || outcomes[0] != "invalid" {
		t.Fatalf("UserConfirmation 'invalid' once expected, got %v", outcomes)
	}
	a := h.log.audits()
	if len(a) != 1 || auditOutcome(t, a[0]) != auditRejectedTenant {
		t.Fatalf("audit must be rejected_tenant_mismatch, got %+v", a)
	}
}

// Pin 10 — resume pipeline failure.
func TestHandleUserConfirmedType_ResumePipelineFailure(t *testing.T) {
	h := newHarness(t)
	h.pending.loadRet = pendingBlob()
	want := model.NewDomainError(model.ErrCodeLLMAllProvidersFailed, model.StageAgentRiskDetection).WithRetryable(true)
	h.resumer.err = want
	m := h.mgr()

	err := m.HandleUserConfirmedType(context.Background(), userCmd("SUPPLY"))
	de, ok := model.AsDomainError(err)
	if !ok || de != want {
		t.Fatalf("Manager must return the resumer error verbatim, got %v", err)
	}
	if h.pending.deleteCount() != 0 {
		t.Fatal("must NOT Delete pending-state on a pipeline failure (may resume again)")
	}
	if len(h.idem.completedKeys()) != 0 {
		t.Fatalf("must NOT SetCompleted lic-trigger on a pipeline failure, got %v", h.idem.completedKeys())
	}
	if got := len(h.status.byStatus(model.StatusFailed)); got != 0 {
		t.Fatalf("Manager must NOT re-publish FAILED (ResumeAfterConfirmation already did), got %d", got)
	}
	_, _, _, outcomes := h.metrics.snap()
	if len(outcomes) != 0 {
		t.Fatalf("no UserConfirmation increment on a pipeline failure, got %v", outcomes)
	}
}

// Pin 11 — RepublishPauseEvents.
func TestRepublishPauseEvents(t *testing.T) {
	h := newHarness(t)
	m := h.mgr()

	if err := m.RepublishPauseEvents(context.Background(), pendingBlob()); err != nil {
		t.Fatalf("RepublishPauseEvents: %v", err)
	}
	order := h.rec.order()
	want := []string{"Uncertain", "Status:IN_PROGRESS"}
	if len(order) != len(want) {
		t.Fatalf("republish must publish only uncertain + IN_PROGRESS, got %v", order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("republish order[%d] = %q, want %q", i, order[i], want[i])
		}
	}
	if len(h.idem.pausedKeys()) != 0 {
		t.Fatal("republish must NOT SetPaused")
	}

	// publish failure ⇒ retryable *DomainError.
	h2 := newHarness(t)
	h2.uncert.err = errors.New("broker NACK")
	m2 := h2.mgr()
	err := m2.RepublishPauseEvents(context.Background(), pendingBlob())
	de, ok := model.AsDomainError(err)
	if !ok || !de.Retryable {
		t.Fatalf("republish publish-failure ⇒ retryable *DomainError, got %v", err)
	}
}

// Pin 12 — NewManager fail-fast surfaces ALL defects at once.
func TestNewManager_FailFast(t *testing.T) {
	_, err := NewManager(
		Config{ConfidenceThreshold: 2.0}, // invalid: TTLs ≤0, threshold>1, nil sentinel
		nil, nil, nil, nil, nil, nil, Deps{},
	)
	if err == nil {
		t.Fatal("expected fail-fast error")
	}
	msg := err.Error()
	for _, want := range []string{
		"PendingStateTTL", "UserConfirmedProcessingTTL", "CompletedTTL",
		"ConfidenceThreshold", "PausedSentinel",
		"pending", "idem", "uncert", "status", "dlq", "resumer",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("joined error must mention %q; got: %s", want, msg)
		}
	}
}

// Pin 13 — audit trail: every decision emits exactly one audit line with the
// correct validation_outcome (the accepted path is covered here; the rejected
// paths are covered in Pins 8/9).
func TestAuditTrail_AcceptedOnce(t *testing.T) {
	h := newHarness(t)
	h.pending.loadRet = pendingBlob()
	m := h.mgr()

	if err := m.HandleUserConfirmedType(context.Background(), userCmd("SUPPLY")); err != nil {
		t.Fatalf("resume: %v", err)
	}
	a := h.log.audits()
	if len(a) != 1 {
		t.Fatalf("exactly one audit line per decision, got %d", len(a))
	}
	if auditOutcome(t, a[0]) != auditAccepted {
		t.Fatalf("accepted resume audit = %q, want accepted", auditOutcome(t, a[0]))
	}
}

// Pin 14 — compile-assert *Manager satisfies port.UserConfirmedTypeHandler
// (allowed import; the pipeline.PauseController assertion is NOT here — D18).
var _ port.UserConfirmedTypeHandler = (*Manager)(nil)

// localResumerShape is a structural mirror of PipelineResumer; *fakeResumer
// satisfies it, proving the seam shape (D18 — no pipeline import here).
type localResumerShape interface {
	ResumeAfterConfirmation(ctx context.Context, state *model.PipelineState) error
}

var _ localResumerShape = (*fakeResumer)(nil)

// Pin 15 — -race clean ×16 over distinct version_ids (the
// TestRun_ConcurrentRaceClean precedent). Distinct instances per goroutine
// (the Manager is immutable; the fakes are per-instance) exercising Pause +
// HandleUserConfirmedType concurrently.
func TestConcurrentRaceClean(t *testing.T) {
	const n = 16
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			h := newHarness(t)
			h.pending.loadRet = pendingBlob()
			m := h.mgr()
			vid := "ver-" + strconv.Itoa(idx)
			st := liveState()
			st.VersionID = vid
			if err := m.Pause(context.Background(), st); err != testPaused {
				t.Errorf("concurrent Pause[%d]: %v", idx, err)
			}
			cmd := userCmd("SUPPLY")
			cmd.VersionID = vid
			if err := m.HandleUserConfirmedType(context.Background(), cmd); err != nil {
				t.Errorf("concurrent Resume[%d]: %v", idx, err)
			}
		}(i)
	}
	wg.Wait()
}
