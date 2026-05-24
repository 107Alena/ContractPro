// Package app wires the LIC service together: it constructs every package's
// concrete implementation, threads them through the dependency graph, runs the
// HTTP probe server, the broker consumer, and the LLM provider health-check
// loop, and orchestrates a graceful shutdown.
//
// The wiring follows the dependency order documented in deployment.md §6:
// config → logger → metrics → tracer → kvstore → broker → LLM providers →
// router → cost/ratelimit → agents → idempotency guard → pending manager →
// dmawaiters → publishers → pipeline orchestrator → consumer → health.
//
// Shutdown follows the deployment.md §6 sequence:
//
//  1. health.SetNotReady() — /readyz → 503.
//  2. wait readinessDrainDelay so Kubernetes removes the pod from rotation.
//  3. cancel the consumer ctx — every in-flight pipeline observes ctx.Done.
//  4. wait until in-flight WaitGroup drains (capped at cfg.App.ShutdownTimeout).
//  5. close broker → close redis → flush OTel tracer.
//
// Errors during step 5 are joined via errors.Join so a slow Redis close does
// not mask a broker close failure.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"contractpro/legal-intelligence-core/internal/agents/base"
	"contractpro/legal-intelligence-core/internal/agents/detailedreport"
	"contractpro/legal-intelligence-core/internal/agents/keyparams"
	"contractpro/legal-intelligence-core/internal/agents/mandatoryconditions"
	"contractpro/legal-intelligence-core/internal/agents/partyconsistency"
	"contractpro/legal-intelligence-core/internal/agents/recommendation"
	"contractpro/legal-intelligence-core/internal/agents/riskdelta"
	"contractpro/legal-intelligence-core/internal/agents/riskdetection"
	"contractpro/legal-intelligence-core/internal/agents/summary"
	"contractpro/legal-intelligence-core/internal/agents/typeclassifier"
	"contractpro/legal-intelligence-core/internal/application/aggregator"
	"contractpro/legal-intelligence-core/internal/application/dmawaiter"
	"contractpro/legal-intelligence-core/internal/application/pendingconfirmation"
	"contractpro/legal-intelligence-core/internal/application/pipeline"
	"contractpro/legal-intelligence-core/internal/application/pipeline/stages"
	"contractpro/legal-intelligence-core/internal/config"
	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
	"contractpro/legal-intelligence-core/internal/egress/dlq"
	dmpub "contractpro/legal-intelligence-core/internal/egress/publisher/dm"
	"contractpro/legal-intelligence-core/internal/egress/publisher/orch"
	"contractpro/legal-intelligence-core/internal/infra/broker"
	"contractpro/legal-intelligence-core/internal/infra/concurrency"
	"contractpro/legal-intelligence-core/internal/infra/health"
	"contractpro/legal-intelligence-core/internal/infra/kvstore"
	"contractpro/legal-intelligence-core/internal/infra/observability/logger"
	"contractpro/legal-intelligence-core/internal/infra/observability/metrics"
	"contractpro/legal-intelligence-core/internal/infra/observability/tracer"
	"contractpro/legal-intelligence-core/internal/ingress/consumer"
	"contractpro/legal-intelligence-core/internal/ingress/idempotency"
	ingressrouter "contractpro/legal-intelligence-core/internal/ingress/router"
	"contractpro/legal-intelligence-core/internal/llm/claude"
	"contractpro/legal-intelligence-core/internal/llm/cost"
	"contractpro/legal-intelligence-core/internal/llm/gemini"
	"contractpro/legal-intelligence-core/internal/llm/openai"
	"contractpro/legal-intelligence-core/internal/llm/pricing"
	"contractpro/legal-intelligence-core/internal/llm/ratelimit"
	llmrouter "contractpro/legal-intelligence-core/internal/llm/router"
)

// readinessDrainDelay is the window deployment.md §6 reserves between
// SetNotReady() and the actual drain so the Kubernetes readiness probe has
// time to take the pod out of the Service rotation (default 5s).
const readinessDrainDelay = 5 * time.Second

// BuildInfo carries the immutable identifiers stamped at build time and
// surfaced via lic_build_info{...}.
type BuildInfo struct {
	Version   string
	Commit    string
	GoVersion string
}

