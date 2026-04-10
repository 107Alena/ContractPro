package model

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"contractpro/api-orchestrator/internal/infra/observability/logger"
)

// ErrorCode is a machine-readable UPPER_SNAKE_CASE error identifier.
// Frontend uses this field for programmatic error handling.
type ErrorCode string

// Authentication errors (401).
const (
	ErrAuthTokenMissing ErrorCode = "AUTH_TOKEN_MISSING"
	ErrAuthTokenExpired ErrorCode = "AUTH_TOKEN_EXPIRED"
	ErrAuthTokenInvalid ErrorCode = "AUTH_TOKEN_INVALID"
)

// Authorization errors (403).
const (
	ErrPermissionDenied ErrorCode = "PERMISSION_DENIED"
)

// File validation errors (400, 413, 415).
const (
	ErrFileTooLarge      ErrorCode = "FILE_TOO_LARGE"
	ErrUnsupportedFormat ErrorCode = "UNSUPPORTED_FORMAT"
	ErrInvalidFile       ErrorCode = "INVALID_FILE"
)

// Resource not found errors (404).
const (
	ErrDocumentNotFound  ErrorCode = "DOCUMENT_NOT_FOUND"
	ErrVersionNotFound   ErrorCode = "VERSION_NOT_FOUND"
	ErrArtifactNotFound  ErrorCode = "ARTIFACT_NOT_FOUND"
	ErrDiffNotFound      ErrorCode = "DIFF_NOT_FOUND"
	ErrPolicyNotFound    ErrorCode = "POLICY_NOT_FOUND"
	ErrChecklistNotFound ErrorCode = "CHECKLIST_NOT_FOUND"
)

// Conflict / state errors (409).
const (
	ErrDocumentArchived       ErrorCode = "DOCUMENT_ARCHIVED"
	ErrDocumentDeleted        ErrorCode = "DOCUMENT_DELETED"
	ErrVersionStillProcessing ErrorCode = "VERSION_STILL_PROCESSING"
	ErrResultsNotReady        ErrorCode = "RESULTS_NOT_READY"
)

// Rate limiting (429).
const (
	ErrRateLimitExceeded ErrorCode = "RATE_LIMIT_EXCEEDED"
)

// Downstream service unavailability (502).
const (
	ErrStorageUnavailable     ErrorCode = "STORAGE_UNAVAILABLE"
	ErrDMUnavailable          ErrorCode = "DM_UNAVAILABLE"
	ErrOPMUnavailable         ErrorCode = "OPM_UNAVAILABLE"
	ErrBrokerUnavailable      ErrorCode = "BROKER_UNAVAILABLE"
	ErrAuthServiceUnavailable ErrorCode = "AUTH_SERVICE_UNAVAILABLE"
)

// UOM authentication errors (401).
const (
	ErrInvalidCredentials ErrorCode = "INVALID_CREDENTIALS"
	ErrTokenRevoked       ErrorCode = "TOKEN_REVOKED"
	ErrRefreshTokenExpired ErrorCode = "REFRESH_TOKEN_EXPIRED"
)

// Validation (400).
const (
	ErrValidationError ErrorCode = "VALIDATION_ERROR"
)

// Internal (500).
const (
	ErrInternalError ErrorCode = "INTERNAL_ERROR"
)

// ErrorEntry holds the fixed HTTP status, Russian message, and optional
// Russian suggestion for a single error code.
type ErrorEntry struct {
	HTTPStatus int
	Message    string
	Suggestion string
}

