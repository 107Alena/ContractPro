package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"contractpro/legal-intelligence-core/internal/application/pipeline"
	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// =============================================================================
// In-package fakes (every seam + every port the Router consumes).
// =============================================================================

// fakeIdempotencyGuard is a faithful in-package fake of IdempotencyGuard.
// Tests program responses per-key and observe every call. -race-clean.
type fakeIdempotencyGuard struct {
	mu sync.Mutex

	// programmed responses
	checkResults    map[string]checkResult // key → response
	setCompletedErr map[string]error       // key → err
	heartbeatStop   func()                 // returned by StartHeartbeat (default a no-op stop)
	pendingLoadErr  error                  // not used here (lives in fakePendingStateLoader)
	loadResult      *model.PendingTypeConfirmation

	// observed calls
	checkCalls     []checkCall
	setCompleted   []completedCall
	heartbeatCalls []heartbeatCall
	stopCalls      int32 // atomic — # of times stop func was invoked
}

type checkResult struct {
	status        port.IdempotencyStatus
	alreadyExists bool
	err           error
}

type checkCall struct {
	key string
	ttl time.Duration
}

type completedCall struct {
	key string
	ttl time.Duration
}

type heartbeatCall struct {
	key string
	ttl time.Duration
}

func newFakeIdempotencyGuard() *fakeIdempotencyGuard {
	return &fakeIdempotencyGuard{
		checkResults:    make(map[string]checkResult),
		setCompletedErr: make(map[string]error),
	}
}

func (f *fakeIdempotencyGuard) programCheck(key string, status port.IdempotencyStatus, exists bool, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checkResults[key] = checkResult{status: status, alreadyExists: exists, err: err}
}

func (f *fakeIdempotencyGuard) SetNX(_ context.Context, _ string, _ time.Duration) (port.IdempotencyStatus, error) {
	return port.IdempotencyAbsent, nil
}
func (f *fakeIdempotencyGuard) Get(_ context.Context, _ string) (port.IdempotencyStatus, error) {
	return port.IdempotencyAbsent, nil
}
func (f *fakeIdempotencyGuard) ExtendTTL(_ context.Context, _ string, _ time.Duration) error {
	return nil
}

func (f *fakeIdempotencyGuard) SetCompleted(_ context.Context, key string, ttl time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setCompleted = append(f.setCompleted, completedCall{key: key, ttl: ttl})
	if err, ok := f.setCompletedErr[key]; ok {
		return err
	}
	return nil
}

func (f *fakeIdempotencyGuard) SetPaused(_ context.Context, _ string, _ time.Duration) error {
	return nil
}

func (f *fakeIdempotencyGuard) CheckAndAcquire(_ context.Context, key string, ttl time.Duration) (port.IdempotencyStatus, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checkCalls = append(f.checkCalls, checkCall{key: key, ttl: ttl})
	if r, ok := f.checkResults[key]; ok {
		return r.status, r.alreadyExists, r.err
	}
	// default: acquired
	return port.IdempotencyAbsent, false, nil
}

func (f *fakeIdempotencyGuard) StartHeartbeat(_ context.Context, key string, ttl time.Duration) (stop func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.heartbeatCalls = append(f.heartbeatCalls, heartbeatCall{key: key, ttl: ttl})
	// Return a stop func that records its invocation count. Concurrent-safe.
	return func() { atomic.AddInt32(&f.stopCalls, 1) }
}

func (f *fakeIdempotencyGuard) numStopCalls() int32 {
	return atomic.LoadInt32(&f.stopCalls)
}

func (f *fakeIdempotencyGuard) numCheckCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.checkCalls)
}

func (f *fakeIdempotencyGuard) numHeartbeats() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.heartbeatCalls)
}

func (f *fakeIdempotencyGuard) numSetCompleted() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.setCompleted)
}

func (f *fakeIdempotencyGuard) setCompletedFor(key string) []completedCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []completedCall
	for _, c := range f.setCompleted {
		if c.key == key {
			out = append(out, c)
		}
	}
	return out
}

// fakePipelineRunner is the in-package fake of PipelineRunner.
type fakePipelineRunner struct {
	mu        sync.Mutex
	runErr    error
	panic     bool
	calls     []port.VersionProcessingArtifactsReady
	runCalled chan struct{}
}

func newFakePipelineRunner(runErr error) *fakePipelineRunner {
	return &fakePipelineRunner{runErr: runErr, runCalled: make(chan struct{}, 1)}
}

func (f *fakePipelineRunner) Run(_ context.Context, trigger port.VersionProcessingArtifactsReady) error {
	f.mu.Lock()
	f.calls = append(f.calls, trigger)
	doPanic := f.panic
	err := f.runErr
	f.mu.Unlock()
	select {
	case f.runCalled <- struct{}{}:
	default:
	}
	if doPanic {
		panic("simulated pipeline panic")
	}
	return err
}

func (f *fakePipelineRunner) numCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// fakePendingMgr is the in-package fake of PendingConfirmationManager.
type fakePendingMgr struct {
	mu           sync.Mutex
	handleErr    error
	republishErr error
	handleCalls  []port.UserConfirmedType
	republishPTC []*model.PendingTypeConfirmation
}

func (f *fakePendingMgr) HandleUserConfirmedType(_ context.Context, cmd port.UserConfirmedType) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handleCalls = append(f.handleCalls, cmd)
	return f.handleErr
}

func (f *fakePendingMgr) RepublishPauseEvents(_ context.Context, ptc *model.PendingTypeConfirmation) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.republishPTC = append(f.republishPTC, ptc)
	return f.republishErr
}

func (f *fakePendingMgr) numHandleCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.handleCalls)
}

func (f *fakePendingMgr) numRepublish() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.republishPTC)
}

// fakeArtifactDeliverer is the in-package fake of ArtifactsAwaiterDeliverer.
type fakeArtifactDeliverer struct {
	mu         sync.Mutex
	deliverErr map[string]error
	calls      []struct {
		correlationID string
		evt           port.ArtifactsProvided
	}
}

func newFakeArtifactDeliverer() *fakeArtifactDeliverer {
	return &fakeArtifactDeliverer{deliverErr: make(map[string]error)}
}

func (f *fakeArtifactDeliverer) Deliver(corrID string, evt port.ArtifactsProvided) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, struct {
		correlationID string
		evt           port.ArtifactsProvided
	}{corrID, evt})
	if err, ok := f.deliverErr[corrID]; ok {
		return err
	}
	return nil
}

