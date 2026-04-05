package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"contractpro/document-management/internal/application/diff"
	"contractpro/document-management/internal/application/ingestion"
	"contractpro/document-management/internal/application/lifecycle"
	"contractpro/document-management/internal/application/orphancleanup"
	"contractpro/document-management/internal/application/query"
	"contractpro/document-management/internal/application/retention"
	"contractpro/document-management/internal/application/version"
	"contractpro/document-management/internal/application/watchdog"
	"contractpro/document-management/internal/config"
	"contractpro/document-management/internal/egress/confirmation"
	"contractpro/document-management/internal/egress/dlq"
	"contractpro/document-management/internal/egress/outbox"
	"contractpro/document-management/internal/infra/circuitbreaker"
	"contractpro/document-management/internal/infra/concurrency"
	"contractpro/document-management/internal/infra/broker"
	"contractpro/document-management/internal/infra/health"
	"contractpro/document-management/internal/infra/kvstore"
	"contractpro/document-management/internal/infra/objectstorage"
	"contractpro/document-management/internal/infra/observability"
	"contractpro/document-management/internal/infra/postgres"
	"contractpro/document-management/internal/ingress/api"
	"contractpro/document-management/internal/ingress/consumer"
	"contractpro/document-management/internal/ingress/idempotency"

	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
)

