package confirmwatchdog

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"

	"contractpro/api-orchestrator/internal/application/statustracker"
	"contractpro/api-orchestrator/internal/infra/kvstore"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
)

const (
	confirmationWaitPrefix = "confirmation:wait:"
	keyspaceChannel        = "__keyevent@0__:expired"
	scanPattern            = "confirmation:wait:*"
	perKeyTimeout          = 10 * time.Second
	retryDelay             = 2 * time.Second
)

// StatusTracker provides the timeout transition for expired confirmations.
type StatusTracker interface {
	TimeoutAwaitingInput(ctx context.Context, orgID, docID, verID string) error
}

// KVStore provides key-value operations for confirmation metadata.
type KVStore interface {
	Get(ctx context.Context, key string) (string, error)
	Delete(ctx context.Context, key string) error
}

// RedisSubscriber provides Redis operations needed for watching key expirations.
type RedisSubscriber interface {
	ConfigSet(ctx context.Context, parameter, value string) *redis.StatusCmd
	PSubscribe(ctx context.Context, patterns ...string) *redis.PubSub
	Scan(ctx context.Context, cursor uint64, match string, count int64) *redis.ScanCmd
	PTTL(ctx context.Context, key string) *redis.DurationCmd
}

// confirmationMeta mirrors the struct stored by classification_uncertain handler.
type confirmationMeta struct {
	OrganizationID string `json:"organization_id"`
	DocumentID     string `json:"document_id"`
	VersionID      string `json:"version_id"`
	JobID          string `json:"job_id"`
}

// Watchdog monitors Redis for expired confirmation:wait:* keys and transitions
// the corresponding versions to FAILED with USER_CONFIRMATION_TIMEOUT.
//
// Primary mechanism: Redis Keyspace Notifications (subscribe to key expiry events).
// Fallback: periodic SCAN of confirmation:wait:* keys with PTTL check
// (if notifications unavailable, e.g. AWS ElastiCache).
type Watchdog struct {
	tracker      StatusTracker
	kv           KVStore
	rdb          RedisSubscriber
	log          *logger.Logger
	timeouts     prometheus.Counter
	scanInterval time.Duration

	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	startOnce sync.Once
	startErr  error
}

// NewWatchdog creates a Watchdog that monitors expired confirmation:wait:* keys.
func NewWatchdog(
	tracker StatusTracker,
	kv KVStore,
	rdb RedisSubscriber,
	log *logger.Logger,
	timeouts prometheus.Counter,
	scanInterval time.Duration,
) *Watchdog {
	ctx, cancel := context.WithCancel(context.Background())
	return &Watchdog{
		tracker:      tracker,
		kv:           kv,
		rdb:          rdb,
		log:          log.With("component", "confirmation-watchdog"),
		timeouts:     timeouts,
		scanInterval: scanInterval,
		ctx:          ctx,
		cancel:       cancel,
	}
}

// Start begins watching for expired confirmation keys. It first attempts to
// enable Redis Keyspace Notifications; if that fails, it falls back to
// periodic SCAN. Start is idempotent.
func (w *Watchdog) Start() error {
	w.startOnce.Do(func() {
		if w.tryEnableKeyspaceNotifications() {
			w.startKeyspaceWatcher()
			w.log.Info(w.ctx, "started with keyspace notifications")
		} else {
			w.startScanLoop()
			w.log.Info(w.ctx, "started with periodic SCAN fallback",
				"interval", w.scanInterval.String())
		}
	})
	return w.startErr
}

// Shutdown stops the watchdog and waits for the background goroutine to exit.
func (w *Watchdog) Shutdown() {
	w.cancel()
	w.wg.Wait()
}

// tryEnableKeyspaceNotifications attempts CONFIG SET notify-keyspace-events Ex.
// Returns true if successful, false if Redis does not permit CONFIG SET
// (e.g., AWS ElastiCache, ACL restrictions).
func (w *Watchdog) tryEnableKeyspaceNotifications() bool {
	ctx, cancel := context.WithTimeout(w.ctx, 5*time.Second)
	defer cancel()

	err := w.rdb.ConfigSet(ctx, "notify-keyspace-events", "Ex").Err()
	if err != nil {
		w.log.Warn(w.ctx, "cannot enable keyspace notifications, falling back to SCAN",
			"error", err.Error())
		return false
	}
	return true
}

// startKeyspaceWatcher subscribes to __keyevent@0__:expired and processes
// expiry events for confirmation:wait:* keys.
func (w *Watchdog) startKeyspaceWatcher() {
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.runKeyspaceWatcher()
	}()
}

func (w *Watchdog) runKeyspaceWatcher() {
	for {
		if w.ctx.Err() != nil {
			return
		}

		pubsub := w.rdb.PSubscribe(w.ctx, keyspaceChannel)

		_, err := pubsub.Receive(w.ctx)
		if err != nil {
			_ = pubsub.Close()
			if w.ctx.Err() != nil {
				return
			}
			w.log.Error(w.ctx, "keyspace subscription failed, retrying in 5s",
				"error", err.Error())
			select {
			case <-time.After(5 * time.Second):
				continue
			case <-w.ctx.Done():
				return
			}
		}

		w.log.Info(w.ctx, "keyspace subscription established")
		ch := pubsub.Channel()

		for {
			select {
			case msg, ok := <-ch:
				if !ok {
					w.log.Warn(w.ctx, "keyspace subscription channel closed, reconnecting")
					goto reconnect
				}
				w.handleExpiredKey(msg.Payload)
			case <-w.ctx.Done():
				_ = pubsub.Close()
				return
			}
		}

	reconnect:
		_ = pubsub.Close()
	}
}

