package logger

// ForbiddenLogKeys enumerates attribute keys that MUST NEVER appear in
// emitted log records (security.md §6.1/§6.2, observability.md §2.4).
//
// The list is the negative half of the allowlist: anything here is content
// (PII, prompt body, model output, secrets) — not metadata. The enforcement
// model is by API discipline plus the static-analyzer test (see
// static_analyzer_test.go), not a runtime drop in the handler:
//
//   - security.md §6.3 specifies "strict whitelist via API discipline" —
//     introducing a runtime guard would create a false sense of safety
//     where developers stop reading the analyzer output, and the failure
//     mode (silent attr drop in prod) is worse than the bug.
//   - the handler already runs Sanitize on KeyError / KeyErrorMessage /
//     KeyRequestBody / KeyResponseBody so secrets that smuggle in through
//     those allowlisted channels are scrubbed.
//
// Allowed-by-contract counterparts:
//
//   - raw_response_hash, raw_fragment_hash — safe surrogates per §6.4
//     (use HashContent).
//   - error_message — auto-sanitized.
//   - prompt_injection_detected — bool only, no content.
//   - model, outcome, tokens_in, tokens_out, latency_ms, cost_usd — pure
//     metadata per observability.md §2.4.
//
// Adding a new key here is the deliberate counterpart of adding a constant
// to keys.go: an entry here documents a denial; an entry there documents
// an allowance. Keep the two files in sync by review.
var ForbiddenLogKeys = []string{
	// Document body / artifacts.
	"contract_text",
	"extracted_text",
	"semantic_tree",
	"document_text",

	// LLM I/O bodies. Multiple spellings to defend against developer drift.
	"raw_llm_response",
	"raw_response",
	"llm_response",
	"full_llm_response",
	"raw_prompt",
	"prompt_body",

	// PII-bearing analysis artifacts (security.md §6.2 explicit list).
	"key_parameters",
	"risks",
	"parties",
	"subject",
	"price",
	"party_details",
	"appendix_content",
	"clause_text",

	// Secrets — should never appear as a structured field.
	"api_key",
	"authorization",
	"bearer_token",
}