// Compile-time interface checks for adapters defined in this file.
var (
	_ port.AuditPort            = (*auditPortAdapter)(nil)
	_ consumer.BrokerSubscriber = (*poolSubscribeAdapter)(nil)
	_ port.OutboxRepository     = (*poolOutboxRepository)(nil)
	_ port.DLQRepository        = (*poolDLQRepository)(nil)
	_ port.VersionRepository    = (*poolVersionRepository)(nil)
	_ port.ArtifactRepository   = (*poolArtifactRepository)(nil)
	_ port.AuditRepository              = (*poolAuditRepository)(nil)
	_ port.OrphanCandidateRepository    = (*poolOrphanCandidateRepository)(nil)
	_ port.DocumentRepository           = (*poolDocumentRepository)(nil)
	_ port.DiffRepository               = (*poolDiffRepository)(nil)
	_ port.AuditPartitionManager        = (*poolAuditPartitionManager)(nil)
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// -----------------------------------------------------------------------
	// Phase 1: Configuration
	// -----------------------------------------------------------------------
	cfg, err := config.Load()
	if err != nil {
		log.Printf("config: %v", err)
		return 1
	}

	// -----------------------------------------------------------------------
	// Phase 2: Observability (Logger + Metrics + Tracer)
	// -----------------------------------------------------------------------
	obs, err := observability.New(ctx, cfg.Observability)
	if err != nil {
		log.Printf("observability: %v", err)
		return 1
	}
	logger := obs.Logger.With("component", "app")

	// -----------------------------------------------------------------------
	// Phase 3: PostgreSQL + migrations
	// -----------------------------------------------------------------------
	pgClient, err := postgres.NewPostgresClient(ctx, cfg.Database)
	if err != nil {
		logger.Error("postgres connect failed", "error", err)
		_ = obs.Shutdown(ctx)
		return 1
	}

	migrator, err := postgres.NewMigrator(cfg.Database.DSN)
	if err != nil {
		logger.Error("migrator init failed", "error", err)
		_ = pgClient.Close()
		_ = obs.Shutdown(ctx)
		return 1
	}
	if err := migrator.Up(); err != nil {
		logger.Error("migration up failed", "error", err)
		_ = migrator.Close()
		_ = pgClient.Close()
		_ = obs.Shutdown(ctx)
		return 1
	}
	_ = migrator.Close()
	logger.Info("database migrations applied")

	// -----------------------------------------------------------------------
	// Phase 4: Redis
	// -----------------------------------------------------------------------
	kvClient, err := kvstore.NewClient(cfg.KVStore)
	if err != nil {
		logger.Error("redis connect failed", "error", err)
		_ = pgClient.Close()
		_ = obs.Shutdown(ctx)
		return 1
	}

	// -----------------------------------------------------------------------
	// Phase 5: RabbitMQ + topology (with consumer backpressure — BRE-007)
	// -----------------------------------------------------------------------
	if cfg.Consumer.Prefetch < cfg.Consumer.Concurrency {
		logger.Warn("DM_CONSUMER_PREFETCH < DM_CONSUMER_CONCURRENCY: some concurrency slots will always be idle",
			"prefetch", cfg.Consumer.Prefetch,
			"concurrency", cfg.Consumer.Concurrency,
		)
	}

	consumerLimiter := concurrency.NewSemaphore(
		cfg.Consumer.Concurrency,
		obs.Logger.With("component", "concurrency-limiter"),
	)

	brokerClient, err := broker.NewClient(cfg.Broker, cfg.Consumer, consumerLimiter)
	if err != nil {
		logger.Error("broker connect failed", "error", err)
		_ = kvClient.Close()
		_ = pgClient.Close()
		_ = obs.Shutdown(ctx)
		return 1
	}
	if err := brokerClient.DeclareTopology(); err != nil {
		logger.Error("broker topology failed", "error", err)
		_ = brokerClient.Close()
		_ = kvClient.Close()
		_ = pgClient.Close()
		_ = obs.Shutdown(ctx)
		return 1
	}

	// -----------------------------------------------------------------------
	// Phase 6: Object Storage (with circuit breaker — BRE-014)
	// -----------------------------------------------------------------------
	rawObjClient := objectstorage.NewClient(cfg.Storage)
	objClient := circuitbreaker.NewObjectStorageBreaker(
		rawObjClient,
		cfg.CircuitBreaker,
		obs.Metrics,
	)

	// -----------------------------------------------------------------------
	// Phase 7: Transactor + Repositories
	// -----------------------------------------------------------------------
	pool := pgClient.Pool()
	transactor := postgres.NewTransactor(pool)
	docRepo := postgres.NewDocumentRepository()
	versionRepo := postgres.NewVersionRepository()
	artifactRepo := postgres.NewArtifactRepository()
	diffRepo := postgres.NewDiffRepository()
	auditRepo := postgres.NewAuditRepository()
	outboxRepo := postgres.NewOutboxRepository()

	// Pool-injecting wrapper for outbox repository: the outbox poller's
	// cleanup() and the metrics collector's collect() create their own
	// context.Background() without a pool — ConnFromCtx would panic.
	// This wrapper ensures every repository call has the pool available.
	poolOutboxRepo := &poolOutboxRepository{inner: outboxRepo, pool: pool}

	// -----------------------------------------------------------------------
	// Phase 8: Outbox Writer
	// -----------------------------------------------------------------------
	outboxWriter := outbox.NewOutboxWriter(outboxRepo)

	// DLQ repository (for replay persistence).
	dlqRepo := postgres.NewDLQRepository()
	poolDLQRepo := &poolDLQRepository{inner: dlqRepo, pool: pool}

	// -----------------------------------------------------------------------
	// Phase 9: Confirmation Publisher (used by query service for direct publish)
	// Notification events go through the outbox, so no NotificationPublisher is needed here.
	// -----------------------------------------------------------------------
	confirmPub := confirmation.NewConfirmationPublisher(brokerClient, cfg.Broker)

	// DLQ Sender — publishes failed messages to DLQ topics and persists to DB.
	dlqSender := dlq.NewSender(
		brokerClient, poolDLQRepo, obs.Metrics,
		obs.Logger.With("component", "dlq"),
		cfg.Broker.TopicDMDLQIngestionFailed,
		cfg.Broker.TopicDMDLQQueryFailed,
		cfg.Broker.TopicDMDLQInvalidMessage,
	)

	// -----------------------------------------------------------------------
	// Phase 10: Idempotency Guard
	// -----------------------------------------------------------------------
	idemGuard := idempotency.NewIdempotencyGuard(
		kvClient, cfg.Idempotency,
		obs.Metrics,
		obs.Logger.With("component", "idempotency"),
	)

	// -----------------------------------------------------------------------
	// Phase 11: Application Services
	// -----------------------------------------------------------------------

	// TEMPORARY: fallback resolver for REV-001/REV-002 — cross-tenant document
	// lookup when producer domains omit organization_id or version_id in events.
	// Remove when DP TASK-056 and TASK-057 are completed.
	fallbackResolver := postgres.NewFallbackResolver()

	// BRE-008 / DM-TASK-047: orphan candidate repository — used by ingestion
	// and diff services to register orphan blob candidates on compensation.
	// The pool-injecting wrapper is needed because registerOrphanCandidates
	// uses context.Background() (no pgxpool in context).
	orphanCandidateRepo := postgres.NewOrphanCandidateRepository()
	poolOrphanCandidateRepo := &poolOrphanCandidateRepository{inner: orphanCandidateRepo, pool: pool}

	ingestionSvc := ingestion.NewArtifactIngestionService(
		transactor, versionRepo, artifactRepo, auditRepo, objClient, outboxWriter,
		fallbackResolver, obs.Metrics,
		docRepo, obs.Metrics,
		poolOrphanCandidateRepo,
		obs.Logger.With("component", "ingestion"),
		cfg.Ingestion.MaxJSONArtifactBytes,
		cfg.Ingestion.MaxBlobSizeBytes,
	)

	querySvc := query.NewArtifactQueryService(
		artifactRepo, objClient, confirmPub, auditRepo,
		fallbackResolver,
		docRepo, obs.Metrics, obs.Metrics,
		obs.Logger.With("component", "query"),
	)

	lifecycleSvc := lifecycle.NewDocumentLifecycleService(
		transactor, docRepo, auditRepo,
		obs.Logger.With("component", "lifecycle"),
	)

	versionSvc := version.NewVersionManagementService(
		transactor, docRepo, versionRepo, auditRepo, outboxWriter,
		obs.Logger.With("component", "version"),
	)

	diffSvc := diff.NewDiffStorageService(
		transactor, versionRepo, diffRepo, auditRepo, objClient, outboxWriter,
		fallbackResolver,
		docRepo, obs.Metrics,
		poolOrphanCandidateRepo,
		obs.Logger.With("component", "diff"),
	)

	// -----------------------------------------------------------------------
	// Phase 11.5: Stale Version Watchdog (DM-TASK-041)
	// -----------------------------------------------------------------------
	// The watchdog creates context.Background() in its scan goroutine, so
	// repositories need pool-injecting wrappers (same pattern as poolOutboxRepo).
	poolVersionRepo := &poolVersionRepository{inner: versionRepo, pool: pool}
	poolArtifactRepo := &poolArtifactRepository{inner: artifactRepo, pool: pool}
	poolAuditRepo := &poolAuditRepository{inner: auditRepo, pool: pool}

	staleWatchdog := watchdog.NewStaleVersionWatchdog(
		transactor, poolVersionRepo, poolArtifactRepo, poolAuditRepo, outboxWriter,
		obs.Metrics,
		obs.Logger.Slog().With("component", "stale-watchdog"),
		cfg.Timeout.StaleVersion,
		cfg.Watchdog,
		cfg.Broker.TopicDMEventsVersionPartiallyAvailable,
	)

	// -----------------------------------------------------------------------
	// Phase 11.6: Orphan Cleanup Job (BRE-008/DM-TASK-031)
	// -----------------------------------------------------------------------
	// orphanCandidateRepo and poolOrphanCandidateRepo are created in Phase 11
	// (shared with ingestion and diff services for orphan candidate registration).

	orphanCleanupJob := orphancleanup.NewOrphanCleanupJob(
		poolOrphanCandidateRepo,
		objClient,
		obs.Metrics,
		obs.Logger.Slog().With("component", "orphan-cleanup"),
		cfg.OrphanCleanup,
	)

	// -----------------------------------------------------------------------
	// Phase 11.7: Retention Jobs (DM-TASK-032)
	// -----------------------------------------------------------------------
	poolDocRepo := &poolDocumentRepository{inner: docRepo, pool: pool}
	poolDiffRepo := &poolDiffRepository{inner: diffRepo, pool: pool}

	blobCleanupJob := retention.NewDeletedBlobCleanupJob(
		poolDocRepo, objClient, obs.Metrics,
		obs.Logger.Slog().With("component", "retention-blob"),
		cfg.Retention,
	)
	metaCleanupJob := retention.NewDeletedMetaCleanupJob(
		transactor, poolDocRepo, poolVersionRepo, poolArtifactRepo,
		poolDiffRepo, poolAuditRepo, obs.Metrics,
		obs.Logger.Slog().With("component", "retention-meta"),
		cfg.Retention,
	)

	auditPartitionMgr := postgres.NewAuditPartitionManager()
	poolAuditPartMgr := &poolAuditPartitionManager{inner: auditPartitionMgr, pool: pool}
	auditPartitionJob := retention.NewAuditPartitionJob(
		poolAuditPartMgr, obs.Metrics,
		obs.Logger.Slog().With("component", "retention-audit"),
		cfg.Retention,
	)

	// -----------------------------------------------------------------------
	// Phase 12: Event Consumer (subscribes to 7 incoming topics)
	// -----------------------------------------------------------------------
	brokerSub := &poolSubscribeAdapter{client: brokerClient, pool: pool}

	eventConsumer := consumer.NewEventConsumer(
		brokerSub,
		idemGuard,
		obs.Logger.With("component", "consumer"),
		obs.Metrics,
		dlqSender,
		ingestionSvc,
		querySvc,
		diffSvc,
		artifactRepo,
		diffRepo,
		consumer.TopicConfig{
			DPArtifactsReady:    cfg.Broker.TopicDPArtifactsProcessingReady,
			DPSemanticTreeReq:   cfg.Broker.TopicDPRequestsSemanticTree,
			DPDiffReady:         cfg.Broker.TopicDPArtifactsDiffReady,
			LICArtifactsReady:   cfg.Broker.TopicLICArtifactsAnalysisReady,
			LICRequestArtifacts: cfg.Broker.TopicLICRequestsArtifacts,
			REArtifactsReady:    cfg.Broker.TopicREArtifactsReportsReady,
			RERequestArtifacts:  cfg.Broker.TopicRERequestsArtifacts,
		},
		cfg.Retry,
	)

	// -----------------------------------------------------------------------
	// Phase 13: API Handler
	// -----------------------------------------------------------------------
	audit := &auditPortAdapter{repo: auditRepo}
	apiHandler := api.NewHandler(
		lifecycleSvc, versionSvc, querySvc, diffSvc,
		audit, objClient,
		obs.Logger.With("component", "api"),
	)
	apiHandler.WithDLQReplay(poolDLQRepo, brokerClient, cfg.DLQ.MaxReplayCount)

	// Per-organization rate limiting (BRE-009).
	var orgRateLimiter *api.OrgRateLimiter
	if cfg.RateLimit.Enabled {
		orgRateLimiter = api.NewOrgRateLimiter(
			cfg.RateLimit.ReadRPS,
			cfg.RateLimit.WriteRPS,
			cfg.RateLimit.CleanupInterval,
			cfg.RateLimit.IdleTTL,
		)
		apiHandler.WithRateLimit(orgRateLimiter, obs.Metrics)
		logger.Info("rate limiting enabled",
			"read_rps", cfg.RateLimit.ReadRPS,
			"write_rps", cfg.RateLimit.WriteRPS,
		)
	}

	// -----------------------------------------------------------------------
	// Phase 14: Outbox Poller + Metrics Collector
	// poolOutboxRepo ensures ConnFromCtx finds the pool in non-transactional
	// contexts (cleanup, PendingStats).
	// -----------------------------------------------------------------------
	outboxPoller := outbox.NewOutboxPoller(
		poolOutboxRepo, transactor, brokerClient, obs.Metrics,
		obs.Logger.Slog().With("component", "outbox-poller"),
		cfg.Outbox,
	)
	outboxMetricsCollector := outbox.NewOutboxMetricsCollector(
		poolOutboxRepo, obs.Metrics,
		obs.Logger.Slog().With("component", "outbox-metrics"),
		5*time.Second,
	)

	// -----------------------------------------------------------------------
	// Phase 15: Health Handler
	// -----------------------------------------------------------------------
	healthHandler := health.NewHandler(
		map[string]health.ComponentChecker{
			"postgres": func(ctx context.Context) error { return pgClient.Ping(ctx) },
			"redis":    func(ctx context.Context) error { return kvClient.Ping(ctx) },
			"rabbitmq": func(ctx context.Context) error {
				if brokerClient.IsConnected() {
					return nil
				}
				return fmt.Errorf("broker not connected")
			},
		},
		map[string]health.ComponentChecker{
			"object_storage": func(ctx context.Context) error {
				// Object Storage is non-core per REV-024.
				// Full S3 probe is expensive; report healthy by default.
				return nil
			},
		},
	)

	// -----------------------------------------------------------------------
	// Phase 16: HTTP Servers
	// -----------------------------------------------------------------------

	// Combined mux: health probes + REST API.
	// Health routes match first (most specific); "/" catch-all delegates to API
	// with pool injection so repositories can access the DB connection.
	rootMux := http.NewServeMux()
	rootMux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		healthHandler.Mux().ServeHTTP(w, r)
	})
	rootMux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		healthHandler.Mux().ServeHTTP(w, r)
	})
	apiMux := apiHandler.Mux(obs.Metrics.APIRequests, obs.Metrics.APIRequestDuration)
	rootMux.Handle("/", poolMiddleware(pool, apiMux))

	httpServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.HTTP.Port),
		Handler:           rootMux,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	metricsHandler := observability.NewMetricsHandler(obs.Metrics.Registry())
	metricsServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Observability.MetricsPort),
		Handler:           metricsHandler.Mux(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Shutdown guard — safe to call from signal handler and error path.
	var shutdownOnce sync.Once
	shutdownFn := func() {
		shutdownOnce.Do(func() {
			doShutdown(logger, cfg.Timeout.Shutdown, healthHandler,
				outboxPoller, outboxMetricsCollector, staleWatchdog,
				orphanCleanupJob,
				blobCleanupJob, metaCleanupJob, auditPartitionJob,
				orgRateLimiter,
				brokerClient, httpServer, metricsServer,
				kvClient, pgClient, obs)
		})
	}

	// ===================================================================
	// START
	// ===================================================================

	// Start HTTP servers; detect early bind failures via an error channel.
	httpErrCh := make(chan error, 1)
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			httpErrCh <- fmt.Errorf("http server: %w", err)
			return
		}
		httpErrCh <- nil
	}()
	go func() {
		if err := metricsServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("metrics server failed", "error", err)
		}
	}()

	// Give the HTTP server a moment to start (or fail).
	select {
	case err := <-httpErrCh:
		if err != nil {
			logger.Error("http server startup failed", "error", err)
			shutdownFn()
			return 1
		}
	case <-time.After(100 * time.Millisecond):
		// Server is listening — proceed.
	}

	// Start event consumer subscriptions.
	if err := eventConsumer.Start(); err != nil {
		logger.Error("consumer start failed", "error", err)
		shutdownFn()
		return 1
	}

	// Start outbox poller, metrics collector, stale version watchdog, orphan cleanup, and retention jobs.
	outboxPoller.Start()
	outboxMetricsCollector.Start()
	staleWatchdog.Start()
	orphanCleanupJob.Start()
	blobCleanupJob.Start()
	metaCleanupJob.Start()
	auditPartitionJob.Start()

	// Mark service ready for traffic.
	healthHandler.SetReady(true)
	logger.Info("dm-service started",
		"http_port", cfg.HTTP.Port,
		"metrics_port", cfg.Observability.MetricsPort,
	)

	// Block until signal.
	<-ctx.Done()

	logger.Info("shutdown signal received")
	shutdownFn()
	return 0
}