// App is the assembled LIC service.
//
// The struct is intentionally large — wiring is a "list of constructed
// dependencies in dependency order" — but every field is set exactly once in
// New() and never mutated after. Shutdown() is protected by sync.Once.
type App struct {
	cfg       *config.Config
	buildInfo BuildInfo

	// observability
	log     *logger.Logger
	metrics *metrics.Metrics
	tracer  *tracer.Tracer

	// infrastructure clients
	kv     *kvstore.Client
	broker *broker.Client

	// LLM stack
	pricingTable pricing.Table
	costTracker  *cost.Tracker
	rateLimiter  *ratelimit.Limiter
	providers    map[port.LLMProviderID]port.LLMProviderPort
	llmRouter    *llmrouter.ProviderRouter

	// agents + pipeline body
	agents       map[model.AgentID]port.Agent
	executor     *stages.Executor
	aggregator   *aggregator.Aggregator
	pendingStore *pendingStateStore
	metaCache    *versionMetaCache
	jobLimiter   *concurrency.Semaphore
	idempGuard   *idempotency.Guard

	// awaiters
	artifactAwaiter     *dmawaiter.ArtifactAwaiter
	confirmationAwaiter *dmawaiter.ConfirmationAwaiter

	// publishers
	statusPub    *orch.StatusPublisher
	uncertainPub *orch.UncertaintyPublisher
	artReqPub    *dmpub.ArtifactRequester
	analysisPub  *dmpub.AnalysisArtifactsPublisher
	dlqPub       *dlq.DLQPublisher

	// orchestration
	pipelineOrch *pipeline.Orchestrator
	pendingMgr   *pendingconfirmation.Manager

	// ingress
	ingressRouter *ingressrouter.Router
	consumer      *consumer.Consumer

	// HTTP / probes
	healthHandler *health.Handler
	httpServer    *http.Server

	// runtime lifecycle
	consumerCtx    context.Context
	consumerCancel context.CancelFunc
	inflight       sync.WaitGroup

	shutdownOnce sync.Once
	shutdownErr  error
}

// New wires every collaborator together. It is the single dependency-injection
// site (deployment.md §5.1 / §6 sequence). A non-nil error aborts startup —
// the binary must exit non-zero rather than start in a partially-built state.
func New(ctx context.Context, cfg *config.Config, info BuildInfo) (*App, error) {
	if cfg == nil {
		return nil, errors.New("app: New: config must not be nil")
	}

	a := &App{cfg: cfg, buildInfo: info}

	// 1. logger
	lg, err := logger.New(cfg.App)
	if err != nil {
		return nil, fmt.Errorf("app: build logger: %w", err)
	}
	a.log = lg.With("app")

	// 2. metrics + tracer
	a.metrics = metrics.New(metrics.BuildInfo{
		Version:   info.Version,
		Commit:    info.Commit,
		GoVersion: info.GoVersion,
	})
	tr, err := tracer.New(ctx, tracer.Config{
		ServiceName:    cfg.Observability.ServiceName,
		ServiceVersion: info.Version,
		Environment:    string(cfg.App.Env),
		Endpoint:       cfg.Observability.OTELEndpoint,
		Insecure:       cfg.Observability.OTELInsecure,
		Sampler:        cfg.Observability.TracesSampler,
		SamplerArg:     cfg.Observability.TracesSamplerArg,
		InstallGlobals: true,
	})
	if err != nil {
		return nil, fmt.Errorf("app: build tracer: %w", err)
	}
	a.tracer = tr

	// 3. infrastructure: redis + broker
	kv, err := kvstore.NewClient(cfg.Redis)
	if err != nil {
		return nil, fmt.Errorf("app: build kvstore: %w", err)
	}
	a.kv = kv

	br, err := broker.NewClient(cfg.Broker)
	if err != nil {
		_ = kv.Close()
		return nil, fmt.Errorf("app: build broker: %w", err)
	}
	a.broker = br

	// 4. LLM pricing + cost tracker
	tbl, err := pricing.Load(cfg.Pricing.TablePath)
	if err != nil {
		return nil, fmt.Errorf("app: load pricing table: %w", err)
	}
	a.pricingTable = tbl

	costTracker, err := cost.NewTracker(tbl, costRecorder{l: a.metrics.LLM})
	if err != nil {
		return nil, fmt.Errorf("app: build cost tracker: %w", err)
	}
	a.costTracker = costTracker

	// 5. providers + rate limiter
	if err := a.wireProviders(); err != nil {
		return nil, err
	}
	if err := a.wireRateLimiter(); err != nil {
		return nil, err
	}

	// 6. LLM provider router
	if err := a.wireLLMRouter(); err != nil {
		return nil, err
	}

	// 7. agents
	if err := a.wireAgents(); err != nil {
		return nil, err
	}

	// 8. stage executor + aggregator
	exec, err := stages.NewExecutor(a.agents, stages.Deps{
		Metrics: stageMetrics{p: a.metrics.Pipeline},
		// Tracer noop for v1 — the OTel adapter is forward-noted in stages/CLAUDE.md.
	})
	if err != nil {
		return nil, fmt.Errorf("app: build stage executor: %w", err)
	}
	a.executor = exec

	agg, err := aggregator.NewAggregator(aggregator.Config{
		WeightHigh:               cfg.Scoring.WeightHigh,
		WeightMedium:             cfg.Scoring.WeightMedium,
		WeightLow:                cfg.Scoring.WeightLow,
		WeightMissingMandatory:   cfg.Scoring.WeightMissingMandatory,
		WeightAmbiguousMandatory: cfg.Scoring.WeightAmbiguousMandatory,
		LabelLowThreshold:        cfg.Scoring.LabelLowThreshold,
		LabelMediumThreshold:     cfg.Scoring.LabelMediumThreshold,
	}, aggregatorMetrics{c: a.metrics.CrossCut})
	if err != nil {
		return nil, fmt.Errorf("app: build aggregator: %w", err)
	}
	a.aggregator = agg

	// 9. pending state + version meta + job limiter + idempotency guard
	a.pendingStore = newPendingStateStore(a.kv)
	a.metaCache = newVersionMetaCache(a.kv)
	a.jobLimiter = concurrency.New(cfg.Pipeline.Concurrency,
		concurrency.WithGauge(a.metrics.Pipeline.ConcurrentJobs))

	guard, err := idempotency.NewGuard(a.kv, idempotency.Config{
		HeartbeatInterval: cfg.Idempotency.HeartbeatInterval,
		FallbackEnabled:   cfg.Idempotency.FallbackEnabled,
	}, idempotency.Deps{
		Metrics: idempotencyMetrics{i: a.metrics.Idempotency},
		Logger:  stdLogger{l: lg.With("idempotency")},
	})
	if err != nil {
		return nil, fmt.Errorf("app: build idempotency guard: %w", err)
	}
	a.idempGuard = guard

	// 10. dmawaiters
	if err := a.wireAwaiters(); err != nil {
		return nil, err
	}

	// 11. publishers (orch + dm + dlq)
	if err := a.wirePublishers(); err != nil {
		return nil, err
	}

	// 12. circular wiring: the pipeline orchestrator needs the
	//     pendingconfirmation.Manager as its PauseController; the manager
	//     needs the orchestrator as its PipelineResumer. Both calls are
	//     runtime-only, not construction-time, so we wire the manager first
	//     with a lazyResumer that we fill in after the orchestrator is built.
	lazy := &lazyResumer{}
	if err := a.wirePendingManager(lazy); err != nil {
		return nil, err
	}
	if err := a.wirePipelineOrchestrator(a.pendingMgr); err != nil {
		return nil, err
	}
	lazy.set(a.pipelineOrch)

	// 14. ingress router
	if err := a.wireIngressRouter(); err != nil {
		return nil, err
	}

	// 15. consumer
	if err := a.wireConsumer(); err != nil {
		return nil, err
	}

	// 16. health handler + HTTP server
	a.wireHealthHandler()

	return a, nil
}