func (f *fakeArtifactDeliverer) numCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// fakePersistDeliverer is the in-package fake of PersistConfirmationDeliverer.
type fakePersistDeliverer struct {
	mu         sync.Mutex
	deliverErr map[string]error
	calls      []struct {
		jobID string
		conf  port.PersistConfirmation
	}
}

func newFakePersistDeliverer() *fakePersistDeliverer {
	return &fakePersistDeliverer{deliverErr: make(map[string]error)}
}

func (f *fakePersistDeliverer) Deliver(jobID string, conf port.PersistConfirmation) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, struct {
		jobID string
		conf  port.PersistConfirmation
	}{jobID, conf})
	if err, ok := f.deliverErr[jobID]; ok {
		return err
	}
	return nil
}

func (f *fakePersistDeliverer) numCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// fakeVersionMetaWriter is the in-package fake of VersionMetaCacheWriter.
type fakeVersionMetaWriter struct {
	mu     sync.Mutex
	setErr error
	calls  []struct {
		versionID string
		payload   []byte
		ttl       time.Duration
	}
}

func (f *fakeVersionMetaWriter) Set(_ context.Context, versionID string, payload []byte, ttl time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, struct {
		versionID string
		payload   []byte
		ttl       time.Duration
	}{versionID, append([]byte(nil), payload...), ttl})
	return f.setErr
}

func (f *fakeVersionMetaWriter) numCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// fakePendingStateLoader is the in-package fake of port.PendingStatePort.
// Only Load is exercised; Save/Delete return nil.
type fakePendingStateLoader struct {
	mu         sync.Mutex
	loadResult *model.PendingTypeConfirmation
	loadErr    error
	loadCalls  []string
}

func (f *fakePendingStateLoader) Save(_ context.Context, _ string, _ *model.PendingTypeConfirmation, _ time.Duration) error {
	return nil
}

func (f *fakePendingStateLoader) Load(_ context.Context, versionID string) (*model.PendingTypeConfirmation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loadCalls = append(f.loadCalls, versionID)
	return f.loadResult, f.loadErr
}

func (f *fakePendingStateLoader) Delete(_ context.Context, _ string) error { return nil }

// fakeStatusPub is the in-package fake of port.StatusPublisherPort.
type fakeStatusPub struct {
	mu         sync.Mutex
	publishErr error
	calls      []port.LICStatusChangedEvent
}

func (f *fakeStatusPub) PublishStatus(_ context.Context, evt port.LICStatusChangedEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, evt)
	return f.publishErr
}

func (f *fakeStatusPub) numCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// fakeLogger is the in-package fake of Logger.
type fakeLogger struct {
	mu        sync.Mutex
	infoMsgs  []string
	warnMsgs  []string
	errorMsgs []string
	infoKV    [][]any
	warnKV    [][]any
	errorKV   [][]any
}

func (f *fakeLogger) Info(_ context.Context, msg string, kv ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.infoMsgs = append(f.infoMsgs, msg)
	f.infoKV = append(f.infoKV, append([]any(nil), kv...))
}

func (f *fakeLogger) Warn(_ context.Context, msg string, kv ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.warnMsgs = append(f.warnMsgs, msg)
	f.warnKV = append(f.warnKV, append([]any(nil), kv...))
}

func (f *fakeLogger) Error(_ context.Context, msg string, kv ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errorMsgs = append(f.errorMsgs, msg)
	f.errorKV = append(f.errorKV, append([]any(nil), kv...))
}

func (f *fakeLogger) numWarn() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.warnMsgs)
}

func (f *fakeLogger) lastWarnKV() []any {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.warnKV) == 0 {
		return nil
	}
	return f.warnKV[len(f.warnKV)-1]
}

// =============================================================================
// Test fixture helpers.
// =============================================================================

func validConfig() Config {
	return Config{
		ProcessingTTL:     150 * time.Second,
		CompletedTTL:      24 * time.Hour,
		PendingStateTTL:   24 * time.Hour,
		MetaCacheTTL:      24 * time.Hour,
		HeartbeatInterval: 30 * time.Second,
	}
}

type harness struct {
	r             *Router
	guard         *fakeIdempotencyGuard
	pipeRunner    *fakePipelineRunner
	pendingMgr    *fakePendingMgr
	artDeliverer  *fakeArtifactDeliverer
	persistDel    *fakePersistDeliverer
	metaWriter    *fakeVersionMetaWriter
	pendingLoader *fakePendingStateLoader
	statusPub     *fakeStatusPub
	log           *fakeLogger
}

func newHarness(t *testing.T, pipeRunErr error) *harness {
	t.Helper()
	g := newFakeIdempotencyGuard()
	pr := newFakePipelineRunner(pipeRunErr)
	pm := &fakePendingMgr{}
	ad := newFakeArtifactDeliverer()
	pd := newFakePersistDeliverer()
	mw := &fakeVersionMetaWriter{}
	pl := &fakePendingStateLoader{}
	sp := &fakeStatusPub{}
	lg := &fakeLogger{}

	r, err := NewRouter(validConfig(), pr, pm, ad, pd, mw, g, pl, sp, Deps{Logger: lg})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	return &harness{
		r: r, guard: g, pipeRunner: pr, pendingMgr: pm,
		artDeliverer: ad, persistDel: pd, metaWriter: mw,
		pendingLoader: pl, statusPub: sp, log: lg,
	}
}

func sampleVAR() port.VersionProcessingArtifactsReady {
	pv := "v-parent"
	return port.VersionProcessingArtifactsReady{
		CorrelationID:   "corr-1",
		Timestamp:       "2026-05-20T12:00:00Z",
		DocumentID:      "doc-1",
		VersionID:       "v-1",
		OrganizationID:  "org-1",
		ArtifactTypes:   []string{"TEXT"},
		JobID:           "job-1",
		OriginType:      "INITIAL",
		ParentVersionID: &pv,
		CreatedByUserID: "user-1",
	}
}

func sampleVC() port.VersionCreated {
	pv := "v-parent"
	return port.VersionCreated{
		CorrelationID:   "corr-2",
		Timestamp:       "2026-05-20T12:00:00Z",
		DocumentID:      "doc-1",
		VersionID:       "v-2",
		VersionNumber:   2,
		OrganizationID:  "org-1",
		OriginType:      "RE_CHECK",
		ParentVersionID: &pv,
		JobID:           "job-2",
		CreatedByUserID: "user-1",
	}
}

func sampleAP() port.ArtifactsProvided {
	return port.ArtifactsProvided{
		CorrelationID: "corr-art-1",
		Timestamp:     "2026-05-20T12:00:00Z",
		JobID:         "job-1",
		DocumentID:    "doc-1",
		VersionID:     "v-1",
	}
}

