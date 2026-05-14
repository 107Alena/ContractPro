package model

import "fmt"

// ErrorCode is a typed machine-readable error code published in
// lic.events.status-changed and lic.dlq.* DLQ envelopes. The catalog below
// (errorCatalog) is the authoritative source for the user-facing RU
// rendering, the developer-facing EN rendering and the Retryable flag.
//
// Adding a new code requires both a constant declaration AND a catalog
// entry — init() panics otherwise (single source of truth invariant).
type ErrorCode string

// 3.1 Inbound errors (consumer side).
const (
	ErrCodeInvalidMessageSchema ErrorCode = "INVALID_MESSAGE_SCHEMA"
	ErrCodeInvalidOrgIDMismatch ErrorCode = "INVALID_ORG_ID_MISMATCH"
)

// 3.2 DM-related errors.
const (
	ErrCodeDMArtifactsTimeout ErrorCode = "DM_ARTIFACTS_TIMEOUT"
	ErrCodeDMArtifactsMissing ErrorCode = "DM_ARTIFACTS_MISSING"
	ErrCodeDMPersistFailed    ErrorCode = "DM_PERSIST_FAILED"
	ErrCodeDMPersistTimeout   ErrorCode = "DM_PERSIST_TIMEOUT"
)

// 3.3 Agent-related errors.
const (
	ErrCodeAgentOutputInvalid     ErrorCode = "AGENT_OUTPUT_INVALID"
	ErrCodeAgentTimeout           ErrorCode = "AGENT_TIMEOUT"
	ErrCodeAgentInputTooLarge     ErrorCode = "AGENT_INPUT_TOO_LARGE"
	ErrCodeAgentDependencyFailed  ErrorCode = "AGENT_DEPENDENCY_FAILED"
)

// 3.4 LLM-provider errors.
const (
	ErrCodeLLMAllProvidersFailed     ErrorCode = "LLM_ALL_PROVIDERS_FAILED"
	ErrCodeLLMQuotaExceeded          ErrorCode = "LLM_QUOTA_EXCEEDED"
	ErrCodeLLMContentPolicyViolation ErrorCode = "LLM_CONTENT_POLICY_VIOLATION"
)

// 3.5 Pipeline-level errors.
const (
	ErrCodeAnalysisTimeout  ErrorCode = "ANALYSIS_TIMEOUT"
	ErrCodeInputRejected    ErrorCode = "INPUT_REJECTED"
	ErrCodeDocumentTooLarge ErrorCode = "DOCUMENT_TOO_LARGE"
)

// 3.6 User-confirmation errors.
const (
	ErrCodeUserConfirmationExpired ErrorCode = "USER_CONFIRMATION_EXPIRED"
	ErrCodeInvalidContractType     ErrorCode = "INVALID_CONTRACT_TYPE"
)

// 3.7 Internal errors.
const (
	ErrCodeInternal                  ErrorCode = "INTERNAL_ERROR"
	ErrCodeIdempotencyStoreUnavail   ErrorCode = "IDEMPOTENCY_STORE_UNAVAILABLE"
)

// String returns the wire representation of the code.
func (c ErrorCode) String() string { return string(c) }

// IsPublishableToOrchestrator reports whether errors carrying this code may
// be forwarded to the Orchestrator via lic.events.status-changed. Codes that
// only appear in DLQ envelopes (because incoming events from DM are trusted
// upstream, or because the failure is internal NACK-able state) have empty
// catalog UserMessage and must NOT be published — publishing an empty
// error_message would silently corrupt the user-facing UX.
//
// Returns false for unknown codes (safe default — refuse to publish).
func (c ErrorCode) IsPublishableToOrchestrator() bool {
	spec, ok := errorCatalog[c]
	if !ok {
		return false
	}
	return spec.userMessage != ""
}

// ErrorSpec is the catalog row backing an ErrorCode. Exported so observability
// and audit-log code can render the user/dev message pair without
// reimplementing the catalog. The struct is read-only by contract — callers
// must not mutate fields obtained via LookupErrorSpec.
//
// retryable is the *default* Retryable value used by NewDomainError; codes
// whose retryable flag depends on the cause (DM_PERSIST_FAILED mirrors DM's
// is_retryable, AGENT_DEPENDENCY_FAILED inherits the wrapped cause) override
// it at the call site via DomainError.WithRetryable.
type ErrorSpec struct {
	Retryable   bool
	UserMessage string // RU, user-facing
	DevMessage  string // EN, log default
}

