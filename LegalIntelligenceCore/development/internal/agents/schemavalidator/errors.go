package schemavalidator

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// SchemaViolation is the typed result of a content document that is either
// not syntactically valid JSON or valid JSON that fails draft-07 validation
// against the agent's embedded output schema (error-handling.md §5.1). It is
// the ONLY validator failure that triggers the repair loop — a model that
// produced almost-right JSON can be coached into fixing it.
//
// Errors is deterministic: sorted and de-duplicated at construction so the
// {validation_errors_pretty_printed} placeholder in the repair prompt is
// byte-stable across runs (xeipuuv/gojsonschema reports errors in
// map-iteration order, which is not stable).
type SchemaViolation struct {
	Errors []string
}

// Error implements error. Kept compact (count + joined detail) for log lines;
// the model-facing rendering used in the repair prompt is Pretty().
func (e *SchemaViolation) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("schema validation failed: %d error(s): %s",
		len(e.Errors), strings.Join(e.Errors, "; "))
}

// Pretty renders the violation list one-per-line for the
// {validation_errors_pretty_printed} slot of the §5.2 repair prompt.
func (e *SchemaViolation) Pretty() string {
	if e == nil {
		return ""
	}
	return strings.Join(e.Errors, "\n")
}

// newSchemaViolation sorts + de-duplicates msgs so the violation list (and
// therefore the repair prompt) is deterministic. An empty/whitespace message
// is dropped; if nothing remains a single sentinel is used so a violation is
// never silently empty.
func newSchemaViolation(msgs []string) *SchemaViolation {
	seen := make(map[string]struct{}, len(msgs))
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		if _, dup := seen[m]; dup {
			continue
		}
		seen[m] = struct{}{}
		out = append(out, m)
	}
	if len(out) == 0 {
		out = []string{"response did not conform to the expected JSON schema"}
	}
	sort.Strings(out)
	return &SchemaViolation{Errors: out}
}

// SchemaCompileError signals that the *schema itself* failed to compile as a
// draft-07 document. This is a LIC build defect (a corrupt embedded
// agents/schemas/*.json), NOT a model mistake — it must surface loudly and
// must NEVER trigger a repair loop (re-prompting the model cannot fix our
// broken schema). schemas.Validate() is a fatal startup check
// (schemas/CLAUDE.md), so reaching this at run time is a defence-in-depth
// internal-invariant breach → escalated as INTERNAL_ERROR (code-architect
// MF-3), not AGENT_OUTPUT_INVALID.
type SchemaCompileError struct {
	Err error
}

// Error implements error.
func (e *SchemaCompileError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("schema failed to compile as JSON Schema draft-07: %v", e.Err)
}

// Unwrap exposes the underlying gojsonschema compile error for errors.Is /
// errors.As traversal and root-cause logging.
func (e *SchemaCompileError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// AsSchemaViolation extracts a *SchemaViolation from err via errors.As,
// returning (nil, false) when err carries no such value.
func AsSchemaViolation(err error) (*SchemaViolation, bool) {
	if err == nil {
		return nil, false
	}
	var v *SchemaViolation
	if errors.As(err, &v) {
		return v, true
	}
	return nil, false
}

// AsSchemaCompileError extracts a *SchemaCompileError from err via errors.As,
// returning (nil, false) when err carries no such value.
func AsSchemaCompileError(err error) (*SchemaCompileError, bool) {
	if err == nil {
		return nil, false
	}
	var c *SchemaCompileError
	if errors.As(err, &c) {
		return c, true
	}
	return nil, false
}

// Compile-time assertions that both error types satisfy error.
var (
	_ error = (*SchemaViolation)(nil)
	_ error = (*SchemaCompileError)(nil)
)
