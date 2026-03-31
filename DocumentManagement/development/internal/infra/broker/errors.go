package broker

import (
	"context"
	"errors"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"

	"contractpro/document-management/internal/domain/port"
)

// nonRetryableAMQPCodes lists AMQP reply codes that are permanent failures.
// Retrying these is pointless — the condition will not resolve on its own.
// 404 (NotFound): queue/exchange does not exist — configuration error.
// 403 (AccessRefused): insufficient permissions — configuration error.
// 406 (PreconditionFailed): queue property mismatch — configuration error.
var nonRetryableAMQPCodes = map[int]bool{
	404: true, // NotFound
	403: true, // AccessRefused
	406: true, // PreconditionFailed
}

// mapError translates AMQP and context errors into domain errors.
// Context errors (Canceled, DeadlineExceeded) pass through raw — this is the
// established pattern across the codebase so the orchestrator can distinguish
// cancellation from infrastructure failures.
func mapError(err error, operation string) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	// Check for typed AMQP errors with reply codes.
	var amqpErr *amqp.Error
	if errors.As(err, &amqpErr) {
		retryable := !nonRetryableAMQPCodes[amqpErr.Code]
		return &port.DomainError{
			Code:      port.ErrCodeBrokerFailed,
			Message:   fmt.Sprintf("broker: %s: %d %s", operation, amqpErr.Code, amqpErr.Reason),
			Retryable: retryable,
			Cause:     err,
		}
	}

	// Unknown / network-level errors — retryable by default.
	return port.NewBrokerError(
		fmt.Sprintf("broker: %s", operation), err,
	)
}
