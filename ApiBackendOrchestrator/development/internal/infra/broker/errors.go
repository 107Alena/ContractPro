package broker

import (
	"context"
	"errors"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Sentinel errors. Callers check with errors.Is().
var (
	// ErrClientClosed is returned by operations on a closed client.
	ErrClientClosed = errors.New("broker: client closed")

	// ErrPublishConfirmFailed indicates the confirmation channel closed
	// before a confirm was received (connection lost during publish).
	ErrPublishConfirmFailed = errors.New("broker: publish confirm failed")

	// ErrPublishNacked indicates the broker explicitly nacked the message.
	ErrPublishNacked = errors.New("broker: publish nacked by broker")

	// ErrNotConnected indicates no active connection is available.
	ErrNotConnected = errors.New("broker: not connected")

	// ErrPublishMaxRetriesExhausted indicates all publish retry attempts failed.
	ErrPublishMaxRetriesExhausted = errors.New("broker: publish max retries exhausted")
)

// BrokerError carries an operation name, retryability flag, and cause.
type BrokerError struct {
	Operation string
	Message   string
	Retryable bool
	Cause     error
}

func (e *BrokerError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("broker: %s: %s: %s", e.Operation, e.Message, e.Cause.Error())
	}
	return fmt.Sprintf("broker: %s: %s", e.Operation, e.Message)
}

func (e *BrokerError) Unwrap() error {
	return e.Cause
}

// nonRetryableAMQPCodes are permanent failures that should not be retried.
var nonRetryableAMQPCodes = map[int]bool{
	404: true, // NotFound
	403: true, // AccessRefused
	406: true, // PreconditionFailed
}

// mapError translates AMQP and context errors into broker errors.
// Context errors pass through raw.
func mapError(err error, operation string) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	var amqpErr *amqp.Error
	if errors.As(err, &amqpErr) {
		retryable := !nonRetryableAMQPCodes[amqpErr.Code]
		return &BrokerError{
			Operation: operation,
			Message:   fmt.Sprintf("%d %s", amqpErr.Code, amqpErr.Reason),
			Retryable: retryable,
			Cause:     err,
		}
	}

	// Unknown / network errors are retryable by default.
	return &BrokerError{
		Operation: operation,
		Message:   "unexpected error",
		Retryable: true,
		Cause:     err,
	}
}

// isRetryable checks if an error is retryable.
func isRetryable(err error) bool {
	var brokerErr *BrokerError
	if errors.As(err, &brokerErr) {
		return brokerErr.Retryable
	}

	if errors.Is(err, ErrPublishConfirmFailed) || errors.Is(err, ErrNotConnected) {
		return true
	}
	if errors.Is(err, ErrPublishNacked) {
		return true
	}

	return false
}

func errNotConnected(operation string) error {
	return fmt.Errorf("broker: %s: %w", operation, ErrNotConnected)
}

func errClientClosed(operation string) error {
	return fmt.Errorf("broker: %s: %w", operation, ErrClientClosed)
}
