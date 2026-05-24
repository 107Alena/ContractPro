// Package lictestapp wires the real production orchestrator / router /
// pending manager / consumer / awaiters / publishers over the in-memory
// fakes (fakes.*) so integration tests (LIC-TASK-049 / 050 / 051 / 054)
// exercise the real code paths without RabbitMQ / Redis / OTel / LLM
// providers.
//
// This file declares the thin seam adapters (the test-side equivalent
// of internal/app/adapters.go). Every adapter is a noop / passthrough
// — the harness owns no real telemetry; the production seams are
// satisfied with zero-friction substitutes so the wiring compiles and
// runs.
//
// The one load-bearing adapter is `kvAdapter`: the production
// idempotency.Guard, pendingStateStore and versionMetaCache branch on
// `errors.Is(err, kvstore.ErrKeyNotFound)`; the FakeKVStore returns
// its OWN ErrKeyNotFound sentinel (a distinct value). The adapter
// translates the fake sentinel to the production sentinel at the seam
// boundary so the production code paths behave identically.
package lictestapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"contractpro/legal-intelligence-core/internal/application/aggregator"
	"contractpro/legal-intelligence-core/internal/application/dmawaiter"
	"contractpro/legal-intelligence-core/internal/application/pendingconfirmation"
	"contractpro/legal-intelligence-core/internal/application/pipeline"
	"contractpro/legal-intelligence-core/internal/application/pipeline/stages"
	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
	"contractpro/legal-intelligence-core/internal/egress/dlq"
	dmpub "contractpro/legal-intelligence-core/internal/egress/publisher/dm"
	"contractpro/legal-intelligence-core/internal/egress/publisher/orch"
	"contractpro/legal-intelligence-core/internal/infra/broker"
	"contractpro/legal-intelligence-core/internal/infra/kvstore"
	"contractpro/legal-intelligence-core/internal/ingress/consumer"
	"contractpro/legal-intelligence-core/internal/ingress/idempotency"
	ingressrouter "contractpro/legal-intelligence-core/internal/ingress/router"
	"contractpro/legal-intelligence-core/internal/integration/fakes"
	"contractpro/legal-intelligence-core/internal/llm/cost"
	llmrouter "contractpro/legal-intelligence-core/internal/llm/router"
	"contractpro/legal-intelligence-core/internal/agents/base"
	"contractpro/legal-intelligence-core/internal/agents/schemavalidator"
)

// -----------------------------------------------------------------------------
// kvAdapter — the load-bearing sentinel translator.
//
// Production code (idempotency.Guard, pendingStateStore, versionMetaCache)
// uses `errors.Is(err, kvstore.ErrKeyNotFound)` to detect a miss. The
// FakeKVStore returns `fakes.ErrKeyNotFound` (a distinct sentinel value
// with the same message). This adapter translates the fake sentinel into
// the production sentinel on every Get; every other method is pure
// passthrough.
// -----------------------------------------------------------------------------

type kvAdapter struct {
	kv *fakes.FakeKVStore
}

func newKVAdapter(kv *fakes.FakeKVStore) *kvAdapter { return &kvAdapter{kv: kv} }

func (a *kvAdapter) Get(ctx context.Context, key string) (string, error) {
	v, err := a.kv.Get(ctx, key)
	if err != nil && errors.Is(err, fakes.ErrKeyNotFound) {
		return "", kvstore.ErrKeyNotFound
	}
	return v, err
}

func (a *kvAdapter) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	return a.kv.Set(ctx, key, value, ttl)
}

func (a *kvAdapter) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	return a.kv.SetNX(ctx, key, value, ttl)
}

func (a *kvAdapter) Delete(ctx context.Context, keys ...string) (int64, error) {
	return a.kv.Delete(ctx, keys...)
}

func (a *kvAdapter) Expire(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return a.kv.Expire(ctx, key, ttl)
}

func (a *kvAdapter) Eval(ctx context.Context, script string, keys []string, args ...any) (any, error) {
	return a.kv.Eval(ctx, script, keys, args...)
}

// Compile-time check that kvAdapter satisfies idempotency.RedisSeam.
var _ idempotency.RedisSeam = (*kvAdapter)(nil)

// -----------------------------------------------------------------------------
// pendingStateStore — local copy of internal/app/pending_state.go that
// uses the kvAdapter so the fake-sentinel translation is in effect.
// -----------------------------------------------------------------------------