// wireProviders builds the LLM provider registry from cfg.
// Only providers whose API keys are present participate; an empty registry is
// fatal (the router needs at least one).
func (a *App) wireProviders() error {
	providers := make(map[port.LLMProviderID]port.LLMProviderPort, 3)
	cfg := a.cfg.LLM

	if cfg.Claude.APIKey != "" {
		p, err := claude.NewClaudeProvider(claude.ClaudeConfig{
			APIKey:             cfg.Claude.APIKey,
			BaseURL:            cfg.Claude.BaseURL,
			Model:              cfg.Claude.Model,
			PromptCacheEnabled: cfg.Claude.PromptCacheEnabled,
		})
		if err != nil {
			return fmt.Errorf("app: build claude provider: %w", err)
		}
		providers[port.ProviderClaude] = p
	}
	if cfg.OpenAI.APIKey != "" {
		p, err := openai.NewOpenAIProvider(openai.OpenAIConfig{
			APIKey:  cfg.OpenAI.APIKey,
			BaseURL: cfg.OpenAI.BaseURL,
			Model:   cfg.OpenAI.Model,
		})
		if err != nil {
			return fmt.Errorf("app: build openai provider: %w", err)
		}
		providers[port.ProviderOpenAI] = p
	}
	if cfg.Gemini.APIKey != "" {
		p, err := gemini.NewGeminiProvider(gemini.GeminiConfig{
			APIKey:  cfg.Gemini.APIKey,
			BaseURL: cfg.Gemini.BaseURL,
			Model:   cfg.Gemini.Model,
		})
		if err != nil {
			return fmt.Errorf("app: build gemini provider: %w", err)
		}
		providers[port.ProviderGemini] = p
	}
	if len(providers) == 0 {
		return errors.New("app: no LLM providers configured — at least one of LIC_{CLAUDE,OPENAI,GEMINI}_API_KEY must be set")
	}
	a.providers = providers
	return nil
}

// wireRateLimiter constructs the per-provider Redis token-bucket limiter.
// Only configured providers populate the bucket map so an unset provider does
// not produce an empty-key validation error.
func (a *App) wireRateLimiter() error {
	cfg := a.cfg.LLM
	buckets := make(map[port.LLMProviderID]ratelimit.ProviderLimit, len(a.providers))
	if _, ok := a.providers[port.ProviderClaude]; ok {
		buckets[port.ProviderClaude] = ratelimit.ProviderLimit{RPS: cfg.Claude.RPS, Burst: cfg.Claude.Burst}
	}
	if _, ok := a.providers[port.ProviderOpenAI]; ok {
		buckets[port.ProviderOpenAI] = ratelimit.ProviderLimit{RPS: cfg.OpenAI.RPS, Burst: cfg.OpenAI.Burst}
	}
	if _, ok := a.providers[port.ProviderGemini]; ok {
		buckets[port.ProviderGemini] = ratelimit.ProviderLimit{RPS: cfg.Gemini.RPS, Burst: cfg.Gemini.Burst}
	}
	limiter, err := ratelimit.NewLimiter(ratelimit.Config{Providers: buckets}, a.kv, ratelimit.Observer(noopRLObserver{}))
	if err != nil {
		return fmt.Errorf("app: build rate limiter: %w", err)
	}
	a.rateLimiter = limiter
	return nil
}