func samplePersisted() port.LegalAnalysisArtifactsPersisted {
	return port.LegalAnalysisArtifactsPersisted{
		CorrelationID: "corr-p-1",
		Timestamp:     "2026-05-20T12:00:00Z",
		JobID:         "job-1",
		DocumentID:    "doc-1",
	}
}

func samplePersistFailed(retryable bool) port.LegalAnalysisArtifactsPersistFailed {
	return port.LegalAnalysisArtifactsPersistFailed{
		CorrelationID: "corr-pf-1",
		Timestamp:     "2026-05-20T12:00:00Z",
		JobID:         "job-1",
		DocumentID:    "doc-1",
		ErrorCode:     "DM_FAILURE",
		ErrorMessage:  "DM rejected",
		IsRetryable:   retryable,
	}
}

func sampleUCT() port.UserConfirmedType {
	return port.UserConfirmedType{
		CorrelationID:  "corr-uct-1",
		Timestamp:      "2026-05-20T12:00:00Z",
		JobID:          "job-1",
		DocumentID:     "doc-1",
		VersionID:      "v-1",
		OrganizationID: "org-1",
		ContractType:   "SERVICE_AGREEMENT",
		UserID:         "user-1",
	}
}

// =============================================================================
// PIN 1: TestHermeticImports — lives in internal_test.go.
// PIN 2: TestGofmtClean      — lives in internal_test.go.
// =============================================================================

// =============================================================================
// PIN 3: TestNewRouter_FailFast
// =============================================================================

func TestNewRouter_FailFast(t *testing.T) {
	g := newFakeIdempotencyGuard()
	pr := newFakePipelineRunner(nil)
	pm := &fakePendingMgr{}
	ad := newFakeArtifactDeliverer()
	pd := newFakePersistDeliverer()
	mw := &fakeVersionMetaWriter{}
	pl := &fakePendingStateLoader{}
	sp := &fakeStatusPub{}

	// table — each entry sets exactly ONE collaborator to nil OR provides an
	// invalid Config, asserts NewRouter returns (nil, err) with the
	// per-arg substring.
	tests := []struct {
		name        string
		cfg         Config
		runner      PipelineRunner
		pending     PendingConfirmationManager
		art         ArtifactsAwaiterDeliverer
		persist     PersistConfirmationDeliverer
		meta        VersionMetaCacheWriter
		guard       IdempotencyGuard
		pendingPort port.PendingStatePort
		stat        port.StatusPublisherPort
		wantSubstr  string
	}{
		{"nil pipelineRunner", validConfig(), nil, pm, ad, pd, mw, g, pl, sp, "pipelineRunner"},
		{"nil pendingMgr", validConfig(), pr, nil, ad, pd, mw, g, pl, sp, "pendingMgr"},
		{"nil artifactDeliverer", validConfig(), pr, pm, nil, pd, mw, g, pl, sp, "artifactDeliverer"},
		{"nil persistDeliverer", validConfig(), pr, pm, ad, nil, mw, g, pl, sp, "persistDeliverer"},
		{"nil versionMetaWriter", validConfig(), pr, pm, ad, pd, nil, g, pl, sp, "versionMetaWriter"},
		{"nil idempGuard", validConfig(), pr, pm, ad, pd, mw, nil, pl, sp, "idempGuard"},
		{"nil pendingStateLoader", validConfig(), pr, pm, ad, pd, mw, g, nil, sp, "pendingStateLoader"},
		{"nil statusPub", validConfig(), pr, pm, ad, pd, mw, g, pl, nil, "statusPub"},
		{"zero ProcessingTTL", Config{CompletedTTL: time.Hour, PendingStateTTL: time.Hour, MetaCacheTTL: time.Hour, HeartbeatInterval: time.Second}, pr, pm, ad, pd, mw, g, pl, sp, "ProcessingTTL"},
		{"zero CompletedTTL", Config{ProcessingTTL: time.Second, PendingStateTTL: time.Hour, MetaCacheTTL: time.Hour, HeartbeatInterval: time.Second}, pr, pm, ad, pd, mw, g, pl, sp, "CompletedTTL"},
		{"zero PendingStateTTL", Config{ProcessingTTL: time.Second, CompletedTTL: time.Hour, MetaCacheTTL: time.Hour, HeartbeatInterval: time.Second}, pr, pm, ad, pd, mw, g, pl, sp, "PendingStateTTL"},
		{"zero MetaCacheTTL", Config{ProcessingTTL: time.Second, CompletedTTL: time.Hour, PendingStateTTL: time.Hour, HeartbeatInterval: time.Second}, pr, pm, ad, pd, mw, g, pl, sp, "MetaCacheTTL"},
		{"zero HeartbeatInterval", Config{ProcessingTTL: time.Second, CompletedTTL: time.Hour, PendingStateTTL: time.Hour, MetaCacheTTL: time.Hour}, pr, pm, ad, pd, mw, g, pl, sp, "HeartbeatInterval"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			r, err := NewRouter(tc.cfg, tc.runner, tc.pending, tc.art, tc.persist, tc.meta, tc.guard, tc.pendingPort, tc.stat, Deps{})
			if r != nil {
				t.Fatalf("expected nil Router, got %v", r)
			}
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantSubstr)
			}
		})
	}

	// Multiple defects: errors.Join surfaces ALL of them.
	t.Run("multiple nils", func(t *testing.T) {
		_, err := NewRouter(Config{}, nil, nil, nil, nil, nil, nil, nil, nil, Deps{})
		if err == nil {
			t.Fatal("expected joined error, got nil")
		}
		for _, want := range []string{"pipelineRunner", "pendingMgr", "ProcessingTTL"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("joined error missing %q: %v", want, err)
			}
		}
	})
}

// =============================================================================
// PIN 4: TestKeyHelpers_DeterministicAndCollisionFree
// =============================================================================

