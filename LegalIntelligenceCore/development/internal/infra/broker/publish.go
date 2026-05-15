package broker

import (
	"context"
	"errors"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// maxPublishAttempts is the total number of publish tries (1 initial + 2
// retries) before giving up — the acceptance criterion "retry 3 раза с
// backoff".
const maxPublishAttempts = 3

// Publish sends payload to exchange with the given routing key and blocks
// until the broker confirms the message (publisher confirms) or the attempt
// budget is exhausted.
//
// Concurrency (refines code-architect Q4 after the three-reviewer pass):
// publishes are serialized on a DEDICATED pubMu (the publisher-confirms
// channel is single-flight), NOT on c.mu. c.mu is taken only for a brief
// snapshot of pubCh per attempt. This deliberately decouples publish
// serialization from the connection-lifecycle lock so a slow/dead broker
// does NOT stall Ping (/readyz), Close (graceful shutdown), or the reconnect
// swap for the worst-case ~3×(confirmTimeout+backoff) (golang-pro S2,
// code-reviewer S1/S2). The original swap-safety invariant is preserved
// without lock-holding: deferred confirmations are per-publish, so if a
// reconnect swaps/closes the channel mid-confirm, the confirmation's Done()
// closes with Acked()=false → the next attempt re-snapshots the NEW pubCh
// and retries. Correctness no longer depends on holding c.mu across the wait.
//
// mandatory=false: topology (and thus routability) is guaranteed by
// DeclareTopology, so an unroutable message is a startup bug, not a
// per-publish concern — no NotifyReturn handling needed (code-architect Q4).
func (c *Client) Publish(ctx context.Context, exchange, routingKey string, payload []byte) error {
	c.pubMu.Lock()
	defer c.pubMu.Unlock()

	var lastErr error = &BrokerError{Op: "Publish", Retryable: true, Cause: ErrNotConnected}
	for attempt := 0; attempt < maxPublishAttempts; attempt++ {
		if attempt > 0 {
			// Back off between attempts; abort early if the caller's
			// context is cancelled or the client is shutting down.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-c.done:
				return &BrokerError{Op: "Publish", Retryable: true, Cause: ErrNotConnected}
			case <-time.After(backoffDelay(attempt - 1)):
			}
		}

		if err := ctx.Err(); err != nil {
			return err
		}

		c.mu.Lock()
		ch := c.pubCh
		c.mu.Unlock()
		if ch == nil {
			lastErr = &BrokerError{Op: "Publish", Retryable: true, Cause: ErrNotConnected}
			continue
		}

		dconf, err := ch.PublishWithDeferredConfirmWithContext(ctx, exchange, routingKey, false, false, amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			Body:         payload,
		})
		if err != nil {
			mapped := mapError(err, "Publish")
			// Non-retryable AMQP faults (404/403/406) are permanent
			// misconfiguration — fail fast instead of burning the budget.
			// Raw context errors are non-retryable too (IsRetryable=false).
			if !IsRetryable(mapped) {
				return mapped
			}
			lastErr = mapped
			continue
		}

		acked, werr := c.waitConfirm(ctx, dconf)
		switch {
		case errors.Is(werr, context.Canceled) || errors.Is(werr, context.DeadlineExceeded):
			// Caller's context ended — surface raw (codebase convention)
			// and stop; retrying under a dead context is pointless.
			return werr
		case errors.Is(werr, ErrNotConnected):
			// Client is shutting down (c.done) — terminal.
			return werr
		case errors.Is(werr, ErrConfirmTimeout):
			// No ack within the budget. Retryable: downstream consumers
			// are idempotent (integration-contracts §1.2), so a possible
			// duplicate on re-publish is tolerated.
			lastErr = werr
			continue
		case acked:
			return nil
		default:
			lastErr = &BrokerError{Op: "Publish", Retryable: true, Cause: ErrPublishNack}
		}
	}

	return lastErr
}

// waitConfirm blocks until the broker acks/nacks the deferred confirmation,
// the caller's context ends, the PublisherConfirmTimeout elapses, or the
// client closes. Returns (acked, nil) on a broker decision; (false, ctxErr)
// when the context ended (passed through raw); a retryable BrokerError
// otherwise.
func (c *Client) waitConfirm(ctx context.Context, dconf PublishConfirm) (bool, error) {
	timer := time.NewTimer(c.cfg.PublisherConfirmTimeout)
	defer timer.Stop()

	select {
	case <-dconf.Done():
		return dconf.Acked(), nil
	case <-ctx.Done():
		return false, ctx.Err()
	case <-c.done:
		return false, &BrokerError{Op: "Publish", Retryable: true, Cause: ErrNotConnected}
	case <-timer.C:
		return false, &BrokerError{Op: "Publish", Retryable: true, Cause: ErrConfirmTimeout}
	}
}
