// Package schemavalidator is the JSON Schema Validator + 1-shot repair loop
// for the Legal Intelligence Core agent pipeline (LIC-TASK-023,
// high-architecture.md §6.6/§6.8, error-handling.md §5).
//
// Two responsibilities:
//
//   - Validator.Validate(schema, content): draft-07 validation of an agent's
//     LLM response against its embedded agents/schemas/*.json. Defence in
//     depth — the same schema is also handed to the provider in
//     CompletionRequest.JSONSchema for strict structured outputs, so an
//     invalid response is already an edge case (§6.8).
//   - RepairLoop.Run: on a SchemaViolation, build the §5.2 repair prompt and
//     issue exactly ONE sticky CompleteRepair on the provider that served the
//     primary call (OQ-10). On a second failure → AGENT_OUTPUT_INVALID
//     (is_retryable=true); on a provider error during repair → escalate
//     immediately with NO fallback (§6.8 — switching providers breaks
//     conversation continuity).
//
// THE ONE NON-HERMETIC internal/* PACKAGE. Every other internal/llm/* and
// internal/agents/* package imports only stdlib + internal/domain — but
// real JSON-Schema validation needs a real library, and schemas/CLAUDE.md
// explicitly defers "the real JSON-Schema library (kaptinlin/jsonschema or
// xeipuuv/gojsonschema)" to THIS task. github.com/xeipuuv/gojsonschema is
// confined here and re-exposed only through Validate(schema, content) error,
// so no other package gains the dependency. validator_internal_test.go pins
// this single-exception confinement (code-architect Q4); the Prometheus vecs
// remain inverted behind the Metrics seam (LIC-TASK-047 wires the adapter).
package schemavalidator

import (
	"github.com/xeipuuv/gojsonschema"
)

// Validator runs draft-07 JSON Schema validation. It is stateless and
// immutable → safe for concurrent use by the parallel errgroup agent
// pipeline without locking (gojsonschema compiles a fresh schema per call;
// no shared mutable state).
type Validator struct{}

// NewValidator constructs a Validator. There is no fail-fast configuration —
// the schema is supplied per Validate call (verbatim bytes from
// schemas.LoadSchema) and a broken schema is reported as *SchemaCompileError,
// not a constructor error (the schema set is validated fatally at startup by
// schemas.Validate()).
func NewValidator() *Validator { return &Validator{} }

// Validate checks content against the draft-07 JSON Schema in schema.
//
// Returns:
//   - nil — content is well-formed JSON and conforms to the schema.
//   - *SchemaCompileError — the schema itself is not a compilable draft-07
//     document (a LIC build defect; NEVER repaired — code-architect MF-3).
//   - *SchemaViolation — content is not syntactically valid JSON
//     (error-handling.md §5.1 "Не-JSON ответ"), or it is valid JSON that
//     violates the schema (missing required, additionalProperties, type /
//     enum mismatch, …). This is the only outcome that triggers the repair
//     loop.
//
// The schema is compiled separately first so a broken schema is never
// mis-reported as a content problem: only after the schema compiles is the
// document parsed/validated, and a document parse failure is then
// unambiguously a not-JSON response.
func (v *Validator) Validate(schema, content []byte) error {
	compiled, err := gojsonschema.NewSchema(gojsonschema.NewBytesLoader(schema))
	if err != nil {
		return &SchemaCompileError{Err: err}
	}

	result, err := compiled.Validate(gojsonschema.NewBytesLoader(content))
	if err != nil {
		// The schema compiled, so this can only be a document-side
		// failure: the response is not syntactically valid JSON
		// (error-handling.md §5.1 first bullet) — a repair trigger.
		return newSchemaViolation([]string{"response is not valid JSON: " + err.Error()})
	}
	if result.Valid() {
		return nil
	}

	msgs := make([]string, 0, len(result.Errors()))
	for _, e := range result.Errors() {
		msgs = append(msgs, e.String())
	}
	return newSchemaViolation(msgs)
}

// Compile-time assertion that gojsonschema is reachable through the chosen
// loader/validate API surface this package depends on.
var _ = gojsonschema.NewBytesLoader
