// Package app wires all Document Processing components together and manages
// the service lifecycle: startup, readiness, and graceful shutdown.
package app

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"contractpro/document-processing/internal/application/comparison"
	"contractpro/document-processing/internal/application/lifecycle"
	"contractpro/document-processing/internal/application/pendingresponse"
	"contractpro/document-processing/internal/application/processing"
	"contractpro/document-processing/internal/application/warning"
	"contractpro/document-processing/internal/config"
	"contractpro/document-processing/internal/egress/dm"
	"contractpro/document-processing/internal/egress/publisher"
	"contractpro/document-processing/internal/egress/storage"
	"contractpro/document-processing/internal/engine/fetcher"
	engineocr "contractpro/document-processing/internal/engine/ocr"
	"contractpro/document-processing/internal/engine/semantictree"
	"contractpro/document-processing/internal/engine/structure"
	"contractpro/document-processing/internal/engine/textextract"
	"contractpro/document-processing/internal/engine/validator"
	"contractpro/document-processing/internal/infra/broker"
	"contractpro/document-processing/internal/infra/concurrency"
	"contractpro/document-processing/internal/infra/health"
	"contractpro/document-processing/internal/infra/httpdownloader"
	"contractpro/document-processing/internal/infra/kvstore"
	"contractpro/document-processing/internal/infra/objectstorage"
	"contractpro/document-processing/internal/infra/ocr"
	"contractpro/document-processing/internal/infra/observability"
	"contractpro/document-processing/internal/ingress/consumer"
	"contractpro/document-processing/internal/ingress/dispatcher"
	"contractpro/document-processing/internal/ingress/idempotency"
	"contractpro/document-processing/internal/pdf"

	enginecomp "contractpro/document-processing/internal/engine/comparison"
)

// shutdownTimeout is the maximum time allowed for orderly shutdown.
const shutdownTimeout = 30 * time.Second

// brokerSubscribeAdapter wraps *broker.Client to satisfy the consumer-side
// BrokerSubscriber interfaces defined in the consumer and dm packages.
// Those interfaces use the unnamed function type func(context.Context, []byte) error,
// while broker.Client.Subscribe uses the named type broker.MessageHandler.
// This adapter bridges the two without coupling consumer/dm to the broker package.
type brokerSubscribeAdapter struct {
	client *broker.Client
}

func (a *brokerSubscribeAdapter) Subscribe(topic string, handler func(ctx context.Context, body []byte) error) error {
	return a.client.Subscribe(topic, broker.MessageHandler(handler))
}

// brokerCloser is a consumer-side interface for closing the broker connection.
type brokerCloser interface {
	Close() error
}

// kvCloser is a consumer-side interface for closing the KV store connection.
type kvCloser interface {
	Close() error
}

// obsShutdowner is a consumer-side interface for flushing observability.
type obsShutdowner interface {
	Shutdown(ctx context.Context) error
}

// subscriber is a consumer-side interface for starting broker subscriptions.
type subscriber interface {
	Start() error
}

// App owns all runtime components and their lifecycle.
// Create via New(); call Run() to start, which blocks until the context is
// cancelled (typically by a signal). Shutdown() is called automatically.
type App struct {
	logger       *observability.Logger
	shutdownOnce sync.Once

	// Closeable infrastructure — shutdown order matters.
	obs       obsShutdowner
	brokerCli brokerCloser
	kvCli     kvCloser

	// Subscribers that register with the broker.
	consumer   subscriber
	dmReceiver subscriber

	// HTTP health server.
	httpServer *http.Server
	health     *health.Handler
}

