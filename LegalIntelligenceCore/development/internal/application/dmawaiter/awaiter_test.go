package dmawaiter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// ---------------------------------------------------------------------------
// In-package fakes for the Metrics / Clock / Logger seams.
// ---------------------------------------------------------------------------

// recOut records one Metrics.RecordOutcome call (T1/T4/T5/.. assertions).
type recOut struct {
	op      string
	outcome string
	seconds float64
}

// fakeMetrics captures every RecordOutcome call. Goroutine-safe (concurrent
// awaiters in T7/T22/T-CTOR-4/T-RACE-1).
type fakeMetrics struct {
	mu    sync.Mutex
	calls []recOut
}

func (f *fakeMetrics) RecordOutcome(op, outcome string, seconds float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, recOut{op: op, outcome: outcome, seconds: seconds})
}

func (f *fakeMetrics) snapshot() []recOut {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]recOut, len(f.calls))
	copy(out, f.calls)
	return out
}

// fakeClock is a deterministic monotonic clock. Now() advances by the
// configured tick each call so duration measurements are non-zero and
// reproducible. Goroutine-safe.
type fakeClock struct {
	mu   sync.Mutex
	now  time.Time
	tick time.Duration
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Unix(1_700_000_000, 0).UTC(), tick: time.Millisecond}
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.now
	f.now = f.now.Add(f.tick)
	return t
}

func (f *fakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

// logEntry records one Logger.Warn / Error call.
type logEntry struct {
	level string
	msg   string
	kv    []any
}

// fakeLogger captures every Warn / Error call. Goroutine-safe.
type fakeLogger struct {
	mu      sync.Mutex
	entries []logEntry
}

func (f *fakeLogger) Warn(_ context.Context, msg string, kv ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries = append(f.entries, logEntry{level: "warn", msg: msg, kv: append([]any(nil), kv...)})
}

func (f *fakeLogger) Error(_ context.Context, msg string, kv ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries = append(f.entries, logEntry{level: "error", msg: msg, kv: append([]any(nil), kv...)})
}

func (f *fakeLogger) snapshot() []logEntry {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]logEntry, len(f.entries))
	copy(out, f.entries)
	return out
}

func (f *fakeLogger) countAtLevelWithKey(level, key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, e := range f.entries {
		if e.level != level {
			continue
		}
		// kv pairs are key, value, key, value, ... — find a "key"
		// followed by the desired value.
		for i := 0; i+1 < len(e.kv); i += 2 {
			if k, ok := e.kv[i].(string); ok && k == "key" {
				if v, vok := e.kv[i+1].(string); vok && v == key {
					n++
					break
				}
			}
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// Builders.
// ---------------------------------------------------------------------------

// newArtifactAwaiterForTest assembles an *ArtifactAwaiter with the given TTL
// and fresh fake seams. Returns the awaiter + each fake so the test body
// asserts against them.
func newArtifactAwaiterForTest(t *testing.T, ttl time.Duration) (*ArtifactAwaiter, *fakeMetrics, *fakeClock, *fakeLogger) {
	t.Helper()
	m := &fakeMetrics{}
	cl := newFakeClock()
	lg := &fakeLogger{}
	a, err := NewArtifactAwaiter(ArtifactConfig{TTL: ttl}, Deps{Metrics: m, Clock: cl, Logger: lg})
	if err != nil {
		t.Fatalf("NewArtifactAwaiter: %v", err)
	}
	return a, m, cl, lg
}

func newConfirmationAwaiterForTest(t *testing.T, ttl time.Duration) (*ConfirmationAwaiter, *fakeMetrics, *fakeClock, *fakeLogger) {
	t.Helper()
	m := &fakeMetrics{}
	cl := newFakeClock()
	lg := &fakeLogger{}
	c, err := NewConfirmationAwaiter(ConfirmationConfig{TTL: ttl}, Deps{Metrics: m, Clock: cl, Logger: lg})
	if err != nil {
		t.Fatalf("NewConfirmationAwaiter: %v", err)
	}
	return c, m, cl, lg
}

// awaiterRegLen returns len(a.reg) safely under the mutex (test-internal use
// only — same-package access).
func awaiterRegLen(a *ArtifactAwaiter) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.reg)
}

func confirmationRegLen(c *ConfirmationAwaiter) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.reg)
}

// shortTTL returns a small TTL useful for timeout tests (10ms in -short,
// 100ms otherwise).
func shortTTL() time.Duration {
	if testing.Short() {
		return 10 * time.Millisecond
	}
	return 100 * time.Millisecond
}

