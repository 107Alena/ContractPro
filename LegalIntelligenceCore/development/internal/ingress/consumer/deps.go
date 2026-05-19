package consumer

// Deps bundles the optional-with-noop telemetry / runtime collaborators (the
// pendingconfirmation.Deps precedent — build-spec D19). A nil field degrades
// to its zero-dependency noop so the common production wiring (LIC-TASK-047)
// passes only the real adapters and tests override exactly what they need.
//
// BrokerSubscriber, EventRouter, port.DLQPublisherPort and dlqHashKey are
// positional/required NewConsumer params (REQUIRED, fail-fast non-nil —
// build-spec D2), NOT in Deps: keeping them off Deps keeps the
// optional/required split honest (the pendingconfirmation
// "required-collaborators are positional, NOT in Deps" rule).
type Deps struct {
	// Metrics — the lic_consumer_messages_total{topic,outcome} series. nil ⇒
	// noopMetrics.
	Metrics Metrics
	// Clock — deterministic time for LICDLQEnvelope.FailedAt. nil ⇒
	// systemClock (UTC wall clock).
	Clock Clock
	// Logger — structured INFO / WARN / ERROR + the ingress-once
	// WithRequestContext attachment (build-spec D6/R4). nil ⇒ noopLogger.
	Logger Logger
}

// withDefaults substitutes a zero-dependency noop for every nil seam and
// returns the completed Deps so the Consumer never nil-checks a seam on the
// hot path (the pendingconfirmation.Deps.withDefaults pattern verbatim).
func (d Deps) withDefaults() Deps {
	if d.Metrics == nil {
		d.Metrics = noopMetrics{}
	}
	if d.Clock == nil {
		d.Clock = systemClock{}
	}
	if d.Logger == nil {
		d.Logger = noopLogger{}
	}
	return d
}
