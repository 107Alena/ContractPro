package gemini

import (
	"encoding/json"
	"strings"
	"testing"
)

// decodeSchema is a test helper: transform then JSON-decode into a generic map
// for structural assertions (key order is irrelevant for a schema).
func decodeSchema(t *testing.T, in string) map[string]any {
	t.Helper()
	out, err := transformSchema(json.RawMessage(in))
	if err != nil {
		t.Fatalf("transformSchema(%s) err=%v", in, err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("transformed schema not valid JSON: %v", err)
	}
	return m
}

func TestTransformSchema_UppercasesTypesAndStripsMeta(t *testing.T) {
	m := decodeSchema(t, `{
		"$schema":"http://json-schema.org/draft-07/schema#",
		"$id":"x",
		"title":"Result",
		"type":"object",
		"additionalProperties":false,
		"properties":{
			"name":{"type":"string","description":"the name"},
			"count":{"type":"integer","minimum":0},
			"tags":{"type":"array","items":{"type":"string"}}
		},
		"required":["name"]
	}`)
	if m["type"] != "OBJECT" {
		t.Errorf("type=%v, want OBJECT", m["type"])
	}
	for _, banned := range []string{"$schema", "$id", "title", "additionalProperties"} {
		if _, ok := m[banned]; ok {
			t.Errorf("banned keyword %q survived: %v", banned, m)
		}
	}
	props := m["properties"].(map[string]any)
	if props["name"].(map[string]any)["type"] != "STRING" {
		t.Errorf("name.type=%v, want STRING", props["name"])
	}
	if props["name"].(map[string]any)["description"] != "the name" {
		t.Errorf("description dropped: %v", props["name"])
	}
	if props["count"].(map[string]any)["type"] != "INTEGER" {
		t.Errorf("count.type=%v, want INTEGER", props["count"])
	}
	tags := props["tags"].(map[string]any)
	if tags["type"] != "ARRAY" || tags["items"].(map[string]any)["type"] != "STRING" {
		t.Errorf("tags transform wrong: %v", tags)
	}
	if req, ok := m["required"].([]any); !ok || len(req) != 1 || req[0] != "name" {
		t.Errorf("required=%v, want [name]", m["required"])
	}
}

func TestTransformSchema_NullableUnion(t *testing.T) {
	m := decodeSchema(t, `{"type":"object","properties":{"mid":{"type":["string","null"]}}}`)
	mid := m["properties"].(map[string]any)["mid"].(map[string]any)
	if mid["type"] != "STRING" {
		t.Errorf("type=%v, want STRING", mid["type"])
	}
	if mid["nullable"] != true {
		t.Errorf("nullable=%v, want true", mid["nullable"])
	}
}

func TestTransformSchema_RefResolution_DefsAndDefinitions(t *testing.T) {
	for _, defsKey := range []string{"$defs", "definitions"} {
		in := `{
			"type":"object",
			"properties":{"item":{"$ref":"#/` + defsKey + `/Item"}},
			"` + defsKey + `":{"Item":{"type":"object","properties":{"id":{"type":"integer"}}}}
		}`
		m := decodeSchema(t, in)
		item := m["properties"].(map[string]any)["item"].(map[string]any)
		if item["type"] != "OBJECT" {
			t.Errorf("[%s] $ref not inlined: %v", defsKey, item)
		}
		if item["properties"].(map[string]any)["id"].(map[string]any)["type"] != "INTEGER" {
			t.Errorf("[%s] nested ref body wrong: %v", defsKey, item)
		}
		if _, leaked := m[defsKey]; leaked {
			t.Errorf("[%s] defs block leaked into output", defsKey)
		}
	}
}

func TestTransformSchema_ConstToEnum(t *testing.T) {
	m := decodeSchema(t, `{"type":"object","properties":{"k":{"type":"string","const":"FIXED"}}}`)
	k := m["properties"].(map[string]any)["k"].(map[string]any)
	en, ok := k["enum"].([]any)
	if !ok || len(en) != 1 || en[0] != "FIXED" {
		t.Errorf("const→enum failed: %v", k)
	}
	if _, hasConst := k["const"]; hasConst {
		t.Errorf("const survived: %v", k)
	}
}

// M1: a draft-07 `const` with no `type` must still emit a Gemini-valid node
// (type inferred from the const value's kind), not a typeless enum.
func TestTransformSchema_TypelessConst_InfersType(t *testing.T) {
	cases := []struct {
		in       string
		wantType string
	}{
		{`{"const":"FIXED"}`, "STRING"},
		{`{"const":true}`, "BOOLEAN"},
		{`{"const":42}`, "NUMBER"},
	}
	for _, c := range cases {
		m := decodeSchema(t, c.in)
		if m["type"] != c.wantType {
			t.Errorf("transform(%s) type=%v, want %s", c.in, m["type"], c.wantType)
		}
		en, ok := m["enum"].([]any)
		if !ok || len(en) != 1 {
			t.Errorf("transform(%s) enum=%v, want single value", c.in, m["enum"])
		}
	}
	// Explicit type is NOT overridden by inference.
	m := decodeSchema(t, `{"type":"string","const":"X"}`)
	if m["type"] != "STRING" {
		t.Errorf("explicit type lost: %v", m)
	}
}

// M2: single-element allOf inline must not clobber sibling keywords already on
// the outer node; it only fills gaps (here: supplies `type`, keeps outer
// `description`/`nullable`).
func TestTransformSchema_SingleAllOf_DoesNotClobberSiblings(t *testing.T) {
	m := decodeSchema(t, `{
		"type":["string","null"],
		"description":"outer desc",
		"allOf":[{"type":"integer","description":"inner desc"}]
	}`)
	if m["description"] != "outer desc" {
		t.Errorf("description=%v, want outer (sibling wins)", m["description"])
	}
	if m["type"] != "STRING" {
		t.Errorf("type=%v, want STRING (outer union wins over allOf child)", m["type"])
	}
	if m["nullable"] != true {
		t.Errorf("nullable=%v, want true (outer union preserved)", m["nullable"])
	}
}

func TestTransformSchema_SingleAllOf_FillsMissingType(t *testing.T) {
	// Outer node has no type; the single allOf child supplies it.
	m := decodeSchema(t, `{"allOf":[{"type":"object","properties":{"a":{"type":"string"}}}]}`)
	if m["type"] != "OBJECT" {
		t.Errorf("type=%v, want OBJECT filled from allOf child", m["type"])
	}
}

func TestTransformSchema_OneOfBecomesAnyOf(t *testing.T) {
	m := decodeSchema(t, `{"oneOf":[{"type":"string"},{"type":"integer"}]}`)
	if _, hasOneOf := m["oneOf"]; hasOneOf {
		t.Errorf("oneOf survived: %v", m)
	}
	any0, ok := m["anyOf"].([]any)
	if !ok || len(any0) != 2 {
		t.Fatalf("anyOf=%v, want 2 members", m["anyOf"])
	}
	if any0[0].(map[string]any)["type"] != "STRING" || any0[1].(map[string]any)["type"] != "INTEGER" {
		t.Errorf("anyOf members not transformed: %v", any0)
	}
}

func TestTransformSchema_SingleAllOfInlined(t *testing.T) {
	m := decodeSchema(t, `{"allOf":[{"type":"object","properties":{"a":{"type":"boolean"}}}]}`)
	if m["type"] != "OBJECT" {
		t.Errorf("single allOf not inlined: %v", m)
	}
	if m["properties"].(map[string]any)["a"].(map[string]any)["type"] != "BOOLEAN" {
		t.Errorf("inlined allOf body wrong: %v", m)
	}
}

func TestTransformSchema_Errors(t *testing.T) {
	cases := map[string]string{
		"empty":              ``,
		"not json":           `{not-json`,
		"non-object root":    `["x"]`,
		"unknown type":       `{"type":"banana"}`,
		"multi-type union":   `{"type":["string","integer"]}`,
		"missing ref":        `{"type":"object","properties":{"x":{"$ref":"#/$defs/Nope"}}}`,
		"remote ref":         `{"$ref":"https://example.com/s.json"}`,
		"recursive ref":      `{"$ref":"#/$defs/Node","$defs":{"Node":{"type":"object","properties":{"next":{"$ref":"#/$defs/Node"}}}}}`,
		"multi allOf":        `{"allOf":[{"type":"string"},{"type":"integer"}]}`,
		"tuple items":        `{"type":"array","items":[{"type":"string"}]}`,
		"empty anyOf":        `{"anyOf":[]}`,
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := transformSchema(json.RawMessage(in)); err == nil {
				t.Errorf("transformSchema(%s) err=nil, want error", in)
			}
		})
	}
}

func TestTransformSchema_DeepNestingWithinLimit(t *testing.T) {
	// 10 levels of nested objects — well within maxSchemaDepth.
	in := `{"type":"object","properties":{"a":{"type":"object","properties":{"b":{"type":"object","properties":{"c":{"type":"string"}}}}}}}`
	m := decodeSchema(t, in)
	if m["type"] != "OBJECT" {
		t.Fatalf("deep nesting failed: %v", m)
	}
}

func TestTransformSchema_OutputIsCompactJSON(t *testing.T) {
	out, err := transformSchema(json.RawMessage(`{"type":"string"}`))
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if strings.ContainsAny(string(out), "\n\t") {
		t.Errorf("output not compact: %s", out)
	}
	if string(out) != `{"type":"STRING"}` {
		t.Errorf("output=%s, want {\"type\":\"STRING\"}", out)
	}
}