// ---------------------------------------------------------------------------
// Sample event builders.
// ---------------------------------------------------------------------------

func sampleArtifactsProvided(corr string) port.ArtifactsProvided {
	return port.ArtifactsProvided{
		CorrelationID: corr,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		JobID:         "j-" + corr,
		DocumentID:    "d-" + corr,
		VersionID:     "v-" + corr,
		Artifacts: map[model.ArtifactType]json.RawMessage{
			model.ArtifactSemanticTree: json.RawMessage(`{"root":{}}`),
		},
	}
}

func samplePersisted(jobID string) port.LegalAnalysisArtifactsPersisted {
	return port.LegalAnalysisArtifactsPersisted{
		CorrelationID: "c-" + jobID,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		JobID:         jobID,
		DocumentID:    "d-" + jobID,
	}
}

func samplePersistFailed(jobID string, retryable bool) port.LegalAnalysisArtifactsPersistFailed {
	return port.LegalAnalysisArtifactsPersistFailed{
		CorrelationID: "c-" + jobID,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		JobID:         jobID,
		DocumentID:    "d-" + jobID,
		ErrorCode:     "DM_INTERNAL",
		ErrorMessage:  "DM transient error",
		IsRetryable:   retryable,
	}
}

// ---------------------------------------------------------------------------
// Artifact awaiter — T1..T16.
// ---------------------------------------------------------------------------

// T1 — Register/Deliver/Await happy path (no timeout).
func TestT1_ArtifactAwaiter_Register_Deliver_Await_HappyPath(t *testing.T) {
	a, m, _, _ := newArtifactAwaiterForTest(t, time.Second)
	const corr = "corr-1"
	if _, err := a.Register(corr); err != nil {
		t.Fatalf("Register: %v", err)
	}

	want := sampleArtifactsProvided(corr)
	type result struct {
		val port.ArtifactsProvided
		err error
	}
	done := make(chan result, 1)
	go func() {
		v, e := a.Await(context.Background(), corr)
		done <- result{val: v, err: e}
	}()

	// Deliver via the router-seam surface.
	if err := a.Deliver(corr, want); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("Await err: %v", r.err)
		}
		if r.val.CorrelationID != corr {
			t.Fatalf("Await val: got %+v want CorrelationID=%s", r.val, corr)
		}
	case <-time.After(time.Second):
		t.Fatal("Await timed out (happy path)")
	}

	if got := awaiterRegLen(a); got != 0 {
		t.Errorf("reg leak: len=%d want 0", got)
	}
	calls := m.snapshot()
	if len(calls) != 1 || calls[0].op != opGetArtifacts || calls[0].outcome != outcomeSuccess {
		t.Errorf("metric calls: got %+v", calls)
	}
}

// T2 — Same happy path via the HandlerInterface entry point.
func TestT2_ArtifactAwaiter_Register_HandleArtifactsProvided_Await_HappyPath(t *testing.T) {
	a, m, _, _ := newArtifactAwaiterForTest(t, time.Second)
	const corr = "corr-2"
	if _, err := a.Register(corr); err != nil {
		t.Fatalf("Register: %v", err)
	}

	want := sampleArtifactsProvided(corr)
	done := make(chan port.ArtifactsProvided, 1)
	go func() {
		v, _ := a.Await(context.Background(), corr)
		done <- v
	}()
	if err := a.HandleArtifactsProvided(context.Background(), want); err != nil {
		t.Fatalf("HandleArtifactsProvided: %v", err)
	}
	select {
	case got := <-done:
		if got.CorrelationID != corr {
			t.Fatalf("got %+v want corr=%s", got, corr)
		}
	case <-time.After(time.Second):
		t.Fatal("Await timed out")
	}
	calls := m.snapshot()
	if len(calls) != 1 || calls[0].outcome != outcomeSuccess {
		t.Errorf("metric: got %+v", calls)
	}
}

// T3 — Register twice ⇒ ErrDuplicateRegistration on the second.
func TestT3_ArtifactAwaiter_DuplicateRegistration(t *testing.T) {
	a, _, _, _ := newArtifactAwaiterForTest(t, time.Second)
	const corr = "corr-3"
	ch1, err := a.Register(corr)
	if err != nil {
		t.Fatalf("first Register: %v", err)
	}
	ch2, err := a.Register(corr)
	if !errors.Is(err, port.ErrDuplicateRegistration) {
		t.Fatalf("second Register err: got %v want ErrDuplicateRegistration", err)
	}
	if ch2 != nil {
		t.Errorf("ErrDuplicateRegistration: channel should be nil, got %v", ch2)
	}
	a.Cancel(corr)
	ch3, err := a.Register(corr)
	if err != nil {
		t.Fatalf("third Register after Cancel: %v", err)
	}
	if ch1 == ch3 {
		t.Errorf("third Register returned the same channel as the first; want a fresh one")
	}
	a.Cancel(corr)
}