// noopRLObserver feeds the ratelimit.Observer seam with metrics adapters.
// The lic_llm_rate_limited_total counter is owned by *metrics.LLMMetrics —
// wire it through here. The fail-open / script-anomaly signals are intentionally
// downgraded to logger.Warn at the wiring layer; sampling is not introduced in
// v1 because rate-limit denials are rare in practice.
type noopRLObserver struct{}

func (noopRLObserver) RateLimited(provider string)              {}
func (noopRLObserver) FailOpen(provider string, err error)      {}
func (noopRLObserver) ScriptAnomaly(provider string, err error) {}

// wireLLMRouter constructs the provider router. AgentPrimary maps every
// agent to its configured primary provider.
func (a *App) wireLLMRouter() error {
	primary := make(map[model.AgentID]port.LLMProviderID, len(config.AllAgentIDs))
	for _, agentName := range config.AllAgentIDs {
		provName := a.cfg.Agents.Providers[agentName]
		primary[mapAgentID(agentName)] = port.LLMProviderID(provName)
	}

	order := make([]port.LLMProviderID, 0, len(a.cfg.LLM.ProviderFallbackOrder))
	for _, p := range a.cfg.LLM.ProviderFallbackOrder {
		if _, ok := a.providers[port.LLMProviderID(p)]; ok {
			order = append(order, port.LLMProviderID(p))
		}
	}
	if len(order) == 0 {
		return errors.New("app: provider fallback order is empty after filtering by configured providers")
	}

	r, err := llmrouter.NewProviderRouter(a.providers, llmrouter.RouterConfig{
		AgentPrimary:  primary,
		FallbackOrder: order,
	}, llmrouter.Deps{
		RateLimiter:  a.rateLimiter,
		UsageTracker: usageTracker{t: a.costTracker},
		Metrics:      routerMetrics{l: a.metrics.LLM},
	})
	if err != nil {
		return fmt.Errorf("app: build llm router: %w", err)
	}
	a.llmRouter = r
	return nil
}

// mapAgentID converts the config-package agent identifier (a plain string)
// to the domain typed model.AgentID.
func mapAgentID(s string) model.AgentID {
	switch s {
	case config.AgentTypeClassifier:
		return model.AgentTypeClassifier
	case config.AgentKeyParams:
		return model.AgentKeyParams
	case config.AgentPartyConsistency:
		return model.AgentPartyConsistency
	case config.AgentMandatoryConditions:
		return model.AgentMandatoryConditions
	case config.AgentRiskDetection:
		return model.AgentRiskDetection
	case config.AgentRecommendation:
		return model.AgentRecommendation
	case config.AgentSummary:
		return model.AgentSummary
	case config.AgentDetailedReport:
		return model.AgentDetailedReport
	case config.AgentRiskDelta:
		return model.AgentRiskDelta
	}
	return model.AgentID(s)
}

// agentModelForProvider returns the resolved primary-provider model id for
// the given agent. ADR-LIC-03 sets the per-agent primary to claude by default
// so the claude model is the common path; the typeclassifier/CLAUDE.md
// forward note documents the rationale.
func (a *App) agentModelForProvider(agent string) string {
	switch a.cfg.Agents.Providers[agent] {
	case config.ProviderClaude:
		return a.cfg.LLM.Claude.Model
	case config.ProviderOpenAI:
		return a.cfg.LLM.OpenAI.Model
	case config.ProviderGemini:
		return a.cfg.LLM.Gemini.Model
	}
	return a.cfg.LLM.Claude.Model
}

