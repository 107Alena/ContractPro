package observability

import "context"

// contextKey is an unexported type used as context key to prevent collisions.
type contextKey struct{}

// JobContext carries correlation fields for a single processing job.
// It is propagated through context.Context so that loggers and tracers
// can automatically enrich spans and log lines without explicit plumbing.
type JobContext struct {
	JobID         string
	DocumentID    string
	CorrelationID string
	Stage         string
	OrgID         string
	UserID        string
}

// WithJobContext returns a new context carrying the given JobContext.
func WithJobContext(ctx context.Context, jc JobContext) context.Context {
	return context.WithValue(ctx, contextKey{}, jc)
}

// JobContextFrom extracts the JobContext from ctx.
// Returns a zero-value JobContext if none is present.
func JobContextFrom(ctx context.Context) JobContext {
	jc, _ := ctx.Value(contextKey{}).(JobContext)
	return jc
}

// WithStage returns a new context with the Stage field updated.
// All other JobContext fields are preserved. If no JobContext is present
// in the parent context, a new one is created with only Stage set.
func WithStage(ctx context.Context, stage string) context.Context {
	jc := JobContextFrom(ctx)
	jc.Stage = stage
	return WithJobContext(ctx, jc)
}