// T4 — Await TTL elapsed ⇒ port.ErrAwaitTimeout + slot removed.
func TestT4_ArtifactAwaiter_Await_TTL_Timeout(t *testing.T) {
	ttl := shortTTL()
	a, m, _, _ := newArtifactAwaiterForTest(t, ttl)
	const corr = "corr-4"
	if _, err := a.Register(corr); err != nil {
		t.Fatalf("Register: %v", err)
	}

	type result struct {
		val port.ArtifactsProvided
		err error
	}
	done := make(chan result, 1)
	go func() {
		v, e := a.Await(context.Background(), corr)
		done <- result{val: v, err: e}
	}()

	select {
	case r := <-done:
		if !errors.Is(r.err, port.ErrAwaitTimeout) {
			t.Fatalf("Await err: got %v want ErrAwaitTimeout", r.err)
		}
	case <-time.After(ttl + 5*time.Second):
		t.Fatal("Await did not return within TTL budget")
	}

	if got := awaiterRegLen(a); got != 0 {
		t.Errorf("reg leak: len=%d want 0", got)
	}
	calls := m.snapshot()
	if len(calls) != 1 || calls[0].outcome != outcomeTimeout {
		t.Errorf("metric: got %+v want outcome=timeout", calls)
	}
}

// T5 — Await ctx cancel before TTL ⇒ ctx.Err() + slot removed + outcome=timeout.
func TestT5_ArtifactAwaiter_Await_CtxCancel(t *testing.T) {
	a, m, _, _ := newArtifactAwaiterForTest(t, time.Second)
	const corr = "corr-5"
	if _, err := a.Register(corr); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	type result struct{ err error }
	done := make(chan result, 1)
	go func() {
		_, e := a.Await(ctx, corr)
		done <- result{err: e}
	}()
	// Brief yield so Await enters the select.
	time.Sleep(5 * time.Millisecond)
	cancel()

	select {
	case r := <-done:
		if !errors.Is(r.err, context.Canceled) {
			t.Fatalf("Await err: got %v want context.Canceled", r.err)
		}
	case <-time.After(time.Second):
		t.Fatal("Await did not return on ctx cancel")
	}

	if got := awaiterRegLen(a); got != 0 {
		t.Errorf("reg leak: len=%d want 0", got)
	}
	calls := m.snapshot()
	if len(calls) != 1 || calls[0].outcome != outcomeTimeout {
		t.Errorf("metric: got %+v want outcome=timeout", calls)
	}
}

// T6 — Await ctx deadline exceeded.
func TestT6_ArtifactAwaiter_Await_CtxDeadline(t *testing.T) {
	a, m, _, _ := newArtifactAwaiterForTest(t, time.Second)
	const corr = "corr-6"
	if _, err := a.Register(corr); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := a.Await(ctx, corr)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Await err: got %v want context.DeadlineExceeded", err)
	}
	if got := awaiterRegLen(a); got != 0 {
		t.Errorf("reg leak: len=%d", got)
	}
	calls := m.snapshot()
	if len(calls) != 1 || calls[0].outcome != outcomeTimeout {
		t.Errorf("metric: got %+v", calls)
	}
}

// T7 — Concurrent N=100 Register/Deliver/Await on distinct keys, x10 iterations.
func TestT7_ArtifactAwaiter_ConcurrentDistinctKeys(t *testing.T) {
	const N = 100
	iters := 10
	if testing.Short() {
		iters = 3
	}
	for it := 0; it < iters; it++ {
		a, _, _, _ := newArtifactAwaiterForTest(t, 2*time.Second)
		var wg sync.WaitGroup
		wg.Add(N)
		for i := 0; i < N; i++ {
			i := i
			go func() {
				defer wg.Done()
				corr := fmt.Sprintf("corr-%d-%d", it, i)
				if _, err := a.Register(corr); err != nil {
					t.Errorf("Register(%s): %v", corr, err)
					return
				}
				go func() {
					_ = a.Deliver(corr, sampleArtifactsProvided(corr))
				}()
				v, err := a.Await(context.Background(), corr)
				if err != nil {
					t.Errorf("Await(%s): %v", corr, err)
					return
				}
				if v.CorrelationID != corr {
					t.Errorf("Await(%s): got CorrelationID=%s", corr, v.CorrelationID)
				}
			}()
		}
		wg.Wait()
		if got := awaiterRegLen(a); got != 0 {
			t.Errorf("iter %d: reg leak: len=%d", it, got)
		}
	}
}