// errorSpec is the internal catalog row. Unexported fields keep the catalog
// definition tight; LookupErrorSpec promotes them to the exported ErrorSpec.
type errorSpec struct {
	retryable   bool
	userMessage string
	devMessage  string
}

// errorCatalog is the authoritative table of all ErrorCode metadata. It is
// the only place where RU user messages live in the domain layer (callers
// MUST NOT inline localised strings).
//
// Source of truth: error-handling.md §3.1–§3.7. Any change here must be
// mirrored in that document.
var errorCatalog = map[ErrorCode]errorSpec{
	// 3.1 Inbound — UserMessage left empty because per spec these codes
	// only appear in DLQ envelopes and are NOT published to the
	// Orchestrator (events from DM are trusted upstream).
	ErrCodeInvalidMessageSchema: {
		retryable:   false,
		userMessage: "",
		devMessage:  "incoming message failed schema validation",
	},
	ErrCodeInvalidOrgIDMismatch: {
		retryable:   false,
		userMessage: "",
		devMessage:  "organization_id mismatch between event and pipeline state",
	},

	// 3.2 DM-related.
	ErrCodeDMArtifactsTimeout: {
		retryable:   true,
		userMessage: "Не удалось получить данные документа за отведённое время. Попробуйте ещё раз.",
		devMessage:  "timed out waiting for ArtifactsProvided from DM",
	},
	ErrCodeDMArtifactsMissing: {
		retryable:   true,
		userMessage: "Данные документа не найдены. Возможно, обработка ещё не завершилась.",
		devMessage:  "DM returned no artifacts for the requested version",
	},
	ErrCodeDMPersistFailed: {
		// retryable depends on DM's is_retryable; default to the
		// retryable case (transient DM failure) — non-retryable
		// callers (DOCUMENT_NOT_FOUND) override at construction.
		retryable:   true,
		userMessage: "Не удалось сохранить результат анализа.",
		devMessage:  "DM rejected lic.artifacts.analysis-ready persist request",
	},
	ErrCodeDMPersistTimeout: {
		retryable:   true,
		userMessage: "Не удалось получить подтверждение сохранения. Попробуйте позже.",
		devMessage:  "timed out waiting for LegalAnalysisArtifactsPersisted from DM",
	},

	// 3.3 Agents.
	ErrCodeAgentOutputInvalid: {
		retryable:   true,
		userMessage: "Не удалось получить корректный анализ. Запустите повторную проверку.",
		devMessage:  "agent produced output that failed JSON schema validation after repair",
	},
	ErrCodeAgentTimeout: {
		retryable:   true,
		userMessage: "Один из этапов анализа занял слишком много времени.",
		devMessage:  "agent invocation exceeded its per-agent timeout",
	},
	ErrCodeAgentInputTooLarge: {
		retryable:   false,
		userMessage: "Документ слишком большой для анализа. Разделите его на части.",
		devMessage:  "agent input exceeded max_input_tokens budget after truncation",
	},
	ErrCodeAgentDependencyFailed: {
		// Retryable depends on cause; default false (most upstream
		// failures that propagate here are non-retryable).
		retryable:   false,
		userMessage: "Не удалось завершить анализ из-за сбоя на предыдущем этапе.",
		devMessage:  "agent dependency in an earlier stage failed",
	},

	// 3.4 LLM-provider.
	ErrCodeLLMAllProvidersFailed: {
		retryable:   true,
		userMessage: "Сервис анализа временно недоступен. Попробуйте позже.",
		devMessage:  "exhausted provider fallback chain — all providers failed",
	},
	ErrCodeLLMQuotaExceeded: {
		retryable:   false,
		userMessage: "Превышен лимит запросов к ИИ-сервису. Обратитесь к администратору.",
		devMessage:  "LLM provider returned 429 quota_exceeded after retry budget",
	},
	ErrCodeLLMContentPolicyViolation: {
		retryable:   false,
		userMessage: "Документ содержит контент, который не может быть обработан.",
		devMessage:  "LLM provider blocked the request due to content policy",
	},

	// 3.5 Pipeline-level.
	ErrCodeAnalysisTimeout: {
		retryable:   true,
		userMessage: "Анализ занял слишком много времени. Запустите повторную проверку.",
		devMessage:  "job-level context deadline exceeded (LIC_JOB_TIMEOUT)",
	},
	ErrCodeInputRejected: {
		retryable:   false,
		userMessage: "Полученные данные документа повреждены или неполные.",
		devMessage:  "input artifacts are malformed or incomplete",
	},
	ErrCodeDocumentTooLarge: {
		retryable:   false,
		userMessage: "Документ слишком большой для юридического анализа.",
		devMessage:  "ingested artifact size exceeded LIC_MAX_INGESTED_BYTES",
	},

	// 3.6 User confirmation.
	ErrCodeUserConfirmationExpired: {
		retryable:   false,
		userMessage: "Время на подтверждение типа договора истекло. Запустите проверку заново.",
		devMessage:  "pending classification confirmation TTL expired in Redis",
	},
	ErrCodeInvalidContractType: {
		retryable:   false,
		userMessage: "Указан некорректный тип договора. Обновите страницу и попробуйте снова.",
		devMessage:  "user-confirmed contract_type is not in the 12-value whitelist",
	},

	// 3.7 Internal.
	ErrCodeInternal: {
		retryable:   true,
		userMessage: "Произошла внутренняя ошибка. Попробуйте позже.",
		devMessage:  "unclassified internal error — see logs",
	},
	ErrCodeIdempotencyStoreUnavail: {
		retryable:   true,
		userMessage: "", // NACK path; not surfaced to the Orchestrator.
		devMessage:  "idempotency store (Redis) is unavailable — falling back / NACKing",
	},
}

