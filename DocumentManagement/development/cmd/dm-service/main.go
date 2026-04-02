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
	"contractpro/document-management/internal/application/query"
	"contractpro/document-management/internal/application/version"
	"contractpro/document-management/internal/config"
	"contractpro/document-management/internal/egress/confirmation"
	"contractpro/document-management/internal/egress/outbox"
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
	// Phase 5: RabbitMQ + topology
	// -----------------------------------------------------------------------
	brokerClient, err := broker.NewClient(cfg.Broker, cfg.Consumer)
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
	// Phase 6: Object Storage
	// -----------------------------------------------------------------------
	objClient := objectstorage.NewClient(cfg.Storage)

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

	// -----------------------------------------------------------------------
	// Phase 9: Confirmation Publisher (used by query service for direct publish)
	// Notification events go through the outbox, so no NotificationPublisher is needed here.
	// -----------------------------------------------------------------------
	confirmPub := confirmation.NewConfirmationPublisher(brokerClient, cfg.Broker)

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
	ingestionSvc := ingestion.NewArtifactIngestionService(
		transactor, versionRepo, artifactRepo, auditRepo, objClient, outboxWriter,
		obs.Logger.With("component", "ingestion"),
	)

	querySvc := query.NewArtifactQueryService(
		artifactRepo, objClient, confirmPub, auditRepo,
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
		obs.Logger.With("component", "diff"),
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
				outboxPoller, outboxMetricsCollector,
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

	// Start outbox poller and metrics collector.
	outboxPoller.Start()
	outboxMetricsCollector.Start()

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
//  4. close broker (stops consumers, drains in-flight handlers)
//  5. stop HTTP servers (API + metrics)
//  6. close Redis
//  7. close PostgreSQL
//  8. flush observability
func doShutdown(
	logger *observability.Logger,
	timeout time.Duration,
	healthHandler *health.Handler,
	outboxPoller *outbox.OutboxPoller,
	outboxMetrics *outbox.OutboxMetricsCollector,
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

	// Phase 3: Close broker (stops consumers, drains in-flight handlers).
	logger.Info("closing broker connection")
	if err := brokerClient.Close(); err != nil {
		logger.Error("broker close error", "error", err)
	}

	// Phase 4: Stop HTTP servers.
	logger.Info("shutting down http servers")
	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Error("http server shutdown error", "error", err)
	}
	if err := metricsServer.Shutdown(ctx); err != nil {
		logger.Error("metrics server shutdown error", "error", err)
	}

	// Phase 5: Close Redis.
	logger.Info("closing redis")
	if err := kvClient.Close(); err != nil {
		logger.Error("redis close error", "error", err)
	}

	// Phase 6: Close PostgreSQL.
	logger.Info("closing postgres")
	if err := pgClient.Close(); err != nil {
		logger.Error("postgres close error", "error", err)
	}

	// Phase 7: Flush observability (traces from all previous phases).
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
