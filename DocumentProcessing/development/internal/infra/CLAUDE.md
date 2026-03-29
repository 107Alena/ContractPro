# Infrastructure Layer — CLAUDE.md

External service clients and platform adapters. All clients implement hexagonal outbound ports.

## broker/ — RabbitMQ Client

Message broker for publishing and subscribing to events.

- **client.go** — PublishEvent(topic, payload), SubscribeTopic(topic, handler) with auto-reconnect
- **reconnect.go** — Background reconnection logic with exponential backoff
- **errors.go** — Typed errors: BrokerError, NotConnectedError, PublishFailedError

Constructor: `NewBrokerClient(cfg *config.Broker)` returns (*Client, error).

## kvstore/ — Redis Client

Key-value store for idempotency tracking and state storage (TTL support).

- **client.go** — Get(key), Set(key, value, ttl), Delete(key), Exists(key), IncrementCounter(key)
- **errors.go** — Typed errors: NotFoundError, RedisError, OperationFailedError

Constructor: `NewKVStoreClient(cfg *config.KVStore)` returns (*Client, error).

## objectstorage/ — Yandex Object Storage (S3)

Temporary artifact storage during processing (supports large binary files).

- **client.go** — Upload(key, data), Download(key), Delete(key), DeleteByPrefix(prefix), Exists(key)
- **errors.go** — Typed errors: NotFoundError, UploadFailedError, DownloadFailedError

Constructor: `NewObjectStorageClient(cfg *config.ObjectStorage)` returns (*Client, error).

## ocr/ — Yandex Cloud Vision OCR

External OCR service for recognizing text in scan PDFs.

- **client.go** — RecognizeImage(imageData) returns recognized text with confidence scores
- **errors.go** — Typed errors: RecognitionFailedError, InvalidImageError, RateLimitError

Constructor: `NewOCRClient(cfg *config.OCR)` returns (*Client, error).

## observability/ — Logging, Metrics, Tracing

Unified observability stack.

- **logger.go** — Structured logging (Info, Warn, Error, Debug) with context fields
- **metrics.go** — Prometheus metrics (counters, histograms, gauges) for all operations
- **tracer.go** — OpenTelemetry tracing integration (spans, attributes)
- **context.go** — Context helpers for tracing/logging across calls
- **handler.go** — HTTP handler for metrics export (GET /metrics)
- **observability.go** — Wires logger, metrics, tracer into unified instance

Constructor: `NewObservability(cfg *config.Observability)` returns (*Observability, error).

## concurrency/ — Concurrency Limiter

Semaphore-based limiter to cap concurrent operations.

- **limiter.go** — Acquire(), Release(), track active/waiting/rejected via metrics

Constructor: `NewConcurrencyLimiter(maxConcurrent int)` returns *Limiter.

## health/ — Health & Readiness Handler

HTTP handlers for orchestrator health checks.

- **handler.go** — GET /healthz (liveness), GET /readyz (readiness), SetReady(bool)

Constructor: `NewHealthHandler()` returns *HealthHandler.

## httpdownloader/ — HTTP File Downloader

Download files via HTTP with SSRF protection.

- **downloader.go** — Download(url, maxSize) returns bytes with size validation
- **ssrf.go** — SSRF protection: deny private IPs, localhost, metadata endpoints

Constructor: `NewHTTPDownloader()` returns *Downloader.

## Patterns

- All clients use constructor pattern: NewClient(cfg) returning (*Client, error)
- Compile-time interface checks: `var _ OutboundPort = (*Client)(nil)` in implementation files
- Typed error exports (NotFoundError, ConnectionFailedError, etc.) for client code
- Graceful shutdown hooks: all clients implement Close() if stateful