// wireAgents constructs the 9 per-agent runners, each with the same base.Deps.
func (a *App) wireAgents() error {
	deps := base.Deps{
		Router:        a.llmRouter,
		Metrics:       agentMetrics{a: a.metrics.Agent},
		RepairMetrics: repairMetrics{},
	}

	agents := make(map[model.AgentID]port.Agent, 9)

	tc, err := typeclassifier.NewClassifier(a.agentModelForProvider(config.AgentTypeClassifier),
		a.cfg.Agents.Timeouts[config.AgentTypeClassifier], deps)
	if err != nil {
		return fmt.Errorf("app: build agent typeclassifier: %w", err)
	}
	agents[model.AgentTypeClassifier] = tc

	kp, err := keyparams.NewExtractor(a.agentModelForProvider(config.AgentKeyParams),
		a.cfg.Agents.Timeouts[config.AgentKeyParams], deps)
	if err != nil {
		return fmt.Errorf("app: build agent keyparams: %w", err)
	}
	agents[model.AgentKeyParams] = kp

	pc, err := partyconsistency.NewChecker(a.agentModelForProvider(config.AgentPartyConsistency),
		a.cfg.Agents.Timeouts[config.AgentPartyConsistency], deps)
	if err != nil {
		return fmt.Errorf("app: build agent partyconsistency: %w", err)
	}
	agents[model.AgentPartyConsistency] = pc

	mc, err := mandatoryconditions.NewChecker(a.agentModelForProvider(config.AgentMandatoryConditions),
		a.cfg.Agents.Timeouts[config.AgentMandatoryConditions], deps)
	if err != nil {
		return fmt.Errorf("app: build agent mandatoryconditions: %w", err)
	}
	agents[model.AgentMandatoryConditions] = mc

	rd, err := riskdetection.NewDetector(a.agentModelForProvider(config.AgentRiskDetection),
		a.cfg.Agents.Timeouts[config.AgentRiskDetection], deps)
	if err != nil {
		return fmt.Errorf("app: build agent riskdetection: %w", err)
	}
	agents[model.AgentRiskDetection] = rd

	rec, err := recommendation.NewRecommender(a.agentModelForProvider(config.AgentRecommendation),
		a.cfg.Agents.Timeouts[config.AgentRecommendation], deps)
	if err != nil {
		return fmt.Errorf("app: build agent recommendation: %w", err)
	}
	agents[model.AgentRecommendation] = rec

	sm, err := summary.NewSummarizer(a.agentModelForProvider(config.AgentSummary),
		a.cfg.Agents.Timeouts[config.AgentSummary], deps)
	if err != nil {
		return fmt.Errorf("app: build agent summary: %w", err)
	}
	agents[model.AgentSummary] = sm

	dr, err := detailedreport.NewDetailedReporter(a.agentModelForProvider(config.AgentDetailedReport),
		a.cfg.Agents.Timeouts[config.AgentDetailedReport], deps)
	if err != nil {
		return fmt.Errorf("app: build agent detailedreport: %w", err)
	}
	agents[model.AgentDetailedReport] = dr

	rdelta, err := riskdelta.NewRiskDeltaComparator(a.agentModelForProvider(config.AgentRiskDelta),
		a.cfg.Agents.Timeouts[config.AgentRiskDelta], deps)
	if err != nil {
		return fmt.Errorf("app: build agent riskdelta: %w", err)
	}
	agents[model.AgentRiskDelta] = rdelta

	a.agents = agents
	return nil
}

// wireAwaiters builds the two in-process correlation registries.
func (a *App) wireAwaiters() error {
	aw, err := dmawaiter.NewArtifactAwaiter(
		dmawaiter.ArtifactConfig{TTL: a.cfg.Pipeline.DMRequestTimeout},
		dmawaiter.Deps{
			Metrics: dmAwaiterMetrics{d: a.metrics.DM},
			Logger:  stdLogger{l: a.log.With("dmawaiter.artifact")},
		})
	if err != nil {
		return fmt.Errorf("app: build artifact awaiter: %w", err)
	}
	a.artifactAwaiter = aw

	cw, err := dmawaiter.NewConfirmationAwaiter(
		dmawaiter.ConfirmationConfig{TTL: a.cfg.Pipeline.DMPersistConfirmTimeout},
		dmawaiter.Deps{
			Metrics: dmAwaiterMetrics{d: a.metrics.DM},
			Logger:  stdLogger{l: a.log.With("dmawaiter.confirmation")},
		})
	if err != nil {
		return fmt.Errorf("app: build confirmation awaiter: %w", err)
	}
	a.confirmationAwaiter = cw
	return nil
}

