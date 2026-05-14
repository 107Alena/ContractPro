package logger

import "regexp"

// Replacement marker emitted in place of redacted secrets. Single token so
// log-aggregator queries can grep for redactions cheaply.
const redactedMarker = "***REDACTED***"

// secretPattern matches LLM provider API keys and OAuth bearer tokens.
// security.md §3.4 lists the required patterns; ai-agents-pipeline.md confirms
// the providers (Anthropic, OpenAI, Gemini).
//
// Notes on the regexes:
//   - sk-ant-...    — Anthropic prefix is fixed; key body uses [A-Za-z0-9_-].
//   - sk-(proj|svcacct|admin|or)-... — modern OpenAI variants are anchored on
//                     these well-known suffixes; tail uses the same alphabet.
//   - sk-...        — legacy classic OpenAI keys; ≥20 alnum/dash chars to
//                     avoid hitting unrelated short literals like the bare
//                     "sk-" inside product names.
//   - AIza...       — Google API keys are AIza followed by 35 base64-url chars.
//   - Bearer X      — single-space separator per RFC 6750. The token tail
//                     accepts the full base64-standard alphabet (`+/=` plus
//                     `_-.~`) — without `=`/`+`/`/` the regex would truncate
//                     opaque bearer tokens leaving the secret tail intact in
//                     the log.
var secretPattern = regexp.MustCompile(
	`Bearer\s+[A-Za-z0-9._\-+/=~]+` +
		`|sk-ant-[A-Za-z0-9_\-]+` +
		`|sk-(?:proj|svcacct|admin|or)-[A-Za-z0-9_\-]+` +
		`|sk-[A-Za-z0-9_\-]{20,}` +
		`|AIza[A-Za-z0-9_\-]{35}`,
)

// queryKeyPattern matches the Gemini-style auth-via-query-string. We keep
// the `?key=` / `&key=` prefix verbatim (capture group 1) and redact only
// the token value — the URL stays diagnosable but the secret is gone.
// Case-insensitive so `?KEY=`, `&Key=` are also handled.
var queryKeyPattern = regexp.MustCompile(`(?i)([?&]key=)[^&\s]+`)

// Sanitize strips known LLM API keys and bearer tokens from s. The function
// is deterministic and allocation-conservative: when nothing matches it
// returns the input string unchanged.
//
// Sanitize is exported so callers can run it on user-controlled data before
// logging (e.g. when an LLM error body is included in an error_message).
// The package's slog handler runs it automatically on:
//   - any record's `msg` at WARN/ERROR/FATAL level;
//   - any attribute keyed by KeyError, KeyErrorMessage, KeyRequestBody,
//     KeyResponseBody, regardless of level.
func Sanitize(s string) string {
	if s == "" {
		return s
	}
	out := secretPattern.ReplaceAllString(s, redactedMarker)
	out = queryKeyPattern.ReplaceAllString(out, "${1}"+redactedMarker)
	return out
}

// sanitizeError returns the redacted form of err.Error(). Returns "" for nil
// err so handler code stays branch-free at the call site.
func sanitizeError(err error) string {
	if err == nil {
		return ""
	}
	return Sanitize(err.Error())
}

// autoSanitizeKeys is the set of attribute keys whose string value (or
// .Error() if the value is an error interface) is run through Sanitize
// automatically by the handler. Keep this list short and explicit — keys
// here represent fields whose content originates from outside our trust
// boundary (LLM provider HTTP response bodies, third-party error chains).
var autoSanitizeKeys = map[string]struct{}{
	KeyError:        {},
	KeyErrorMessage: {},
	KeyRequestBody:  {},
	KeyResponseBody: {},
}