func TestKeyHelpers_DeterministicAndCollisionFree(t *testing.T) {
	const id = "v-123"
	cases := []struct {
		got, want string
	}{
		{keyTrigger(id), "lic-trigger:v-123"},
		{keyVersionCreated(id), "lic-version-created:v-123"},
		{keyArtifactsResp(id), "lic-artifacts-resp:v-123"},
		{keyPersistResp(id), "lic-persist-resp:v-123"},
		{keyPersistFail(id), "lic-persist-fail:v-123"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}

	// determinism: same input ⇒ same output, even invoked multiple times.
	for i := 0; i < 10; i++ {
		if keyTrigger(id) != "lic-trigger:v-123" {
			t.Fatal("keyTrigger non-deterministic")
		}
	}

	// collision-free property test: any two helpers given non-equal inputs
	// produce non-equal outputs (small property — 50 iterations, fixed seed
	// for determinism).
	rng := rand.New(rand.NewSource(42))
	helpers := []func(string) string{
		keyTrigger, keyVersionCreated, keyArtifactsResp, keyPersistResp, keyPersistFail,
	}
	for i := 0; i < 50; i++ {
		a := fmt.Sprintf("id-%d", rng.Intn(1000))
		b := fmt.Sprintf("id-%d", rng.Intn(1000)+5000)
		for hi, ha := range helpers {
			for hj, hb := range helpers {
				if hi == hj && a == b {
					continue
				}
				if a == b {
					if ha(a) == hb(b) && hi != hj {
						t.Errorf("collision: helper[%d](%q)==helper[%d](%q)", hi, a, hj, b)
					}
					continue
				}
				// Cross-prefix collision protection: keyA != keyB whenever
				// either prefix or id differs.
				if ha(a) == hb(b) {
					t.Errorf("collision: helper[%d](%q)=%q == helper[%d](%q)=%q", hi, a, ha(a), hj, b, hb(b))
				}
			}
		}
	}
}

// =============================================================================
// RouteVersionArtifactsReady pins (5..15)
// =============================================================================

// PIN 5
func TestRouteVAR_HappyPath_AcquireRunSetCompleted(t *testing.T) {
	h := newHarness(t, nil) // Run returns nil
	evt := sampleVAR()

	err := h.r.RouteVersionArtifactsReady(context.Background(), evt)
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if got := h.guard.numHeartbeats(); got != 1 {
		t.Errorf("StartHeartbeat calls: got %d want 1", got)
	}
	if got := h.guard.numStopCalls(); got != 1 {
		t.Errorf("stopHB invocations: got %d want 1", got)
	}
	if got := h.pipeRunner.numCalls(); got != 1 {
		t.Errorf("PipelineRunner.Run calls: got %d want 1", got)
	}
	if !reflect.DeepEqual(h.pipeRunner.calls[0], evt) {
		t.Errorf("trigger mismatch: got %+v want %+v", h.pipeRunner.calls[0], evt)
	}
	completed := h.guard.setCompletedFor(keyTrigger(evt.VersionID))
	if len(completed) != 1 {
		t.Fatalf("SetCompleted(lic-trigger) calls: got %d want 1", len(completed))
	}
	if completed[0].ttl != h.r.cfg.CompletedTTL {
		t.Errorf("SetCompleted ttl: got %v want %v", completed[0].ttl, h.r.cfg.CompletedTTL)
	}
}

// PIN 6
func TestRouteVAR_HeartbeatStartsAndStops(t *testing.T) {
	h := newHarness(t, nil)
	evt := sampleVAR()
	if err := h.r.RouteVersionArtifactsReady(context.Background(), evt); err != nil {
		t.Fatal(err)
	}
	if h.guard.numHeartbeats() != 1 {
		t.Errorf("expected exactly 1 heartbeat call")
	}
	if h.guard.numStopCalls() != 1 {
		t.Errorf("expected exactly 1 stop invocation")
	}
}

// PIN 7
func TestRouteVAR_RunReturnsPausedSentinel_AckNoSetCompleted(t *testing.T) {
	h := newHarness(t, pipeline.ErrPipelinePaused)
	evt := sampleVAR()
	err := h.r.RouteVersionArtifactsReady(context.Background(), evt)
	if err != nil {
		t.Fatalf("expected nil ACK on paused, got %v", err)
	}
	// SetCompleted(lic-trigger) MUST NOT be called.
	if completed := h.guard.setCompletedFor(keyTrigger(evt.VersionID)); len(completed) != 0 {
		t.Errorf("SetCompleted on paused: got %d want 0", len(completed))
	}
	// statusPub MUST NOT be called.
	if h.statusPub.numCalls() != 0 {
		t.Errorf("statusPub on paused: got %d want 0", h.statusPub.numCalls())
	}
	// Heartbeat still stops.
	if h.guard.numStopCalls() != 1 {
		t.Errorf("stop calls on paused: got %d want 1", h.guard.numStopCalls())
	}
}

// PIN 8
func TestRouteVAR_RunReturnsRetryableDomainError_AckSetCompleted(t *testing.T) {
	de := model.NewDomainError(model.ErrCodeInternal, model.StageReceived).WithRetryable(true)
	h := newHarness(t, de)
	evt := sampleVAR()
	err := h.r.RouteVersionArtifactsReady(context.Background(), evt)
	if err != de {
		t.Errorf("expected verbatim DomainError, got %v", err)
	}
	if !model.IsRetryable(err) {
		t.Errorf("expected retryable")
	}
	if completed := h.guard.setCompletedFor(keyTrigger(evt.VersionID)); len(completed) != 1 {
		t.Errorf("SetCompleted on retryable fail: got %d want 1", len(completed))
	}
	if h.statusPub.numCalls() != 0 {
		t.Errorf("statusPub on pipeline FAILED (already published by 036): got %d want 0", h.statusPub.numCalls())
	}
}

// PIN 9
func TestRouteVAR_RunReturnsNonRetryableDomainError_AckSetCompleted(t *testing.T) {
	de := model.NewDomainError(model.ErrCodeDocumentTooLarge, model.StageArtifactsReceived).WithRetryable(false)
	h := newHarness(t, de)
	evt := sampleVAR()
	err := h.r.RouteVersionArtifactsReady(context.Background(), evt)
	if err != de {
		t.Errorf("expected verbatim DomainError, got %v", err)
	}
	if completed := h.guard.setCompletedFor(keyTrigger(evt.VersionID)); len(completed) != 1 {
		t.Errorf("SetCompleted on non-retryable fail: got %d want 1", len(completed))
	}
	if h.statusPub.numCalls() != 0 {
		t.Errorf("statusPub on pipeline FAILED (already published by 036): got %d want 0", h.statusPub.numCalls())
	}
}

// PIN 10
func TestRouteVAR_GuardReturnsProcessing_NackForRetry(t *testing.T) {
	h := newHarness(t, nil)
	evt := sampleVAR()
	h.guard.programCheck(keyTrigger(evt.VersionID), port.IdempotencyProcessing, true, nil)

	err := h.r.RouteVersionArtifactsReady(context.Background(), evt)
	if err == nil {
		t.Fatal("expected non-nil DomainError")
	}
	var de *model.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("expected *model.DomainError, got %T", err)
	}
	if de.Code != model.ErrCodeIdempotencyStoreUnavail {
		t.Errorf("Code: got %v want %v", de.Code, model.ErrCodeIdempotencyStoreUnavail)
	}
	if !model.IsRetryable(err) {
		t.Errorf("expected retryable")
	}
	if h.pipeRunner.numCalls() != 0 {
		t.Errorf("PipelineRunner.Run on PROCESSING: got %d want 0", h.pipeRunner.numCalls())
	}
	if h.guard.numHeartbeats() != 0 {
		t.Errorf("Heartbeat on PROCESSING: got %d want 0", h.guard.numHeartbeats())
	}
	if h.guard.numSetCompleted() != 0 {
		t.Errorf("SetCompleted on PROCESSING: got %d want 0", h.guard.numSetCompleted())
	}
}