const keyPrefixPendingState = "lic-pending-state:"

type pendingStateStore struct {
	kv *kvAdapter
}

func newPendingStateStore(kv *kvAdapter) *pendingStateStore {
	return &pendingStateStore{kv: kv}
}

func (s *pendingStateStore) Save(ctx context.Context, versionID string, pts *model.PendingTypeConfirmation, ttl time.Duration) error {
	if pts == nil {
		return errors.New("lictestapp/pending-state: state must not be nil")
	}
	if versionID == "" {
		return errors.New("lictestapp/pending-state: versionID must not be empty")
	}
	payload, err := pts.Encode()
	if err != nil {
		return fmt.Errorf("lictestapp/pending-state: encode: %w", err)
	}
	return s.kv.Set(ctx, keyPrefixPendingState+versionID, string(payload), ttl)
}

func (s *pendingStateStore) Load(ctx context.Context, versionID string) (*model.PendingTypeConfirmation, error) {
	if versionID == "" {
		return nil, errors.New("lictestapp/pending-state: versionID must not be empty")
	}
	raw, err := s.kv.Get(ctx, keyPrefixPendingState+versionID)
	if err != nil {
		if errors.Is(err, kvstore.ErrKeyNotFound) {
			return nil, port.ErrPendingStateNotFound
		}
		return nil, fmt.Errorf("lictestapp/pending-state: get: %w", err)
	}
	pts, err := model.DecodePendingTypeConfirmation([]byte(raw))
	if err != nil {
		return nil, fmt.Errorf("lictestapp/pending-state: decode: %w", err)
	}
	return pts, nil
}

func (s *pendingStateStore) Delete(ctx context.Context, versionID string) error {
	if versionID == "" {
		return errors.New("lictestapp/pending-state: versionID must not be empty")
	}
	_, err := s.kv.Delete(ctx, keyPrefixPendingState+versionID)
	if err != nil {
		return fmt.Errorf("lictestapp/pending-state: delete: %w", err)
	}
	return nil
}

var _ port.PendingStatePort = (*pendingStateStore)(nil)

// -----------------------------------------------------------------------------
// versionMetaCache — local copy of internal/app/version_meta_cache.go.
// -----------------------------------------------------------------------------

const keyPrefixVersionMeta = "lic-version-meta:"

type versionMetaPayload struct {
	ParentVersionID *string `json:"parent_version_id,omitempty"`
	OriginType      string  `json:"origin_type,omitempty"`
}

type versionMetaCache struct {
	kv *kvAdapter
}

func newVersionMetaCache(kv *kvAdapter) *versionMetaCache {
	return &versionMetaCache{kv: kv}
}

func (c *versionMetaCache) Set(ctx context.Context, versionID string, payload []byte, ttl time.Duration) error {
	if versionID == "" {
		return errors.New("lictestapp/version-meta: versionID must not be empty")
	}
	return c.kv.Set(ctx, keyPrefixVersionMeta+versionID, string(payload), ttl)
}

func (c *versionMetaCache) GetParentVersionID(ctx context.Context, versionID string) (*string, error) {
	if versionID == "" {
		return nil, nil
	}
	raw, err := c.kv.Get(ctx, keyPrefixVersionMeta+versionID)
	if err != nil {
		if errors.Is(err, kvstore.ErrKeyNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("lictestapp/version-meta: get: %w", err)
	}
	var p versionMetaPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return nil, fmt.Errorf("lictestapp/version-meta: unmarshal: %w", err)
	}
	return p.ParentVersionID, nil
}

var (
	_ ingressrouter.VersionMetaCacheWriter = (*versionMetaCache)(nil)
	_ pipeline.VersionMetaCache            = (*versionMetaCache)(nil)
)

// -----------------------------------------------------------------------------
// lazyResumer — mirrors internal/app/app.go's lazyResumer. Closes the
// circular dependency between pendingconfirmation.Manager (needs a
// PipelineResumer) and pipeline.Orchestrator (needs a PauseController).
// -----------------------------------------------------------------------------

type lazyResumer struct {
	mu  sync.RWMutex
	val pendingconfirmation.PipelineResumer
}

func (l *lazyResumer) set(r pendingconfirmation.PipelineResumer) {
	l.mu.Lock()
	l.val = r
	l.mu.Unlock()
}

