package observability

import "context"

// contextKey is an unexported type used as context key to prevent collisions.
type contextKey struct{}

// EventContext carries correlation fields for a single event being processed.
// It is propagated through context.Context so that loggers and tracers
// can automatically enrich spans and log lines without explicit plumbing.
type EventContext struct {
	CorrelationID  string
	JobID          string
	DocumentID     string
	VersionID      string
	OrganizationID string
	Stage          string
}

// WithEventContext returns a new context carrying the given EventContext.
func WithEventContext(ctx context.Context, ec EventContext) context.Context {
	return context.WithValue(ctx, contextKey{}, ec)
}

// EventContextFrom extracts the EventContext from ctx.
// Returns a zero-value EventContext if none is present.
func EventContextFrom(ctx context.Context) EventContext {
	ec, _ := ctx.Value(contextKey{}).(EventContext)
	return ec
}

// WithStage returns a new context with the Stage field updated.
// All other EventContext fields are preserved. If no EventContext is present
// in the parent context, a new one is created with only Stage set.
func WithStage(ctx context.Context, stage string) context.Context {
	ec := EventContextFrom(ctx)
	ec.Stage = stage
	return WithEventContext(ctx, ec)
}
