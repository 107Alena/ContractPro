// Package app wires all application components together and manages
// the lifecycle (startup and shutdown) of the API/Backend Orchestrator.
//
// NewApp constructs all dependencies in the correct order and returns an
// App ready to start. Start runs the event consumer and HTTP server.
// Shutdown performs an ordered 7-phase teardown:
// not-ready → SSE close → HTTP drain → broker → Redis → observability flush → done.
package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"contractpro/api-orchestrator/internal/application/authproxy"
	"contractpro/api-orchestrator/internal/application/comparison"
	"contractpro/api-orchestrator/internal/application/contracts"
	"contractpro/api-orchestrator/internal/application/export"
	"contractpro/api-orchestrator/internal/application/results"
	"contractpro/api-orchestrator/internal/application/statustracker"
	"contractpro/api-orchestrator/internal/application/upload"
	"contractpro/api-orchestrator/internal/application/versions"
	"contractpro/api-orchestrator/internal/config"
	"contractpro/api-orchestrator/internal/egress/commandpub"
	"contractpro/api-orchestrator/internal/egress/dmclient"
	"contractpro/api-orchestrator/internal/egress/ssebroadcast"
	"contractpro/api-orchestrator/internal/egress/uomclient"
	"github.com/redis/go-redis/v9"

	"contractpro/api-orchestrator/internal/infra/broker"
	"contractpro/api-orchestrator/internal/infra/health"
	"contractpro/api-orchestrator/internal/infra/kvstore"
	"contractpro/api-orchestrator/internal/infra/objectstorage"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/infra/observability/metrics"
	"contractpro/api-orchestrator/internal/ingress/api"
	"contractpro/api-orchestrator/internal/ingress/consumer"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"
	"contractpro/api-orchestrator/internal/ingress/middleware/ratelimit"
	"contractpro/api-orchestrator/internal/ingress/middleware/rbac"
	"contractpro/api-orchestrator/internal/ingress/sse"
)

// ObservabilityShutdown flushes pending traces, metrics, and log buffers
// during graceful shutdown. Prometheus metrics (ORCH-TASK-032) are wired;
// OpenTelemetry tracing (ORCH-TASK-033) will extend this interface.
type ObservabilityShutdown interface {
	Shutdown(ctx context.Context) error
}

// App holds all wired components and manages the application lifecycle.
type App struct {
	log    *logger.Logger
	health *health.Handler

	// Infrastructure clients (need explicit Close on shutdown).
	kvClient     *kvstore.Client
	brokerClient *broker.Client

	// HTTP server (main + metrics).
	server *api.Server

	// Event consumer (subscribes to RabbitMQ topics).
	consumer *consumer.Consumer

	// SSE handler (tracks active connections for graceful close).
	sseHandler *sse.Handler

	// Observability shutdown (flush traces/metrics/logs).
	// Prometheus metrics (ORCH-TASK-032) are wired; OpenTelemetry tracing
	// (ORCH-TASK-033) will be added when implemented.
	observability ObservabilityShutdown
}