// T8 — Late Deliver after Cancel ⇒ silent drop + WARN log + nil return.
func TestT8_ArtifactAwaiter_LateDeliverAfterCancel(t *testing.T) {
	a, _, _, lg := newArtifactAwaiterForTest(t, time.Second)
	const corr = "corr-8"
	if _, err := a.Register(corr); err != nil {
		t.Fatalf("Register: %v", err)
	}
	a.Cancel(corr)

	err := a.HandleArtifactsProvided(context.Background(), sampleArtifactsProvided(corr))
	if err != nil {
		t.Fatalf("HandleArtifactsProvided after Cancel: got %v want nil", err)
	}
	if got := lg.countAtLevelWithKey("warn", corr); got != 1 {
		t.Errorf("expected exactly 1 WARN with key=%s; got %d (entries=%+v)", corr, got, lg.snapshot())
	}
	if got := awaiterRegLen(a); got != 0 {
		t.Errorf("reg leak: len=%d", got)
	}
}

// T9 — Cancel idempotency.
func TestT9_ArtifactAwaiter_CancelIdempotent(t *testing.T) {
	a, _, _, lg := newArtifactAwaiterForTest(t, time.Second)
	const corr = "corr-9"
	if _, err := a.Register(corr); err != nil {
		t.Fatalf("Register: %v", err)
	}
	a.Cancel(corr)
	a.Cancel(corr)
	a.Cancel(corr)
	if got := awaiterRegLen(a); got != 0 {
		t.Errorf("reg len: %d want 0", got)
	}
	if entries := lg.snapshot(); len(entries) != 0 {
		t.Errorf("unexpected log entries on idempotent Cancel: %+v", entries)
	}
}

// T10 — Duplicate Deliver ⇒ first-wins; second drops with WARN; Await receives first.
func TestT10_ArtifactAwaiter_DuplicateDeliver_FirstWins(t *testing.T) {
	a, _, _, lg := newArtifactAwaiterForTest(t, time.Second)
	const corr = "corr-10"
	if _, err := a.Register(corr); err != nil {
		t.Fatalf("Register: %v", err)
	}

	evt1 := sampleArtifactsProvided(corr)
	evt1.DocumentID = "doc-first"
	evt2 := sampleArtifactsProvided(corr)
	evt2.DocumentID = "doc-second"

	// Deliver both BEFORE Await starts: with cap=1, the first wins and
	// the second falls into the select-default WARN branch.
	if err := a.Deliver(corr, evt1); err != nil {
		t.Fatalf("first Deliver: %v", err)
	}
	if err := a.Deliver(corr, evt2); err != nil {
		t.Fatalf("second Deliver: got %v want nil (drop silently)", err)
	}

	v, err := a.Await(context.Background(), corr)
	if err != nil {
		t.Fatalf("Await: %v", err)
	}
	if v.DocumentID != "doc-first" {
		t.Errorf("Await val: got %s want doc-first (first-wins)", v.DocumentID)
	}

	dupWarns := 0
	for _, e := range lg.snapshot() {
		if e.level == "warn" && strings.Contains(e.msg, "duplicate deliver dropped") {
			dupWarns++
		}
	}
	if dupWarns != 1 {
		t.Errorf("duplicate WARN count: got %d want 1", dupWarns)
	}
	if got := awaiterRegLen(a); got != 0 {
		t.Errorf("reg leak: len=%d", got)
	}
}

