package validation

import (
	"encoding/json"
	"fmt"
	"testing"
)

// ---------------------------------------------------------------------------
// ValidationCode
// ---------------------------------------------------------------------------

func TestValidationCode_IsValid_AllKnownCodes(t *testing.T) {
	codes := []ValidationCode{
		CodeRequired, CodeInvalidFormat, CodeInvalidUUID, CodeInvalidDate,
		CodeInvalidEnum, CodeNotInWhitelist, CodeTooShort, CodeTooLong,
		CodeOutOfRange, CodeMismatch, CodeDuplicate, CodeInvalidReference,
	}
	for _, c := range codes {
		if !c.IsValid() {
			t.Errorf("expected %q to be valid", c)
		}
	}
}

func TestValidationCode_IsValid_UnknownCode(t *testing.T) {
	unknown := []ValidationCode{"UNKNOWN", "", "required", "Too_Long"}
	for _, c := range unknown {
		if c.IsValid() {
			t.Errorf("expected %q to be invalid", c)
		}
	}
}

func TestValidationCode_Count(t *testing.T) {
	if len(validCodes) != 12 {
		t.Fatalf("expected 12 validation codes, got %d", len(validCodes))
	}
}

// ---------------------------------------------------------------------------
// Message templates coverage
// ---------------------------------------------------------------------------

func TestMessageTemplates_AllCodesHaveTemplates(t *testing.T) {
	for code := range validCodes {
		if _, ok := messageTemplates[code]; !ok {
			t.Errorf("missing message template for code %q", code)
		}
	}
}

func TestRenderMessage_NoParams(t *testing.T) {
	msg := renderMessage(CodeRequired, nil)
	if msg != "Поле обязательно для заполнения." {
		t.Errorf("unexpected message: %s", msg)
	}
}

func TestRenderMessage_WithParams(t *testing.T) {
	params := map[string]any{"max": 100}
	msg := renderMessage(CodeTooLong, params)
	expected := "Максимум 100 символов."
	if msg != expected {
		t.Errorf("expected %q, got %q", expected, msg)
	}
}

func TestRenderMessage_MultipleParams(t *testing.T) {
	params := map[string]any{"min": 1, "max": 100}
	msg := renderMessage(CodeOutOfRange, params)
	expected := "Значение должно быть от 1 до 100."
	if msg != expected {
		t.Errorf("expected %q, got %q", expected, msg)
	}
}

func TestRenderMessage_UnknownCode(t *testing.T) {
	msg := renderMessage("UNKNOWN_CODE", nil)
	if msg != "UNKNOWN_CODE" {
		t.Errorf("expected fallback to code string, got %q", msg)
	}
}

// ---------------------------------------------------------------------------
// Helper builders
// ---------------------------------------------------------------------------

func TestNewRequired(t *testing.T) {
	fe := NewRequired("title")
	assertFieldError(t, fe, "title", CodeRequired, "Поле обязательно для заполнения.", nil)
}

func TestNewInvalidFormat(t *testing.T) {
	fe := NewInvalidFormat("email", "user@example.com")
	assertFieldError(t, fe, "email", CodeInvalidFormat, "Неверный формат. Ожидается: user@example.com.", map[string]any{"expected": "user@example.com"})
}

func TestNewInvalidUUID(t *testing.T) {
	fe := NewInvalidUUID("contract_id")
	assertFieldError(t, fe, "contract_id", CodeInvalidUUID, "Значение должно быть валидным UUID.", nil)
}

func TestNewInvalidDate(t *testing.T) {
	fe := NewInvalidDate("start_date", "YYYY-MM-DD")
	assertFieldError(t, fe, "start_date", CodeInvalidDate, "Неверный формат даты. Ожидается: YYYY-MM-DD.", map[string]any{"expected": "YYYY-MM-DD"})
}

func TestNewInvalidEnum(t *testing.T) {
	fe := NewInvalidEnum("status", []string{"ACTIVE", "ARCHIVED"})
	assertFieldError(t, fe, "status", CodeInvalidEnum, "Значение должно быть одним из: ACTIVE, ARCHIVED.", map[string]any{"allowed": "ACTIVE, ARCHIVED"})
}

func TestNewNotInWhitelist(t *testing.T) {
	fe := NewNotInWhitelist("tag", 5)
	assertFieldError(t, fe, "tag", CodeNotInWhitelist, "Значение не входит в список разрешённых (5 допустимых).", map[string]any{"count": 5})
}

func TestNewTooShort(t *testing.T) {
	fe := NewTooShort("password", 8)
	assertFieldError(t, fe, "password", CodeTooShort, "Минимум 8 символов.", map[string]any{"min": 8})
}

