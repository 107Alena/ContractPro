package kvstore

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/redis/go-redis/v9"
)

// Package-local typed errors — same rationale as internal/infra/broker.
//
// internal/domain/model.errorCatalog is the single source of truth for codes
// published to the Orchestrator via lic.events.status-changed, and its init()
// panics if a code lacks a catalog row (SSOT invariant). Redis/infra failures
// are never Orchestrator-published — they are retried, NACKed, or routed to a
// DLQ by the idempotency / pending adapters (LIC-TASK-037/038) — so a
// model.ErrorCode would both break that invariant and be semantically wrong.
// LIC already keeps infra-adjacent typed errors out of the catalog
// (port.LLMProviderError, broker.BrokerError); kvstore.RedisError is the
// narrower correct analog. The DocumentProcessing kvstore sibling maps to
// port.ErrCodeStorageFailed, but LIC has no storage domain port and no such
// code, so that mapping is deliberately not copied (code-architect Q1).

// ErrKeyNotFound is returned by Get when the requested key does not exist, and
// surfaces (via mapError) whenever a command receives redis.Nil. It is a
// PLAIN sentinel, NOT a *RedisError: the LIC-TASK-038 IdempotencyStorePort
// adapter translates a miss into IdempotencyAbsent with no error, and the
// LIC-TASK-037 PendingStatePort adapter into ErrPendingStateNotFound — both
// rely on a clean errors.Is(err, kvstore.ErrKeyNotFound). It is a normal
// "not found" signal, not an infrastructure failure, so it carries no
// Retryable flag. Mirrors the DP kvstore sentinel.
var ErrKeyNotFound = errors.New("kvstore: key not found")

// RedisError is the typed error returned across the kvstore package boundary.
// Op identifies the failing operation ("Get", "SetNX", "Eval", "Ping", …);
// Retryable tells callers (the idempotency / pending adapters) whether
// re-attempting can succeed; Cause is the wrapped underlying error for
// errors.Is / errors.As traversal. Mirrors broker.BrokerError.
type RedisError struct {
	Op        string
	Retryable bool
	Cause     error
}

func (e *RedisError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Cause != nil {
		return fmt.Sprintf("kvstore: %s: %v", e.Op, e.Cause)
	}
	return fmt.Sprintf("kvstore: %s", e.Op)
}

// Unwrap exposes the wrapped Cause so errors.Is / errors.As traverse the chain.
func (e *RedisError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// IsRetryable reports the Retryable flag of err if it is (or wraps) a
// *RedisError, or false otherwise.
func IsRetryable(err error) bool {
	var re *RedisError
	if errors.As(err, &re) {
		return re.Retryable
	}
	return false
}

// mapError translates Redis and context errors into kvstore errors.
//
// context.Canceled / context.DeadlineExceeded pass through RAW — the
// established codebase-wide convention (matches broker.mapError and the LLM
// adapters) so callers distinguish shutdown / caller-timeout from
// infrastructure failure. redis.Nil → ErrKeyNotFound (kept here as a
// defensive guard even though Get short-circuits it first — belt and
// suspenders, mirrors DP). redis.ErrClosed (pool already closed) is a
// permanent, non-retryable failure. Everything else is a network / server
// failure: retryable by default — a fresh pooled connection may resolve it.
func mapError(err error, op string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, redis.Nil) {
		return ErrKeyNotFound
	}
	if errors.Is(err, redis.ErrClosed) {
		return &RedisError{Op: op, Retryable: false, Cause: err}
	}
	return &RedisError{Op: op, Retryable: true, Cause: err}
}

// errClientClosed returns a non-retryable error for operations attempted on a
// Client whose Close() has been called. Mirrors the DP kvstore guard.
func errClientClosed(op string) error {
	return &RedisError{
		Op:        op,
		Retryable: false,
		Cause:     errors.New("client closed"),
	}
}

// redactURLCredentials returns err with the password component of rawURL (if
// any) replaced by "***". go-redis surfaces the dialled URL on ParseURL /
// handshake-failure paths; LIC_REDIS_URL can carry redis://:password@host and
// this system processes 152-ФЗ PII, so the credential must never reach an
// error string a caller logs (security bar set by broker.redactURLCredentials,
// MF-1). Deliberately duplicated from the broker rather than shared: there is
// no common infra util package, it is a small pure function, and coupling
// kvstore to the broker package for it would be worse than the duplication
// (code-architect must-fix 5).
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