// NewApp constructs all application components in dependency order and
// returns a fully wired App. If any required infrastructure client fails
// to connect, already-opened resources are closed before returning the error.
func NewApp(cfg *config.Config) (*App, error) {
	// 1. Logger — no external dependency.
	log := logger.NewLogger(cfg.Observability.LogLevel)
	log.Info(context.Background(), "initializing application")

	// 1b. Prometheus metrics — no external dependency, always enabled.
	appMetrics := metrics.NewMetrics()

	// 2. Redis client — connects and pings on construction.
	kvClient, err := kvstore.NewClient(cfg.Redis)
	if err != nil {
		return nil, fmt.Errorf("redis: %w", err)
	}

	// 3. RabbitMQ broker — connects and pings on construction.
	brokerClient, err := broker.NewClient(
		cfg.Broker.Address, cfg.Broker.TLS, cfg.Broker.Prefetch, log,
	)
	if err != nil {
		kvClient.Close()
		return nil, fmt.Errorf("broker: %w", err)
	}

	// 4. S3 Object Storage — lazy connect, no error.
	s3Client := objectstorage.NewClient(cfg.Storage, cfg.CircuitBreaker, log)

	// 5. Health handler — depends on Redis and broker for readiness probes.
	healthHandler := health.NewHandler(kvClient, brokerClient, cfg.DMClient.BaseURL)

	// 6. JWT public key and auth middleware.
	publicKey, err := auth.LoadPublicKey(cfg.JWT.PublicKeyPath)
	if err != nil {
		brokerClient.Close()
		kvClient.Close()
		return nil, fmt.Errorf("jwt public key: %w", err)
	}
	authMW, err := auth.NewMiddleware(publicKey, log)
	if err != nil {
		brokerClient.Close()
		kvClient.Close()
		return nil, fmt.Errorf("auth middleware: %w", err)
	}

	// 7. RBAC middleware.
	rbacMW := rbac.NewMiddleware(log)

	// 7b. Rate limiter middleware.
	var rateLimitMW func(http.Handler) http.Handler
	if cfg.RateLimit.Enabled {
		rdb, ok := kvClient.RawRedis().(redis.Cmdable)
		if !ok {
			brokerClient.Close()
			kvClient.Close()
			return nil, fmt.Errorf("rate limiter: Redis client does not support Lua scripting")
		}
		rlStore := ratelimit.NewRedisStore(rdb)
		rlMiddleware, err := ratelimit.NewMiddleware(cfg.RateLimit, rlStore, log)
		if err != nil {
			brokerClient.Close()
			kvClient.Close()
			return nil, fmt.Errorf("rate limiter: %w", err)
		}
		rateLimitMW = rlMiddleware.Handler()
	} else {
		// Disabled: pass-through middleware (no Redis needed).
		rlMiddleware, _ := ratelimit.NewMiddleware(cfg.RateLimit, nil, log)
		rateLimitMW = rlMiddleware.Handler()
	}

	// 8. Egress clients.
	dmClient := dmclient.NewClient(cfg.DMClient, cfg.CircuitBreaker, log)
	uomClient := uomclient.NewClient(cfg.UOMClient, log)
	cmdPub := commandpub.NewPublisher(
		brokerClient,
		cfg.Broker.TopicProcessDocument,
		cfg.Broker.TopicCompareVersions,
		log,
	)

	// 9. SSE broadcaster (must be created BEFORE status tracker).
	broadcaster := ssebroadcast.NewBroadcaster(kvClient, log)

	// 10. Status tracker — consumes events, broadcasts via SSE.
	tracker := statustracker.NewTracker(kvClient, broadcaster, log)

	// 11. Event consumer — subscribes to 12 RabbitMQ topics.
	//     Uses adapter because broker.Client.Subscribe takes the named type
	//     broker.MessageHandler, while consumer.BrokerSubscriber expects an
	//     unnamed func signature.
	brokerAdapter := &brokerSubscriberAdapter{client: brokerClient}
	eventConsumer := consumer.NewConsumer(brokerAdapter, tracker, log, cfg.Broker)

	// 12. Application handlers.
	//     Upload handler needs adapters because it defines its own DTO types
	//     (decoupled from egress layer).
	uploadDM := &uploadDMAdapter{client: dmClient}
	uploadCmd := &uploadCmdPubAdapter{pub: cmdPub}
	uploadHandler := upload.NewHandler(s3Client, uploadDM, uploadCmd, kvClient, log, cfg.Upload.MaxSize)
	contractHandler := contracts.NewHandler(dmClient, log)
	versionHandler := versions.NewHandler(dmClient, s3Client, cmdPub, kvClient, log, cfg.Upload.MaxSize)
	resultsHandler := results.NewHandler(dmClient, log)
	comparisonHandler := comparison.NewHandler(dmClient, cmdPub, log)
	exportHandler := export.NewHandler(dmClient, log)
	authProxyHandler := authproxy.NewHandler(uomClient, log)

	// 13. SSE handler — uses auth middleware as token validator.
	sseAdapter := sse.NewKVStoreAdapter(kvClient)
	sseHandler := sse.NewHandler(authMW, sseAdapter, cfg.SSE, log)

	// 14. HTTP server — assembles all handlers and middleware.
	server := api.NewServer(api.Deps{
		Config:                cfg.HTTP,
		CORSConfig:            cfg.CORS,
		Health:                healthHandler,
		Logger:                log,
		AuthMiddleware:        authMW.Handler(),
		RBACMiddleware:        rbacMW.Handler(),
		RateLimitMiddleware:   rateLimitMW,
		MetricsHandler:        appMetrics.Handler(),
		HTTPMetricsMiddleware: appMetrics.HTTPMiddleware(),
		AuthHandler:           authProxyHandler,
		UploadHandler:         uploadHandler.Handle(),
		ContractHandler:       contractHandler,
		VersionHandler:        versionHandler,
		ResultsHandler:        resultsHandler,
		ComparisonHandler:     comparisonHandler,
		ExportHandler:         exportHandler,
		SSEHandler:            sseHandler,
	})

	log.Info(context.Background(), "application initialized successfully")

	return &App{
		log:           log,
		health:        healthHandler,
		kvClient:      kvClient,
		brokerClient:  brokerClient,
		server:        server,
		consumer:      eventConsumer,
		sseHandler:    sseHandler,
		observability: appMetrics,
	}, nil
}