// =========================================================================
// Shutdown (BRE-019)
// =========================================================================

// doShutdown performs ordered teardown of all components.
//
// Order:
//  1. readiness=false (new traffic rejected)
//  2. stop outbox poller (finish current publish batch — needs broker alive)
//  3. stop outbox metrics collector
//  4. stop stale version watchdog
//  5. stop orphan cleanup job
//  6. close broker (stops consumers, drains in-flight handlers)
//  7. stop HTTP servers (API + metrics)
//  8. close Redis
//  9. close PostgreSQL
//  10. flush observability
func doShutdown(
	logger *observability.Logger,
	timeout time.Duration,
	healthHandler *health.Handler,
	outboxPoller *outbox.OutboxPoller,
	outboxMetrics *outbox.OutboxMetricsCollector,
	staleWatchdog *watchdog.StaleVersionWatchdog,
	orphanCleanup *orphancleanup.OrphanCleanupJob,
	blobCleanup *retention.DeletedBlobCleanupJob,
	metaCleanup *retention.DeletedMetaCleanupJob,
	auditPartition *retention.AuditPartitionJob,
	rateLimiter *api.OrgRateLimiter,
	brokerClient *broker.Client,
	httpServer *http.Server,
	metricsServer *http.Server,
	kvClient *kvstore.Client,
	pgClient *postgres.Client,
	obs *observability.SDK,
) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Phase 0: Signal not ready.
	healthHandler.SetReady(false)

	// Phase 1: Stop outbox poller (finish current batch — needs broker for publish).
	logger.Info("stopping outbox poller")
	outboxPoller.Stop()
	select {
	case <-outboxPoller.Done():
	case <-ctx.Done():
		logger.Warn("outbox poller stop timeout")
	}

	// Phase 2: Stop outbox metrics collector.
	outboxMetrics.Stop()
	select {
	case <-outboxMetrics.Done():
	case <-ctx.Done():
		logger.Warn("outbox metrics stop timeout")
	}

	// Phase 3: Stop stale version watchdog.
	logger.Info("stopping stale version watchdog")
	staleWatchdog.Stop()
	select {
	case <-staleWatchdog.Done():
	case <-ctx.Done():
		logger.Warn("stale watchdog stop timeout")
	}

	// Phase 3.5: Stop orphan cleanup job.
	logger.Info("stopping orphan cleanup job")
	orphanCleanup.Stop()
	select {
	case <-orphanCleanup.Done():
	case <-ctx.Done():
		logger.Warn("orphan cleanup stop timeout")
	}

	// Phase 3.6: Stop retention blob cleanup job.
	logger.Info("stopping retention blob cleanup")
	blobCleanup.Stop()
	select {
	case <-blobCleanup.Done():
	case <-ctx.Done():
		logger.Warn("retention blob cleanup stop timeout")
	}

	// Phase 3.7: Stop retention meta cleanup job.
	logger.Info("stopping retention meta cleanup")
	metaCleanup.Stop()
	select {
	case <-metaCleanup.Done():
	case <-ctx.Done():
		logger.Warn("retention meta cleanup stop timeout")
	}

	// Phase 3.8: Stop audit partition job.
	logger.Info("stopping audit partition job")
	auditPartition.Stop()
	select {
	case <-auditPartition.Done():
	case <-ctx.Done():
		logger.Warn("audit partition stop timeout")
	}

	// Phase 3.9: Stop rate limiter GC goroutine.
	if rateLimiter != nil {
		rateLimiter.Close()
	}

	// Phase 4: Close broker (stops consumers, drains in-flight handlers).
	logger.Info("closing broker connection")
	if err := brokerClient.Close(); err != nil {
		logger.Error("broker close error", "error", err)
	}

	// Phase 5: Stop HTTP servers.
	logger.Info("shutting down http servers")
	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Error("http server shutdown error", "error", err)
	}
	if err := metricsServer.Shutdown(ctx); err != nil {
		logger.Error("metrics server shutdown error", "error", err)
	}

	// Phase 6: Close Redis.
	logger.Info("closing redis")
	if err := kvClient.Close(); err != nil {
		logger.Error("redis close error", "error", err)
	}

	// Phase 7: Close PostgreSQL.
	logger.Info("closing postgres")
	if err := pgClient.Close(); err != nil {
		logger.Error("postgres close error", "error", err)
	}

	// Phase 8: Flush observability (traces from all previous phases).
	logger.Info("flushing observability")
	if err := obs.Shutdown(ctx); err != nil {
		logger.Error("observability shutdown error", "error", err)
	}
}

