package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"contractpro/document-management/internal/config"
	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
	"contractpro/document-management/internal/ingress/idempotency"
)

// ---------------------------------------------------------------------------
// Consumer-side interfaces (dependency inversion for testability)
// ---------------------------------------------------------------------------

// Logger is the consumer-side interface for structured logging.
type Logger interface {
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// MetricsCollector is the consumer-side interface for event processing metrics.
type MetricsCollector interface {
	IncEventsReceived(topic string)
	IncEventsProcessed(topic string, status string)
}

// BrokerSubscriber is the consumer-side interface for subscribing to topics.
type BrokerSubscriber interface {
	Subscribe(topic string, handler func(ctx context.Context, body []byte) error) error
}

// IdempotencyChecker provides event deduplication with DB fallback.
type IdempotencyChecker interface {
	Check(ctx context.Context, key string, topic string, fallback idempotency.FallbackChecker) (idempotency.CheckResult, error)
	MarkCompleted(ctx context.Context, key string) error
	Cleanup(ctx context.Context, key string) error
}

// ---------------------------------------------------------------------------
// TopicConfig
// ---------------------------------------------------------------------------

// TopicConfig holds the 7 incoming topic names, loaded from DM configuration.
type TopicConfig struct {
	DPArtifactsReady    string
	DPSemanticTreeReq   string
	DPDiffReady         string
	LICArtifactsReady   string
	LICRequestArtifacts string
	REArtifactsReady    string
	RERequestArtifacts  string
}

// ---------------------------------------------------------------------------
// EventConsumer
// ---------------------------------------------------------------------------

// EventConsumer subscribes to 7 incoming event topics from DP, LIC, and RE,
// and dispatches deserialized events to DM application-layer handlers.
//
// Processing flow per message:
//  1. Deserialize JSON → Go struct
//  2. Validate required fields (correlation_id, timestamp, job_id, document_id)
//  3. Warn on unknown schema_version (REV-031)
//  4. Idempotency check → skip / process / reprocess
//  5. Route to correct application handler
//  6. Always return nil (prevent poison-pill requeue)
type EventConsumer struct {
	broker      BrokerSubscriber
	idempotency IdempotencyChecker
	logger      Logger
	metrics     MetricsCollector
	dlq         port.DLQPort

	ingestion   port.ArtifactIngestionHandler
	query       port.ArtifactQueryHandler
	diffHandler port.DiffStorageHandler

	artifactRepo port.ArtifactRepository // for idempotency fallback
	diffRepo     port.DiffRepository     // for idempotency fallback

	topics    TopicConfig
	retryCfg  config.RetryConfig
	startOnce sync.Once
	startErr  error
}

// dlqContext carries metadata for building a DLQRecord.
type dlqContext struct {
	category      model.DLQCategory
	rawBody       json.RawMessage
	correlationID string
	jobID         string
}

// NewEventConsumer creates an EventConsumer wired to all dependencies.
// Panics if any required dependency is nil or any topic is empty.
func NewEventConsumer(
	broker BrokerSubscriber,
	idem IdempotencyChecker,
	logger Logger,
	metrics MetricsCollector,
	dlq port.DLQPort,
	ingestion port.ArtifactIngestionHandler,
	query port.ArtifactQueryHandler,
	diffHandler port.DiffStorageHandler,
	artifactRepo port.ArtifactRepository,
	diffRepo port.DiffRepository,
	topics TopicConfig,
	retryCfg config.RetryConfig,
) *EventConsumer {
	if broker == nil {
		panic("consumer: broker must not be nil")
	}
	if idem == nil {
		panic("consumer: idempotency checker must not be nil")
	}
	if logger == nil {
		panic("consumer: logger must not be nil")
	}
	if metrics == nil {
		panic("consumer: metrics must not be nil")
	}
	if dlq == nil {
		panic("consumer: DLQ port must not be nil")
	}
	if ingestion == nil {
		panic("consumer: ingestion handler must not be nil")
	}
	if query == nil {
		panic("consumer: query handler must not be nil")
	}
	if diffHandler == nil {
		panic("consumer: diff handler must not be nil")
	}
	if artifactRepo == nil {
		panic("consumer: artifact repository must not be nil")
	}
	if diffRepo == nil {
		panic("consumer: diff repository must not be nil")
	}
	validateTopics(topics)

	return &EventConsumer{
		broker:       broker,
		idempotency:  idem,
		logger:       logger,
		metrics:      metrics,
		dlq:          dlq,
		ingestion:    ingestion,
		query:        query,
		diffHandler:  diffHandler,
		artifactRepo: artifactRepo,
		diffRepo:     diffRepo,
		topics:       topics,
		retryCfg:     retryCfg,
	}
}

func validateTopics(t TopicConfig) {
	check := func(name, value string) {
		if strings.TrimSpace(value) == "" {
			panic(fmt.Sprintf("consumer: topic %s must not be empty", name))
		}
	}
	check("DPArtifactsReady", t.DPArtifactsReady)
	check("DPSemanticTreeReq", t.DPSemanticTreeReq)
	check("DPDiffReady", t.DPDiffReady)
	check("LICArtifactsReady", t.LICArtifactsReady)
	check("LICRequestArtifacts", t.LICRequestArtifacts)
	check("REArtifactsReady", t.REArtifactsReady)
	check("RERequestArtifacts", t.RERequestArtifacts)
}

// Start subscribes to all 7 incoming topics. Idempotent via sync.Once.
func (c *EventConsumer) Start() error {
	c.startOnce.Do(func() {
		subs := []struct {
			topic   string
			handler func(ctx context.Context, body []byte)
		}{
			{c.topics.DPArtifactsReady, c.handleDPArtifacts},
			{c.topics.DPSemanticTreeReq, c.handleGetSemanticTree},
			{c.topics.DPDiffReady, c.handleDiffReady},
			{c.topics.LICArtifactsReady, c.handleLICArtifacts},
			{c.topics.LICRequestArtifacts, c.handleLICRequestArtifacts},
			{c.topics.REArtifactsReady, c.handleREArtifacts},
			{c.topics.RERequestArtifacts, c.handleRERequestArtifacts},
		}
		for _, sub := range subs {
			wrapped := c.wrapHandler(sub.topic, sub.handler)
			if err := c.broker.Subscribe(sub.topic, wrapped); err != nil {
				c.startErr = fmt.Errorf("consumer: subscribe to %s: %w", sub.topic, err)
				return
			}
			c.logger.Info("subscribed to topic", "topic", sub.topic)
		}
	})
	return c.startErr
}

// wrapHandler wraps a per-topic handler with panic recovery and nil return.
func (c *EventConsumer) wrapHandler(
	topic string,
	fn func(ctx context.Context, body []byte),
) func(ctx context.Context, body []byte) error {
	return func(ctx context.Context, body []byte) (retErr error) {
		defer func() {
			if r := recover(); r != nil {
				c.logger.Error("panic in event handler",
					"topic", topic, "panic", fmt.Sprintf("%v", r),
					"stack", string(debug.Stack()))
				c.metrics.IncEventsProcessed(topic, "panic")
				retErr = nil
			}
		}()
		fn(ctx, body)
		return nil // always nil — prevent poison-pill requeue
	}
}

// ---------------------------------------------------------------------------
// Per-topic handlers
// ---------------------------------------------------------------------------

func (c *EventConsumer) handleDPArtifacts(ctx context.Context, body []byte) {
	topic := c.topics.DPArtifactsReady
	c.metrics.IncEventsReceived(topic)

	var event model.DocumentProcessingArtifactsReady
	if err := json.Unmarshal(body, &event); err != nil {
		c.logInvalidMessage(topic, body, err)
		c.sendToDLQ(ctx, topic, dlqContext{category: model.DLQCategoryInvalid, rawBody: body},
			port.NewInvalidPayloadError("unmarshal failed", err))
		return
	}

	checkSchemaVersion(c.logger, body)

	if err := validateCommon(event.CorrelationID, event.JobID, event.DocumentID, event.Timestamp); err != nil {
		c.logValidationFailure(topic, event.JobID, err)
		c.sendToDLQ(ctx, topic, dlqContext{
			category: model.DLQCategoryInvalid, rawBody: body,
			correlationID: event.CorrelationID, jobID: event.JobID,
		}, port.NewValidationError(err.Error()))
		return
	}

	c.logger.Info("received event",
		"topic", topic, "job_id", event.JobID, "document_id", event.DocumentID)

	key := idempotency.KeyForDPArtifacts(event.JobID)
	fallback := idempotency.ArtifactFallback(
		c.artifactRepo, event.OrgID, event.DocumentID, event.VersionID, event.JobID, model.ProducerDomainDP,
	)

	dc := dlqContext{
		category: model.DLQCategoryIngestion, rawBody: body,
		correlationID: event.CorrelationID, jobID: event.JobID,
	}
	c.processWithIdempotency(ctx, topic, key, fallback, func(ctx context.Context) error {
		return c.ingestion.HandleDPArtifacts(ctx, event)
	}, dc)
}

func (c *EventConsumer) handleGetSemanticTree(ctx context.Context, body []byte) {
	topic := c.topics.DPSemanticTreeReq
	c.metrics.IncEventsReceived(topic)

	var event model.GetSemanticTreeRequest
	if err := json.Unmarshal(body, &event); err != nil {
		c.logInvalidMessage(topic, body, err)
		c.sendToDLQ(ctx, topic, dlqContext{category: model.DLQCategoryInvalid, rawBody: body},
			port.NewInvalidPayloadError("unmarshal failed", err))
		return
	}

	checkSchemaVersion(c.logger, body)

	if err := validateCommon(event.CorrelationID, event.JobID, event.DocumentID, event.Timestamp); err != nil {
		c.logValidationFailure(topic, event.JobID, err)
		c.sendToDLQ(ctx, topic, dlqContext{
			category: model.DLQCategoryInvalid, rawBody: body,
			correlationID: event.CorrelationID, jobID: event.JobID,
		}, port.NewValidationError(err.Error()))
		return
	}
	if strings.TrimSpace(event.VersionID) == "" {
		validationErr := fmt.Errorf("missing required field: version_id")
		c.logValidationFailure(topic, event.JobID, validationErr)
		c.sendToDLQ(ctx, topic, dlqContext{
			category: model.DLQCategoryInvalid, rawBody: body,
			correlationID: event.CorrelationID, jobID: event.JobID,
		}, port.NewValidationError(validationErr.Error()))
		return
	}

	c.logger.Info("received event",
		"topic", topic, "job_id", event.JobID, "version_id", event.VersionID)

	key := idempotency.KeyForSemanticTreeRequest(event.JobID, event.VersionID)
	dc := dlqContext{
		category: model.DLQCategoryQuery, rawBody: body,
		correlationID: event.CorrelationID, jobID: event.JobID,
	}
	c.processWithIdempotency(ctx, topic, key, noopFallback, func(ctx context.Context) error {
		return c.query.HandleGetSemanticTree(ctx, event)
	}, dc)
}

func (c *EventConsumer) handleDiffReady(ctx context.Context, body []byte) {
	topic := c.topics.DPDiffReady
	c.metrics.IncEventsReceived(topic)

	var event model.DocumentVersionDiffReady
	if err := json.Unmarshal(body, &event); err != nil {
		c.logInvalidMessage(topic, body, err)
		c.sendToDLQ(ctx, topic, dlqContext{category: model.DLQCategoryInvalid, rawBody: body},
			port.NewInvalidPayloadError("unmarshal failed", err))
		return
	}

	checkSchemaVersion(c.logger, body)

	if err := validateCommon(event.CorrelationID, event.JobID, event.DocumentID, event.Timestamp); err != nil {
		c.logValidationFailure(topic, event.JobID, err)
		c.sendToDLQ(ctx, topic, dlqContext{
			category: model.DLQCategoryInvalid, rawBody: body,
			correlationID: event.CorrelationID, jobID: event.JobID,
		}, port.NewValidationError(err.Error()))
		return
	}
	if strings.TrimSpace(event.BaseVersionID) == "" || strings.TrimSpace(event.TargetVersionID) == "" {
		validationErr := fmt.Errorf("missing required fields: base_version_id and/or target_version_id")
		c.logValidationFailure(topic, event.JobID, validationErr)
		c.sendToDLQ(ctx, topic, dlqContext{
			category: model.DLQCategoryInvalid, rawBody: body,
			correlationID: event.CorrelationID, jobID: event.JobID,
		}, port.NewValidationError(validationErr.Error()))
		return
	}

	c.logger.Info("received event",
		"topic", topic, "job_id", event.JobID, "document_id", event.DocumentID)

	key := idempotency.KeyForDiffReady(event.JobID)
	fallback := idempotency.DiffFallback(
		c.diffRepo, event.OrgID, event.DocumentID, event.BaseVersionID, event.TargetVersionID,
	)

	dc := dlqContext{
		category: model.DLQCategoryIngestion, rawBody: body,
		correlationID: event.CorrelationID, jobID: event.JobID,
	}
	c.processWithIdempotency(ctx, topic, key, fallback, func(ctx context.Context) error {
		return c.diffHandler.HandleDiffReady(ctx, event)
	}, dc)
}

func (c *EventConsumer) handleLICArtifacts(ctx context.Context, body []byte) {
	topic := c.topics.LICArtifactsReady
	c.metrics.IncEventsReceived(topic)

	var event model.LegalAnalysisArtifactsReady
	if err := json.Unmarshal(body, &event); err != nil {
		c.logInvalidMessage(topic, body, err)
		c.sendToDLQ(ctx, topic, dlqContext{category: model.DLQCategoryInvalid, rawBody: body},
			port.NewInvalidPayloadError("unmarshal failed", err))
		return
	}

	checkSchemaVersion(c.logger, body)

	if err := validateCommon(event.CorrelationID, event.JobID, event.DocumentID, event.Timestamp); err != nil {
		c.logValidationFailure(topic, event.JobID, err)
		c.sendToDLQ(ctx, topic, dlqContext{
			category: model.DLQCategoryInvalid, rawBody: body,
			correlationID: event.CorrelationID, jobID: event.JobID,
		}, port.NewValidationError(err.Error()))
		return
	}

	c.logger.Info("received event",
		"topic", topic, "job_id", event.JobID, "document_id", event.DocumentID)

	key := idempotency.KeyForLICArtifacts(event.JobID)
	fallback := idempotency.ArtifactFallback(
		c.artifactRepo, event.OrgID, event.DocumentID, event.VersionID, event.JobID, model.ProducerDomainLIC,
	)

	dc := dlqContext{
		category: model.DLQCategoryIngestion, rawBody: body,
		correlationID: event.CorrelationID, jobID: event.JobID,
	}
	c.processWithIdempotency(ctx, topic, key, fallback, func(ctx context.Context) error {
		return c.ingestion.HandleLICArtifacts(ctx, event)
	}, dc)
}

func (c *EventConsumer) handleLICRequestArtifacts(ctx context.Context, body []byte) {
	c.handleGetArtifactsRequest(ctx, body, c.topics.LICRequestArtifacts,
		func(jobID, versionID string) string { return idempotency.KeyForLICRequest(jobID, versionID) },
		model.DLQCategoryQuery,
	)
}

func (c *EventConsumer) handleREArtifacts(ctx context.Context, body []byte) {
	topic := c.topics.REArtifactsReady
	c.metrics.IncEventsReceived(topic)

	var event model.ReportsArtifactsReady
	if err := json.Unmarshal(body, &event); err != nil {
		c.logInvalidMessage(topic, body, err)
		c.sendToDLQ(ctx, topic, dlqContext{category: model.DLQCategoryInvalid, rawBody: body},
			port.NewInvalidPayloadError("unmarshal failed", err))
		return
	}

	checkSchemaVersion(c.logger, body)

	if err := validateCommon(event.CorrelationID, event.JobID, event.DocumentID, event.Timestamp); err != nil {
		c.logValidationFailure(topic, event.JobID, err)
		c.sendToDLQ(ctx, topic, dlqContext{
			category: model.DLQCategoryInvalid, rawBody: body,
			correlationID: event.CorrelationID, jobID: event.JobID,
		}, port.NewValidationError(err.Error()))
		return
	}

	c.logger.Info("received event",
		"topic", topic, "job_id", event.JobID, "document_id", event.DocumentID)

	key := idempotency.KeyForREArtifacts(event.JobID)
	fallback := idempotency.ArtifactFallback(
		c.artifactRepo, event.OrgID, event.DocumentID, event.VersionID, event.JobID, model.ProducerDomainRE,
	)

	dc := dlqContext{
		category: model.DLQCategoryIngestion, rawBody: body,
		correlationID: event.CorrelationID, jobID: event.JobID,
	}
	c.processWithIdempotency(ctx, topic, key, fallback, func(ctx context.Context) error {
		return c.ingestion.HandleREArtifacts(ctx, event)
	}, dc)
}

func (c *EventConsumer) handleRERequestArtifacts(ctx context.Context, body []byte) {
	c.handleGetArtifactsRequest(ctx, body, c.topics.RERequestArtifacts,
		func(jobID, versionID string) string { return idempotency.KeyForRERequest(jobID, versionID) },
		model.DLQCategoryQuery,
	)
}

// handleGetArtifactsRequest is the shared implementation for both LIC and RE
// artifact request topics. They use the same event struct (GetArtifactsRequest)
// but arrive on different topics with different idempotency keys.
func (c *EventConsumer) handleGetArtifactsRequest(
	ctx context.Context,
	body []byte,
	topic string,
	keyFn func(jobID, versionID string) string,
	category model.DLQCategory,
) {
	c.metrics.IncEventsReceived(topic)

	var event model.GetArtifactsRequest
	if err := json.Unmarshal(body, &event); err != nil {
		c.logInvalidMessage(topic, body, err)
		c.sendToDLQ(ctx, topic, dlqContext{category: model.DLQCategoryInvalid, rawBody: body},
			port.NewInvalidPayloadError("unmarshal failed", err))
		return
	}

	checkSchemaVersion(c.logger, body)

	if err := validateCommon(event.CorrelationID, event.JobID, event.DocumentID, event.Timestamp); err != nil {
		c.logValidationFailure(topic, event.JobID, err)
		c.sendToDLQ(ctx, topic, dlqContext{
			category: model.DLQCategoryInvalid, rawBody: body,
			correlationID: event.CorrelationID, jobID: event.JobID,
		}, port.NewValidationError(err.Error()))
		return
	}
	if strings.TrimSpace(event.VersionID) == "" {
		validationErr := fmt.Errorf("missing required field: version_id")
		c.logValidationFailure(topic, event.JobID, validationErr)
		c.sendToDLQ(ctx, topic, dlqContext{
			category: model.DLQCategoryInvalid, rawBody: body,
			correlationID: event.CorrelationID, jobID: event.JobID,
		}, port.NewValidationError(validationErr.Error()))
		return
	}

	c.logger.Info("received event",
		"topic", topic, "job_id", event.JobID, "version_id", event.VersionID)

	key := keyFn(event.JobID, event.VersionID)
	dc := dlqContext{
		category: category, rawBody: body,
		correlationID: event.CorrelationID, jobID: event.JobID,
	}
	c.processWithIdempotency(ctx, topic, key, noopFallback, func(ctx context.Context) error {
		return c.query.HandleGetArtifacts(ctx, event)
	}, dc)
}

// ---------------------------------------------------------------------------
// Shared processing logic
// ---------------------------------------------------------------------------

// processWithIdempotency runs the idempotency check and calls the handler.
// On success: marks the key as completed.
// On retryable error: applies backoff delay (BRE-025), cleans up idempotency key.
// On non-retryable error: sends to DLQ immediately.
// Never propagates errors — all outcomes are logged and metered.
func (c *EventConsumer) processWithIdempotency(
	ctx context.Context,
	topic string,
	key string,
	fallback idempotency.FallbackChecker,
	handler func(ctx context.Context) error,
	dc dlqContext,
) {
	result, err := c.idempotency.Check(ctx, key, topic, fallback)
	if err != nil {
		c.logger.Error("idempotency check failed",
			"topic", topic, "key", key, "error", err)
		c.metrics.IncEventsProcessed(topic, "error")
		return
	}

	switch result {
	case idempotency.ResultSkip:
		c.logger.Info("duplicate event, skipping", "topic", topic, "key", key)
		c.metrics.IncEventsProcessed(topic, "skipped")
		return
	case idempotency.ResultReprocess:
		c.logger.Warn("reprocessing stale event", "topic", topic, "key", key)
	}

	if err := handler(ctx); err != nil {
		c.logger.Error("handler failed",
			"topic", topic, "key", key, "error", err,
			"retryable", port.IsRetryable(err))

		if cleanupErr := c.idempotency.Cleanup(ctx, key); cleanupErr != nil {
			c.logger.Error("failed to cleanup idempotency key",
				"topic", topic, "key", key, "error", cleanupErr)
		}

		if port.IsRetryable(err) {
			// BRE-025: client-side backoff delay to prevent tight loop
			// when the same event is rapidly re-delivered.
			c.applyBackoff(ctx)
		} else {
			// Non-retryable: send to DLQ immediately.
			c.sendToDLQ(ctx, topic, dc, err)
		}

		c.metrics.IncEventsProcessed(topic, "error")
		return
	}

	if markErr := c.idempotency.MarkCompleted(ctx, key); markErr != nil {
		c.logger.Error("failed to mark idempotency completed",
			"topic", topic, "key", key, "error", markErr)
	}
	c.metrics.IncEventsProcessed(topic, "success")
}

// sendToDLQ builds a DLQRecord and publishes it via the DLQ port.
// DLQ publish failures are logged but do not block processing — the message
// is already ACKed.
func (c *EventConsumer) sendToDLQ(ctx context.Context, topic string, dc dlqContext, handlerErr error) {
	errCode := port.ErrorCode(handlerErr)
	if errCode == "" {
		errCode = "UNKNOWN"
	}

	record := model.DLQRecord{
		OriginalTopic:   topic,
		OriginalMessage: dc.rawBody,
		ErrorCode:       errCode,
		ErrorMessage:    handlerErr.Error(),
		RetryCount:      0,
		CorrelationID:   dc.correlationID,
		JobID:           dc.jobID,
		FailedAt:        time.Now().UTC(),
		Category:        dc.category,
	}

	if dlqErr := c.dlq.SendToDLQ(ctx, record); dlqErr != nil {
		c.logger.Error("failed to send to DLQ",
			"topic", topic, "job_id", dc.jobID, "error", dlqErr)
	}
}

// applyBackoff sleeps for BackoffBase duration to prevent tight loops on
// persistent failures (BRE-025). Respects context cancellation.
func (c *EventConsumer) applyBackoff(ctx context.Context) {
	delay := c.retryCfg.BackoffBase
	if delay <= 0 {
		return
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}

// ---------------------------------------------------------------------------
// Validation helpers
// ---------------------------------------------------------------------------

// validateCommon checks the 4 required fields shared by all incoming events.
func validateCommon(correlationID, jobID, documentID string, timestamp time.Time) error {
	var missing []string
	if strings.TrimSpace(correlationID) == "" {
		missing = append(missing, "correlation_id")
	}
	if strings.TrimSpace(jobID) == "" {
		missing = append(missing, "job_id")
	}
	if strings.TrimSpace(documentID) == "" {
		missing = append(missing, "document_id")
	}
	if timestamp.IsZero() {
		missing = append(missing, "timestamp")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required fields: %s", strings.Join(missing, ", "))
	}
	return nil
}

// schemaEnvelope extracts schema_version from raw JSON for REV-031 warning.
type schemaEnvelope struct {
	SchemaVersion string `json:"schema_version"`
}

// checkSchemaVersion warns on unknown schema_version (REV-031).
// Unknown versions are processed with best effort — no rejection.
func checkSchemaVersion(log Logger, body []byte) {
	var env schemaEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return
	}
	if env.SchemaVersion != "" && env.SchemaVersion != "1.0" {
		log.Warn("unknown schema_version, processing with best effort",
			"schema_version", env.SchemaVersion)
	}
}

// noopFallback is used for query requests that have no DB side effects
// to check. Always returns false (not processed), allowing processing.
func noopFallback(_ context.Context) (bool, error) {
	return false, nil
}

// ---------------------------------------------------------------------------
// Logging helpers
// ---------------------------------------------------------------------------

const maxPreviewLen = 200

func (c *EventConsumer) logInvalidMessage(topic string, body []byte, err error) {
	c.logger.Error("failed to unmarshal event",
		"topic", topic, "error", err, "raw_preview", rawPreview(body))
	c.metrics.IncEventsProcessed(topic, "invalid")
}

func (c *EventConsumer) logValidationFailure(topic, jobID string, err error) {
	c.logger.Error("event validation failed",
		"topic", topic, "job_id", jobID, "error", err)
	c.metrics.IncEventsProcessed(topic, "invalid")
}

// rawPreview returns a truncated preview of raw message body for error logging.
// Truncates at a valid UTF-8 rune boundary to avoid splitting multi-byte characters.
func rawPreview(body []byte) string {
	if len(body) <= maxPreviewLen {
		return string(body)
	}
	// Find the last valid rune boundary at or before maxPreviewLen.
	end := maxPreviewLen
	for end > 0 && !utf8.RuneStart(body[end]) {
		end--
	}
	return string(body[:end]) + "..."
}
