package logger

import "context"

// ctxKey is an unexported, zero-sized struct used as a context.WithValue key.
// The unexported type makes accidental collision impossible (security.md §6.2
// — IDs come from envelope; we want exactly one channel for them).
type ctxKey struct{}

// RequestContext carries the correlation IDs that every log line must carry
// (observability.md §1 — "Correlation ID везде"). Field set is the allowlist
// from security.md §6.2: IDs only, no PII.
//
// All fields are optional at the type level; the handler emits only the
// non-empty ones, so a partially populated RequestContext is safe.
type RequestContext struct {
	CorrelationID     string
	JobID             string
	DocumentID        string
	VersionID         string
	OrganizationID    string
	CreatedByUserID   string
	ConfirmedByUserID string
	MessageID         string
}

// WithRequestContext stores rc in ctx. Call this once at ingress (broker
// consumer reads the envelope, builds RequestContext, attaches it) — every
// downstream log line will pick the IDs up automatically.
func WithRequestContext(ctx context.Context, rc RequestContext) context.Context {
	return context.WithValue(ctx, ctxKey{}, rc)
}

// RequestContextFrom extracts the RequestContext from ctx. Returns the zero
// value if nothing was attached — that is the normal case for boot-time
// logs.
func RequestContextFrom(ctx context.Context) RequestContext {
	if ctx == nil {
		return RequestContext{}
	}
	rc, _ := ctx.Value(ctxKey{}).(RequestContext)
	return rc
}