// T11 — Channel is closed after Cancel.
func TestT11_ArtifactAwaiter_CancelClosesChannel(t *testing.T) {
	a, _, _, _ := newArtifactAwaiterForTest(t, time.Second)
	const corr = "corr-11"
	ch, err := a.Register(corr)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	a.Cancel(corr)

	select {
	case v, ok := <-ch:
		if ok {
			t.Fatalf("expected closed channel; got value %+v ok=true", v)
		}
		if v.CorrelationID != "" || v.JobID != "" || v.DocumentID != "" || v.VersionID != "" || len(v.Artifacts) != 0 || len(v.MissingTypes) != 0 {
			t.Errorf("zero value: got %+v", v)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("channel was not closed within budget")
	}
}

// T12 — Await on never-Registered key ⇒ ErrAwaitTimeout, NO metric recorded.
func TestT12_ArtifactAwaiter_Await_NeverRegistered(t *testing.T) {
	a, m, _, _ := newArtifactAwaiterForTest(t, time.Second)
	v, err := a.Await(context.Background(), "nope")
	if !errors.Is(err, port.ErrAwaitTimeout) {
		t.Fatalf("Await err: got %v want ErrAwaitTimeout", err)
	}
	if v.CorrelationID != "" || v.JobID != "" || v.DocumentID != "" || v.VersionID != "" || len(v.Artifacts) != 0 || len(v.MissingTypes) != 0 {
		t.Errorf("val: got %+v want zero", v)
	}
	if calls := m.snapshot(); len(calls) != 0 {
		t.Errorf("metric: got %+v want EMPTY (D5 NOTE)", calls)
	}
}

// T13 — Outcome classification: ErrorCode set ⇒ outcome=missing.
func TestT13_ArtifactAwaiter_Outcome_ErrorCode_Missing(t *testing.T) {
	a, m, _, _ := newArtifactAwaiterForTest(t, time.Second)
	const corr = "corr-13"
	if _, err := a.Register(corr); err != nil {
		t.Fatalf("Register: %v", err)
	}
	evt := port.ArtifactsProvided{
		CorrelationID: corr,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		JobID:         "j",
		DocumentID:    "d",
		VersionID:     "v",
		Artifacts:     map[model.ArtifactType]json.RawMessage{model.ArtifactSemanticTree: json.RawMessage(`{}`)},
		ErrorCode:     "UNKNOWN_ARTIFACT_TYPE",
		ErrorMessage:  "ADR-LIC-05",
	}
	done := make(chan struct{})
	go func() {
		v, err := a.Await(context.Background(), corr)
		if err != nil {
			t.Errorf("Await: %v", err)
		}
		if v.ErrorCode != "UNKNOWN_ARTIFACT_TYPE" {
			t.Errorf("ErrorCode: got %q", v.ErrorCode)
		}
		close(done)
	}()
	if err := a.Deliver(corr, evt); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	<-done
	if calls := m.snapshot(); len(calls) != 1 || calls[0].outcome != outcomeMissing {
		t.Errorf("metric: got %+v want outcome=missing", calls)
	}
}

// T14 — Outcome classification: MissingTypes non-empty ⇒ outcome=missing.
func TestT14_ArtifactAwaiter_Outcome_MissingTypes_Missing(t *testing.T) {
	a, m, _, _ := newArtifactAwaiterForTest(t, time.Second)
	const corr = "corr-14"
	if _, err := a.Register(corr); err != nil {
		t.Fatalf("Register: %v", err)
	}
	evt := port.ArtifactsProvided{
		CorrelationID: corr,
		JobID:         "j",
		Artifacts:     map[model.ArtifactType]json.RawMessage{model.ArtifactSemanticTree: json.RawMessage(`{}`)},
		MissingTypes:  []model.ArtifactType{model.ArtifactRiskAnalysis},
	}
	done := make(chan struct{})
	go func() {
		_, err := a.Await(context.Background(), corr)
		if err != nil {
			t.Errorf("Await: %v", err)
		}
		close(done)
	}()
	if err := a.Deliver(corr, evt); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	<-done
	if calls := m.snapshot(); len(calls) != 1 || calls[0].outcome != outcomeMissing {
		t.Errorf("metric: got %+v want outcome=missing", calls)
	}
}

// T15 — Real-timer TTL pin (the ONE pin exercising time.NewTimer end-to-end).
func TestT15_ArtifactAwaiter_RealTimer_TTL(t *testing.T) {
	ttl := 50 * time.Millisecond
	jitter := 200 * time.Millisecond
	if !testing.Short() {
		jitter = 200 * time.Millisecond
	}
	a, m, _, _ := newArtifactAwaiterForTest(t, ttl)
	const corr = "corr-15"
	if _, err := a.Register(corr); err != nil {
		t.Fatalf("Register: %v", err)
	}
	start := time.Now()
	_, err := a.Await(context.Background(), corr)
	elapsed := time.Since(start)
	if !errors.Is(err, port.ErrAwaitTimeout) {
		t.Fatalf("Await err: got %v want ErrAwaitTimeout", err)
	}
	if elapsed < ttl {
		t.Errorf("elapsed=%v < TTL=%v", elapsed, ttl)
	}
	if elapsed > ttl+jitter {
		t.Errorf("elapsed=%v > TTL+jitter=%v", elapsed, ttl+jitter)
	}
	calls := m.snapshot()
	if len(calls) != 1 || calls[0].outcome != outcomeTimeout {
		t.Errorf("metric: got %+v", calls)
	}
}

// T16 — Leak-free under contract: N=100 register+await+(timeout|deliver) cycles.
func TestT16_ArtifactAwaiter_LeakFreeUnderContract(t *testing.T) {
	n := 100
	if testing.Short() {
		n = 20
	}
	a, _, _, _ := newArtifactAwaiterForTest(t, 10*time.Millisecond)
	for i := 0; i < n; i++ {
		corr := fmt.Sprintf("corr-leak-%d", i)
		if _, err := a.Register(corr); err != nil {
			t.Fatalf("Register: %v", err)
		}
		if i%2 == 0 {
			done := make(chan struct{})
			go func() {
				_, _ = a.Await(context.Background(), corr)
				close(done)
			}()
			_ = a.Deliver(corr, sampleArtifactsProvided(corr))
			<-done
		} else {
			_, _ = a.Await(context.Background(), corr)
		}
		if got := awaiterRegLen(a); got != 0 {
			t.Fatalf("iter %d (%s): reg leak: len=%d", i, corr, got)
		}
	}
	if got := awaiterRegLen(a); got != 0 {
		t.Errorf("final reg leak: len=%d", got)
	}
}

// ---------------------------------------------------------------------------
// Confirmation awaiter — T17..T22.
// ---------------------------------------------------------------------------

// T17 — Persisted (success) happy path.
func TestT17_ConfirmationAwaiter_Persisted_HappyPath(t *testing.T) {
	c, m, _, _ := newConfirmationAwaiterForTest(t, time.Second)
	const jobID = "j-17"
	if _, err := c.Register(jobID); err != nil {
		t.Fatalf("Register: %v", err)
	}
	type result struct {
		conf port.PersistConfirmation
		err  error
	}
	done := make(chan result, 1)
	go func() {
		v, e := c.Await(context.Background(), jobID)
		done <- result{conf: v, err: e}
	}()
	if err := c.HandlePersisted(context.Background(), samplePersisted(jobID)); err != nil {
		t.Fatalf("HandlePersisted: %v", err)
	}
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("Await: %v", r.err)
		}
		if !r.conf.IsSuccess() {
			t.Errorf("IsSuccess=false, conf=%+v", r.conf)
		}
		if r.conf.Success == nil {
			t.Errorf("Success: nil")
		}
	case <-time.After(time.Second):
		t.Fatal("Await timed out")
	}
	calls := m.snapshot()
	if len(calls) != 1 || calls[0].op != opPersistArtifacts || calls[0].outcome != outcomeSuccess {
		t.Errorf("metric: got %+v", calls)
	}
}

