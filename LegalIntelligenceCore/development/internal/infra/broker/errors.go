package broker

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Broker errors are intentionally NOT modelled as a model.ErrorCode.
//
// internal/domain/model.errorCatalog is the single source of truth for codes
// published to the Orchestrator via lic.events.status-changed, and its init()
// panics if a code lacks a catalog row (SSOT invariant). Broker/infra failures
// are never Orchestrator-published — they are retried, NACKed, or routed to a
// DLQ internally — so a model code would both break that invariant and be
// semantically wrong. LIC already keeps infra-adjacent typed errors out of the
// model catalog (port.LLMProviderError for the LLM adapters); a
// broker-package-local error is the narrower correct analog here. The
// DocumentProcessing sibling maps to port.ErrCodeBrokerFailed, but LIC has no
// broker domain port and no such code, so that mapping is deliberately not
// copied (code-architect Q1).

// Sentinel errors callers can match with errors.Is.
var (
	// ErrNotConnected is returned by Publish/Subscribe/Ping when the client
	// has no live AMQP connection or channel (e.g. mid-reconnect or after
	// Close). It is retryable: the reconnect loop will re-establish the
	// connection.
	ErrNotConnected = errors.New("broker: not connected")

	// ErrPublishNack is returned when the broker negatively acknowledged a
	// published message (publisher confirms). Retryable — the broker
	// rejected the message transiently (e.g. mirrored queue unavailable).
	ErrPublishNack = errors.New("broker: publish negatively acknowledged by broker")

	// ErrConfirmTimeout is returned when no publisher confirm arrived within
	// BrokerConfig.PublisherConfirmTimeout. Retryable.
	ErrConfirmTimeout = errors.New("broker: timed out waiting for publisher confirm")
)

// BrokerError is the typed error returned across the broker package boundary.
// Op identifies the failing operation ("Publish", "Subscribe/Consume",
// "DeclareTopology/QueueDeclare", …); Retryable tells callers (consumer /
// publisher adapters) whether re-attempting can succeed; Cause is the wrapped
// underlying error for errors.Is / errors.As traversal.
type BrokerError struct {
	Op        string
	Retryable bool
	Cause     error
}

func (e *BrokerError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Cause != nil {
		return fmt.Sprintf("broker: %s: %v", e.Op, e.Cause)
	}
	return fmt.Sprintf("broker: %s", e.Op)
}

// Unwrap exposes the wrapped Cause so errors.Is(err, ErrNotConnected) and
// friends traverse the chain.
func (e *BrokerError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// IsRetryable reports the Retryable flag of err if it is (or wraps) a
// *BrokerError, or false otherwise. Consumer/publisher adapters use this to
// decide between NACK-to-retry-DLX and routing straight to a terminal DLQ.
func IsRetryable(err error) bool {
	var be *BrokerError
	if errors.As(err, &be) {
		return be.Retryable
	}
	return false
}

// nonRetryableAMQPCodes lists AMQP reply codes that are permanent failures.
// Retrying these is pointless — the condition will not resolve on its own.
//
//	404 NotFound          — queue/exchange does not exist (topology bug)
//	403 AccessRefused     — insufficient permissions (credentials/vhost bug)
//	406 PreconditionFailed— queue/exchange arg mismatch (e.g. TTL drift between
//	                        startup and reconnect re-declare) — config bug
//
// Mirrors the DocumentProcessing broker mapping (code-architect Q1).
var nonRetryableAMQPCodes = map[int]bool{
	404: true,
	403: true,
	406: true,
}

// mapError translates AMQP and context errors into broker errors.
//
// context.Canceled / context.DeadlineExceeded pass through RAW — this is the
// established codebase-wide convention so callers can distinguish shutdown /
// caller-timeout from infrastructure failure (code-architect Q1; matches the
// DP broker and the LLM adapters' treatment of context errors).
func mapError(err error, op string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	var amqpErr *amqp.Error
	if errors.As(err, &amqpErr) {
		return &BrokerError{
			Op:        fmt.Sprintf("%s: %d %s", op, amqpErr.Code, amqpErr.Reason),
			Retryable: !nonRetryableAMQPCodes[amqpErr.Code],
			Cause:     err,
		}
	}

	// Unknown / network-level errors — retryable by default (a fresh
	// connection from the reconnect loop may resolve them).
	return &BrokerError{Op: op, Retryable: true, Cause: err}
}

// redactURLCredentials returns err with the password component of rawURL (if
// any) replaced by "***". amqp.Dial surfaces the dialed URI verbatim on
// URI-parse / handshake-failure paths; the broker password is a long-lived
// credential that must never reach an error string a caller logs — this
// system processes 152-ФЗ PII (security-engineer MF-1). The cause chain is
// intentionally flattened to a plain error here: the dial path does not rely
// on errors.Is against specific net errors, and credential hygiene wins.
func redactURLCredentials(err error, rawURL string) error {
	if err == nil {
		return nil
	}
	u, perr := url.Parse(rawURL)
	if perr != nil || u.User == nil {
		return err
	}
	pw, ok := u.User.Password()
	if !ok || pw == "" {
		return err
	}
	msg := strings.ReplaceAll(err.Error(), pw, "***")
	if msg == err.Error() {
		return err // password not present in the message — keep wrapping intact
	}
	return errors.New(msg)
}
