package idempotency

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrIdempotencyKeyVanished is the package-local sentinel ExtendTTL returns
// when the underlying EXPIRE reports the key is gone (kvstore.Expire →
// (false, nil), ops.go:74-78, high-architecture.md:566). It is NOT a
// model.ErrorCode and NOT kvstore.ErrKeyNotFound (that is Get-specific): "the
// key you wanted to extend is gone" is not a transient failure (build-spec
// D6.1). The heartbeat loop branches on errors.Is(err,
// ErrIdempotencyKeyVanished) to stop-vs-continue (D6).
var ErrIdempotencyKeyVanished = errors.New("idempotency: key vanished")

// StartHeartbeat launches a goroutine that, every cfg.HeartbeatInterval,
// calls g.ExtendTTL(ctx, key, ttl) (EXPIRE key ttl — idempotency.go:55-61,
// high-architecture.md:564) to keep a PROCESSING key alive while a pipeline
// holds it. It returns a stop func() the caller invokes on terminal status
// switch (COMPLETED/PAUSED/cleanup — high-architecture.md:576). ttl is the
// per-call PROCESSING TTL the caller used at SETNX (R3 — the adapter never
// hardcodes 150s). LIC-TASK-040 drives this for lic-trigger; the mechanism is
// key-agnostic (D12, high-architecture.md:576 "общий механизм для всех
// PROCESSING-ключей"). It returns ONLY stop func() (no error): launching a
// goroutine cannot fail; an ExtendTTL failure is handled inside the loop (D6).
//
// The goroutine terminates on ANY of three stop conditions:
//   - <-ctx.Done()                          — caller ctx cancelled (shutdown /
//     pipeline ended; idempotency.go:57-60 "on crash the heartbeat stops").
//   - <-stopCh (closed by stop())           — terminal status switch.
//   - ExtendTTL → ErrIdempotencyKeyVanished — the PROCESSING marker is gone
//     (Expire→false; the §6.3:566 contract says the heartbeat stops).
//
// A transient (non-vanished) ExtendTTL error is logged WARN and the loop
// CONTINUES — a single Redis blip must not abandon a live pipeline's
// PROCESSING marker; the next tick retries (the 150s TTL has slack over the
// 30s interval — configuration.md:63-64). The heartbeat emits NO
// lic_idempotency_* metric (the §3.6 enum has no heartbeat result —
// observability.md:154-159); only WARN logs on vanished/transient.
//
// stop is sync.Once-guarded close(stopCh): calling it twice (or after
// ctx-cancel already returned) is safe and does not panic. Every exit path
// stops the ticker and returns; there is no goroutine leak (PART C #8 asserts
// via a done channel, not time.Sleep).
func (g *Guard) StartHeartbeat(
	ctx context.Context, key string, ttl time.Duration,
) (stop func()) {
	stopCh := make(chan struct{})
	var once sync.Once
	stop = func() {
		once.Do(func() { close(stopCh) })
	}

	ticker := g.clock.NewTicker(g.cfg.HeartbeatInterval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stopCh:
				return
			case <-ticker.C():
				err := g.ExtendTTL(ctx, key, ttl)
				if err == nil {
					continue // TTL refreshed.
				}
				if errors.Is(err, ErrIdempotencyKeyVanished) {
					g.log.Warn(ctx, "idempotency heartbeat: key vanished, stopping", "key", key)
					return
				}
				// Transient (transport) error — WARN + continue; the
				// next tick retries (D6).
				g.log.Warn(ctx, "idempotency heartbeat: ExtendTTL transient error",
					"key", key, "cause", err)
			}
		}
	}()

	return stop
}