// AllErrorCodes returns a fresh slice with every ErrorCode constant declared
// in this file, in the same section/declaration order as error-handling.md
// §3.1–§3.7. Callers may mutate the returned slice.
//
// Used by tests to assert catalog completeness against the constant list.
func AllErrorCodes() []ErrorCode {
	return []ErrorCode{
		// 3.1
		ErrCodeInvalidMessageSchema,
		ErrCodeInvalidOrgIDMismatch,
		// 3.2
		ErrCodeDMArtifactsTimeout,
		ErrCodeDMArtifactsMissing,
		ErrCodeDMPersistFailed,
		ErrCodeDMPersistTimeout,
		// 3.3
		ErrCodeAgentOutputInvalid,
		ErrCodeAgentTimeout,
		ErrCodeAgentInputTooLarge,
		ErrCodeAgentDependencyFailed,
		// 3.4
		ErrCodeLLMAllProvidersFailed,
		ErrCodeLLMQuotaExceeded,
		ErrCodeLLMContentPolicyViolation,
		// 3.5
		ErrCodeAnalysisTimeout,
		ErrCodeInputRejected,
		ErrCodeDocumentTooLarge,
		// 3.6
		ErrCodeUserConfirmationExpired,
		ErrCodeInvalidContractType,
		// 3.7
		ErrCodeInternal,
		ErrCodeIdempotencyStoreUnavail,
	}
}

// LookupErrorSpec returns the catalog row for code as an ErrorSpec, or
// (zero ErrorSpec, false) if code is not registered. Exported so test
// helpers and the audit/observability layer can render user-facing strings
// without re-implementing the catalog.
func LookupErrorSpec(code ErrorCode) (ErrorSpec, bool) {
	spec, found := errorCatalog[code]
	if !found {
		return ErrorSpec{}, false
	}
	return ErrorSpec{
		Retryable:   spec.retryable,
		UserMessage: spec.userMessage,
		DevMessage:  spec.devMessage,
	}, true
}

// init enforces the single-source-of-truth invariant: every ErrorCode listed
// by AllErrorCodes must have a catalog entry, and the catalog must not
// contain entries that are not declared as constants. A mismatch is a build
// bug — the panic at startup catches it before production.
func init() {
	declared := AllErrorCodes()
	declaredSet := make(map[ErrorCode]struct{}, len(declared))
	for _, c := range declared {
		declaredSet[c] = struct{}{}
		if _, ok := errorCatalog[c]; !ok {
			panic(fmt.Sprintf("model.init: ErrorCode %q is declared but missing from errorCatalog", c))
		}
	}
	for c := range errorCatalog {
		if _, ok := declaredSet[c]; !ok {
			panic(fmt.Sprintf("model.init: errorCatalog entry %q has no matching ErrorCode constant in AllErrorCodes()", c))
		}
	}
}