func (l *lazyResumer) ResumeAfterConfirmation(ctx context.Context, state *model.PipelineState) error {
	l.mu.RLock()
	r := l.val
	l.mu.RUnlock()
	if r == nil {
		return errors.New("lictestapp: pipeline resumer not yet wired (construction-time race)")
	}
	return r.ResumeAfterConfirmation(ctx, state)
}

var _ pendingconfirmation.PipelineResumer = (*lazyResumer)(nil)

// -----------------------------------------------------------------------------
// sysClock — UTC wall clock shared across every Clock seam.
// -----------------------------------------------------------------------------

type sysClock struct{}

func (sysClock) Now() time.Time                  { return time.Now().UTC() }
func (sysClock) Since(t time.Time) time.Duration { return time.Since(t) }

var (
	_ pipeline.Clock            = sysClock{}
	_ pendingconfirmation.Clock = sysClock{}
	_ dmawaiter.Clock           = sysClock{}
	_ ingressrouter.Clock       = sysClock{}
	_ consumer.Clock            = sysClock{}
	_ dmpub.Clock               = sysClock{}
	_ orch.Clock                = sysClock{}
	_ dlq.Clock                 = sysClock{}
)

// -----------------------------------------------------------------------------
// Noop loggers — one per seam where the method-set differs.
// -----------------------------------------------------------------------------

// stdNoopLogger satisfies pipeline.Logger / pendingconfirmation.Logger /
// dmawaiter.Logger / ingressrouter.Logger / idempotency.Logger /
// dmpub.Logger / orch.Logger / dlq.Logger. The intersection of method
// sets is {Warn, Error}; pendingconfirmation / ingressrouter additionally
// expose Info. We implement all three so a single value satisfies every
// seam.
type stdNoopLogger struct{}

func (stdNoopLogger) Info(context.Context, string, ...any)  {}
func (stdNoopLogger) Warn(context.Context, string, ...any)  {}
func (stdNoopLogger) Error(context.Context, string, ...any) {}

var (
	_ pipeline.Logger            = stdNoopLogger{}
	_ pendingconfirmation.Logger = stdNoopLogger{}
	_ dmawaiter.Logger           = stdNoopLogger{}
	_ ingressrouter.Logger       = stdNoopLogger{}
	_ idempotency.Logger         = stdNoopLogger{}
	_ dmpub.Logger               = stdNoopLogger{}
	_ orch.Logger                = stdNoopLogger{}
	_ dlq.Logger                 = stdNoopLogger{}
)

// consumerNoopLogger satisfies consumer.Logger which additionally
// declares WithRequestContext.
type consumerNoopLogger struct{}

func (consumerNoopLogger) Info(context.Context, string, ...any)  {}
func (consumerNoopLogger) Warn(context.Context, string, ...any)  {}
func (consumerNoopLogger) Error(context.Context, string, ...any) {}
func (consumerNoopLogger) WithRequestContext(ctx context.Context, _ consumer.RequestIDs) context.Context {
	return ctx
}

var _ consumer.Logger = consumerNoopLogger{}

// -----------------------------------------------------------------------------
// Noop metrics — one per seam.
// -----------------------------------------------------------------------------

type pipelineNoopMetrics struct{}

func (pipelineNoopMetrics) PipelineStarted(string)                   {}
func (pipelineNoopMetrics) PipelineFinished(string, string, float64) {}
func (pipelineNoopMetrics) PipelineOutcome(string, string, string)   {}

var _ pipeline.PipelineMetrics = pipelineNoopMetrics{}

type stageNoopMetrics struct{}

func (stageNoopMetrics) StageDuration(string, float64) {}

var _ stages.StageMetrics = stageNoopMetrics{}

type agentNoopMetrics struct{}

func (agentNoopMetrics) Invocation(string, string) {}
func (agentNoopMetrics) Duration(string, float64)  {}
func (agentNoopMetrics) InputTokens(string, int)   {}
func (agentNoopMetrics) OutputTokens(string, int)  {}

var _ base.Metrics = agentNoopMetrics{}

type repairNoopMetrics struct{}

func (repairNoopMetrics) RepairAttempt(string, string)         {}
func (repairNoopMetrics) RepairOutcome(string, string, string) {}

var _ schemavalidator.Metrics = repairNoopMetrics{}