// PIN 11
func TestRouteVAR_GuardReturnsPaused_PendingHit_RepublishAck(t *testing.T) {
	h := newHarness(t, nil)
	evt := sampleVAR()
	h.guard.programCheck(keyTrigger(evt.VersionID), port.IdempotencyPaused, true, nil)
	h.pendingLoader.loadResult = &model.PendingTypeConfirmation{
		JobID: evt.JobID, VersionID: evt.VersionID, OrganizationID: evt.OrganizationID,
	}

	err := h.r.RouteVersionArtifactsReady(context.Background(), evt)
	if err != nil {
		t.Fatalf("expected nil ACK on PAUSED+hit, got %v", err)
	}
	if h.pendingMgr.numRepublish() != 1 {
		t.Errorf("RepublishPauseEvents: got %d want 1", h.pendingMgr.numRepublish())
	}
	if h.statusPub.numCalls() != 0 {
		t.Errorf("statusPub on PAUSED+hit: got %d want 0", h.statusPub.numCalls())
	}
	if h.guard.numSetCompleted() != 0 {
		t.Errorf("SetCompleted on PAUSED+hit: got %d want 0", h.guard.numSetCompleted())
	}
	if h.pipeRunner.numCalls() != 0 {
		t.Errorf("PipelineRunner on PAUSED+hit: got %d want 0", h.pipeRunner.numCalls())
	}
}

// PIN 12
func TestRouteVAR_GuardReturnsPaused_PendingMiss_PublishFailedSetCompletedAck(t *testing.T) {
	h := newHarness(t, nil)
	evt := sampleVAR()
	h.guard.programCheck(keyTrigger(evt.VersionID), port.IdempotencyPaused, true, nil)
	h.pendingLoader.loadErr = port.ErrPendingStateNotFound

	err := h.r.RouteVersionArtifactsReady(context.Background(), evt)
	if err != nil {
		t.Fatalf("expected nil ACK on PAUSED+miss, got %v", err)
	}
	if h.statusPub.numCalls() != 1 {
		t.Fatalf("statusPub on PAUSED+miss: got %d want 1", h.statusPub.numCalls())
	}
	pub := h.statusPub.calls[0]
	if pub.Status != model.StatusFailed {
		t.Errorf("Status: got %v want FAILED", pub.Status)
	}
	if pub.Stage != model.StageAwaitingUserConfirmation {
		t.Errorf("Stage: got %v want STAGE_AWAITING_USER_CONFIRMATION", pub.Stage)
	}
	if pub.ErrorCode != model.ErrCodeUserConfirmationExpired {
		t.Errorf("ErrorCode: got %v want USER_CONFIRMATION_EXPIRED", pub.ErrorCode)
	}
	if pub.IsRetryable == nil || *pub.IsRetryable != false {
		t.Errorf("IsRetryable: got %v want *false", pub.IsRetryable)
	}
	completed := h.guard.setCompletedFor(keyTrigger(evt.VersionID))
	if len(completed) != 1 {
		t.Fatalf("SetCompleted on PAUSED+miss: got %d want 1", len(completed))
	}
	if completed[0].ttl != h.r.cfg.PendingStateTTL {
		t.Errorf("SetCompleted ttl: got %v want PendingStateTTL", completed[0].ttl)
	}
	if h.pendingMgr.numRepublish() != 0 {
		t.Errorf("Republish on PAUSED+miss: got %d want 0", h.pendingMgr.numRepublish())
	}
	if h.pipeRunner.numCalls() != 0 {
		t.Errorf("PipelineRunner on PAUSED+miss: got %d want 0", h.pipeRunner.numCalls())
	}
}

// PIN 13
func TestRouteVAR_GuardReturnsPaused_PendingLoadError_Nack(t *testing.T) {
	h := newHarness(t, nil)
	evt := sampleVAR()
	h.guard.programCheck(keyTrigger(evt.VersionID), port.IdempotencyPaused, true, nil)
	h.pendingLoader.loadErr = errors.New("redis transient: dial timeout")

	err := h.r.RouteVersionArtifactsReady(context.Background(), evt)
	if err == nil {
		t.Fatal("expected non-nil DomainError")
	}
	var de *model.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("expected *model.DomainError, got %T", err)
	}
	if de.Code != model.ErrCodeIdempotencyStoreUnavail {
		t.Errorf("Code: got %v want IDEMPOTENCY_STORE_UNAVAILABLE", de.Code)
	}
	if !model.IsRetryable(err) {
		t.Errorf("expected retryable")
	}
	if h.statusPub.numCalls() != 0 {
		t.Errorf("statusPub on Load transient: got %d want 0", h.statusPub.numCalls())
	}
	if h.guard.numSetCompleted() != 0 {
		t.Errorf("SetCompleted on Load transient: got %d want 0", h.guard.numSetCompleted())
	}
}

// PIN 14
func TestRouteVAR_GuardReturnsCompleted_Ack(t *testing.T) {
	h := newHarness(t, nil)
	evt := sampleVAR()
	h.guard.programCheck(keyTrigger(evt.VersionID), port.IdempotencyCompleted, true, nil)

	err := h.r.RouteVersionArtifactsReady(context.Background(), evt)
	if err != nil {
		t.Fatalf("expected nil ACK on COMPLETED, got %v", err)
	}
	if h.pipeRunner.numCalls() != 0 || h.pendingMgr.numRepublish() != 0 ||
		h.statusPub.numCalls() != 0 || h.guard.numSetCompleted() != 0 ||
		h.guard.numHeartbeats() != 0 {
		t.Errorf("COMPLETED branch must not invoke any other collaborator")
	}
}

