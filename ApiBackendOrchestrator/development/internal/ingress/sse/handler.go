// Package sse implements the Server-Sent Events connection manager for the
// API/Backend Orchestrator.
//
// The SSE endpoint (GET /api/v1/events/stream?token=JWT) provides real-time
// processing status updates to frontend clients. It uses query-param
// authentication because the browser EventSource API does not support custom
// HTTP headers.
//
// Each connection subscribes to a Redis Pub/Sub channel scoped to the user's
// organization (sse:broadcast:{org_id}). The statustracker package publishes
// SSEEvent payloads to these channels when processing statuses change.
//
// Connections are registered in Redis for observability and have a configurable
// maximum lifetime (default 24h). Heartbeats are sent at a configurable
// interval (default 15s) to keep the connection alive through proxies and
// load balancers.
package sse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"contractpro/api-orchestrator/internal/config"
	"contractpro/api-orchestrator/internal/domain/model"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"
)

// ---------------------------------------------------------------------------
// Consumer-side interfaces (dependency inversion)
// ---------------------------------------------------------------------------

// TokenValidator validates a raw JWT string and returns the parsed claims.
// Satisfied by: *auth.Middleware (via its exported ValidateToken method).
type TokenValidator interface {
	ValidateToken(tokenString string) (*auth.Claims, error)
}

// Subscription represents an active Redis Pub/Sub subscription.
// Satisfied by: *kvstore.Subscription (which has Close() error).
type Subscription interface {
	Close() error
}

// KVStore provides the subset of Redis operations needed by the SSE handler:
// Pub/Sub subscription for receiving broadcast events, and key-value
// operations for connection registration.
//
// Satisfied by: *kvstore.Client
type KVStore interface {
	Subscribe(ctx context.Context, channel string, handler func(msg string)) (Subscription, error)
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
}

// ---------------------------------------------------------------------------
// UUIDGenerator (seam for testing)
// ---------------------------------------------------------------------------

// UUIDGenerator generates UUID v4 strings. The default implementation uses
// google/uuid. Tests can replace this with a deterministic generator.
type UUIDGenerator func() string

// defaultUUIDGenerator is the production UUID generator.
func defaultUUIDGenerator() string {
	return uuid.New().String()
}

// ---------------------------------------------------------------------------
// Connection registration record
// ---------------------------------------------------------------------------

// connRecord is the JSON value stored in Redis for each active SSE connection.
type connRecord struct {
	ConnectedAt string `json:"connected_at"`
	UserID      string `json:"user_id"`
	OrgID       string `json:"org_id"`
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// eventBufferSize is the capacity of the internal events channel.
	// If the channel is full, incoming events are dropped to prevent
	// backpressure from a slow client blocking the Redis subscription
	// goroutine.
	eventBufferSize = 64

	// sseChannelPrefix matches the channel prefix used by the statustracker.
	sseChannelPrefix = "sse:broadcast"

	// connKeyPrefix is the Redis key prefix for connection registration.
	connKeyPrefix = "sse:conn"
)

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

// Handler manages SSE connections. It is stateless and safe for concurrent
// use by multiple goroutines.
type Handler struct {
	validator TokenValidator
	kv        KVStore
	cfg       config.SSEConfig
	log       *logger.Logger
	uuidGen   UUIDGenerator
	now       func() time.Time
}

// NewHandler creates an SSE Handler with the given dependencies.
//
// Parameters:
//   - validator: JWT token validator (typically *auth.Middleware).
//   - kv: Redis client for Pub/Sub and connection registration.
//   - cfg: SSE configuration (heartbeat interval, max connection age).
//   - log: root logger; the handler creates a component-scoped child.
func NewHandler(
	validator TokenValidator,
	kv KVStore,
	cfg config.SSEConfig,
	log *logger.Logger,
) *Handler {
	return &Handler{
		validator: validator,
		kv:        kv,
		cfg:       cfg,
		log:       log.With("component", "sse-handler"),
		uuidGen:   defaultUUIDGenerator,
		now:       time.Now,
	}
}

