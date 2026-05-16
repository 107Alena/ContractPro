package schemavalidator_test

import (
	"strings"
	"testing"

	"contractpro/legal-intelligence-core/internal/agents/schemavalidator"
)

// classifierSchema mirrors agents/schemas/type_classifier.json: a draft-07
// object schema with required fields, additionalProperties:false, an enum and
// a bounded number — exercises every §5.1 violation class.
const classifierSchema = `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "title": "ClassificationResult",
  "type": "object",
  "additionalProperties": false,
  "required": ["contract_type", "confidence"],
  "properties": {
    "contract_type": {"type": "string", "enum": ["SERVICES", "SUPPLY", "OTHER"]},
    "confidence": {"type": "number", "minimum": 0.0, "maximum": 1.0}
  }
}`

func TestValidate_Valid(t *testing.T) {
	v := schemavalidator.NewValidator()
	content := `{"contract_type":"SERVICES","confidence":0.91}`
	if err := v.Validate([]byte(classifierSchema), []byte(content)); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestValidate_NotJSON(t *testing.T) {
	v := schemavalidator.NewValidator()
	// error-handling.md §5.1 first bullet: a non-JSON response is a repair
	// trigger → *SchemaViolation, NOT *SchemaCompileError.
	err := v.Validate([]byte(classifierSchema), []byte("not json at all {"))
	viol, ok := schemavalidator.AsSchemaViolation(err)
	if !ok {
		t.Fatalf("Validate() = %T %v, want *SchemaViolation", err, err)
	}
	if _, isCompile := schemavalidator.AsSchemaCompileError(err); isCompile {
		t.Fatal("not-JSON content must NOT be a *SchemaCompileError")
	}
	if !strings.Contains(viol.Pretty(), "not valid JSON") {
		t.Fatalf("Pretty()=%q, want it to mention 'not valid JSON'", viol.Pretty())
	}
}

func TestValidate_SchemaViolations(t *testing.T) {
	v := schemavalidator.NewValidator()
	cases := map[string]string{
		"missing required":     `{"contract_type":"SERVICES"}`,
		"additionalProperties": `{"contract_type":"SERVICES","confidence":0.5,"extra":1}`,
		"type mismatch":        `{"contract_type":"SERVICES","confidence":"high"}`,
		"enum mismatch":        `{"contract_type":"EXOTIC","confidence":0.5}`,
		"number out of bounds": `{"contract_type":"SERVICES","confidence":1.7}`,
		"wrong root type":      `["not","an","object"]`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			err := v.Validate([]byte(classifierSchema), []byte(content))
			viol, ok := schemavalidator.AsSchemaViolation(err)
			if !ok {
				t.Fatalf("Validate() = %T %v, want *SchemaViolation", err, err)
			}
			if len(viol.Errors) == 0 || strings.TrimSpace(viol.Pretty()) == "" {
				t.Fatalf("SchemaViolation must carry at least one non-empty error; got %#v", viol.Errors)
			}
		})
	}
}

func TestValidate_BrokenSchema_IsCompileError(t *testing.T) {
	v := schemavalidator.NewValidator()
	// "type":"banana" is not a draft-07 primitive → schema fails to compile.
	broken := `{"$schema":"http://json-schema.org/draft-07/schema#","type":"banana"}`
	err := v.Validate([]byte(broken), []byte(`{}`))
	ce, ok := schemavalidator.AsSchemaCompileError(err)
	if !ok {
		t.Fatalf("Validate() = %T %v, want *SchemaCompileError", err, err)
	}
	if _, isViol := schemavalidator.AsSchemaViolation(err); isViol {
		t.Fatal("a broken schema must NOT be a *SchemaViolation (it is never repaired)")
	}
	if ce.Unwrap() == nil {
		t.Fatal("SchemaCompileError.Unwrap() = nil, want the underlying compile error")
	}
}

func TestValidate_NotJSONSchema_IsCompileError(t *testing.T) {
	v := schemavalidator.NewValidator()
	err := v.Validate([]byte("this is not even json"), []byte(`{}`))
	if _, ok := schemavalidator.AsSchemaCompileError(err); !ok {
		t.Fatalf("Validate() with non-JSON schema = %T %v, want *SchemaCompileError", err, err)
	}
}

// TestValidate_Deterministic guards the sorted/de-duped violation list so the
// repair prompt is byte-stable across runs (gojsonschema reports errors in
// map-iteration order).
func TestValidate_Deterministic(t *testing.T) {
	v := schemavalidator.NewValidator()
	content := `{"contract_type":"EXOTIC","confidence":"high","extra":true}`
	first := v.Validate([]byte(classifierSchema), []byte(content))
	fv, _ := schemavalidator.AsSchemaViolation(first)
	for i := 0; i < 20; i++ {
		got := v.Validate([]byte(classifierSchema), []byte(content))
		gv, ok := schemavalidator.AsSchemaViolation(got)
		if !ok {
			t.Fatalf("iteration %d: not a SchemaViolation", i)
		}
		if gv.Pretty() != fv.Pretty() {
			t.Fatalf("non-deterministic violation rendering:\nfirst=%q\ngot  =%q", fv.Pretty(), gv.Pretty())
		}
	}
}

// TestValidate_Concurrent — the Validator is stateless; the parallel errgroup
// agent pipeline shares one instance.
func TestValidate_Concurrent(t *testing.T) {
	v := schemavalidator.NewValidator()
	const g = 16
	done := make(chan struct{})
	for i := 0; i < g; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 64; j++ {
				_ = v.Validate([]byte(classifierSchema), []byte(`{"contract_type":"SUPPLY","confidence":0.4}`))
				_ = v.Validate([]byte(classifierSchema), []byte(`{"bad":true}`))
			}
		}()
	}
	for i := 0; i < g; i++ {
		<-done
	}
}