func TestNewTooLong(t *testing.T) {
	fe := NewTooLong("title", 255)
	assertFieldError(t, fe, "title", CodeTooLong, "Максимум 255 символов.", map[string]any{"max": 255})
}

func TestNewOutOfRange(t *testing.T) {
	fe := NewOutOfRange("page", 1, 100)
	assertFieldError(t, fe, "page", CodeOutOfRange, "Значение должно быть от 1 до 100.", map[string]any{"min": 1, "max": 100})
}

func TestNewMismatch(t *testing.T) {
	fe := NewMismatch("password_confirm", "password")
	assertFieldError(t, fe, "password_confirm", CodeMismatch, "Значение не совпадает с полем «password».", map[string]any{"other": "password"})
}

func TestNewDuplicate(t *testing.T) {
	fe := NewDuplicate("email")
	assertFieldError(t, fe, "email", CodeDuplicate, "Значение уже существует.", nil)
}

func TestNewInvalidReference(t *testing.T) {
	fe := NewInvalidReference("organization_id")
	assertFieldError(t, fe, "organization_id", CodeInvalidReference, "Указанный объект не найден.", nil)
}

func assertFieldError(t *testing.T, fe ValidationFieldError, field string, code ValidationCode, message string, params map[string]any) {
	t.Helper()
	if fe.Field != field {
		t.Errorf("Field: expected %q, got %q", field, fe.Field)
	}
	if fe.Code != code {
		t.Errorf("Code: expected %q, got %q", code, fe.Code)
	}
	if fe.Message != message {
		t.Errorf("Message: expected %q, got %q", message, fe.Message)
	}
	if params == nil {
		if fe.Params != nil {
			t.Errorf("Params: expected nil, got %v", fe.Params)
		}
		return
	}
	if len(fe.Params) != len(params) {
		t.Errorf("Params length: expected %d, got %d", len(params), len(fe.Params))
		return
	}
	for k, v := range params {
		got, ok := fe.Params[k]
		if !ok {
			t.Errorf("Params: missing key %q", k)
			continue
		}
		if fmt.Sprint(v) != fmt.Sprint(got) {
			t.Errorf("Params[%q]: expected %v, got %v", k, v, got)
		}
	}
}

// ---------------------------------------------------------------------------
// Builder
// ---------------------------------------------------------------------------

func TestBuilder_Empty_ReturnsNil(t *testing.T) {
	b := NewBuilder()
	if b.HasErrors() {
		t.Error("empty builder should not have errors")
	}
	if b.Build() != nil {
		t.Error("Build() on empty builder should return nil")
	}
}

func TestBuilder_SingleField(t *testing.T) {
	ve := NewBuilder().Add(NewRequired("title")).Build()
	if ve == nil {
		t.Fatal("expected non-nil ValidationError")
	}
	if ve.ErrorCode != "VALIDATION_ERROR" {
		t.Errorf("ErrorCode: expected VALIDATION_ERROR, got %s", ve.ErrorCode)
	}
	if ve.Message != "Данные запроса содержат ошибки." {
		t.Errorf("Message: %s", ve.Message)
	}
	if ve.Suggestion != "Исправьте указанные ошибки и повторите запрос." {
		t.Errorf("Suggestion: %s", ve.Suggestion)
	}
	if len(ve.Details.Fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(ve.Details.Fields))
	}
	if ve.Details.Fields[0].Code != CodeRequired {
		t.Errorf("expected REQUIRED, got %s", ve.Details.Fields[0].Code)
	}
}

func TestBuilder_Chaining(t *testing.T) {
	ve := NewBuilder().
		Add(NewRequired("title")).
		Add(NewInvalidUUID("contract_id")).
		Add(NewTooLong("description", 1000)).
		Build()
	if ve == nil {
		t.Fatal("expected non-nil ValidationError")
	}
	if len(ve.Details.Fields) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(ve.Details.Fields))
	}
	codes := []ValidationCode{CodeRequired, CodeInvalidUUID, CodeTooLong}
	for i, want := range codes {
		if ve.Details.Fields[i].Code != want {
			t.Errorf("field[%d]: expected %s, got %s", i, want, ve.Details.Fields[i].Code)
		}
	}
}

func TestBuilder_HasErrors(t *testing.T) {
	b := NewBuilder()
	if b.HasErrors() {
		t.Error("empty builder should not have errors")
	}
	b.Add(NewRequired("x"))
	if !b.HasErrors() {
		t.Error("builder with one error should report HasErrors=true")
	}
}