// PIN 15
func TestRouteVAR_GuardTransportError_FallbackDisabled_Nack(t *testing.T) {
	h := newHarness(t, nil)
	evt := sampleVAR()
	h.guard.programCheck(keyTrigger(evt.VersionID), port.IdempotencyAbsent, false, errors.New("redis: connection refused"))

	err := h.r.RouteVersionArtifactsReady(context.Background(), evt)
	if err == nil {
		t.Fatal("expected non-nil DomainError")
	}
	var de *model.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("expected *model.DomainError, got %T", err)
	}
	if de.Code != model.ErrCodeIdempotencyStoreUnavail {
		t.Errorf("Code: got %v want IDEMPOTENCY_STORE_UNAVAILABLE", de.Code)
	}
	if !model.IsRetryable(err) {
		t.Errorf("expected retryable")
	}
	if h.guard.numHeartbeats() != 0 {
		t.Errorf("Heartbeat on Guard transport error: got %d want 0", h.guard.numHeartbeats())
	}
	if h.pipeRunner.numCalls() != 0 {
		t.Errorf("PipelineRunner on Guard transport error: got %d want 0", h.pipeRunner.numCalls())
	}
}

// =============================================================================
// RouteVersionCreated pins (16..19)
// =============================================================================

// PIN 16
func TestRouteVC_HappyPath_WriteMetaSetCompleted(t *testing.T) {
	h := newHarness(t, nil)
	evt := sampleVC()
	err := h.r.RouteVersionCreated(context.Background(), evt)
	if err != nil {
		t.Fatalf("expected nil ACK, got %v", err)
	}
	if h.metaWriter.numCalls() != 1 {
		t.Fatalf("VersionMetaCacheWriter.Set: got %d want 1", h.metaWriter.numCalls())
	}
	c := h.metaWriter.calls[0]
	if c.versionID != evt.VersionID {
		t.Errorf("versionID: got %q want %q", c.versionID, evt.VersionID)
	}
	if c.ttl != h.r.cfg.MetaCacheTTL {
		t.Errorf("ttl: got %v want MetaCacheTTL", c.ttl)
	}
	var dec struct {
		ParentVersionID *string `json:"parent_version_id,omitempty"`
		OriginType      string  `json:"origin_type,omitempty"`
	}
	if jerr := json.Unmarshal(c.payload, &dec); jerr != nil {
		t.Fatalf("payload not valid JSON: %v (payload=%s)", jerr, c.payload)
	}
	if dec.OriginType != evt.OriginType {
		t.Errorf("origin_type: got %q want %q", dec.OriginType, evt.OriginType)
	}
	if dec.ParentVersionID == nil || *dec.ParentVersionID != *evt.ParentVersionID {
		t.Errorf("parent_version_id mismatch")
	}
	completed := h.guard.setCompletedFor(keyVersionCreated(evt.VersionID))
	if len(completed) != 1 {
		t.Fatalf("SetCompleted: got %d want 1", len(completed))
	}
	if completed[0].ttl != h.r.cfg.CompletedTTL {
		t.Errorf("SetCompleted ttl: got %v want CompletedTTL", completed[0].ttl)
	}
}

// PIN 17
func TestRouteVC_DuplicateCompleted_Ack(t *testing.T) {
	h := newHarness(t, nil)
	evt := sampleVC()
	h.guard.programCheck(keyVersionCreated(evt.VersionID), port.IdempotencyCompleted, true, nil)

	err := h.r.RouteVersionCreated(context.Background(), evt)
	if err != nil {
		t.Fatalf("expected nil ACK, got %v", err)
	}
	if h.metaWriter.numCalls() != 0 {
		t.Errorf("VersionMetaCacheWriter.Set on duplicate: got %d want 0", h.metaWriter.numCalls())
	}
	if h.guard.numSetCompleted() != 0 {
		t.Errorf("SetCompleted on duplicate: got %d want 0", h.guard.numSetCompleted())
	}
}

// PIN 18
func TestRouteVC_MetaWriteFails_AckWithWarn(t *testing.T) {
	h := newHarness(t, nil)
	evt := sampleVC()
	h.metaWriter.setErr = errors.New("redis: timeout")

	err := h.r.RouteVersionCreated(context.Background(), evt)
	if err != nil {
		t.Fatalf("expected nil ACK on meta-write fail, got %v", err)
	}
	if h.guard.numSetCompleted() != 0 {
		t.Errorf("SetCompleted on meta-write fail: got %d want 0 (leave PROCESSING for redelivery retry)", h.guard.numSetCompleted())
	}
	if h.log.numWarn() != 1 {
		t.Errorf("Logger.Warn calls: got %d want 1", h.log.numWarn())
	}
	// kv contains "cause" with the redis error.
	found := false
	for _, kv := range h.log.lastWarnKV() {
		if e, ok := kv.(error); ok && strings.Contains(e.Error(), "redis: timeout") {
			found = true
			break
		}
		if s, ok := kv.(string); ok && strings.Contains(s, "redis: timeout") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Logger.Warn kv missing 'redis: timeout' cause; kv=%v", h.log.lastWarnKV())
	}
}

// PIN 19
func TestRouteVC_GuardTransportError_AckWithWarn(t *testing.T) {
	h := newHarness(t, nil)
	evt := sampleVC()
	h.guard.programCheck(keyVersionCreated(evt.VersionID), port.IdempotencyAbsent, false, errors.New("redis: down"))

	err := h.r.RouteVersionCreated(context.Background(), evt)
	if err != nil {
		t.Fatalf("expected nil ACK on Guard transport error (degrade per 036 DEFECT-1), got %v", err)
	}
	if h.metaWriter.numCalls() != 0 {
		t.Errorf("VersionMetaCacheWriter.Set on transport error: got %d want 0", h.metaWriter.numCalls())
	}
	if h.log.numWarn() != 1 {
		t.Errorf("Logger.Warn calls: got %d want 1", h.log.numWarn())
	}
}

// =============================================================================
// RouteArtifactsProvided pins (20..22)
// =============================================================================

// PIN 20
func TestRouteAP_DeliversAndSetCompleted(t *testing.T) {
	h := newHarness(t, nil)
	evt := sampleAP()

	err := h.r.RouteArtifactsProvided(context.Background(), evt)
	if err != nil {
		t.Fatalf("expected nil ACK, got %v", err)
	}
	if h.artDeliverer.numCalls() != 1 {
		t.Fatalf("Deliver: got %d want 1", h.artDeliverer.numCalls())
	}
	if h.artDeliverer.calls[0].correlationID != evt.CorrelationID {
		t.Errorf("correlationID: got %q want %q", h.artDeliverer.calls[0].correlationID, evt.CorrelationID)
	}
	if !reflect.DeepEqual(h.artDeliverer.calls[0].evt, evt) {
		t.Errorf("evt mismatch")
	}
	completed := h.guard.setCompletedFor(keyArtifactsResp(evt.CorrelationID))
	if len(completed) != 1 {
		t.Errorf("SetCompleted: got %d want 1", len(completed))
	}
}