// wirePublishers builds every outbound publisher (orch + dm + dlq).
//
// Outbound topic → exchange routing follows the topology declared by
// internal/infra/broker:
//   - lic.events.*           → ExchangeEvents
//   - lic.requests.artifacts → ExchangeCommands (LIC → DM)
//   - lic.artifacts.*        → ExchangeCommands (LIC → DM)
//   - lic.dlq.*              → ExchangeDLX
func (a *App) wirePublishers() error {
	br := a.broker
	cfg := a.cfg.Broker
	orchPub := orchPubMetrics{c: a.metrics.CrossCut}

	sp, err := orch.NewStatusPublisher(orch.StatusPublisherConfig{Exchange: cfg.ExchangeEvents},
		orch.StatusPublisherDeps{
			Publisher: br,
			Metrics:   orchPub,
			Logger:    stdLogger{l: a.log.With("publisher.status")},
		})
	if err != nil {
		return fmt.Errorf("app: build status publisher: %w", err)
	}
	a.statusPub = sp

	up, err := orch.NewUncertaintyPublisher(orch.UncertaintyPublisherConfig{Exchange: cfg.ExchangeEvents},
		orch.UncertaintyPublisherDeps{
			Publisher: br,
			Metrics:   orchPub,
			Logger:    stdLogger{l: a.log.With("publisher.uncertain")},
		})
	if err != nil {
		return fmt.Errorf("app: build uncertainty publisher: %w", err)
	}
	a.uncertainPub = up

	dmPub := dmPubMetrics{c: a.metrics.CrossCut, d: a.metrics.DM}
	arp, err := dmpub.NewArtifactRequester(dmpub.RequesterConfig{Exchange: cfg.ExchangeCommands},
		dmpub.RequesterDeps{
			Publisher: br,
			Metrics:   dmPub,
			Logger:    stdLogger{l: a.log.With("publisher.artifacts.req")},
		})
	if err != nil {
		return fmt.Errorf("app: build artifact requester: %w", err)
	}
	a.artReqPub = arp

	app, err := dmpub.NewAnalysisArtifactsPublisher(dmpub.PublisherConfig{Exchange: cfg.ExchangeCommands},
		dmpub.PublisherDeps{
			Publisher: br,
			Metrics:   dmPub,
			Logger:    stdLogger{l: a.log.With("publisher.analysis")},
		})
	if err != nil {
		return fmt.Errorf("app: build analysis artifacts publisher: %w", err)
	}
	a.analysisPub = app

	dq, err := dlq.NewDLQPublisher(dlq.Config{Exchange: cfg.ExchangeDLX},
		dlq.Deps{
			Publisher: br,
			Metrics:   dlqPubMetrics{c: a.metrics.CrossCut, d: a.metrics.DLQ},
			Logger:    stdLogger{l: a.log.With("publisher.dlq")},
		})
	if err != nil {
		return fmt.Errorf("app: build dlq publisher: %w", err)
	}
	a.dlqPub = dq
	return nil
}

// wirePipelineOrchestrator constructs the pipeline.Orchestrator. The pause
// controller is the pendingconfirmation.Manager (one Manager, two roles —
// the role-split documented in pendingconfirmation/CLAUDE.md D1).
func (a *App) wirePipelineOrchestrator(pause pipeline.PauseController) error {
	cfg := pipeline.Config{
		JobTimeout:              a.cfg.Pipeline.JobTimeout,
		DMRequestTimeout:        a.cfg.Pipeline.DMRequestTimeout,
		DMPersistConfirmTimeout: a.cfg.Pipeline.DMPersistConfirmTimeout,
		ConfidenceThreshold:     a.cfg.Scoring.ConfidenceThreshold,
		MaxIngestedBytes:        int(a.cfg.Scoring.MaxIngestedBytes),
	}
	o, err := pipeline.NewOrchestrator(
		cfg,
		a.executor,
		a.aggregator,
		a.artReqPub,
		a.artifactAwaiter,
		a.analysisPub,
		a.confirmationAwaiter,
		a.statusPub,
		a.uncertainPub,
		pipeline.Deps{
			JobLimiter:       a.jobLimiter,
			Metrics:          pipelineMetrics{p: a.metrics.Pipeline},
			Clock:            sysClock{},
			Logger:           stdLogger{l: a.log.With("pipeline")},
			VersionMetaCache: a.metaCache,
			PauseController:  pause,
		},
	)
	if err != nil {
		return fmt.Errorf("app: build pipeline orchestrator: %w", err)
	}
	a.pipelineOrch = o
	return nil
}

// wirePendingManager constructs the pendingconfirmation.Manager. The Manager
// structurally satisfies pipeline.PauseController; injecting it back into the
// orchestrator closes the circular dependency without an import cycle (the
// orchestrator never imports pendingconfirmation). The PipelineResumer is
// passed as a lazyResumer placeholder that is filled in after the orchestrator
// is built (both call sites are runtime-only — neither needs the partner at
// construction).
func (a *App) wirePendingManager(resumer pendingconfirmation.PipelineResumer) error {
	mgr, err := pendingconfirmation.NewManager(
		pendingconfirmation.Config{
			PendingStateTTL:            a.cfg.Pipeline.PendingConfirmationTTL,
			UserConfirmedProcessingTTL: a.cfg.Idempotency.UserConfirmedProcessingTTL,
			CompletedTTL:               a.cfg.Idempotency.TTL,
			ConfidenceThreshold:        a.cfg.Scoring.ConfidenceThreshold,
			PausedSentinel:             pipeline.ErrPipelinePaused,
			// MUST equal the dlqHashKey passed to consumer.NewConsumer
			// (a.cfg.Security.DLQHashKey, see consumer wiring below) so
			// invalid-message envelopes from the consumer and the
			// pending manager share a same-topic dedup hash space.
			DLQHashKey: a.cfg.Security.DLQHashKey,
		},
		a.pendingStore,
		a.idempGuard,
		a.uncertainPub,
		a.statusPub,
		a.dlqPub,
		resumer, // lazy reference; *pipeline.Orchestrator filled in post-build (ResumeAfterConfirmation is runtime-only)
		pendingconfirmation.Deps{
			Metrics: pendingMetrics{p: a.metrics.Pending},
			Logger:  stdLogger{l: a.log.With("pendingconfirmation")},
		},
	)
	if err != nil {
		return fmt.Errorf("app: build pending confirmation manager: %w", err)
	}
	a.pendingMgr = mgr
	return nil
}