// errorCatalog maps every ErrorCode to its fixed ErrorEntry.
// This is the single source of truth for HTTP status codes and user-facing
// Russian text. The map is never mutated after package init.
var errorCatalog = map[ErrorCode]ErrorEntry{
	ErrAuthTokenMissing: {
		HTTPStatus: http.StatusUnauthorized,
		Message:    "Требуется авторизация. Токен доступа не предоставлен.",
		Suggestion: "Войдите в систему и повторите запрос.",
	},
	ErrAuthTokenExpired: {
		HTTPStatus: http.StatusUnauthorized,
		Message:    "Срок действия токена истёк.",
		Suggestion: "Обновите токен или войдите в систему повторно.",
	},
	ErrAuthTokenInvalid: {
		HTTPStatus: http.StatusUnauthorized,
		Message:    "Токен доступа недействителен.",
		Suggestion: "Войдите в систему повторно.",
	},
	ErrPermissionDenied: {
		HTTPStatus: http.StatusForbidden,
		Message:    "У вас недостаточно прав для выполнения этой операции.",
		Suggestion: "Обратитесь к администратору организации для получения необходимых прав.",
	},
	ErrFileTooLarge: {
		HTTPStatus: http.StatusRequestEntityTooLarge,
		Message:    "Файл превышает максимальный размер 20 МБ.",
		Suggestion: "Попробуйте загрузить файл меньшего размера или сжать PDF.",
	},
	ErrUnsupportedFormat: {
		HTTPStatus: http.StatusUnsupportedMediaType,
		Message:    "Формат файла не поддерживается. В текущей версии поддерживается только PDF.",
		Suggestion: "Сконвертируйте документ в формат PDF и загрузите повторно.",
	},
	ErrInvalidFile: {
		HTTPStatus: http.StatusBadRequest,
		Message:    "Загруженный файл повреждён или не может быть прочитан.",
		Suggestion: "Проверьте файл и попробуйте загрузить его повторно.",
	},
	ErrDocumentNotFound: {
		HTTPStatus: http.StatusNotFound,
		Message:    "Договор не найден.",
		Suggestion: "Проверьте идентификатор договора и попробуйте снова.",
	},
	ErrVersionNotFound: {
		HTTPStatus: http.StatusNotFound,
		Message:    "Версия договора не найдена.",
		Suggestion: "Проверьте идентификатор версии и попробуйте снова.",
	},
	ErrArtifactNotFound: {
		HTTPStatus: http.StatusNotFound,
		Message:    "Запрашиваемые данные ещё не готовы или отсутствуют.",
		Suggestion: "Дождитесь завершения обработки и повторите запрос.",
	},
	ErrDiffNotFound: {
		HTTPStatus: http.StatusNotFound,
		Message:    "Результат сравнения не найден.",
		Suggestion: "Запустите сравнение версий или дождитесь его завершения.",
	},
	ErrPolicyNotFound: {
		HTTPStatus: http.StatusNotFound,
		Message:    "Политика не найдена.",
		Suggestion: "Проверьте идентификатор политики и попробуйте снова.",
	},
	ErrChecklistNotFound: {
		HTTPStatus: http.StatusNotFound,
		Message:    "Чек-лист не найден.",
		Suggestion: "Проверьте идентификатор чек-листа и попробуйте снова.",
	},
	ErrDocumentArchived: {
		HTTPStatus: http.StatusConflict,
		Message:    "Договор находится в архиве. Операция невозможна.",
		Suggestion: "Восстановите договор из архива перед выполнением операции.",
	},
	ErrDocumentDeleted: {
		HTTPStatus: http.StatusConflict,
		Message:    "Договор был удалён. Операция невозможна.",
	},
	ErrVersionStillProcessing: {
		HTTPStatus: http.StatusConflict,
		Message:    "Текущая версия ещё обрабатывается. Дождитесь завершения.",
		Suggestion: "Дождитесь завершения текущей обработки перед запуском повторной проверки.",
	},
	ErrResultsNotReady: {
		HTTPStatus: http.StatusConflict,
		Message:    "Результаты анализа ещё не готовы.",
		Suggestion: "Дождитесь завершения обработки. Текущий статус можно отслеживать в реальном времени.",
	},
	ErrRateLimitExceeded: {
		HTTPStatus: http.StatusTooManyRequests,
		Message:    "Превышен лимит запросов. Попробуйте позже.",
		Suggestion: "Повторите запрос через несколько секунд.",
	},
	ErrStorageUnavailable: {
		HTTPStatus: http.StatusBadGateway,
		Message:    "Сервис временно недоступен. Не удалось загрузить файл.",
		Suggestion: "Попробуйте повторить загрузку через несколько минут.",
	},
	ErrDMUnavailable: {
		HTTPStatus: http.StatusBadGateway,
		Message:    "Сервис временно недоступен. Попробуйте позже.",
		Suggestion: "Повторите запрос через несколько минут. Если проблема сохраняется, обратитесь в поддержку.",
	},
	ErrOPMUnavailable: {
		HTTPStatus: http.StatusBadGateway,
		Message:    "Сервис управления политиками временно недоступен.",
		Suggestion: "Попробуйте повторить операцию через несколько минут.",
	},
	ErrBrokerUnavailable: {
		HTTPStatus: http.StatusBadGateway,
		Message:    "Сервис временно недоступен. Команда не была отправлена.",
		Suggestion: "Повторите операцию через несколько минут.",
	},
	ErrAuthServiceUnavailable: {
		HTTPStatus: http.StatusBadGateway,
		Message:    "Сервис авторизации временно недоступен.",
		Suggestion: "Попробуйте повторить операцию через несколько минут.",
	},
	ErrInvalidCredentials: {
		HTTPStatus: http.StatusUnauthorized,
		Message:    "Неверный email или пароль.",
		Suggestion: "Проверьте введённые данные и попробуйте снова.",
	},
	ErrTokenRevoked: {
		HTTPStatus: http.StatusUnauthorized,
		Message:    "Токен обновления был отозван.",
		Suggestion: "Войдите в систему повторно.",
	},
	ErrRefreshTokenExpired: {
		HTTPStatus: http.StatusUnauthorized,
		Message:    "Срок действия токена обновления истёк.",
		Suggestion: "Войдите в систему повторно.",
	},
	ErrValidationError: {
		HTTPStatus: http.StatusBadRequest,
		Message:    "Данные запроса содержат ошибки.",
		Suggestion: "Исправьте указанные ошибки и повторите запрос.",
	},
	ErrInternalError: {
		HTTPStatus: http.StatusInternalServerError,
		Message:    "Произошла внутренняя ошибка сервиса.",
		Suggestion: "Попробуйте повторить запрос. Если проблема сохраняется, обратитесь в поддержку с указанием идентификатора ошибки.",
	},
}

