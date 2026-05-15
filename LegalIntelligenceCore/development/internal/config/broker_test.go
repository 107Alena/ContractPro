package config

import (
	"strings"
	"testing"
	"time"
)

func validBrokerConfig() BrokerConfig {
	return BrokerConfig{
		URL:                     "amqp://guest:guest@localhost:5672/",
		ExchangeEvents:          "contractpro.events",
		ExchangeResponses:       "contractpro.responses",
		ExchangeCommands:        "contractpro.commands",
		ExchangeDLX:             "contractpro.dlx",
		ConsumerPrefetch:        10,
		ConsumerMaxRedeliveries: 3,
		ConsumerRetryTTL1:       2 * time.Second,
		ConsumerRetryTTL2:       10 * time.Second,
		ConsumerRetryTTL3:       60 * time.Second,
		PublisherConfirmTimeout: 5 * time.Second,
		PublishBufferSize:       100,
	}
}

func TestBrokerConfig_Validate_Valid(t *testing.T) {
	if err := validBrokerConfig().validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
}

func TestBrokerConfig_Validate_RetryTTLUpperBound(t *testing.T) {
	// A "30d" fat-finger overflows int32 milliseconds in the broker
	// adapter; config must reject it at startup (security-engineer MF-2).
	b := validBrokerConfig()
	b.ConsumerRetryTTL3 = 30 * 24 * time.Hour
	err := b.validate()
	if err == nil {
		t.Fatal("expected error for oversized LIC_CONSUMER_RETRY_TTL_3")
	}
	if !strings.Contains(err.Error(), "LIC_CONSUMER_RETRY_TTL_3") {
		t.Errorf("error must name the offending var, got %v", err)
	}
}

func TestBrokerConfig_Validate_ConfirmTimeoutUpperBound(t *testing.T) {
	b := validBrokerConfig()
	b.PublisherConfirmTimeout = time.Hour
	if err := b.validate(); err == nil {
		t.Fatal("expected error for oversized LIC_PUBLISHER_CONFIRM_TIMEOUT")
	}
}

func TestBrokerConfig_Validate_NonPositiveStillRejected(t *testing.T) {
	b := validBrokerConfig()
	b.ConsumerRetryTTL1 = 0
	if err := b.validate(); err == nil {
		t.Fatal("expected error for zero LIC_CONSUMER_RETRY_TTL_1")
	}
}