// Handle returns an http.HandlerFunc for GET /api/v1/events/stream.
//
// Authentication is performed via the "token" query parameter because the
// browser EventSource API does not support custom HTTP headers.
//
// The handler holds the HTTP connection open and streams SSE events until:
//   - The client disconnects
//   - The maximum connection age is reached
//   - A fatal error occurs
func (h *Handler) Handle() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// -----------------------------------------------------------
		// Step 1: Extract and validate JWT from query parameter
		// -----------------------------------------------------------
		tokenString := r.URL.Query().Get("token")
		if tokenString == "" {
			model.WriteError(w, r, model.ErrAuthTokenMissing, nil)
			return
		}

		claims, err := h.validator.ValidateToken(tokenString)
		if err != nil {
			h.log.Warn(ctx, "SSE token validation failed",
				"error", err.Error(),
			)
			if isExpiredError(err) {
				model.WriteError(w, r, model.ErrAuthTokenExpired, nil)
			} else {
				model.WriteError(w, r, model.ErrAuthTokenInvalid, nil)
			}
			return
		}

		orgID := claims.Org
		userID := claims.Subject

		// Enrich context for structured logging.
		ctx = logger.WithRequestContext(ctx, logger.RequestContext{
			OrganizationID: orgID,
			UserID:         userID,
		})

		// -----------------------------------------------------------
		// Step 2: Verify http.Flusher support
		// -----------------------------------------------------------
		flusher, ok := w.(http.Flusher)
		if !ok {
			h.log.Error(ctx, "ResponseWriter does not implement http.Flusher")
			model.WriteError(w, r, model.ErrInternalError, nil)
			return
		}

		// -----------------------------------------------------------
		// Step 3: Disable server WriteTimeout for this SSE connection
		// -----------------------------------------------------------
		// The main HTTP server has a WriteTimeout (~35s) that would kill
		// long-lived SSE connections. Use http.ResponseController (Go 1.20+)
		// to disable the write deadline for this connection only.
		rc := http.NewResponseController(w)
		if err := rc.SetWriteDeadline(time.Time{}); err != nil {
			h.log.Warn(ctx, "failed to disable write deadline for SSE",
				"error", err.Error(),
			)
			// Continue anyway — the connection may still work if the
			// server's WriteTimeout is generous enough.
		}

		// Set SSE headers and send 200.
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)

		// -----------------------------------------------------------
		// Step 4: Generate connection ID and set up context
		// -----------------------------------------------------------
		connID := h.uuidGen()

		connCtx, connCancel := context.WithCancel(ctx)
		defer connCancel()

		connLog := h.log.With(
			"conn_id", connID,
			"org_id", orgID,
			"user_id", userID,
		)

		connLog.Info(connCtx, "SSE connection opened")

		// -----------------------------------------------------------
		// Step 5: Register connection in Redis
		// -----------------------------------------------------------
		h.registerConnection(connCtx, connLog, orgID, userID, connID)

		// -----------------------------------------------------------
		// Step 6: Subscribe to Redis Pub/Sub channel
		// -----------------------------------------------------------
		channel := sseChannel(orgID)
		events := make(chan string, eventBufferSize)

		sub, err := h.kv.Subscribe(connCtx, channel, func(msg string) {
			select {
			case events <- msg:
			default:
				// Channel full — slow client. Drop the event to prevent
				// backpressure on the Redis subscription goroutine.
				connLog.Warn(connCtx, "SSE event dropped, client too slow")
			}
		})
		if err != nil {
			connLog.Error(connCtx, "failed to subscribe to Redis Pub/Sub",
				logger.ErrorAttr(err),
				"channel", channel,
			)
			// Headers already sent — cannot return an HTTP error response.
			// Write an SSE error event and close.
			writeSSEEvent(w, flusher, "error", `{"error_code":"BROKER_UNAVAILABLE","message":"Не удалось подключиться к потоку событий"}`)
			h.unregisterConnection(connCtx, connLog, orgID, userID, connID)
			return
		}
		defer sub.Close()
		defer h.unregisterConnection(connCtx, connLog, orgID, userID, connID)

		// -----------------------------------------------------------
		// Step 7: Send connected comment
		// -----------------------------------------------------------
		if writeErr := writeComment(w, flusher, "connected"); writeErr != nil {
			connLog.Debug(connCtx, "write error on connected comment",
				"error", writeErr.Error(),
			)
			return
		}

		// -----------------------------------------------------------
		// Step 8: Enter event loop
		// -----------------------------------------------------------
		h.eventLoop(connCtx, connLog, w, flusher, events, orgID, userID, connID)

		connLog.Info(connCtx, "SSE connection closed")
	}
}