// =========================================================================
// Adapters
// =========================================================================

// poolSubscribeAdapter bridges broker.Client.Subscribe and the consumer-side
// BrokerSubscriber interface, while also injecting the pgxpool.Pool into
// every handler's context so repositories can find the DB connection via
// postgres.ConnFromCtx.
//
// Event handlers that need transactional guarantees must explicitly call
// Transactor.WithTransaction (which will find and reuse the pool from context).
type poolSubscribeAdapter struct {
	client *broker.Client
	pool   *pgxpool.Pool
}

func (a *poolSubscribeAdapter) Subscribe(topic string, handler func(ctx context.Context, body []byte) error) error {
	return a.client.Subscribe(topic, broker.MessageHandler(func(ctx context.Context, body []byte) error {
		ctx = postgres.InjectPool(ctx, a.pool)
		return handler(ctx, body)
	}))
}

// poolMiddleware injects the pgxpool.Pool into every HTTP request context so
// repositories can access the DB connection via postgres.ConnFromCtx.
func poolMiddleware(pool *pgxpool.Pool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := postgres.InjectPool(r.Context(), pool)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// poolOutboxRepository wraps port.OutboxRepository to inject the pgxpool.Pool
// into every context before delegating. This prevents ConnFromCtx panics in
// non-transactional paths like OutboxPoller.cleanup() and
// OutboxMetricsCollector.collect(), which create context.Background().
type poolOutboxRepository struct {
	inner port.OutboxRepository
	pool  *pgxpool.Pool
}

func (r *poolOutboxRepository) Insert(ctx context.Context, entries ...port.OutboxEntry) error {
	return r.inner.Insert(postgres.InjectPool(ctx, r.pool), entries...)
}

func (r *poolOutboxRepository) FetchUnpublished(ctx context.Context, limit int) ([]port.OutboxEntry, error) {
	return r.inner.FetchUnpublished(postgres.InjectPool(ctx, r.pool), limit)
}

func (r *poolOutboxRepository) MarkPublished(ctx context.Context, ids []string) error {
	return r.inner.MarkPublished(postgres.InjectPool(ctx, r.pool), ids)
}

func (r *poolOutboxRepository) DeletePublished(ctx context.Context, olderThan time.Time, limit int) (int64, error) {
	return r.inner.DeletePublished(postgres.InjectPool(ctx, r.pool), olderThan, limit)
}

func (r *poolOutboxRepository) PendingStats(ctx context.Context) (int64, float64, error) {
	return r.inner.PendingStats(postgres.InjectPool(ctx, r.pool))
}

// poolDLQRepository wraps port.DLQRepository to inject the pgxpool.Pool
// into every context before delegating. This ensures ConnFromCtx finds
// the pool in non-transactional paths (DLQ sender, replay endpoint).
type poolDLQRepository struct {
	inner port.DLQRepository
	pool  *pgxpool.Pool
}

func (r *poolDLQRepository) Insert(ctx context.Context, record *model.DLQRecord) error {
	return r.inner.Insert(postgres.InjectPool(ctx, r.pool), record)
}

func (r *poolDLQRepository) FindByFilter(ctx context.Context, params port.DLQFilterParams) ([]*model.DLQRecordWithMeta, error) {
	return r.inner.FindByFilter(postgres.InjectPool(ctx, r.pool), params)
}

func (r *poolDLQRepository) IncrementReplayCount(ctx context.Context, id string) error {
	return r.inner.IncrementReplayCount(postgres.InjectPool(ctx, r.pool), id)
}

// auditPortAdapter bridges port.AuditRepository (Insert/List) to
// port.AuditPort (Record/List) for the API handler.
type auditPortAdapter struct {
	repo port.AuditRepository
}

func (a *auditPortAdapter) Record(ctx context.Context, record *model.AuditRecord) error {
	return a.repo.Insert(ctx, record)
}

func (a *auditPortAdapter) List(ctx context.Context, params port.AuditListParams) ([]*model.AuditRecord, int, error) {
	return a.repo.List(ctx, params)
}

// poolVersionRepository wraps port.VersionRepository to inject the pgxpool.Pool
// into every context. Required by the stale version watchdog which creates
// context.Background() in its scan goroutine.
type poolVersionRepository struct {
	inner port.VersionRepository
	pool  *pgxpool.Pool
}

func (r *poolVersionRepository) Insert(ctx context.Context, v *model.DocumentVersion) error {
	return r.inner.Insert(postgres.InjectPool(ctx, r.pool), v)
}

func (r *poolVersionRepository) FindByID(ctx context.Context, orgID, docID, versionID string) (*model.DocumentVersion, error) {
	return r.inner.FindByID(postgres.InjectPool(ctx, r.pool), orgID, docID, versionID)
}

func (r *poolVersionRepository) FindByIDForUpdate(ctx context.Context, orgID, docID, versionID string) (*model.DocumentVersion, error) {
	return r.inner.FindByIDForUpdate(postgres.InjectPool(ctx, r.pool), orgID, docID, versionID)
}

func (r *poolVersionRepository) List(ctx context.Context, orgID, docID string, page, pageSize int) ([]*model.DocumentVersion, int, error) {
	return r.inner.List(postgres.InjectPool(ctx, r.pool), orgID, docID, page, pageSize)
}

func (r *poolVersionRepository) Update(ctx context.Context, v *model.DocumentVersion) error {
	return r.inner.Update(postgres.InjectPool(ctx, r.pool), v)
}

func (r *poolVersionRepository) NextVersionNumber(ctx context.Context, orgID, docID string) (int, error) {
	return r.inner.NextVersionNumber(postgres.InjectPool(ctx, r.pool), orgID, docID)
}

func (r *poolVersionRepository) DeleteByDocument(ctx context.Context, documentID string) error {
	return r.inner.DeleteByDocument(postgres.InjectPool(ctx, r.pool), documentID)
}

func (r *poolVersionRepository) ListByDocument(ctx context.Context, documentID string) ([]*model.DocumentVersion, error) {
	return r.inner.ListByDocument(postgres.InjectPool(ctx, r.pool), documentID)
}

func (r *poolVersionRepository) FindStaleInIntermediateStatus(ctx context.Context, cutoff time.Time, limit int) ([]*model.DocumentVersion, error) {
	return r.inner.FindStaleInIntermediateStatus(postgres.InjectPool(ctx, r.pool), cutoff, limit)
}

// poolArtifactRepository wraps port.ArtifactRepository to inject the pgxpool.Pool.
type poolArtifactRepository struct {
	inner port.ArtifactRepository
	pool  *pgxpool.Pool
}

func (r *poolArtifactRepository) Insert(ctx context.Context, d *model.ArtifactDescriptor) error {
	return r.inner.Insert(postgres.InjectPool(ctx, r.pool), d)
}

func (r *poolArtifactRepository) FindByVersionAndType(ctx context.Context, orgID, docID, versionID string, at model.ArtifactType) (*model.ArtifactDescriptor, error) {
	return r.inner.FindByVersionAndType(postgres.InjectPool(ctx, r.pool), orgID, docID, versionID, at)
}

func (r *poolArtifactRepository) ListByVersion(ctx context.Context, orgID, docID, versionID string) ([]*model.ArtifactDescriptor, error) {
	return r.inner.ListByVersion(postgres.InjectPool(ctx, r.pool), orgID, docID, versionID)
}

func (r *poolArtifactRepository) ListByVersionAndTypes(ctx context.Context, orgID, docID, versionID string, types []model.ArtifactType) ([]*model.ArtifactDescriptor, error) {
	return r.inner.ListByVersionAndTypes(postgres.InjectPool(ctx, r.pool), orgID, docID, versionID, types)
}

func (r *poolArtifactRepository) DeleteByVersion(ctx context.Context, orgID, docID, versionID string) error {
	return r.inner.DeleteByVersion(postgres.InjectPool(ctx, r.pool), orgID, docID, versionID)
}

// poolOrphanCandidateRepository wraps port.OrphanCandidateRepository to inject
// the pgxpool.Pool. Required by the orphan cleanup job which creates
// context.Background() in its scan goroutine.
type poolOrphanCandidateRepository struct {
	inner port.OrphanCandidateRepository
	pool  *pgxpool.Pool
}

func (r *poolOrphanCandidateRepository) FindOlderThan(ctx context.Context, cutoff time.Time, limit int) ([]port.OrphanCandidate, error) {
	return r.inner.FindOlderThan(postgres.InjectPool(ctx, r.pool), cutoff, limit)
}

func (r *poolOrphanCandidateRepository) ExistsByStorageKey(ctx context.Context, storageKey string) (bool, error) {
	return r.inner.ExistsByStorageKey(postgres.InjectPool(ctx, r.pool), storageKey)
}

func (r *poolOrphanCandidateRepository) DeleteByKeys(ctx context.Context, storageKeys []string) error {
	return r.inner.DeleteByKeys(postgres.InjectPool(ctx, r.pool), storageKeys)
}

func (r *poolOrphanCandidateRepository) Insert(ctx context.Context, candidate port.OrphanCandidate) error {
	return r.inner.Insert(postgres.InjectPool(ctx, r.pool), candidate)
}

// poolAuditRepository wraps port.AuditRepository to inject the pgxpool.Pool.
type poolAuditRepository struct {
	inner port.AuditRepository
	pool  *pgxpool.Pool
}

func (r *poolAuditRepository) Insert(ctx context.Context, record *model.AuditRecord) error {
	return r.inner.Insert(postgres.InjectPool(ctx, r.pool), record)
}

func (r *poolAuditRepository) List(ctx context.Context, params port.AuditListParams) ([]*model.AuditRecord, int, error) {
	return r.inner.List(postgres.InjectPool(ctx, r.pool), params)
}

func (r *poolAuditRepository) DeleteByDocument(ctx context.Context, documentID string) error {
	return r.inner.DeleteByDocument(postgres.InjectPool(ctx, r.pool), documentID)
}

// poolDocumentRepository wraps port.DocumentRepository to inject the pgxpool.Pool.
// Required by retention jobs which create context.Background() in scan goroutines.
type poolDocumentRepository struct {
	inner port.DocumentRepository
	pool  *pgxpool.Pool
}

func (r *poolDocumentRepository) Insert(ctx context.Context, doc *model.Document) error {
	return r.inner.Insert(postgres.InjectPool(ctx, r.pool), doc)
}

func (r *poolDocumentRepository) FindByID(ctx context.Context, orgID, docID string) (*model.Document, error) {
	return r.inner.FindByID(postgres.InjectPool(ctx, r.pool), orgID, docID)
}

func (r *poolDocumentRepository) FindByIDForUpdate(ctx context.Context, orgID, docID string) (*model.Document, error) {
	return r.inner.FindByIDForUpdate(postgres.InjectPool(ctx, r.pool), orgID, docID)
}

func (r *poolDocumentRepository) List(ctx context.Context, orgID string, sf *model.DocumentStatus, page, pageSize int) ([]*model.Document, int, error) {
	return r.inner.List(postgres.InjectPool(ctx, r.pool), orgID, sf, page, pageSize)
}

func (r *poolDocumentRepository) Update(ctx context.Context, doc *model.Document) error {
	return r.inner.Update(postgres.InjectPool(ctx, r.pool), doc)
}

func (r *poolDocumentRepository) ExistsByID(ctx context.Context, orgID, docID string) (bool, error) {
	return r.inner.ExistsByID(postgres.InjectPool(ctx, r.pool), orgID, docID)
}

func (r *poolDocumentRepository) FindDeletedOlderThan(ctx context.Context, cutoff time.Time, limit int) ([]*model.Document, error) {
	return r.inner.FindDeletedOlderThan(postgres.InjectPool(ctx, r.pool), cutoff, limit)
}

func (r *poolDocumentRepository) DeleteByID(ctx context.Context, documentID string) error {
	return r.inner.DeleteByID(postgres.InjectPool(ctx, r.pool), documentID)
}

// poolDiffRepository wraps port.DiffRepository to inject the pgxpool.Pool.
// Required by retention meta cleanup job.
type poolDiffRepository struct {
	inner port.DiffRepository
	pool  *pgxpool.Pool
}

func (r *poolDiffRepository) Insert(ctx context.Context, ref *model.VersionDiffReference) error {
	return r.inner.Insert(postgres.InjectPool(ctx, r.pool), ref)
}

func (r *poolDiffRepository) FindByVersionPair(ctx context.Context, orgID, docID, baseVersionID, targetVersionID string) (*model.VersionDiffReference, error) {
	return r.inner.FindByVersionPair(postgres.InjectPool(ctx, r.pool), orgID, docID, baseVersionID, targetVersionID)
}

func (r *poolDiffRepository) ListByDocument(ctx context.Context, orgID, docID string) ([]*model.VersionDiffReference, error) {
	return r.inner.ListByDocument(postgres.InjectPool(ctx, r.pool), orgID, docID)
}

func (r *poolDiffRepository) DeleteByDocument(ctx context.Context, orgID, docID string) error {
	return r.inner.DeleteByDocument(postgres.InjectPool(ctx, r.pool), orgID, docID)
}

// poolAuditPartitionManager wraps port.AuditPartitionManager to inject the pgxpool.Pool.
// Required by audit partition job which creates context.Background() in scan goroutine.
type poolAuditPartitionManager struct {
	inner port.AuditPartitionManager
	pool  *pgxpool.Pool
}

func (m *poolAuditPartitionManager) EnsurePartitions(ctx context.Context, monthsAhead int) (int, error) {
	return m.inner.EnsurePartitions(postgres.InjectPool(ctx, m.pool), monthsAhead)
}

func (m *poolAuditPartitionManager) DropPartitionsOlderThan(ctx context.Context, cutoff time.Time) (int, error) {
	return m.inner.DropPartitionsOlderThan(postgres.InjectPool(ctx, m.pool), cutoff)
}
