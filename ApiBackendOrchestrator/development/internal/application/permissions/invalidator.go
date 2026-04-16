package permissions

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"
)

// KVDeleter provides key deletion used by the invalidator.
type KVDeleter interface {
	Delete(ctx context.Context, key string) error
}

// RedisPSubscriber is the subset of go-redis client methods the invalidator
// uses to pattern-subscribe for cache invalidation events.
type RedisPSubscriber interface {
	PSubscribe(ctx context.Context, channels ...string) *redis.PubSub
}

// CacheInvalidator subscribes to "permissions:invalidate:*" and removes all
// cached permissions entries for the triggering organization. One PSUBSCRIBE
// covers every org — no subscription churn when organizations appear.
//
// The invalidator is best-effort: if the subscription drops, it reconnects
// with a short backoff. If an individual DEL fails, it logs WARN and moves
// on (stale cache resolves itself at TTL expiry — worst case 5 min).
type CacheInvalidator struct {
	kv       KVDeleter
	rdb      RedisPSubscriber
	roles    []auth.Role
	log      *logger.Logger

	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	startOnce sync.Once
}

// NewCacheInvalidator constructs a CacheInvalidator. The `roles` argument
// defaults to permissions.KnownRoles when nil.
func NewCacheInvalidator(
	kv KVDeleter,
	rdb RedisPSubscriber,
	log *logger.Logger,
	roles []auth.Role,
) *CacheInvalidator {
	if roles == nil {
		roles = KnownRoles
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &CacheInvalidator{
		kv:     kv,
		rdb:    rdb,
		roles:  roles,
		log:    log.With("component", "permissions-invalidator"),
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start launches the PSUBSCRIBE loop in a background goroutine. Idempotent.
func (i *CacheInvalidator) Start() error {
	i.startOnce.Do(func() {
		i.wg.Add(1)
		go func() {
			defer i.wg.Done()
			i.run()
		}()
	})
	return nil
}

// Shutdown stops the subscriber and waits for the goroutine to exit.
func (i *CacheInvalidator) Shutdown() {
	i.cancel()
	i.wg.Wait()
}

// run is the subscribe/reconnect loop.
func (i *CacheInvalidator) run() {
	const reconnectBackoff = 2 * time.Second

	for {
		if i.ctx.Err() != nil {
			return
		}

		pubsub := i.rdb.PSubscribe(i.ctx, InvalidatePattern())

		if _, err := pubsub.Receive(i.ctx); err != nil {
			_ = pubsub.Close()
			if i.ctx.Err() != nil {
				return
			}
			i.log.Error(i.ctx, "permissions invalidation subscription failed, retrying",
				"backoff", reconnectBackoff.String(),
				logger.ErrorAttr(err),
			)
			select {
			case <-time.After(reconnectBackoff):
				continue
			case <-i.ctx.Done():
				return
			}
		}

		i.log.Info(i.ctx, "permissions invalidation subscription established",
			"pattern", InvalidatePattern())

		ch := pubsub.Channel()
		reconnect := false
		for !reconnect {
			select {
			case msg, ok := <-ch:
				if !ok {
					i.log.Warn(i.ctx, "permissions invalidation channel closed, reconnecting")
					reconnect = true
				} else {
					i.handleMessage(msg.Channel)
				}
			case <-i.ctx.Done():
				_ = pubsub.Close()
				return
			}
		}
		_ = pubsub.Close()
	}
}

// handleMessage extracts the orgID from the channel name and removes all
// cached entries for that organization. Channel format:
// "permissions:invalidate:{org_id}".
func (i *CacheInvalidator) handleMessage(channel string) {
	orgID := strings.TrimPrefix(channel, invalidateChanPref)
	if orgID == "" || orgID == channel {
		i.log.Warn(i.ctx, "permissions invalidation: unexpected channel name",
			"channel", channel)
		return
	}
	i.invalidateOrg(orgID)
}

// invalidateOrg removes every permissions:{org_id}:{role} entry for the org.
// Uses a per-call timeout to bound work under slow Redis.
func (i *CacheInvalidator) invalidateOrg(orgID string) {
	ctx, cancel := context.WithTimeout(i.ctx, 5*time.Second)
	defer cancel()

	for _, role := range i.roles {
		key := CacheKey(orgID, role)
		if err := i.kv.Delete(ctx, key); err != nil {
			i.log.Warn(i.ctx, "permissions cache delete failed",
				"key", key, logger.ErrorAttr(err))
			continue
		}
	}
	i.log.Info(i.ctx, "permissions cache invalidated",
		"organization_id", orgID, "roles", len(i.roles))
}

// ---------------------------------------------------------------------------
// Publisher — used by Admin Proxy to signal cache invalidation.
// ---------------------------------------------------------------------------

// pubSubPublisher is the subset of kvstore operations used to publish
// invalidation events (implemented by kvstore.Client).
type pubSubPublisher interface {
	Publish(ctx context.Context, channel string, message string) error
}

// InvalidationPublisher publishes cache-invalidation events over Redis Pub/Sub.
// It encapsulates the channel-naming convention so admin handlers only need
// to call InvalidateOrg(ctx, orgID) — the transport stays hidden.
type InvalidationPublisher struct {
	pub pubSubPublisher
}

// NewInvalidationPublisher wraps a Redis Pub/Sub client.
func NewInvalidationPublisher(pub pubSubPublisher) *InvalidationPublisher {
	return &InvalidationPublisher{pub: pub}
}

// InvalidateOrg publishes an invalidation event for the given organization.
// Best-effort: callers should log the error and continue — stale cache
// resolves itself at TTL.
func (p *InvalidationPublisher) InvalidateOrg(ctx context.Context, orgID string) error {
	return p.pub.Publish(ctx, InvalidateChannel(orgID), "")
}