// T18 — PersistFailed (retryable=true) happy path.
func TestT18_ConfirmationAwaiter_PersistFailed_Retryable(t *testing.T) {
	c, m, _, _ := newConfirmationAwaiterForTest(t, time.Second)
	const jobID = "j-18"
	if _, err := c.Register(jobID); err != nil {
		t.Fatalf("Register: %v", err)
	}
	done := make(chan port.PersistConfirmation, 1)
	go func() {
		v, _ := c.Await(context.Background(), jobID)
		done <- v
	}()
	if err := c.HandlePersistFailed(context.Background(), samplePersistFailed(jobID, true)); err != nil {
		t.Fatalf("HandlePersistFailed: %v", err)
	}
	conf := <-done
	if !conf.IsFailure() {
		t.Errorf("IsFailure=false, conf=%+v", conf)
	}
	if conf.Failure == nil || !conf.Failure.IsRetryable {
		t.Errorf("Failure/IsRetryable: %+v", conf.Failure)
	}
	calls := m.snapshot()
	if len(calls) != 1 || calls[0].outcome != outcomePersistFailed {
		t.Errorf("metric: got %+v", calls)
	}
}

// T19 — PersistFailed (retryable=false) happy path — awaiter does NOT interpret IsRetryable.
func TestT19_ConfirmationAwaiter_PersistFailed_NonRetryable_DeliveredVerbatim(t *testing.T) {
	c, m, _, _ := newConfirmationAwaiterForTest(t, time.Second)
	const jobID = "j-19"
	if _, err := c.Register(jobID); err != nil {
		t.Fatalf("Register: %v", err)
	}
	done := make(chan port.PersistConfirmation, 1)
	go func() {
		v, _ := c.Await(context.Background(), jobID)
		done <- v
	}()
	if err := c.HandlePersistFailed(context.Background(), samplePersistFailed(jobID, false)); err != nil {
		t.Fatalf("HandlePersistFailed: %v", err)
	}
	conf := <-done
	if !conf.IsFailure() {
		t.Errorf("IsFailure=false, conf=%+v", conf)
	}
	if conf.Failure == nil || conf.Failure.IsRetryable {
		t.Errorf("Failure.IsRetryable: want false, got %+v", conf.Failure)
	}
	calls := m.snapshot()
	if len(calls) != 1 || calls[0].outcome != outcomePersistFailed {
		t.Errorf("metric: got %+v want outcome=persist_failed", calls)
	}
}