// startScanLoop periodically scans for confirmation:wait:* keys approaching
// expiry and proactively triggers timeout.
func (w *Watchdog) startScanLoop() {
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.runScanLoop()
	}()
}

func (w *Watchdog) runScanLoop() {
	ticker := time.NewTicker(w.scanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			w.scanExpiredKeys()
		case <-w.ctx.Done():
			return
		}
	}
}

// scanExpiredKeys scans for confirmation:wait:* keys and checks their PTTL.
// Keys with TTL <= scanInterval are proactively timed out (they will expire
// before the next scan). The Lua-based TimeoutAwaitingInput provides the
// atomicity guarantee — if the user confirms between SCAN and timeout, the
// Lua script returns ErrInvalidTransition and the watchdog skips gracefully.
func (w *Watchdog) scanExpiredKeys() {
	ctx, cancel := context.WithTimeout(w.ctx, 30*time.Second)
	defer cancel()

	var cursor uint64
	for {
		keys, nextCursor, err := w.rdb.Scan(ctx, cursor, scanPattern, 100).Result()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			w.log.Error(w.ctx, "SCAN failed",
				"error", err.Error())
			return
		}

		for _, key := range keys {
			ttl, err := w.rdb.PTTL(ctx, key).Result()
			if err != nil {
				w.log.Warn(w.ctx, "PTTL failed, skipping key",
					"key", key,
					"error", err.Error())
				continue
			}

			// PTTL returns:
			//   -2 = key does not exist (expired between SCAN and PTTL)
			//   -1 = key exists with no TTL (shouldn't happen for watchdog keys)
			//   ≥0 = remaining TTL in milliseconds
			//
			// Trigger timeout if key expired (-2), has no TTL (-1), or
			// will expire before the next SCAN cycle.
			if ttl < 0 || ttl <= w.scanInterval {
				w.handleExpiredKey(key)
			}
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
}

// handleExpiredKey processes a single expired (or expiring) confirmation:wait:* key.
func (w *Watchdog) handleExpiredKey(key string) {
	if !strings.HasPrefix(key, confirmationWaitPrefix) {
		return
	}

	verID := strings.TrimPrefix(key, confirmationWaitPrefix)
	if verID == "" {
		w.log.Warn(w.ctx, "expired key has empty version_id", "key", key)
		return
	}

	if _, err := uuid.Parse(verID); err != nil {
		w.log.Warn(w.ctx, "expired key has invalid version_id format",
			"key", key,
			"version_id", verID)
		return
	}

	ctx, cancel := context.WithTimeout(w.ctx, perKeyTimeout)
	defer cancel()

	metaKey := statustracker.ConfirmationMetaKey(verID)
	raw, err := w.kv.Get(ctx, metaKey)
	if err != nil {
		if errors.Is(err, kvstore.ErrKeyNotFound) {
			w.log.Warn(w.ctx, "confirmation meta not found for expired key, skipping",
				"version_id", verID,
				"meta_key", metaKey)
			return
		}
		w.log.Error(w.ctx, "failed to read confirmation meta",
			"version_id", verID,
			logger.ErrorAttr(err))
		return
	}

	var meta confirmationMeta
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		w.log.Warn(w.ctx, "corrupt confirmation meta, skipping",
			"version_id", verID,
			"raw", raw,
			logger.ErrorAttr(err))
		return
	}

	if meta.OrganizationID == "" || meta.DocumentID == "" {
		w.log.Warn(w.ctx, "confirmation meta missing identity fields, skipping",
			"version_id", verID)
		return
	}

	err = w.tracker.TimeoutAwaitingInput(ctx, meta.OrganizationID, meta.DocumentID, verID)
	if err != nil {
		if errors.Is(err, statustracker.ErrInvalidTransition) {
			w.log.Info(w.ctx, "watchdog: version already transitioned, skipping",
				"version_id", verID)
			return
		}

		// Transient failure — retry once after brief delay. Without retry,
		// the version would remain stuck in AWAITING_USER_INPUT permanently
		// because the watchdog key is already gone.
		w.log.Warn(w.ctx, "watchdog: TimeoutAwaitingInput failed, retrying",
			"version_id", verID,
			logger.ErrorAttr(err))

		select {
		case <-time.After(retryDelay):
		case <-ctx.Done():
			return
		}

		err = w.tracker.TimeoutAwaitingInput(ctx, meta.OrganizationID, meta.DocumentID, verID)
		if err != nil {
			if errors.Is(err, statustracker.ErrInvalidTransition) {
				w.log.Info(w.ctx, "watchdog: version transitioned during retry, skipping",
					"version_id", verID)
				return
			}
			w.log.Error(w.ctx, "watchdog: retry also failed, version may be stuck",
				"version_id", verID,
				logger.ErrorAttr(err))
			return
		}
	}

	w.timeouts.Inc()
	w.log.Info(w.ctx, "watchdog: version timed out",
		"version_id", verID,
		"organization_id", meta.OrganizationID,
		"document_id", meta.DocumentID)

	// Clean up stale meta key (best-effort).
	if err := w.kv.Delete(ctx, metaKey); err != nil {
		w.log.Warn(w.ctx, "watchdog: failed to delete confirmation meta",
			"meta_key", metaKey,
			logger.ErrorAttr(err))
	}
}
