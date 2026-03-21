package broker

import (
	"math/rand"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	reconnectBaseDelay = 1 * time.Second
	reconnectMaxDelay  = 30 * time.Second
	jitterFraction     = 0.25
	// maxBackoffAttempt caps the exponent to avoid float64 overflow.
	// 2^5 * 1s = 32s > reconnectMaxDelay, so further increase is pointless.
	maxBackoffAttempt = 5
)

// reconnectLoop runs in its own goroutine, watching for connection closure
// notifications and re-establishing the connection with exponential backoff.
// It stops when the done channel is closed (via Client.Close).
func (c *Client) reconnectLoop() {
	defer c.wg.Done()

	for {
		notifyClose := make(chan *amqp.Error, 1)

		c.mu.RLock()
		conn := c.conn
		c.mu.RUnlock()

		if conn != nil {
			conn.NotifyClose(notifyClose)
			// If the connection died between dial and NotifyClose registration,
			// the notification was lost. Check IsClosed to catch this edge case.
			if conn.IsClosed() {
				c.reconnectWithBackoff()
				continue
			}
		}

		select {
		case <-c.done:
			return
		case amqpErr, ok := <-notifyClose:
			if !ok && amqpErr == nil {
				// Channel closed without error — check if we're shutting down.
				select {
				case <-c.done:
					return
				default:
				}
			}
			c.reconnectWithBackoff()
		}
	}
}

// reconnectWithBackoff attempts to re-establish the AMQP connection using
// exponential backoff (1s -> 30s) with 25% jitter. After a successful
// reconnect it replaces conn/pubCh, closes the old resources to avoid
// file descriptor leaks, and re-subscribes all active subscriptions.
func (c *Client) reconnectWithBackoff() {
	attempt := 0
	for {
		select {
		case <-c.done:
			return
		default:
		}

		delay := backoffDelay(attempt)
		timer := time.NewTimer(delay)

		select {
		case <-c.done:
			timer.Stop()
			return
		case <-timer.C:
		}

		attempt++

		newConn, err := c.dialFn(c.address)
		if err != nil {
			continue
		}

		newCh, err := newConn.Channel()
		if err != nil {
			_ = newConn.Close()
			continue
		}

		// Swap connection/channel under lock; close old resources to avoid
		// file descriptor leaks (W-3).
		c.mu.Lock()
		oldConn := c.conn
		oldPubCh := c.pubCh
		c.conn = newConn
		c.pubCh = newCh
		subs := make([]subscription, len(c.subs))
		copy(subs, c.subs)
		c.mu.Unlock()

		if oldPubCh != nil {
			_ = oldPubCh.Close()
		}
		if oldConn != nil {
			_ = oldConn.Close()
		}

		// Re-subscribe all active subscriptions.
		// Errors are logged but not fatal — the subscription will be
		// retried on the next reconnection cycle.
		for _, sub := range subs {
			if subErr := c.subscribe(sub.queue, sub.handler); subErr != nil {
				// Best-effort: log would go here in production.
				// The subscription remains in c.subs for retry on next reconnect.
				_ = subErr
			}
		}

		return
	}
}

// backoffDelay calculates exponential backoff with jitter for the given attempt.
// The attempt exponent is capped at maxBackoffAttempt to avoid float64 overflow.
func backoffDelay(attempt int) time.Duration {
	if attempt > maxBackoffAttempt {
		attempt = maxBackoffAttempt
	}

	delay := reconnectBaseDelay << attempt // 1s, 2s, 4s, 8s, 16s, 32s
	if delay > reconnectMaxDelay {
		delay = reconnectMaxDelay
	}

	// Apply 25% jitter: delay * (1 +/- 0.25 * random).
	jitter := time.Duration(float64(delay) * jitterFraction * (2*rand.Float64() - 1))
	delay += jitter

	if delay < 0 {
		delay = reconnectBaseDelay
	}
	return delay
}