// LookupError returns the ErrorEntry for the given code.
// If the code is unknown, it returns the entry for ErrInternalError
// and ok=false.
func LookupError(code ErrorCode) (ErrorEntry, bool) {
	entry, ok := errorCatalog[code]
	if !ok {
		return errorCatalog[ErrInternalError], false
	}
	return entry, true
}

// StatusCode returns the HTTP status code for the given error code.
// Returns 500 for unknown codes.
func StatusCode(code ErrorCode) int {
	entry, _ := LookupError(code)
	return entry.HTTPStatus
}

// ErrorResponse is the unified JSON error body returned by all API endpoints.
type ErrorResponse struct {
	ErrorCode     string `json:"error_code"`
	Message       string `json:"message"`
	Details       any    `json:"details,omitempty"`
	CorrelationID string `json:"correlation_id"`
	Suggestion    string `json:"suggestion,omitempty"`
}

// WriteError writes a standardized JSON error response for the given error code.
// It auto-fills message and suggestion from the error catalog, extracts
// correlation_id from the request's logger.RequestContext, and sets the
// X-Correlation-Id response header.
//
// The details parameter is included in the response body as-is. Pass nil
// for error codes that do not carry additional context.
func WriteError(w http.ResponseWriter, r *http.Request, code ErrorCode, details any) {
	entry, _ := LookupError(code)
	rc := logger.RequestContextFrom(r.Context())

	resp := ErrorResponse{
		ErrorCode:     string(code),
		Message:       entry.Message,
		Details:       details,
		CorrelationID: rc.CorrelationID,
		Suggestion:    entry.Suggestion,
	}

	writeErrorJSON(w, entry.HTTPStatus, rc.CorrelationID, resp)
}

// WriteErrorWithMessage writes a standardized JSON error response with a
// custom message, overriding the catalog default. The HTTP status and
// suggestion are still taken from the catalog.
func WriteErrorWithMessage(w http.ResponseWriter, r *http.Request, code ErrorCode, message string, details any) {
	entry, _ := LookupError(code)
	rc := logger.RequestContextFrom(r.Context())

	resp := ErrorResponse{
		ErrorCode:     string(code),
		Message:       message,
		Details:       details,
		CorrelationID: rc.CorrelationID,
		Suggestion:    entry.Suggestion,
	}

	writeErrorJSON(w, entry.HTTPStatus, rc.CorrelationID, resp)
}

// writeErrorJSON sets headers, writes the HTTP status, and encodes the response
// as JSON. Encoding failures are logged at ERROR level via slog as a last-resort
// safety net — the HTTP status and headers are already sent at that point.
func writeErrorJSON(w http.ResponseWriter, status int, correlationID string, resp ErrorResponse) {
	w.Header().Set("X-Correlation-Id", correlationID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode error response",
			"error", err,
			"error_code", resp.ErrorCode,
			"correlation_id", correlationID,
		)
	}
}