// T20 — Defensive pin: port.NewPersistConfirmationSuccess(nil) panics.
func TestT20_NewPersistConfirmationSuccess_PanicsOnNil(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on nil envelope")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic value: got %T want string", r)
		}
		if !strings.Contains(msg, "NewPersistConfirmationSuccess called with nil envelope") {
			t.Errorf("panic message: got %q", msg)
		}
	}()
	_ = port.NewPersistConfirmationSuccess(nil)
}

// T21 — Router-seam Deliver entry point on confirmation.
func TestT21_ConfirmationAwaiter_Deliver_RouterSurface(t *testing.T) {
	c, _, _, _ := newConfirmationAwaiterForTest(t, time.Second)
	const jobID = "j-21"
	if _, err := c.Register(jobID); err != nil {
		t.Fatalf("Register: %v", err)
	}
	done := make(chan port.PersistConfirmation, 1)
	go func() {
		v, _ := c.Await(context.Background(), jobID)
		done <- v
	}()
	persisted := samplePersisted(jobID)
	envelope := port.NewPersistConfirmationSuccess(&persisted)
	if err := c.Deliver(jobID, envelope); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	got := <-done
	if !got.IsSuccess() {
		t.Errorf("IsSuccess=false, got=%+v", got)
	}
	if got.Success == nil || got.Success.JobID != jobID {
		t.Errorf("JobID: got %+v want %s", got.Success, jobID)
	}
}

// T22 — Confirmation awaiter parallel keys (race-clean).
func TestT22_ConfirmationAwaiter_ParallelKeys_RaceClean(t *testing.T) {
	const N = 100
	c, _, _, _ := newConfirmationAwaiterForTest(t, 2*time.Second)
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			jobID := fmt.Sprintf("j-par-%d", i)
			if _, err := c.Register(jobID); err != nil {
				t.Errorf("Register(%s): %v", jobID, err)
				return
			}
			go func() {
				if i%2 == 0 {
					_ = c.HandlePersisted(context.Background(), samplePersisted(jobID))
				} else {
					_ = c.HandlePersistFailed(context.Background(), samplePersistFailed(jobID, true))
				}
			}()
			if _, err := c.Await(context.Background(), jobID); err != nil {
				t.Errorf("Await(%s): %v", jobID, err)
			}
		}()
	}
	wg.Wait()
	if got := confirmationRegLen(c); got != 0 {
		t.Errorf("reg leak: len=%d", got)
	}
}

// ---------------------------------------------------------------------------
// Constructor & race pins — T-CTOR-1..4 + T-RACE-1.
// ---------------------------------------------------------------------------

// T-CTOR-1 — NewArtifactAwaiter fail-fast on TTL<=0.
func TestTCTOR1_NewArtifactAwaiter_FailFastOnTTL(t *testing.T) {
	cases := []time.Duration{0, -time.Millisecond, -time.Hour}
	for _, ttl := range cases {
		a, err := NewArtifactAwaiter(ArtifactConfig{TTL: ttl}, Deps{})
		if err == nil {
			t.Errorf("ttl=%v: want error, got nil (a=%v)", ttl, a)
			continue
		}
		if !strings.Contains(err.Error(), "TTL must be > 0") {
			t.Errorf("ttl=%v: err %q lacks 'TTL must be > 0'", ttl, err.Error())
		}
		if a != nil {
			t.Errorf("ttl=%v: awaiter must be nil on err", ttl)
		}
	}
}

// T-CTOR-2 — NewConfirmationAwaiter fail-fast on TTL<=0.
func TestTCTOR2_NewConfirmationAwaiter_FailFastOnTTL(t *testing.T) {
	cases := []time.Duration{0, -time.Millisecond, -time.Hour}
	for _, ttl := range cases {
		c, err := NewConfirmationAwaiter(ConfirmationConfig{TTL: ttl}, Deps{})
		if err == nil {
			t.Errorf("ttl=%v: want error, got nil (c=%v)", ttl, c)
			continue
		}
		if !strings.Contains(err.Error(), "TTL must be > 0") {
			t.Errorf("ttl=%v: err %q lacks 'TTL must be > 0'", ttl, err.Error())
		}
		if c != nil {
			t.Errorf("ttl=%v: awaiter must be nil on err", ttl)
		}
	}
}