type aggregatorNoopMetrics struct{}

func (aggregatorNoopMetrics) PromptInjectionDetected(string) {}

var _ aggregator.Metrics = aggregatorNoopMetrics{}

type dmAwaiterNoopMetrics struct{}

func (dmAwaiterNoopMetrics) RecordOutcome(string, string, float64) {}

var _ dmawaiter.Metrics = dmAwaiterNoopMetrics{}

type pendingNoopMetrics struct{}

func (pendingNoopMetrics) PendingStateInc()                  {}
func (pendingNoopMetrics) PendingStateDec()                  {}
func (pendingNoopMetrics) PendingStateAgeMaxSeconds(float64) {}
func (pendingNoopMetrics) UserConfirmation(string)           {}

var _ pendingconfirmation.Metrics = pendingNoopMetrics{}

type idempotencyNoopMetrics struct{}

func (idempotencyNoopMetrics) Lookup(string) {}
func (idempotencyNoopMetrics) Fallback()     {}

var _ idempotency.Metrics = idempotencyNoopMetrics{}

type consumerNoopMetrics struct{}

func (consumerNoopMetrics) ConsumerMessage(string, string) {}

var _ consumer.Metrics = consumerNoopMetrics{}

type dmPubNoopMetrics struct{}

func (dmPubNoopMetrics) IncPublish(string, dmpub.PublishOutcome) {}
func (dmPubNoopMetrics) ObservePublishedSize(int)                {}

var _ dmpub.Metrics = dmPubNoopMetrics{}

type orchPubNoopMetrics struct{}

func (orchPubNoopMetrics) IncPublish(string, orch.PublishOutcome) {}

var _ orch.Metrics = orchPubNoopMetrics{}

type dlqPubNoopMetrics struct{}

func (dlqPubNoopMetrics) IncPublish(string, dlq.PublishOutcome) {}
func (dlqPubNoopMetrics) IncDLQPublished(string, string)        {}

var _ dlq.Metrics = dlqPubNoopMetrics{}

// -----------------------------------------------------------------------------
// LLM router seams.
// -----------------------------------------------------------------------------

type routerNoopMetrics struct{}

func (routerNoopMetrics) ProviderFallback(port.LLMProviderID, port.LLMProviderID, model.AgentID) {
}
func (routerNoopMetrics) ProviderSkippedUnhealthy(port.LLMProviderID) {}
func (routerNoopMetrics) ProviderFailed(port.LLMProviderID, port.LLMErrorCode) {
}
func (routerNoopMetrics) ProviderHealthState(port.LLMProviderID, llmrouter.HealthState) {
}

var _ llmrouter.Metrics = routerNoopMetrics{}

// usageTracker bridges *cost.Tracker onto llmrouter.UsageTracker — copied
// verbatim from internal/app/adapters.go.
type usageTracker struct{ t *cost.Tracker }

func (u usageTracker) ObserveSuccess(provider port.LLMProviderID, _ string, agent model.AgentID, resp port.CompletionResponse) {
	u.t.ObserveSuccess(cost.Usage{
		Provider:          provider,
		Model:             resp.Model,
		Agent:             agent,
		InputTokens:       resp.InputTokens,
		CachedInputTokens: resp.CachedInputTokens,
		OutputTokens:      resp.OutputTokens,
		Latency:           time.Duration(resp.LatencyMs) * time.Millisecond,
	})
}

func (u usageTracker) ObserveCall(provider port.LLMProviderID, mdl string, agent model.AgentID, outcome llmrouter.CallOutcome) {
	u.t.ObserveCall(provider, mdl, agent, cost.Outcome(outcome))
}

var _ llmrouter.UsageTracker = usageTracker{}

// noopRLObserver mirrors the production rate-limiter observer (also a noop
// in production for the test-relevant signals).
type noopRLObserver struct{}

func (noopRLObserver) RateLimited(string)            {}
func (noopRLObserver) FailOpen(string, error)        {}
func (noopRLObserver) ScriptAnomaly(string, error)   {}

// noopCostRecorder satisfies cost.Recorder.
type noopCostRecorder struct{}

func (noopCostRecorder) RecordUsage(string, string, string, int, int, int, float64, time.Duration) {}
func (noopCostRecorder) RecordCall(string, string, string, string)                                 {}
func (noopCostRecorder) UnknownModel(string, string)                                               {}

