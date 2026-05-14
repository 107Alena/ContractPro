package logger

// Exported attribute keys. Call sites use these constants instead of string
// literals so the lint is mechanical and the allowlist (security.md §6.2) is
// enforced by reference.
//
// Adding a new key here is an explicit decision — keep the list short and
// conform to the allowlist. Anything that is not on the allowlist must NOT
// become a new constant here.
const (
	// Mandatory per-record fields (observability.md §2.1).
	KeyService        = "service"
	KeyCorrelationID  = "correlation_id"
	KeyJobID          = "job_id"
	KeyVersionID      = "version_id"
	KeyOrganizationID = "organization_id"
	KeyComponent      = "component"

	// Optional ID fields (allowlist — observability.md §2.4 / security.md §6.2).
	KeyDocumentID         = "document_id"
	KeyCreatedByUserID    = "created_by_user_id"
	KeyConfirmedByUserID  = "confirmed_by_user_id"
	KeyAgentID            = "agent_id"
	KeyProviderID         = "provider_id"
	KeyMessageID          = "message_id"

	// Pipeline metadata.
	KeyStage     = "stage"
	KeyStatus    = "status"
	KeyOutcome   = "outcome"
	KeyErrorCode = "error_code"

	// Auto-redacted attribute keys. Any attribute with one of these keys is
	// passed through Sanitize before emission, regardless of the value's
	// slog.Kind (string vs. error vs. any). Use them whenever you log
	// content that originates outside our trust boundary (provider error
	// chains, raw HTTP bodies).
	KeyError        = "error"
	KeyErrorMessage = "error_message"
	KeyRequestBody  = "request_body"
	KeyResponseBody = "response_body"

	// Service identity — emitted on every record.
	ServiceName = "lic-service"
)