// ---------------------------------------------------------------------------
// Event loop
// ---------------------------------------------------------------------------

// eventLoop runs the main SSE event loop. It blocks until the client
// disconnects, the maximum connection age is reached, or a write error
// occurs (broken pipe).
func (h *Handler) eventLoop(
	ctx context.Context,
	log *logger.Logger,
	w http.ResponseWriter,
	flusher http.Flusher,
	events <-chan string,
	orgID, userID, connID string,
) {
	heartbeatTicker := time.NewTicker(h.cfg.HeartbeatInterval)
	defer heartbeatTicker.Stop()

	maxAgeTimer := time.NewTimer(h.cfg.MaxConnectionAge)
	defer maxAgeTimer.Stop()

	for {
		select {
		case msg, ok := <-events:
			if !ok {
				// Channel closed — subscription ended.
				log.Info(ctx, "events channel closed, ending SSE connection")
				return
			}

			eventType := extractEventType(msg)
			if writeErr := writeSSEEvent(w, flusher, eventType, msg); writeErr != nil {
				log.Debug(ctx, "write error, client likely disconnected",
					"error", writeErr.Error(),
				)
				return
			}

		case <-heartbeatTicker.C:
			if writeErr := writeHeartbeat(w, flusher); writeErr != nil {
				log.Debug(ctx, "heartbeat write error, client likely disconnected",
					"error", writeErr.Error(),
				)
				return
			}
			// Refresh connection TTL in Redis.
			h.refreshConnection(ctx, log, orgID, userID, connID)

		case <-maxAgeTimer.C:
			log.Info(ctx, "max connection age reached, closing SSE connection",
				"max_age", h.cfg.MaxConnectionAge.String(),
			)
			// Send a reconnect hint so the client knows to reconnect.
			_ = writeSSEEvent(w, flusher, "connection_expired",
				`{"message":"Превышено максимальное время соединения. Переподключитесь."}`)
			return

		case <-ctx.Done():
			// Client disconnected or server shutting down.
			log.Debug(ctx, "context cancelled, ending SSE connection")
			return
		}
	}
}

// ---------------------------------------------------------------------------
// SSE wire format helpers
// ---------------------------------------------------------------------------

