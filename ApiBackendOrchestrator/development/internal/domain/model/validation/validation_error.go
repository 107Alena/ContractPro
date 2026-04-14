package validation

import (
	"fmt"
	"sort"
	"strings"
)

// ValidationCode is a machine-readable code identifying the type of validation failure.
// Corresponds to the enum in api-specification.yaml ValidationFieldError.code.
type ValidationCode string

const (
	CodeRequired         ValidationCode = "REQUIRED"
	CodeInvalidFormat    ValidationCode = "INVALID_FORMAT"
	CodeInvalidUUID      ValidationCode = "INVALID_UUID"
	CodeInvalidDate      ValidationCode = "INVALID_DATE"
	CodeInvalidEnum      ValidationCode = "INVALID_ENUM"
	CodeNotInWhitelist   ValidationCode = "NOT_IN_WHITELIST"
	CodeTooShort         ValidationCode = "TOO_SHORT"
	CodeTooLong          ValidationCode = "TOO_LONG"
	CodeOutOfRange       ValidationCode = "OUT_OF_RANGE"
	CodeMismatch         ValidationCode = "MISMATCH"
	CodeDuplicate        ValidationCode = "DUPLICATE"
	CodeInvalidReference ValidationCode = "INVALID_REFERENCE"
)

var validCodes = map[ValidationCode]struct{}{
	CodeRequired: {}, CodeInvalidFormat: {}, CodeInvalidUUID: {},
	CodeInvalidDate: {}, CodeInvalidEnum: {}, CodeNotInWhitelist: {},
	CodeTooShort: {}, CodeTooLong: {}, CodeOutOfRange: {},
	CodeMismatch: {}, CodeDuplicate: {}, CodeInvalidReference: {},
}

// IsValid reports whether c is one of the 12 known validation codes.
func (c ValidationCode) IsValid() bool {
	_, ok := validCodes[c]
	return ok
}

// Russian-language i18n message templates (NFR-5.2).
// Placeholders use {key} syntax and are interpolated from Params.
var messageTemplates = map[ValidationCode]string{
	CodeRequired:         "Поле обязательно для заполнения.",
	CodeInvalidFormat:    "Неверный формат. Ожидается: {expected}.",
	CodeInvalidUUID:      "Значение должно быть валидным UUID.",
	CodeInvalidDate:      "Неверный формат даты. Ожидается: {expected}.",
	CodeInvalidEnum:      "Значение должно быть одним из: {allowed}.",
	CodeNotInWhitelist:   "Значение не входит в список разрешённых ({count} допустимых).",
	CodeTooShort:         "Минимум {min} символов.",
	CodeTooLong:          "Максимум {max} символов.",
	CodeOutOfRange:       "Значение должно быть от {min} до {max}.",
	CodeMismatch:         "Значение не совпадает с полем «{other}».",
	CodeDuplicate:        "Значение уже существует.",
	CodeInvalidReference: "Указанный объект не найден.",
}