func TestBuilder_Build_CopiesSlice(t *testing.T) {
	b := NewBuilder().Add(NewRequired("a"))
	ve := b.Build()
	b.Add(NewRequired("b"))
	if len(ve.Details.Fields) != 1 {
		t.Errorf("Build() result mutated: expected 1 field, got %d", len(ve.Details.Fields))
	}
}

// ---------------------------------------------------------------------------
// ValidationError as error interface
// ---------------------------------------------------------------------------

func TestValidationError_ErrorInterface(t *testing.T) {
	ve := NewBuilder().Add(NewRequired("title")).Build()
	var err error = ve
	if err.Error() != "Данные запроса содержат ошибки." {
		t.Errorf("Error(): %s", err.Error())
	}
}

// ---------------------------------------------------------------------------
// JSON serialization
// ---------------------------------------------------------------------------

func TestFieldError_JSON_WithParams(t *testing.T) {
	fe := NewTooLong("title", 255)
	data, err := json.Marshal(fe)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["field"] != "title" {
		t.Errorf("field: %v", got["field"])
	}
	if got["code"] != "TOO_LONG" {
		t.Errorf("code: %v", got["code"])
	}
	if got["message"] != "Максимум 255 символов." {
		t.Errorf("message: %v", got["message"])
	}
	params, ok := got["params"].(map[string]any)
	if !ok {
		t.Fatal("params should be a map")
	}
	if params["max"] != float64(255) {
		t.Errorf("params.max: %v", params["max"])
	}
}

func TestFieldError_JSON_WithoutParams_OmitsParamsField(t *testing.T) {
	fe := NewRequired("title")
	data, err := json.Marshal(fe)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, exists := got["params"]; exists {
		t.Error("params should be omitted when nil")
	}
}

func TestValidationErrorDetails_JSON_Structure(t *testing.T) {
	details := ValidationErrorDetails{
		Fields: []ValidationFieldError{
			NewRequired("title"),
			NewInvalidUUID("contract_id"),
		},
	}
	data, err := json.Marshal(details)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	fields, ok := got["fields"].([]any)
	if !ok {
		t.Fatal("fields should be an array")
	}
	if len(fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(fields))
	}
	first := fields[0].(map[string]any)
	if first["field"] != "title" || first["code"] != "REQUIRED" {
		t.Errorf("first field: %v", first)
	}
	second := fields[1].(map[string]any)
	if second["field"] != "contract_id" || second["code"] != "INVALID_UUID" {
		t.Errorf("second field: %v", second)
	}
}

func TestValidationError_JSON_FullPayload(t *testing.T) {
	ve := NewBuilder().
		Add(NewRequired("title")).
		Add(NewTooLong("description", 1000)).
		Build()
	data, err := json.Marshal(ve)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["error_code"] != "VALIDATION_ERROR" {
		t.Errorf("error_code: %v", got["error_code"])
	}
	if got["message"] != "Данные запроса содержат ошибки." {
		t.Errorf("message: %v", got["message"])
	}
	if got["suggestion"] != "Исправьте указанные ошибки и повторите запрос." {
		t.Errorf("suggestion: %v", got["suggestion"])
	}
	details, ok := got["details"].(map[string]any)
	if !ok {
		t.Fatal("details should be an object")
	}
	fields, ok := details["fields"].([]any)
	if !ok {
		t.Fatal("details.fields should be an array")
	}
	if len(fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(fields))
	}
}

func TestFieldError_JSON_RoundTrip(t *testing.T) {
	original := NewInvalidEnum("status", []string{"ACTIVE", "ARCHIVED", "DELETED"})
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded ValidationFieldError
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Field != original.Field {
		t.Errorf("Field: expected %q, got %q", original.Field, decoded.Field)
	}
	if decoded.Code != original.Code {
		t.Errorf("Code: expected %q, got %q", original.Code, decoded.Code)
	}
	if decoded.Message != original.Message {
		t.Errorf("Message: expected %q, got %q", original.Message, decoded.Message)
	}
	if decoded.Params["allowed"] != original.Params["allowed"] {
		t.Errorf("Params[allowed]: expected %v, got %v", original.Params["allowed"], decoded.Params["allowed"])
	}
}

// ---------------------------------------------------------------------------
// JSONPath-lite field paths
// ---------------------------------------------------------------------------

func TestFieldPaths_JSONPathLite(t *testing.T) {
	cases := []struct {
		field string
	}{
		{"title"},
		{"parties.0.name"},
		{"policy.severity"},
		{"file"},
	}
	for _, tc := range cases {
		fe := NewRequired(tc.field)
		if fe.Field != tc.field {
			t.Errorf("expected field %q, got %q", tc.field, fe.Field)
		}
	}
}