// writeSSEEvent writes a single SSE event. If the rawJSON contains newlines,
// each line is emitted with its own "data:" prefix per the SSE specification.
// This prevents stream corruption from payloads with embedded newlines.
//
// Returns an error if the write or flush fails (typically broken pipe).
func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, eventType, rawJSON string) error {
	if _, err := fmt.Fprintf(w, "event: %s\n", eventType); err != nil {
		return err
	}
	for _, line := range strings.Split(rawJSON, "\n") {
		if _, err := fmt.Fprintf(w, "data: %s\n", line); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprint(w, "\n"); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

// writeHeartbeat writes an SSE comment in the format:
//
//	:ping\n\n
//
// SSE comments (lines starting with ':') are ignored by the browser
// EventSource API but keep the TCP connection alive through proxies.
func writeHeartbeat(w http.ResponseWriter, flusher http.Flusher) error {
	_, err := fmt.Fprint(w, ":ping\n\n")
	if err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

// writeComment writes an SSE comment with a custom text.
func writeComment(w http.ResponseWriter, flusher http.Flusher, text string) error {
	_, err := fmt.Fprintf(w, ": %s\n\n", text)
	if err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

// ---------------------------------------------------------------------------
// Connection registration helpers
// ---------------------------------------------------------------------------

// registerConnection stores the connection record in Redis with a TTL of
// heartbeatInterval * 3. This allows other components to discover active
// SSE connections per organization.
func (h *Handler) registerConnection(
	ctx context.Context,
	log *logger.Logger,
	orgID, userID, connID string,
) {
	key := connKey(orgID, userID, connID)
	ttl := h.cfg.HeartbeatInterval * 3

	record := connRecord{
		ConnectedAt: h.now().UTC().Format(time.RFC3339),
		UserID:      userID,
		OrgID:       orgID,
	}
	data, _ := json.Marshal(record) // connRecord marshalling cannot fail.

	if err := h.kv.Set(ctx, key, string(data), ttl); err != nil {
		log.Warn(ctx, "failed to register SSE connection in Redis",
			logger.ErrorAttr(err),
			"redis_key", key,
		)
	}
}

// refreshConnection extends the TTL of the connection key in Redis.
// Called on each heartbeat to indicate the connection is still alive.
func (h *Handler) refreshConnection(
	ctx context.Context,
	log *logger.Logger,
	orgID, userID, connID string,
) {
	key := connKey(orgID, userID, connID)
	ttl := h.cfg.HeartbeatInterval * 3

	record := connRecord{
		ConnectedAt: h.now().UTC().Format(time.RFC3339),
		UserID:      userID,
		OrgID:       orgID,
	}
	data, _ := json.Marshal(record)

	if err := h.kv.Set(ctx, key, string(data), ttl); err != nil {
		log.Warn(ctx, "failed to refresh SSE connection TTL",
			logger.ErrorAttr(err),
			"redis_key", key,
		)
	}
}

// unregisterConnection removes the connection record from Redis.
// Called when the connection is closing for any reason.
func (h *Handler) unregisterConnection(
	ctx context.Context,
	log *logger.Logger,
	orgID, userID, connID string,
) {
	key := connKey(orgID, userID, connID)

	// Use a detached context for cleanup — the original context may already
	// be cancelled (client disconnect, server shutdown). A short timeout
	// prevents the cleanup from blocking indefinitely.
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := h.kv.Delete(cleanupCtx, key); err != nil {
		log.Warn(ctx, "failed to unregister SSE connection from Redis",
			logger.ErrorAttr(err),
			"redis_key", key,
		)
	}
}

// ---------------------------------------------------------------------------
// Key/channel builders
// ---------------------------------------------------------------------------

// sseChannel builds the Redis Pub/Sub channel name for an organization.
// Must match the channel used by the statustracker's broadcast method.
func sseChannel(orgID string) string {
	return sseChannelPrefix + ":" + orgID
}

// connKey builds the Redis key for a connection registration record.
func connKey(orgID, userID, connID string) string {
	return connKeyPrefix + ":" + orgID + ":" + userID + ":" + connID
}

// ---------------------------------------------------------------------------
// JSON helpers
// ---------------------------------------------------------------------------

// extractEventType reads the "event_type" field from a JSON payload without
// fully unmarshalling the entire object. Falls back to "message" if the field
// is missing or the payload is not valid JSON. The returned value is sanitized
// to prevent SSE injection via newline/CR characters.
func extractEventType(rawJSON string) string {
	var partial struct {
		EventType string `json:"event_type"`
	}
	if err := json.Unmarshal([]byte(rawJSON), &partial); err != nil || partial.EventType == "" {
		return "message"
	}
	return sanitizeEventType(partial.EventType)
}

// sanitizeEventType strips newline and carriage return characters from an
// event type string to prevent SSE field injection. Returns "message" if the
// result is empty after sanitization.
func sanitizeEventType(s string) string {
	s = strings.NewReplacer("\n", "", "\r", "").Replace(s)
	if s == "" {
		return "message"
	}
	return s
}

// ---------------------------------------------------------------------------
// Token error classification
// ---------------------------------------------------------------------------

// isExpiredError checks whether the JWT validation error is specifically about
// token expiration, so the handler can return AUTH_TOKEN_EXPIRED instead of
// the generic AUTH_TOKEN_INVALID. Mirrors the logic in auth.isExpiredError
// (which is unexported).
func isExpiredError(err error) bool {
	return errors.Is(err, jwt.ErrTokenExpired)
}