// T-CTOR-3 — NewArtifactAwaiter with all nil Deps fields ⇒ noop defaults used.
func TestTCTOR3_NewArtifactAwaiter_NilDeps_NoopDefaults(t *testing.T) {
	a, err := NewArtifactAwaiter(ArtifactConfig{TTL: 100 * time.Millisecond}, Deps{})
	if err != nil {
		t.Fatalf("NewArtifactAwaiter: %v", err)
	}
	if a == nil {
		t.Fatal("awaiter is nil")
	}
	const corr = "corr-ctor3"
	ch, err := a.Register(corr)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if ch == nil {
		t.Fatal("Register returned nil channel")
	}
	a.Cancel(corr)

	// Confirmation symmetric.
	c, err := NewConfirmationAwaiter(ConfirmationConfig{TTL: 100 * time.Millisecond}, Deps{})
	if err != nil {
		t.Fatalf("NewConfirmationAwaiter: %v", err)
	}
	const jobID = "j-ctor3"
	if _, err := c.Register(jobID); err != nil {
		t.Fatalf("Register: %v", err)
	}
	c.Cancel(jobID)
}

// T-CTOR-4 — Concurrent Register+Cancel pairs do not race.
func TestTCTOR4_ArtifactAwaiter_ConcurrentRegisterCancel(t *testing.T) {
	const N = 1000
	a, _, _, _ := newArtifactAwaiterForTest(t, time.Second)
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			corr := fmt.Sprintf("corr-ctor4-%d", i)
			if _, err := a.Register(corr); err != nil {
				t.Errorf("Register(%s): %v", corr, err)
				return
			}
			a.Cancel(corr)
		}()
	}
	wg.Wait()
	if got := awaiterRegLen(a); got != 0 {
		t.Errorf("reg leak: len=%d want 0", got)
	}
}

// T-RACE-1 — Combined Artifact+Confirmation stress.
func TestTRACE1_ArtifactAndConfirmation_Stress(t *testing.T) {
	N := 200
	if testing.Short() {
		N = 20
	}
	a, _, _, _ := newArtifactAwaiterForTest(t, 2*time.Second)
	c, _, _, _ := newConfirmationAwaiterForTest(t, 2*time.Second)
	var wg sync.WaitGroup
	wg.Add(2 * N)
	var artErrors, confErrors atomic.Int32
	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			corr := fmt.Sprintf("corr-race-%d", i)
			if _, err := a.Register(corr); err != nil {
				artErrors.Add(1)
				return
			}
			go func() { _ = a.Deliver(corr, sampleArtifactsProvided(corr)) }()
			if _, err := a.Await(context.Background(), corr); err != nil {
				artErrors.Add(1)
			}
		}()
		go func() {
			defer wg.Done()
			jobID := fmt.Sprintf("j-race-%d", i)
			if _, err := c.Register(jobID); err != nil {
				confErrors.Add(1)
				return
			}
			go func() { _ = c.HandlePersisted(context.Background(), samplePersisted(jobID)) }()
			if _, err := c.Await(context.Background(), jobID); err != nil {
				confErrors.Add(1)
			}
		}()
	}
	wg.Wait()
	if v := artErrors.Load(); v != 0 {
		t.Errorf("artifact errors: %d", v)
	}
	if v := confErrors.Load(); v != 0 {
		t.Errorf("confirmation errors: %d", v)
	}
	if got := awaiterRegLen(a); got != 0 {
		t.Errorf("artifact reg leak: %d", got)
	}
	if got := confirmationRegLen(c); got != 0 {
		t.Errorf("confirmation reg leak: %d", got)
	}
}

// ---------------------------------------------------------------------------
// Targeted classifier unit pins (cover ConfirmationAwaiter's defensive
// outcomeMissing branch — build-spec R3).
// ---------------------------------------------------------------------------

func TestClassifyConfirmationOutcome_BothZero_DefensiveMissing(t *testing.T) {
	if got := classifyConfirmationOutcome(port.PersistConfirmation{}, nil); got != outcomeMissing {
		t.Errorf("classify zero conf: got %q want %q", got, outcomeMissing)
	}
}