// PIN 21
func TestRouteAP_AwaiterRegistryMiss_AckSilently(t *testing.T) {
	h := newHarness(t, nil)
	evt := sampleAP()
	h.artDeliverer.deliverErr[evt.CorrelationID] = errors.New("awaiter: no slot")

	err := h.r.RouteArtifactsProvided(context.Background(), evt)
	if err != nil {
		t.Fatalf("expected nil ACK on registry-miss, got %v", err)
	}
	if h.log.numWarn() != 1 {
		t.Errorf("Logger.Warn calls: got %d want 1", h.log.numWarn())
	}
	if h.guard.numSetCompleted() != 0 {
		t.Errorf("SetCompleted on registry-miss: got %d want 0", h.guard.numSetCompleted())
	}
}

// PIN 22
func TestRouteAP_DuplicateCompleted_Ack(t *testing.T) {
	h := newHarness(t, nil)
	evt := sampleAP()
	h.guard.programCheck(keyArtifactsResp(evt.CorrelationID), port.IdempotencyCompleted, true, nil)

	err := h.r.RouteArtifactsProvided(context.Background(), evt)
	if err != nil {
		t.Fatalf("expected nil ACK on duplicate, got %v", err)
	}
	if h.artDeliverer.numCalls() != 0 {
		t.Errorf("Deliver on duplicate: got %d want 0", h.artDeliverer.numCalls())
	}
}

// =============================================================================
// RoutePersisted / RoutePersistFailed pins (23..24)
// =============================================================================

// PIN 23
func TestRoutePersisted_DeliversSuccessConfirmation(t *testing.T) {
	h := newHarness(t, nil)
	evt := samplePersisted()

	err := h.r.RoutePersisted(context.Background(), evt)
	if err != nil {
		t.Fatalf("expected nil ACK, got %v", err)
	}
	if h.persistDel.numCalls() != 1 {
		t.Fatalf("Deliver: got %d want 1", h.persistDel.numCalls())
	}
	c := h.persistDel.calls[0]
	if c.jobID != evt.JobID {
		t.Errorf("jobID: got %q want %q", c.jobID, evt.JobID)
	}
	if !c.conf.IsSuccess() {
		t.Errorf("expected IsSuccess()==true")
	}
	if c.conf.Success == nil || c.conf.Success.JobID != evt.JobID {
		t.Errorf("Success envelope mismatch")
	}
	completed := h.guard.setCompletedFor(keyPersistResp(evt.JobID))
	if len(completed) != 1 {
		t.Errorf("SetCompleted: got %d want 1", len(completed))
	}
}

// PIN 24
func TestRoutePersistFailed_DeliversFailureConfirmation(t *testing.T) {
	h := newHarness(t, nil)
	evt := samplePersistFailed(true)

	err := h.r.RoutePersistFailed(context.Background(), evt)
	if err != nil {
		t.Fatalf("expected nil ACK, got %v", err)
	}
	if h.persistDel.numCalls() != 1 {
		t.Fatalf("Deliver: got %d want 1", h.persistDel.numCalls())
	}
	c := h.persistDel.calls[0]
	if !c.conf.IsFailure() {
		t.Errorf("expected IsFailure()==true")
	}
	if c.conf.Failure == nil || c.conf.Failure.IsRetryable != true {
		t.Errorf("Failure envelope IsRetryable: got %v want true", c.conf.Failure)
	}
	completed := h.guard.setCompletedFor(keyPersistFail(evt.JobID))
	if len(completed) != 1 {
		t.Errorf("SetCompleted: got %d want 1", len(completed))
	}

	// Non-retryable variant: still routes the same way; Router does not
	// inspect IsRetryable (orchestrator's awaitPersist does).
	h2 := newHarness(t, nil)
	evt2 := samplePersistFailed(false)
	if err := h2.r.RoutePersistFailed(context.Background(), evt2); err != nil {
		t.Fatalf("expected nil ACK on non-retryable, got %v", err)
	}
	if !h2.persistDel.calls[0].conf.IsFailure() {
		t.Errorf("non-retryable: expected IsFailure()==true")
	}
}

// =============================================================================
// RouteUserConfirmedType pins (25..27)
// =============================================================================

// PIN 25
func TestRouteUCT_ManagerNil_Ack(t *testing.T) {
	h := newHarness(t, nil)
	evt := sampleUCT()
	// Default fakePendingMgr.handleErr is nil ⇒ Manager returns nil.

	err := h.r.RouteUserConfirmedType(context.Background(), evt)
	if err != nil {
		t.Fatalf("expected nil ACK, got %v", err)
	}
	if h.pendingMgr.numHandleCalls() != 1 {
		t.Errorf("HandleUserConfirmedType: got %d want 1", h.pendingMgr.numHandleCalls())
	}
	if h.guard.numCheckCalls() != 0 {
		t.Errorf("Guard touched on UCT: got %d want 0 (Manager owns its own SETNX)", h.guard.numCheckCalls())
	}
	if h.statusPub.numCalls() != 0 {
		t.Errorf("statusPub touched on UCT: got %d want 0 (Manager owns its own FAILED publish)", h.statusPub.numCalls())
	}
}

// PIN 26
func TestRouteUCT_ManagerRetryableErr_ReturnsError(t *testing.T) {
	h := newHarness(t, nil)
	evt := sampleUCT()
	mgrErr := model.NewDomainError(model.ErrCodeInternal, model.StageAwaitingUserConfirmation).WithRetryable(true)
	h.pendingMgr.handleErr = mgrErr

	err := h.r.RouteUserConfirmedType(context.Background(), evt)
	if err != mgrErr {
		t.Errorf("expected verbatim mgr err, got %v", err)
	}
	if h.guard.numCheckCalls() != 0 {
		t.Errorf("Guard touched on UCT retryable: got %d want 0", h.guard.numCheckCalls())
	}
	if h.statusPub.numCalls() != 0 {
		t.Errorf("statusPub touched on UCT retryable: got %d want 0", h.statusPub.numCalls())
	}
}

