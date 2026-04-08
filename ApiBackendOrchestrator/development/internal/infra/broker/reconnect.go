package broker

import (
	"context"
	"math/rand/v2"
	"time"

	"contractpro/api-orchestrator/internal/infra/observability/logger"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Reconnect backoff constants.
const (
	reconnectBaseDelay = 1 * time.Second
	reconnectMaxDelay  = 30 * time.Second
	jitterFraction     = 0.25
	maxBackoffAttempt  = 5 // 2^5 * 1s = 32s > 30s max
)

// reconnectLoop monitors the connection and triggers reconnection on failure.
func (c *Client) reconnectLoop() {
	defer c.wg.Done()

	for {
		notifyClose := make(chan *amqp.Error, 1)

		c.mu.RLock()
		conn := c.conn
		c.mu.RUnlock()

		if conn != nil {
			conn.NotifyClose(notifyClose)
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

// reconnectWithBackoff attempts to re-establish the connection with
// exponential backoff and jitter. On success it re-enables publisher
// confirms and re-subscribes all active subscriptions.
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
			c.log.Warn(context.Background(), "broker: reconnect dial failed",
				"attempt", attempt,
				logger.ErrorAttr(err),
			)
			continue
		}

		newCh, err := newConn.Channel()
		if err != nil {
			c.log.Warn(context.Background(), "broker: reconnect channel failed",
				"attempt", attempt,
				logger.ErrorAttr(err),
			)
			_ = newConn.Close()
			continue
		}

		if err := newCh.Confirm(false); err != nil {
			c.log.Warn(context.Background(), "broker: reconnect confirm mode failed",
				"attempt", attempt,
				logger.ErrorAttr(err),
			)
			_ = newCh.Close()
			_ = newConn.Close()
			continue
		}

		newNotify := newCh.NotifyPublish(make(chan amqp.Confirmation, 1))

		c.mu.Lock()
		oldConn := c.conn
		oldPubCh := c.pubCh
		c.conn = newConn
		c.pubCh = newCh
		c.pubNotify = newNotify
		subs := make([]subscription, len(c.subs))
		copy(subs, c.subs)
		c.mu.Unlock()

		if oldPubCh != nil {
			_ = oldPubCh.Close()
		}
		if oldConn != nil {
			_ = oldConn.Close()
		}

		for _, sub := range subs {
			if subErr := c.subscribe(sub.topic, sub.queue, sub.handler); subErr != nil {
				c.log.Warn(context.Background(), "broker: re-subscribe failed",
					"queue", sub.queue,
					logger.ErrorAttr(subErr),
				)
			}
		}

		c.log.Info(context.Background(), "broker: reconnected", "attempt", attempt)
		return
	}
}

// backoffDelay computes exponential backoff with jitter.
// Delays: 1s, 2s, 4s, 8s, 16s, capped at 30s, ±25% jitter.
func backoffDelay(attempt int) time.Duration {
	if attempt > maxBackoffAttempt {
		attempt = maxBackoffAttempt
	}

	delay := reconnectBaseDelay << attempt
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
