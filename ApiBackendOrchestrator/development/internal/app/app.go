// Package app wires all application components together and manages
// the lifecycle (startup and shutdown) of the API/Backend Orchestrator.
//
// NewApp constructs all dependencies in the correct order and returns an
// App ready to start. Start runs the event consumer and HTTP server.
// Shutdown performs an ordered teardown: readiness=false → HTTP → broker → Redis.
package app

import (
	"context"
	"errors"
	"fmt"

	"contractpro/api-orchestrator/internal/application/authproxy"
	"contractpro/api-orchestrator/internal/application/comparison"
	"contractpro/api-orchestrator/internal/application/contracts"
	"contractpro/api-orchestrator/internal/application/results"
	"contractpro/api-orchestrator/internal/application/statustracker"
	"contractpro/api-orchestrator/internal/application/upload"
	"contractpro/api-orchestrator/internal/application/versions"
	"contractpro/api-orchestrator/internal/config"
	"contractpro/api-orchestrator/internal/egress/commandpub"
	"contractpro/api-orchestrator/internal/egress/dmclient"
	"contractpro/api-orchestrator/internal/egress/ssebroadcast"
	"contractpro/api-orchestrator/internal/egress/uomclient"
	"contractpro/api-orchestrator/internal/infra/broker"
	"contractpro/api-orchestrator/internal/infra/health"
	"contractpro/api-orchestrator/internal/infra/kvstore"
	"contractpro/api-orchestrator/internal/infra/objectstorage"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/api"
	"contractpro/api-orchestrator/internal/ingress/consumer"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"
	"contractpro/api-orchestrator/internal/ingress/middleware/rbac"
	"contractpro/api-orchestrator/internal/ingress/sse"
)

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
}

// NewApp constructs all application components in dependency order and
// returns a fully wired App. If any required infrastructure client fails
// to connect, already-opened resources are closed before returning the error.
func NewApp(cfg *config.Config) (*App, error) {
	// 1. Logger — no external dependency.
	log := logger.NewLogger(cfg.Observability.LogLevel)
	log.Info(context.Background(), "initializing application")

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
	authProxyHandler := authproxy.NewHandler(uomClient, log)

	// 13. SSE handler — uses auth middleware as token validator.
	sseAdapter := sse.NewKVStoreAdapter(kvClient)
	sseHandler := sse.NewHandler(authMW, sseAdapter, cfg.SSE, log)

	// 14. HTTP server — assembles all handlers and middleware.
	server := api.NewServer(api.Deps{
		Config:            cfg.HTTP,
		CORSConfig:        cfg.CORS,
		Health:            healthHandler,
		Logger:            log,
		AuthMiddleware:    authMW.Handler(),
		RBACMiddleware:    rbacMW.Handler(),
		AuthHandler:       authProxyHandler,
		UploadHandler:     uploadHandler.Handle(),
		ContractHandler:   contractHandler,
		VersionHandler:    versionHandler,
		ResultsHandler:    resultsHandler,
		ComparisonHandler: comparisonHandler,
		SSEHandler:        sseHandler,
	})

	log.Info(context.Background(), "application initialized successfully")

	return &App{
		log:          log,
		health:       healthHandler,
		kvClient:     kvClient,
		brokerClient: brokerClient,
		server:       server,
		consumer:     eventConsumer,
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
// requests and connections to drain.
//
// Shutdown order:
//  1. Health: SetNotReady (readiness probe returns 503 immediately)
//  2. HTTP server: graceful shutdown (drain in-flight requests)
//  3. Broker: Close (stops event consumer, disconnects RabbitMQ)
//  4. Redis: Close (disconnects Redis connection pool)
func (a *App) Shutdown(ctx context.Context) error {
	a.log.Info(ctx, "shutdown initiated")

	var errs []error

	// 1. Signal not ready so load balancers stop sending traffic.
	a.health.SetNotReady()
	a.log.Info(ctx, "readiness probe disabled")

	// 2. Drain in-flight HTTP requests.
	if err := a.server.Shutdown(ctx); err != nil {
		a.log.Error(ctx, "HTTP server shutdown error", logger.ErrorAttr(err))
		errs = append(errs, fmt.Errorf("http server: %w", err))
	}

	// 3. Close broker (stops consumer subscriptions, disconnects AMQP).
	if err := a.brokerClient.Close(); err != nil {
		a.log.Error(ctx, "broker shutdown error", logger.ErrorAttr(err))
		errs = append(errs, fmt.Errorf("broker: %w", err))
	}

	// 4. Close Redis connection pool.
	if err := a.kvClient.Close(); err != nil {
		a.log.Error(ctx, "redis shutdown error", logger.ErrorAttr(err))
		errs = append(errs, fmt.Errorf("redis: %w", err))
	}

	a.log.Info(ctx, "shutdown complete")
	return errors.Join(errs...)
}
