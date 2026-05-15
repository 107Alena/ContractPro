package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// BrokerConfig holds RabbitMQ connection, exchange, and consumer settings.
type BrokerConfig struct {
	URL string // LIC_BROKER_URL (required)

	// Topic exchanges (FROZEN by integration-contracts §6.1).
	ExchangeEvents    string // LIC_BROKER_EXCHANGE_EVENTS
	ExchangeResponses string // LIC_BROKER_EXCHANGE_RESPONSES
	ExchangeCommands  string // LIC_BROKER_EXCHANGE_COMMANDS
	ExchangeDLX       string // LIC_BROKER_EXCHANGE_DLX

	// Consumer.
	ConsumerPrefetch        int           // LIC_CONSUMER_PREFETCH
	ConsumerMaxRedeliveries int           // LIC_CONSUMER_MAX_REDELIVERIES (must equal retry layer count)
	ConsumerRetryTTL1       time.Duration // LIC_CONSUMER_RETRY_TTL_1
	ConsumerRetryTTL2       time.Duration // LIC_CONSUMER_RETRY_TTL_2
	ConsumerRetryTTL3       time.Duration // LIC_CONSUMER_RETRY_TTL_3

	// Publisher.
	PublisherConfirmTimeout time.Duration // LIC_PUBLISHER_CONFIRM_TIMEOUT
	PublishBufferSize       int           // LIC_PUBLISH_BUFFER_SIZE

	// TLS toggle — configuration.md §3 rule 10 allows TLS to be expressed
	// either by amqps:// in the URL or by LIC_BROKER_TLS=true.
	TLS bool // LIC_BROKER_TLS
}

func loadBrokerConfig() BrokerConfig {
	return BrokerConfig{
		URL: envString("LIC_BROKER_URL", ""),

		ExchangeEvents:    envString("LIC_BROKER_EXCHANGE_EVENTS", "contractpro.events"),
		ExchangeResponses: envString("LIC_BROKER_EXCHANGE_RESPONSES", "contractpro.responses"),
		ExchangeCommands:  envString("LIC_BROKER_EXCHANGE_COMMANDS", "contractpro.commands"),
		ExchangeDLX:       envString("LIC_BROKER_EXCHANGE_DLX", "contractpro.dlx"),

		ConsumerPrefetch:        envInt("LIC_CONSUMER_PREFETCH", 10),
		ConsumerMaxRedeliveries: envInt("LIC_CONSUMER_MAX_REDELIVERIES", 3),
		ConsumerRetryTTL1:       envDuration("LIC_CONSUMER_RETRY_TTL_1", 2*time.Second),
		ConsumerRetryTTL2:       envDuration("LIC_CONSUMER_RETRY_TTL_2", 10*time.Second),
		ConsumerRetryTTL3:       envDuration("LIC_CONSUMER_RETRY_TTL_3", 60*time.Second),

		PublisherConfirmTimeout: envDuration("LIC_PUBLISHER_CONFIRM_TIMEOUT", 5*time.Second),
		PublishBufferSize:       envInt("LIC_PUBLISH_BUFFER_SIZE", 100),

		TLS: envBool("LIC_BROKER_TLS", false),
	}
}

func (b BrokerConfig) validate() error {
	var errs []error
	if b.URL == "" {
		errs = append(errs, missingVarErr("LIC_BROKER_URL"))
	} else if u, err := url.Parse(b.URL); err != nil {
		errs = append(errs, fmt.Errorf("config: LIC_BROKER_URL is not a valid URL: %w", err))
	} else if scheme := strings.ToLower(u.Scheme); scheme != "amqp" && scheme != "amqps" {
		errs = append(errs, fmt.Errorf("config: LIC_BROKER_URL must use amqp:// or amqps://, got %q", u.Scheme))
	}
	if b.ConsumerPrefetch < 1 {
		errs = append(errs, fmt.Errorf("config: LIC_CONSUMER_PREFETCH must be >= 1, got %d", b.ConsumerPrefetch))
	}
	if b.ConsumerMaxRedeliveries < 0 {
		errs = append(errs, fmt.Errorf("config: LIC_CONSUMER_MAX_REDELIVERIES must be >= 0, got %d", b.ConsumerMaxRedeliveries))
	}
	// maxRetryTTL bounds each LIC_CONSUMER_RETRY_TTL_*. A retry TTL becomes
	// the queue's x-message-ttl, which RabbitMQ takes as a 32-bit
	// millisecond count; the broker adapter casts via int32. 24h
	// (86_400_000 ms) is well under the int32 ceiling (~24.8 days) and is
	// far beyond any sane DLX-loop backoff. Capping here makes a fat-finger
	// (e.g. "30d") fail fast at startup with a clear message instead of
	// silently overflowing to a negative x-message-ttl that RabbitMQ
	// rejects with 406 — which would turn every reconnect into a permanent
	// self-inflicted outage (security-engineer MF-2).
	const maxRetryTTL = 24 * time.Hour
	// maxConfirmTimeout bounds LIC_PUBLISHER_CONFIRM_TIMEOUT: a publish
	// holds the publish lock for the confirm wait, so an absurd value would
	// stall the publisher indefinitely.
	const maxConfirmTimeout = 5 * time.Minute
	// Ordered list keeps error output deterministic across runs.
	for _, dv := range []struct {
		name string
		d    time.Duration
		max  time.Duration
	}{
		{"LIC_CONSUMER_RETRY_TTL_1", b.ConsumerRetryTTL1, maxRetryTTL},
		{"LIC_CONSUMER_RETRY_TTL_2", b.ConsumerRetryTTL2, maxRetryTTL},
		{"LIC_CONSUMER_RETRY_TTL_3", b.ConsumerRetryTTL3, maxRetryTTL},
		{"LIC_PUBLISHER_CONFIRM_TIMEOUT", b.PublisherConfirmTimeout, maxConfirmTimeout},
	} {
		if dv.d <= 0 {
			errs = append(errs, fmt.Errorf("config: %s must be > 0, got %s", dv.name, dv.d))
		} else if dv.d > dv.max {
			errs = append(errs, fmt.Errorf("config: %s must be <= %s, got %s", dv.name, dv.max, dv.d))
		}
	}
	if b.PublishBufferSize < 0 {
		errs = append(errs, fmt.Errorf("config: LIC_PUBLISH_BUFFER_SIZE must be >= 0, got %d", b.PublishBufferSize))
	}
	return errors.Join(errs...)
}

// usesTLS returns true when either the broker URL uses amqps:// OR
// LIC_BROKER_TLS=true (configuration.md §3 rule 10).
//
// Precondition: validate() has passed; URL parse errors are swallowed here
// because validate() already surfaces them.
func (b BrokerConfig) usesTLS() bool {
	if b.TLS {
		return true
	}
	u, err := url.Parse(b.URL)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Scheme, "amqps")
}