// PIN 27
func TestRouteUCT_ManagerNonRetryableErr_AcksTrustsManagerDLQ(t *testing.T) {
	h := newHarness(t, nil)
	evt := sampleUCT()
	mgrErr := model.NewDomainError(model.ErrCodeInternal, model.StageAwaitingUserConfirmation).
		WithRetryable(false).
		WithDevMessage("resume: pending-state has nil ClassificationResult")
	h.pendingMgr.handleErr = mgrErr

	err := h.r.RouteUserConfirmedType(context.Background(), evt)
	if err != nil {
		t.Fatalf("expected nil ACK on non-retryable (Manager-trusted), got %v", err)
	}
	if h.guard.numCheckCalls() != 0 {
		t.Errorf("Guard touched on UCT non-retryable: got %d want 0", h.guard.numCheckCalls())
	}
	if h.statusPub.numCalls() != 0 {
		t.Errorf("statusPub touched on UCT non-retryable: got %d want 0", h.statusPub.numCalls())
	}
	// Router has no DLQ port — TestRouter_NoDLQPublisherField pin enforces
	// this at the struct level.
}

// =============================================================================
// Cross-cutting pins (28..30)
// =============================================================================

// PIN 28: concurrent Route* invocations across all 6 methods + 200 goroutines.
func TestRouter_ConcurrentRouteRaceClean(t *testing.T) {
	h := newHarness(t, nil)

	const N = 200
	var wg sync.WaitGroup
	wg.Add(N)
	deadline := time.After(10 * time.Second)
	done := make(chan struct{})

	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			ctx := context.Background()
			id := fmt.Sprintf("v-%d", i)
			pv := "v-parent"
			switch i % 6 {
			case 0:
				_ = h.r.RouteVersionArtifactsReady(ctx, port.VersionProcessingArtifactsReady{
					CorrelationID: "c-" + id, VersionID: id, JobID: "j-" + id,
					DocumentID: "d-" + id, OrganizationID: "o", ParentVersionID: &pv,
				})
			case 1:
				_ = h.r.RouteVersionCreated(ctx, port.VersionCreated{
					CorrelationID: "c-" + id, VersionID: id, DocumentID: "d-" + id,
					OrganizationID: "o", ParentVersionID: &pv, OriginType: "INITIAL",
				})
			case 2:
				_ = h.r.RouteArtifactsProvided(ctx, port.ArtifactsProvided{
					CorrelationID: "ca-" + id, JobID: "j-" + id, DocumentID: "d-" + id, VersionID: id,
				})
			case 3:
				_ = h.r.RoutePersisted(ctx, port.LegalAnalysisArtifactsPersisted{
					CorrelationID: "cp-" + id, JobID: "j-" + id, DocumentID: "d-" + id,
				})
			case 4:
				_ = h.r.RoutePersistFailed(ctx, port.LegalAnalysisArtifactsPersistFailed{
					CorrelationID: "cpf-" + id, JobID: "j-" + id, DocumentID: "d-" + id,
					ErrorMessage: "x", IsRetryable: false,
				})
			case 5:
				_ = h.r.RouteUserConfirmedType(ctx, port.UserConfirmedType{
					CorrelationID: "cu-" + id, JobID: "j-" + id, DocumentID: "d-" + id,
					VersionID: id, OrganizationID: "o", ContractType: "SERVICE_AGREEMENT",
				})
			}
		}()
	}

	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
		// ok
	case <-deadline:
		t.Fatal("deadlock — concurrent Route* did not complete within 10s")
	}
}

// PIN 29: TestRouter_NoJobLimiterField — reflection-walk over *Router fields
// + grep-style assertion via go/parser on seams.go that NO interface named
// JobLimiter/Semaphore is declared in this package (R2).
func TestRouter_NoJobLimiterField(t *testing.T) {
	rt := reflect.TypeOf(Router{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		typeName := f.Type.String()
		// Field type name must NOT contain "JobLimiter" or "Semaphore" —
		// 036 owns concurrency (R2). The struct holds 6 seam fields + 2
		// ports + 4 Deps fields + Config — no slot for a JobLimiter.
		if strings.Contains(typeName, "JobLimiter") {
			t.Errorf("Router has field %q of type %q — Router MUST NOT hold a JobLimiter (R2)", f.Name, typeName)
		}
		if strings.Contains(typeName, "Semaphore") {
			t.Errorf("Router has field %q of type %q — Router MUST NOT hold a Semaphore (R2)", f.Name, typeName)
		}
	}

	// Also assert no interface named JobLimiter is declared in any
	// non-test file in this package (build-spec D9 — Router does NOT
	// declare a JobLimiter seam; 036 owns concurrency).
	for _, fname := range []string{"router.go", "routes.go", "seams.go", "deps.go"} {
		src, err := os.ReadFile(fname)
		if err != nil {
			t.Fatalf("read %s: %v", fname, err)
		}
		if strings.Contains(string(src), "type JobLimiter interface") {
			t.Errorf("%s declares interface JobLimiter — Router MUST NOT (R2)", fname)
		}
		if strings.Contains(string(src), "type Semaphore interface") {
			t.Errorf("%s declares interface Semaphore — Router MUST NOT (R2)", fname)
		}
	}
}

// PIN 30: TestRouter_NoDLQPublisherField — reflection-walk over *Router
// fields asserts NO field of type port.DLQPublisherPort (R4).
func TestRouter_NoDLQPublisherField(t *testing.T) {
	rt := reflect.TypeOf(Router{})
	dlqType := reflect.TypeOf((*port.DLQPublisherPort)(nil)).Elem()
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if f.Type == dlqType {
			t.Errorf("Router has field %q of type port.DLQPublisherPort — Router MUST NOT publish DLQ (R4)", f.Name)
		}
		// Also catch interface-typed names that happen to mention DLQPublisher.
		typeName := f.Type.String()
		if strings.Contains(typeName, "DLQPublisher") {
			t.Errorf("Router has field %q of type %q (DLQ-related) — Router MUST NOT publish DLQ (R4)", f.Name, typeName)
		}
	}
}

// =============================================================================
// Stop-heartbeat-on-panic (Reviewer gate o).
// =============================================================================

// TestRouteVAR_PipelinePanic_StopFuncFiredOnce asserts the defer stopHB()
// fires even when the pipeline panics (build-spec gate o). The Router does
// NOT recover (consumer/seams.go is the panic boundary); the test recovers
// inside an inner func so the assertion below runs.
func TestRouteVAR_PipelinePanic_StopFuncFiredOnce(t *testing.T) {
	h := newHarness(t, nil)
	h.pipeRunner.panic = true

	func() {
		defer func() {
			_ = recover()
		}()
		_ = h.r.RouteVersionArtifactsReady(context.Background(), sampleVAR())
	}()

	if h.guard.numStopCalls() != 1 {
		t.Errorf("stop invocations on panic: got %d want 1", h.guard.numStopCalls())
	}
}
