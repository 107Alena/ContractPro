package logger

import (
	"context"
	"log/slog"
)

// licHandler decorates an inner slog.Handler with three responsibilities:
//
//  1. Drain the RequestContext from ctx and emit each non-empty ID as a
//     top-level attribute (observability.md §1 — IDs on every line, no
//     manual plumbing at call sites). The IDs are written into the
//     record itself (not via WithAttrs) so they stay top-level even
//     after WithGroup nesting.
//  2. Auto-redact attributes whose key is in autoSanitizeKeys. We handle
//     both KindString values and KindAny values implementing the `error`
//     interface — slog.Any("error", err) is the idiomatic shape and must
//     not be a leak channel (security.md §3.4 / §6.2).
//  3. Sanitize the record's `msg` itself at WARN/ERROR/FATAL — call sites
//     that build a message via fmt.Sprintf("...: %v", err) cannot smuggle
//     a secret out via the message. DEBUG/INFO are left untouched to keep
//     dev logs cheap and verbose.
//
// The handler also overrides the level rendering: stdlib slog encodes our
// custom LevelFatal as "ERROR+4". We rewrite it to the canonical "FATAL"
// label that operator alerts and SIEM rules expect.
type licHandler struct {
	inner slog.Handler
}

var _ slog.Handler = (*licHandler)(nil)

// newHandler wraps a stdlib JSON handler with our context/sanitize logic.
// `service` is bound on every emit through Handle, so a call site cannot
// forge a different service name even via WithGroup nesting.
func newHandler(inner slog.Handler) *licHandler {
	return &licHandler{inner: inner}
}

func (h *licHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *licHandler) Handle(ctx context.Context, r slog.Record) error {
	// Sanitize the message itself at WARN+. If a call site formats a
	// secret into the msg via fmt.Sprintf, this is the only line of
	// defense — there is no per-attr branch that catches it.
	msg := r.Message
	if r.Level >= slog.LevelWarn {
		msg = Sanitize(msg)
	}

	// Build the new record. IDs go in first so they appear at the top of
	// the JSON object (humans read top-down, log aggregators key on them).
	r2 := slog.NewRecord(r.Time, r.Level, msg, r.PC)
	r2.AddAttrs(h.collectIDAttrs(ctx)...)

	// Walk the original record and append each attr — sanitizing those
	// whose keys are flagged as untrusted-content channels. This works
	// even with WithGroup wrappers because we never call h.inner.WithAttrs
	// for the IDs; they live on the record itself.
	r.Attrs(func(a slog.Attr) bool {
		if _, ok := autoSanitizeKeys[a.Key]; ok {
			a = sanitizeAttr(a)
		}
		r2.AddAttrs(a)
		return true
	})

	return h.inner.Handle(ctx, r2)
}

func (h *licHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &licHandler{inner: h.inner.WithAttrs(attrs)}
}

// WithGroup is a documented no-op for licHandler. observability.md §2.1
// requires service/correlation IDs at the top level of every JSON record.
// Honouring WithGroup would nest them inside the named group and break the
// log-aggregator queries that key on those fields. Callers wanting a
// logical sub-component should use Logger.With(component) instead, which
// adds a top-level `component` attribute.
//
// This intentionally diverges from the slog.Handler contract; the
// divergence is local (only callers reaching for *slog.Logger via Slog()
// can hit it) and it preserves a stronger invariant — operator dashboards
// can rely on the IDs being where the spec promises them.
func (h *licHandler) WithGroup(name string) slog.Handler {
	_ = name
	return h
}

// collectIDAttrs builds the per-record attribute prefix from RequestContext
// and the service identity. The slice grows only as far as actually needed —
// empty fields are skipped per the allowlist policy.
func (h *licHandler) collectIDAttrs(ctx context.Context) []slog.Attr {
	rc := RequestContextFrom(ctx)
	out := make([]slog.Attr, 0, 8)
	out = append(out, slog.String(KeyService, ServiceName))
	if rc.CorrelationID != "" {
		out = append(out, slog.String(KeyCorrelationID, rc.CorrelationID))
	}
	if rc.JobID != "" {
		out = append(out, slog.String(KeyJobID, rc.JobID))
	}
	if rc.DocumentID != "" {
		out = append(out, slog.String(KeyDocumentID, rc.DocumentID))
	}
	if rc.VersionID != "" {
		out = append(out, slog.String(KeyVersionID, rc.VersionID))
	}
	if rc.OrganizationID != "" {
		out = append(out, slog.String(KeyOrganizationID, rc.OrganizationID))
	}
	if rc.CreatedByUserID != "" {
		out = append(out, slog.String(KeyCreatedByUserID, rc.CreatedByUserID))
	}
	if rc.ConfirmedByUserID != "" {
		out = append(out, slog.String(KeyConfirmedByUserID, rc.ConfirmedByUserID))
	}
	if rc.MessageID != "" {
		out = append(out, slog.String(KeyMessageID, rc.MessageID))
	}
	return out
}

// sanitizeAttr returns a redacted copy of `a` while preserving the key.
// Handles three real-world shapes:
//   - slog.String(KeyError, err.Error())       → KindString
//   - slog.Any(KeyError, err)                  → KindAny + error iface
//   - slog.Any(KeyError, "raw response body")  → KindAny + string
//
// Any other Kind is left alone (numbers, bools, durations — secrets don't
// hide there).
func sanitizeAttr(a slog.Attr) slog.Attr {
	switch a.Value.Kind() {
	case slog.KindString:
		return slog.String(a.Key, Sanitize(a.Value.String()))
	case slog.KindAny:
		v := a.Value.Any()
		if err, ok := v.(error); ok && err != nil {
			return slog.String(a.Key, Sanitize(err.Error()))
		}
		if s, ok := v.(string); ok {
			return slog.String(a.Key, Sanitize(s))
		}
	}
	return a
}

// replaceAttr is the slog.HandlerOptions.ReplaceAttr hook we wire into the
// underlying JSONHandler at construction time. It rewrites the level and
// time keys to match the spec (observability.md §2.1: timestamp + level).
func replaceAttr(_ []string, a slog.Attr) slog.Attr {
	switch a.Key {
	case slog.LevelKey:
		if lvl, ok := a.Value.Any().(slog.Level); ok {
			return slog.String(slog.LevelKey, levelLabel(lvl))
		}
	case slog.TimeKey:
		// JSONHandler emits RFC3339Nano by default; we just rename the
		// field to "timestamp" to match observability.md §2.1.
		return slog.Attr{Key: "timestamp", Value: a.Value}
	}
	return a
}