// Start launches the event consumer and HTTP server. The event consumer
// runs in a background goroutine; the HTTP server blocks the calling
// goroutine until Shutdown is called or an error occurs.
//
// If the event consumer fails to start, the error is returned immediately
// without starting the HTTP server.
func (a *App) Start() error {
	a.log.Info(context.Background(), "starting event consumer")
	if err := a.consumer.Start(); err != nil {
		return fmt.Errorf("event consumer: %w", err)
	}

	a.log.Info(context.Background(), "starting HTTP server")
	return a.server.Start()
}

// Shutdown performs an ordered teardown of all application components.
// The provided context controls the maximum time to wait for in-flight
// requests and connections to drain. Each phase is executed sequentially;
// errors are collected but do not abort subsequent phases.
//
// Shutdown phases (see deployment.md §5):
//  1. Mark not ready — readiness probe returns 503 immediately
//  2. Close SSE — send close event, cancel connection contexts
//  3. Drain HTTP — stop listener, wait for in-flight requests
//  4. Close broker — stop consumer subscriptions, disconnect AMQP
//  5. Close Redis — disconnect connection pool
//  6. Flush observability — flush traces, metrics, logs (no-op until wired)
//  7. Done
//
// SSE close (phase 2) MUST happen before HTTP drain (phase 3) because
// http.Server.Shutdown waits for all active connections to become idle.
// SSE connections are long-lived and never idle — cancelling their contexts
// first lets the HTTP handlers return, which unblocks server.Shutdown.
func (a *App) Shutdown(ctx context.Context) error {
	a.log.Info(ctx, "shutdown phase 1/7: marking not ready")
	a.health.SetNotReady()

	var errs []error

	// Phase 2: Close SSE connections. Send a close event so clients know to
	// reconnect to another instance, then cancel their contexts. This MUST
	// happen before HTTP drain so SSE handlers can return.
	a.log.Info(ctx, "shutdown phase 2/7: closing SSE connections")
	if a.sseHandler != nil {
		a.sseHandler.Shutdown()
	}

	// Phase 3: Drain in-flight HTTP requests. Go's http.Server.Shutdown
	// stops the listener and waits for active requests to complete. SSE
	// connections have already been cancelled in phase 2, so this completes
	// promptly.
	a.log.Info(ctx, "shutdown phase 3/7: draining HTTP requests")
	if err := a.server.Shutdown(ctx); err != nil {
		a.log.Error(ctx, "HTTP server shutdown error", logger.ErrorAttr(err))
		errs = append(errs, fmt.Errorf("http server: %w", err))
	}

	// Phase 4: Close broker (stops consumer subscriptions, disconnects AMQP).
	a.log.Info(ctx, "shutdown phase 4/7: closing broker")
	if a.brokerClient != nil {
		if err := a.brokerClient.Close(); err != nil {
			a.log.Error(ctx, "broker shutdown error", logger.ErrorAttr(err))
			errs = append(errs, fmt.Errorf("broker: %w", err))
		}
	}

	// Phase 5: Close Redis connection pool.
	a.log.Info(ctx, "shutdown phase 5/7: closing Redis")
	if a.kvClient != nil {
		if err := a.kvClient.Close(); err != nil {
			a.log.Error(ctx, "redis shutdown error", logger.ErrorAttr(err))
			errs = append(errs, fmt.Errorf("redis: %w", err))
		}
	}

	// Phase 6: Flush observability (Prometheus no-op, OpenTelemetry TBD).
	a.log.Info(ctx, "shutdown phase 6/7: flushing observability")
	if a.observability != nil {
		if err := a.observability.Shutdown(ctx); err != nil {
			a.log.Error(ctx, "observability shutdown error", logger.ErrorAttr(err))
			errs = append(errs, fmt.Errorf("observability: %w", err))
		}
	}

	a.log.Info(ctx, "shutdown phase 7/7: complete")
	return errors.Join(errs...)
}