func renderMessage(code ValidationCode, params map[string]any) string {
	tmpl, ok := messageTemplates[code]
	if !ok {
		return string(code)
	}
	if len(params) == 0 {
		return tmpl
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	result := tmpl
	for _, key := range keys {
		result = strings.ReplaceAll(result, "{"+key+"}", fmt.Sprint(params[key]))
	}
	return result
}

// ValidationFieldError represents a single field-level validation failure.
// JSON structure matches api-specification.yaml ValidationFieldError schema.
type ValidationFieldError struct {
	Field   string         `json:"field"`
	Code    ValidationCode `json:"code"`
	Message string         `json:"message"`
	Params  map[string]any `json:"params,omitempty"`
}

// ValidationErrorDetails holds the structured details for a VALIDATION_ERROR response.
// Passed as the details field of ErrorResponse when error_code is VALIDATION_ERROR.
type ValidationErrorDetails struct {
	Fields []ValidationFieldError `json:"fields"`
}

// ValidationError is the domain error returned by Build() when validation fails.
// Implements the error interface for use in Go error chains.
type ValidationError struct {
	ErrorCode  string                 `json:"error_code"`
	Message    string                 `json:"message"`
	Suggestion string                 `json:"suggestion"`
	Details    ValidationErrorDetails `json:"details"`
}

func (e *ValidationError) Error() string {
	return e.Message
}

// --- Helper builders for all 12 validation codes ---

func NewRequired(field string) ValidationFieldError {
	return ValidationFieldError{
		Field:   field,
		Code:    CodeRequired,
		Message: renderMessage(CodeRequired, nil),
	}
}

func NewInvalidFormat(field, expected string) ValidationFieldError {
	params := map[string]any{"expected": expected}
	return ValidationFieldError{
		Field:   field,
		Code:    CodeInvalidFormat,
		Message: renderMessage(CodeInvalidFormat, params),
		Params:  params,
	}
}

func NewInvalidUUID(field string) ValidationFieldError {
	return ValidationFieldError{
		Field:   field,
		Code:    CodeInvalidUUID,
		Message: renderMessage(CodeInvalidUUID, nil),
	}
}

func NewInvalidDate(field, expected string) ValidationFieldError {
	params := map[string]any{"expected": expected}
	return ValidationFieldError{
		Field:   field,
		Code:    CodeInvalidDate,
		Message: renderMessage(CodeInvalidDate, params),
		Params:  params,
	}
}

func NewInvalidEnum(field string, allowed []string) ValidationFieldError {
	joined := strings.Join(allowed, ", ")
	if joined == "" {
		joined = "-"
	}
	params := map[string]any{"allowed": joined}
	return ValidationFieldError{
		Field:   field,
		Code:    CodeInvalidEnum,
		Message: renderMessage(CodeInvalidEnum, params),
		Params:  params,
	}
}

func NewNotInWhitelist(field string, count int) ValidationFieldError {
	params := map[string]any{"count": count}
	return ValidationFieldError{
		Field:   field,
		Code:    CodeNotInWhitelist,
		Message: renderMessage(CodeNotInWhitelist, params),
		Params:  params,
	}
}

func NewTooShort(field string, min int) ValidationFieldError {
	params := map[string]any{"min": min}
	return ValidationFieldError{
		Field:   field,
		Code:    CodeTooShort,
		Message: renderMessage(CodeTooShort, params),
		Params:  params,
	}
}

func NewTooLong(field string, max int) ValidationFieldError {
	params := map[string]any{"max": max}
	return ValidationFieldError{
		Field:   field,
		Code:    CodeTooLong,
		Message: renderMessage(CodeTooLong, params),
		Params:  params,
	}
}

func NewOutOfRange(field string, min, max int) ValidationFieldError {
	params := map[string]any{"min": min, "max": max}
	return ValidationFieldError{
		Field:   field,
		Code:    CodeOutOfRange,
		Message: renderMessage(CodeOutOfRange, params),
		Params:  params,
	}
}

func NewMismatch(field, other string) ValidationFieldError {
	params := map[string]any{"other": other}
	return ValidationFieldError{
		Field:   field,
		Code:    CodeMismatch,
		Message: renderMessage(CodeMismatch, params),
		Params:  params,
	}
}

func NewDuplicate(field string) ValidationFieldError {
	return ValidationFieldError{
		Field:   field,
		Code:    CodeDuplicate,
		Message: renderMessage(CodeDuplicate, nil),
	}
}

func NewInvalidReference(field string) ValidationFieldError {
	return ValidationFieldError{
		Field:   field,
		Code:    CodeInvalidReference,
		Message: renderMessage(CodeInvalidReference, nil),
	}
}

// --- Builder ---

// ValidationErrorBuilder accumulates field-level validation errors
// and produces a single ValidationError when Build() is called.
type ValidationErrorBuilder struct {
	fields []ValidationFieldError
}

// NewBuilder creates a new empty ValidationErrorBuilder.
func NewBuilder() *ValidationErrorBuilder {
	return &ValidationErrorBuilder{}
}

// Add appends a field error. Returns the builder for chaining.
func (b *ValidationErrorBuilder) Add(err ValidationFieldError) *ValidationErrorBuilder {
	b.fields = append(b.fields, err)
	return b
}

// HasErrors reports whether any validation errors have been accumulated.
func (b *ValidationErrorBuilder) HasErrors() bool {
	return len(b.fields) > 0
}

// Build creates a *ValidationError if any errors were accumulated, or nil if none.
// The returned error carries error_code=VALIDATION_ERROR with all accumulated field errors.
// The internal fields slice is copied so subsequent Add calls do not mutate the result.
func (b *ValidationErrorBuilder) Build() *ValidationError {
	if len(b.fields) == 0 {
		return nil
	}
	fields := make([]ValidationFieldError, len(b.fields))
	copy(fields, b.fields)
	return &ValidationError{
		ErrorCode:  "VALIDATION_ERROR",
		Message:    "Данные запроса содержат ошибки.",
		Suggestion: "Исправьте указанные ошибки и повторите запрос.",
		Details: ValidationErrorDetails{
			Fields: fields,
		},
	}
}