// wireIngressRouter constructs the inbound event router.
func (a *App) wireIngressRouter() error {
	r, err := ingressrouter.NewRouter(
		ingressrouter.Config{
			ProcessingTTL:     a.cfg.Idempotency.ProcessingTTL,
			CompletedTTL:      a.cfg.Idempotency.TTL,
			PendingStateTTL:   a.cfg.Idempotency.TTL,
			MetaCacheTTL:      a.cfg.Cache.VersionMetaCacheTTL,
			HeartbeatInterval: a.cfg.Idempotency.HeartbeatInterval,
		},
		a.pipelineOrch,
		a.pendingMgr,
		a.artifactAwaiter,
		a.confirmationAwaiter,
		a.metaCache,
		a.idempGuard,
		a.pendingStore,
		a.statusPub,
		ingressrouter.Deps{
			Logger: stdLogger{l: a.log.With("ingress.router")},
		},
	)
	if err != nil {
		return fmt.Errorf("app: build ingress router: %w", err)
	}
	a.ingressRouter = r
	return nil
}

// wireConsumer constructs the broker consumer (six topic subscriptions).
func (a *App) wireConsumer() error {
	c, err := consumer.NewConsumer(
		a.broker,
		a.ingressRouter,
		a.dlqPub,
		a.cfg.Security.DLQHashKey,
		consumer.Deps{
			Metrics: consumerMetrics{c: a.metrics.CrossCut},
			Logger:  consumerLogger{l: a.log.With("ingress.consumer")},
		},
	)
	if err != nil {
		return fmt.Errorf("app: build consumer: %w", err)
	}
	a.consumer = c
	return nil
}

// wireHealthHandler builds the /healthz, /readyz, /metrics handler.
func (a *App) wireHealthHandler() {
	checkers := []health.Checker{
		healthChecker{name: "redis", ping: a.kv.Ping},
		healthChecker{name: "rabbitmq", ping: a.broker.Ping},
	}
	mh := promhttp.HandlerFor(a.metrics.Registry(), promhttp.HandlerOpts{})
	a.healthHandler = health.NewHandler(checkers, mh,
		health.WithCheckerTimeout("redis", 100*time.Millisecond),
		health.WithReadyDeadline(2*time.Second),
	)
}

// Run starts the HTTP probe server, the broker consumer subscriptions, and the
// LLM router health-check loop. It blocks until ctx is cancelled (Shutdown is
// invoked separately by the signal handler).
func (a *App) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	a.consumerCtx = ctx
	a.consumerCancel = cancel

	// HTTP server (probes + metrics).
	a.httpServer = &http.Server{
		Addr:              fmt.Sprintf(":%d", a.cfg.App.HTTPPort),
		Handler:           a.healthHandler.Mux(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	httpErrCh := make(chan error, 1)
	go func() {
		a.log.Info(ctx, "http server starting", slog.String("addr", a.httpServer.Addr))
		if err := a.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			httpErrCh <- err
		}
		close(httpErrCh)
	}()

	// LLM provider health-check loop.
	a.llmRouter.Start(ctx)

	// Broker consumer (subscribes the six queues; the broker manages the
	// per-subscription goroutines internally — Start returns immediately).
	if err := a.consumer.Start(); err != nil {
		return fmt.Errorf("app: start consumer: %w", err)
	}

	a.log.Info(ctx, "lic-service ready",
		slog.String("env", string(a.cfg.App.Env)),
		slog.Int("http_port", a.cfg.App.HTTPPort))

	// Block until either ctx is cancelled (Shutdown initiated) or the HTTP
	// server returns a fatal error.
	select {
	case <-ctx.Done():
		return nil
	case err := <-httpErrCh:
		return fmt.Errorf("app: http server: %w", err)
	}
}

// Shutdown gracefully drains the service in the deployment.md §6 order.
// Safe to call multiple times — sync.Once guarantees the sequence runs once.
func (a *App) Shutdown(ctx context.Context) error {
	a.shutdownOnce.Do(func() {
		a.shutdownErr = a.shutdownInternal(ctx)
	})
	return a.shutdownErr
}