var _ cost.Recorder = noopCostRecorder{}

// -----------------------------------------------------------------------------
// filteredBrokerSubscriber — wraps *FakeBroker so only an allow-listed
// subset of queue names actually subscribes via the production consumer.
//
// The orchestrator publishes artifact requests with a ":current" /
// ":parent" suffix on correlation_id; the DM response echoes that
// suffix back; the consumer's decodeArtifactsProvided strictly
// validates canonical UUID on correlation_id, so a suffixed value
// fails validation and the response is DLQ'd. In production this
// indicates a known design tension (forward-noted in the task brief).
// For LIC-TASK-049's happy-path integration the harness uses this
// subscriber to keep the consumer wired to the three TRIGGER queues
// only (version-artifacts-ready / version-created /
// user-confirmed-type) and routes the three DM-response queues
// directly into the ingress router via registerDMResponseRoutes
// below.
// -----------------------------------------------------------------------------

type filteredBrokerSubscriber struct {
	inner *fakes.FakeBroker
	allow map[string]struct{}
}

func (f *filteredBrokerSubscriber) Subscribe(queue string, h broker.MessageHandler) error {
	if _, ok := f.allow[queue]; !ok {
		// Silently drop the subscription — the consumer expects no
		// error so its Start() proceeds. The dropped subscriptions are
		// the DM-response queues, which the directDMRouter wires
		// independently.
		return nil
	}
	return f.inner.Subscribe(queue, h)
}

var _ consumer.BrokerSubscriber = (*filteredBrokerSubscriber)(nil)

// registerDMResponseRoutes wires the three DM-response queues
// (artifacts-provided / persisted / persist-failed) straight into the
// production ingress router, bypassing the consumer's canonical-UUID
// validation that is incompatible with the orchestrator's correlation
// suffix design.
func registerDMResponseRoutes(t testingT, fb *fakes.FakeBroker, r dmResponseRouter) {
	t.Helper()
	if err := fb.Subscribe(fakes.QueueArtifactsProvided, func(ctx context.Context, d broker.Delivery) error {
		var evt port.ArtifactsProvided
		if err := json.Unmarshal(d.Body(), &evt); err != nil {
			return d.Ack()
		}
		_ = r.RouteArtifactsProvided(ctx, evt)
		return d.Ack()
	}); err != nil {
		t.Fatalf("lictestapp: subscribe artifacts-provided: %v", err)
	}
	if err := fb.Subscribe(fakes.QueueLICPersistConfirm, func(ctx context.Context, d broker.Delivery) error {
		var evt port.LegalAnalysisArtifactsPersisted
		if err := json.Unmarshal(d.Body(), &evt); err != nil {
			return d.Ack()
		}
		_ = r.RoutePersisted(ctx, evt)
		return d.Ack()
	}); err != nil {
		t.Fatalf("lictestapp: subscribe lic-persist-confirm: %v", err)
	}
	if err := fb.Subscribe(fakes.QueueLICPersistFail, func(ctx context.Context, d broker.Delivery) error {
		var evt port.LegalAnalysisArtifactsPersistFailed
		if err := json.Unmarshal(d.Body(), &evt); err != nil {
			return d.Ack()
		}
		_ = r.RoutePersistFailed(ctx, evt)
		return d.Ack()
	}); err != nil {
		t.Fatalf("lictestapp: subscribe lic-persist-fail: %v", err)
	}
}

// testingT is the subset of *testing.T the harness needs — keeping the
// helpers usable from non-test contexts (and easier to fake when the
// LIC-TASK-049+ test suite grows beyond the *testing.T entrypoint).
type testingT interface {
	Helper()
	Fatalf(format string, args ...any)
}

// dmResponseRouter is the subset of *ingressrouter.Router the
// directDMRouter calls.
type dmResponseRouter interface {
	RouteArtifactsProvided(ctx context.Context, evt port.ArtifactsProvided) error
	RoutePersisted(ctx context.Context, evt port.LegalAnalysisArtifactsPersisted) error
	RoutePersistFailed(ctx context.Context, evt port.LegalAnalysisArtifactsPersistFailed) error
}

// -----------------------------------------------------------------------------
// Suppress slog unused-import on platforms that strip it.
// -----------------------------------------------------------------------------

var _ = slog.LevelInfo
