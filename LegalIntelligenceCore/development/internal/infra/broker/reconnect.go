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
	// maxBackoffAttempt caps the exponent to avoid shifting past the max
	// delay (2^5 * 1s = 32s > reconnectMaxDelay).
	maxBackoffAttempt = 5
)

// reconnectLoop watches the live connection for closure and re-establishes it
// with exponential backoff. It exits when Close() closes c.done.
func (c *Client) reconnectLoop() {
	defer c.wg.Done()

	for {
		notifyClose := make(chan *amqp.Error, 1)

		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()

		if conn != nil {
			conn.NotifyClose(notifyClose)
			// The connection can die in the window between dial and
			// NotifyClose registration AND in the topology-setup window of
			// the previous reconnectWithBackoff (before this loop iteration
			// re-registers). IsClosed catches a closure observed up to this
			// point; the buffered (cap-1) notifyClose catches one that
			// arrives after registration. Together they close the
			// lost-wakeup window (code-reviewer M3).
			if conn.IsClosed() {
				c.reconnectWithBackoff()
				continue
			}
		}

		select {
		case <-c.done:
			return
		case <-notifyClose:
			select {
			case <-c.done:
				return
			default:
			}
			c.reconnectWithBackoff()
		}
	}
}

// reconnectWithBackoff re-dials and, once a new connection is FULLY validated
// (publish channel in Confirm mode + topology asserted), atomically adopts
// it and re-subscribes. Ordering mandated by code-architect Q6:
//
//	dial → publish channel (Confirm) → DeclareTopology → swap → re-subscribe.
//
// Invariants enforced here (golang-pro M2/M3, security-engineer MF-2/SF-3):
//   - The first dial is attempted IMMEDIATELY; backoff applies only between
//     retries, so a clean broker bounce is not penalised ~1s.
//   - A freshly dialed connection is never stored into c until it is fully
//     validated, so a connection that fails mid-setup cannot leak or become
//     observable.
//   - The swap and the c.done check happen under the SAME lock, so a
//     concurrent Close() can never race wg.Add with wg.Wait nor miss
//     closing the new connection.
//   - A non-retryable topology error (e.g. 406 from externally drifted
//     queue args) backs off to the max delay instead of hot-looping the
//     re-dial.
func (c *Client) reconnectWithBackoff() {
	attempt := 0
	for {
		select {
		case <-c.done:
			return
		default:
		}

		if attempt > 0 {
			timer := time.NewTimer(backoffDelay(attempt - 1))
			select {
			case <-c.done:
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		attempt++

		newConn, err := c.dialFn(c.cfg.URL)
		if err != nil {
			continue
		}

		newPubCh, err := c.openPublishChannelOn(newConn)
		if err != nil {
			_ = newConn.Close()
			continue
		}

		if derr := c.declareTopologyOn(c.cancelCtx, newConn); derr != nil {
			_ = newPubCh.Close()
			_ = newConn.Close()
			if !IsRetryable(derr) {
				// Permanent topology fault (externally drifted args): a
				// fast re-dial would just churn the broker forever. Back
				// off to the max delay and keep trying slowly so the
				// service self-heals if an operator fixes it.
				timer := time.NewTimer(reconnectMaxDelay)
				select {
				case <-c.done:
					timer.Stop()
					return
				case <-timer.C:
				}
			}
			continue
		}

		// Adopt the validated connection atomically with the shutdown
		// check.
		c.mu.Lock()
		select {
		case <-c.done:
			c.mu.Unlock()
			_ = newPubCh.Close()
			_ = newConn.Close()
			return
		default:
		}
		oldConn := c.conn
		oldPubCh := c.pubCh
		c.conn = newConn
		c.pubCh = newPubCh
		subs := make([]subscription, len(c.subs))
		copy(subs, c.subs)
		c.mu.Unlock()

		if oldPubCh != nil {
			_ = oldPubCh.Close()
		}
		if oldConn != nil {
			_ = oldConn.Close()
		}

		// Re-establish all subscriptions (fresh channel + Qos + Consume).
		// A failed re-subscribe stays in c.subs and is retried on the next
		// reconnect cycle. TODO(LIC-TASK-039/043): inject a logger/metric
		// so a re-subscribe failure against a live connection is not
		// silent (security-engineer SF-3, code-reviewer S4).
		for _, sub := range subs {
			_ = c.startConsumer(sub.queue, sub.handler)
		}

		return
	}
}

// backoffDelay computes exponential backoff with 25% jitter for the given
// attempt. Shared by the reconnect loop and the publish retry path. The
// exponent is capped so the shift cannot overflow, and the result is clamped
// to [reconnectBaseDelay, reconnectMaxDelay ± jitter] and never negative.
//
// math/rand's global source is goroutine-safe (locked) and auto-seeded on
// Go 1.20+; jitter is not a security primitive, so this is correct as-is.
func backoffDelay(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if attempt > maxBackoffAttempt {
		attempt = maxBackoffAttempt
	}

	delay := reconnectBaseDelay << attempt // 1s, 2s, 4s, 8s, 16s, 32s
	if delay > reconnectMaxDelay {
		delay = reconnectMaxDelay
	}

	jitter := time.Duration(float64(delay) * jitterFraction * (2*rand.Float64() - 1))
	delay += jitter
	if delay < 0 {
		delay = reconnectBaseDelay
	}
	return delay
}
