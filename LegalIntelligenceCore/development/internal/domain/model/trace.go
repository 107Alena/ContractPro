package model

// TraceContext capture W3C Trace Context propagation headers, persisted alongside
// pending pipeline state so a paused-and-resumed pipeline can rebuild its OTel
// span lineage. Two fields only — fixed by the W3C specification.
//
// Field names map to the lowercase HTTP header form used on the wire.
type TraceContext struct {
	TraceParent string `json:"traceparent"`
	TraceState  string `json:"tracestate,omitempty"`
}

// IsZero reports whether no trace context was captured (both fields empty).
func (t TraceContext) IsZero() bool {
	return t.TraceParent == "" && t.TraceState == ""
}