// New creates an App by loading the given config and wiring all components.
// It opens connections to the broker and KV store; returns an error if any
// connection fails. On partial failure, already-opened resources are closed.
func New(ctx context.Context, cfg *config.Config) (*App, error) {
	// --- Group 1: Observability ---
	obs, err := observability.New(ctx, cfg.Observability)
	if err != nil {
		return nil, fmt.Errorf("app: observability init: %w", err)
	}
	logger := obs.Logger.With("component", "app")

	// --- Group 2: Infrastructure clients ---
	brokerCli, err := broker.NewClient(cfg.Broker)
	if err != nil {
		_ = obs.Shutdown(ctx)
		return nil, fmt.Errorf("app: broker connect: %w", err)
	}

	kvCli, err := kvstore.NewClient(cfg.KVStore)
	if err != nil {
		_ = brokerCli.Close()
		_ = obs.Shutdown(ctx)
		return nil, fmt.Errorf("app: kvstore connect: %w", err)
	}

	objCli := objectstorage.NewClient(cfg.Storage)
	ocrCli := ocr.NewClient(cfg.OCR)

	// --- Group 3: Low-level adapters ---
	downloader := httpdownloader.NewDownloader(cfg.Limits.JobTimeout)
	tempStorage := storage.NewAdapter(objCli, "dp/tmp/")
	idempotencyStore := idempotency.NewStore(kvCli, cfg.Idempotency.TTL)
	eventPublisher := publisher.NewPublisher(brokerCli, cfg.Broker)
	dmSender := dm.NewSender(brokerCli, cfg.Broker)
	limiter := concurrency.New(cfg.Concurrency.MaxConcurrentJobs, obs.Metrics, obs.Logger)
	pendingRegistry := pendingresponse.New()
	warningCollector := warning.NewCollector()

	// --- Group 4: Engine components ---
	pdfUtil := pdf.NewUtil()
	inputValidator := validator.NewValidator(cfg.Limits.MaxFileSize, "application/pdf")
	fileFetcher := fetcher.NewFetcher(downloader, tempStorage, pdfUtil, cfg.Limits.MaxFileSize, cfg.Limits.MaxPages)
	ocrAdapter := engineocr.NewAdapter(ocrCli, tempStorage, warningCollector, cfg.OCR.RPSLimit, cfg.Retry.MaxAttempts, cfg.Retry.BackoffBase)
	textExtractor := textextract.NewExtractor(pdfUtil, tempStorage)
	structExtractor := structure.NewExtractor()
	treeBuilder := semantictree.NewBuilder()
	versionComparer := enginecomp.NewComparer()

	// --- Group 5: Application orchestrators ---
	cleanupFunc := func(ctx context.Context, jobID string) error {
		return tempStorage.DeleteByPrefix(ctx, jobID+"/")
	}

	lifecycleMgr := lifecycle.NewLifecycleManager(
		eventPublisher, idempotencyStore, cfg.Limits.JobTimeout, cleanupFunc,
	)

	procOrch := processing.NewOrchestrator(
		lifecycleMgr, warningCollector,
		inputValidator, fileFetcher, ocrAdapter,
		textExtractor, structExtractor, treeBuilder,
		tempStorage, eventPublisher, dmSender,
		cfg.Retry.MaxAttempts, cfg.Retry.BackoffBase,
	)

	compOrch := comparison.NewOrchestrator(
		lifecycleMgr, warningCollector,
		dmSender, dmSender, pendingRegistry, versionComparer,
		eventPublisher,
		cfg.Retry.MaxAttempts, cfg.Retry.BackoffBase,
	)

	// --- Group 6: DM response handler ---
	dmHandler := newDMResponseHandler(obs.Logger)

	// --- Group 7: Ingress layer ---
	brokerSub := &brokerSubscribeAdapter{client: brokerCli}
	disp := dispatcher.NewDispatcher(idempotencyStore, limiter, procOrch, compOrch, obs.Logger)
	cmdConsumer := consumer.NewConsumer(brokerSub, disp, obs.Logger, cfg.Broker)
	dmReceiver := dm.NewReceiver(brokerSub, dmHandler, pendingRegistry, obs.Logger, cfg.Broker)

	// --- Group 8: HTTP health server ---
	healthHandler := health.NewHandler()
	httpServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.HTTP.Port),
		Handler:           healthHandler.Mux(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	return &App{
		logger:     logger,
		obs:        obs,
		brokerCli:  brokerCli,
		kvCli:      kvCli,
		consumer:   cmdConsumer,
		dmReceiver: dmReceiver,
		httpServer: httpServer,
		health:     healthHandler,
	}, nil
}

// Run starts the HTTP health server and broker subscriptions, then blocks
// until ctx is cancelled. Returns 0 on clean shutdown, 1 on startup failure.
func (a *App) Run(ctx context.Context) int {
	// Start HTTP health server.
	go func() {
		if err := a.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			a.logger.Error(ctx, "health server failed", "error", err)
		}
	}()

	// Start broker subscriptions.
	if err := a.consumer.Start(); err != nil {
		a.logger.Error(ctx, "consumer start failed", "error", err)
		a.Shutdown(context.Background())
		return 1
	}
	if err := a.dmReceiver.Start(); err != nil {
		a.logger.Error(ctx, "dm receiver start failed", "error", err)
		a.Shutdown(context.Background())
		return 1
	}

	// Mark service as ready for traffic.
	a.health.SetReady(true)
	a.logger.Info(ctx, "dp-worker started, listening for commands")

	// Block until signal.
	<-ctx.Done()

	a.logger.Info(ctx, "shutdown signal received")
	a.Shutdown(context.Background())
	return 0
}

// Shutdown performs ordered teardown of all components.
// Safe to call multiple times — uses sync.Once internally.
//
// Order: stop readiness → close broker (drains in-flight handlers) →
// stop HTTP → close KV → flush observability.
func (a *App) Shutdown(ctx context.Context) {
	a.shutdownOnce.Do(func() {
		a.doShutdown(ctx)
	})
}

func (a *App) doShutdown(ctx context.Context) {
	shutdownCtx, cancel := context.WithTimeout(ctx, shutdownTimeout)
	defer cancel()

	// Phase 0: Signal not ready.
	if a.health != nil {
		a.health.SetReady(false)
	}

	// Phase 1: Close broker — stops consumers, drains in-flight handlers.
	if a.brokerCli != nil {
		a.logger.Info(shutdownCtx, "closing broker connection")
		if err := a.brokerCli.Close(); err != nil {
			a.logger.Error(shutdownCtx, "broker close error", "error", err)
		}
	}

	// Phase 2: Stop HTTP server.
	if a.httpServer != nil {
		a.logger.Info(shutdownCtx, "shutting down health server")
		if err := a.httpServer.Shutdown(shutdownCtx); err != nil {
			a.logger.Error(shutdownCtx, "health server shutdown error", "error", err)
		}
	}

	// Phase 3: Close KV store.
	if a.kvCli != nil {
		a.logger.Info(shutdownCtx, "closing kv store")
		if err := a.kvCli.Close(); err != nil {
			a.logger.Error(shutdownCtx, "kv store close error", "error", err)
		}
	}

	// Phase 4: Flush observability (traces/logs from all previous phases).
	if a.obs != nil {
		a.logger.Info(shutdownCtx, "flushing observability")
		if err := a.obs.Shutdown(shutdownCtx); err != nil {
			a.logger.Error(shutdownCtx, "observability shutdown error", "error", err)
		}
	}
}