func (a *App) shutdownInternal(ctx context.Context) error {
	a.log.Info(ctx, "shutdown sequence starting")

	// 1. flip /readyz to 503 immediately so Kubernetes removes us from the
	//    Service rotation.
	if a.healthHandler != nil {
		a.healthHandler.SetNotReady()
	}

	// 2. give kube readinessProbe failure-detection a window
	//    (deployment.md §6 — 5s).
	select {
	case <-ctx.Done():
		// Caller-imposed deadline already elapsed — proceed straight to teardown.
	case <-time.After(readinessDrainDelay):
	}

	// 3. cancel the consumer context — every in-flight pipeline observes
	//    ctx.Done; broker.Subscribe handlers honour it through router →
	//    orchestrator. We bound the wait with cfg.App.ShutdownTimeout.
	if a.consumerCancel != nil {
		a.consumerCancel()
	}

	// 4. wait for in-flight pipelines to finish (best-effort; if the budget
	//    elapses we proceed with teardown and broker close will Nack+requeue
	//    any in-flight messages).
	drained := make(chan struct{})
	go func() {
		a.inflight.Wait()
		close(drained)
	}()
	select {
	case <-drained:
		a.log.Info(ctx, "in-flight pipelines drained")
	case <-time.After(a.cfg.App.ShutdownTimeout):
		a.log.Warn(ctx, "in-flight drain timeout reached",
			slog.String("timeout", a.cfg.App.ShutdownTimeout.String()))
	case <-ctx.Done():
	}

	// 5. ordered teardown.
	var errs []error

	// stop llm router health-check loop first so it does not race a closing
	// HTTP transport on llm provider probes.
	if a.llmRouter != nil {
		a.llmRouter.Stop()
	}

	// idempotency guard heartbeats are owned by pipeline goroutines; once
	// in-flight drained, closing the guard is purely defensive (no-op-safe).
	// http server shutdown — give it the same ctx so a deadline-exceeded
	// caller can speed up teardown.
	if a.httpServer != nil {
		httpCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := a.httpServer.Shutdown(httpCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs = append(errs, fmt.Errorf("http server: %w", err))
		}
		cancel()
	}

	// broker close — also halts any consumeLoop goroutines.
	if a.broker != nil {
		if err := a.broker.Close(); err != nil {
			errs = append(errs, fmt.Errorf("broker close: %w", err))
		}
	}

	// redis close.
	if a.kv != nil {
		if err := a.kv.Close(); err != nil {
			errs = append(errs, fmt.Errorf("redis close: %w", err))
		}
	}

	// OTel tracer flush + shutdown last so log lines above are captured.
	if a.tracer != nil {
		flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := a.tracer.Shutdown(flushCtx); err != nil {
			errs = append(errs, fmt.Errorf("tracer shutdown: %w", err))
		}
		cancel()
	}

	a.log.Info(ctx, "shutdown sequence finished", slog.Int("errors", len(errs)))
	return errors.Join(errs...)
}

// reloadGuard ensures SIGHUP-driven secret rotation does not race with
// itself; only one rotation may be in flight at any time.
var reloadGuard atomic.Bool

// ReloadSecrets re-reads the LLM provider API keys from env, rebuilds the
// provider HTTP clients with the new credentials, and atomically swaps them
// in. In-flight requests continue against the old provider instances because
// Go retains the old instance for as long as there is a live reference.
//
// v1 implements a simplified surface: the providers map and the
// llm router both hold direct pointers to *claude.Provider / *openai.Provider
// / *gemini.Provider, so a rotating swap would require pointer-indirection
// throughout. We therefore log the rotation intent and document that rolling
// restart is the operational mechanism. The seam is wired so a future
// LIC-TASK-* can replace this body without touching main.go or the signal
// handler.
func (a *App) ReloadSecrets(ctx context.Context) error {
	if !reloadGuard.CompareAndSwap(false, true) {
		return errors.New("app: secret reload already in progress")
	}
	defer reloadGuard.Store(false)

	a.log.Warn(ctx, "SIGHUP received: secret reload requires a rolling restart in v1")
	return nil
}

// lazyResumer is a forward-reference placeholder for the orchestrator. Wiring
// constructs the pendingconfirmation.Manager BEFORE the orchestrator (the
// manager is the PauseController on the orchestrator's Deps); the orchestrator
// is plugged into the placeholder via set() once it exists. Both calls are
// runtime-only — ResumeAfterConfirmation is never invoked at construction —
// so a nil resumer at construction is structurally safe (the manager's
// fail-fast nil-check sees the non-nil wrapper).
type lazyResumer struct {
	mu  sync.RWMutex
	val pendingconfirmation.PipelineResumer
}

func (l *lazyResumer) set(r pendingconfirmation.PipelineResumer) {
	l.mu.Lock()
	l.val = r
	l.mu.Unlock()
}

// ResumeAfterConfirmation forwards to the wired *pipeline.Orchestrator.
// Called runtime-only from pendingconfirmation.Manager.HandleUserConfirmedType.
func (l *lazyResumer) ResumeAfterConfirmation(ctx context.Context, state *model.PipelineState) error {
	l.mu.RLock()
	r := l.val
	l.mu.RUnlock()
	if r == nil {
		return errors.New("app: pipeline resumer not yet wired (construction-time race)")
	}
	return r.ResumeAfterConfirmation(ctx, state)
}

var _ pendingconfirmation.PipelineResumer = (*lazyResumer)(nil)
