package gemini

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// schema.go implements the JSON Schema draft-07 → Gemini Schema transform
// (code-architect MUST-FIX #1).
//
// WHY THIS EXISTS. The Claude (`tool_use.input_schema`) and OpenAI
// (`text.format.schema`) siblings can pass an agent's JSON Schema draft-07
// document through as an opaque json.RawMessage — both providers accept
// draft-07. Gemini's `generationConfig.responseSchema` does NOT: for the v1
// default model (gemini-2.5-pro) it accepts only the OpenAPI-3.0 *Schema*
// subset:
//
//   - `type` is an UPPERCASE enum: OBJECT | ARRAY | STRING | INTEGER |
//     NUMBER | BOOLEAN (no "null" type; nullability is the `nullable` bool).
//   - `type:["string","null"]` draft-07 unions are NOT accepted — they become
//     `type:"STRING", nullable:true`.
//   - `$schema`, `$id`, `$defs`/`definitions`, `$ref`, `additionalProperties`,
//     `patternProperties`, `unevaluatedProperties`, `$comment`, `default`,
//     `examples`, `title` are NOT accepted and must be stripped (`$ref` is
//     inlined first).
//   - `const` is not supported; it is rewritten as a single-value `enum`.
//   - `oneOf` is rewritten as `anyOf` (Gemini supports `anyOf`); a single-
//     element `allOf` is inlined; a multi-element `allOf` is rejected.
//
// Passing draft-07 through unmodified would 400 (`INVALID_ARGUMENT`) every
// structured agent call routed to Gemini — i.e. silently break the Gemini
// fallback in production. The agent schemas (agents/schemas/*.json,
// LIC-TASK-020) are draft-07 (port.CompletionRequest.JSONSchema godoc), so the
// transform runs on every JSONSchema Complete call.
//
// SCOPE. The transform covers the constructs LIC v1 agent schemas use
// (objects, arrays, scalars, enum, required, nested properties/items,
// $defs+$ref, X|null unions, descriptions). Genuinely un-representable inputs
// (recursive $ref cycle, multi-type unions beyond X|null, tuple `items`,
// multi-element `allOf`, non-object root) return an error → the caller maps it
// to LLMErrorMalformedRequest before any wire I/O (payload.go), exactly the
// fail-fast posture arch §1.5 expects for an unsupported schema.

// maxSchemaDepth bounds recursion. It also breaks any $ref chain that escaped
// the explicit cycle guard. LIC agent schemas are shallow (objects of scalars
// / small arrays); 64 is far above any realistic legal-analysis schema while
// still bounding a hostile / accidentally-recursive document.
const maxSchemaDepth = 64

// geminiTypeMap maps lowercase JSON Schema draft-07 `type` tokens to Gemini's
// UPPERCASE enum. "null" is intentionally absent — it is never a standalone
// Gemini type; it only appears inside a draft-07 union and is folded into the
// `nullable` boolean (see normalizeType).
var geminiTypeMap = map[string]string{
	"object":  "OBJECT",
	"array":   "ARRAY",
	"string":  "STRING",
	"integer": "INTEGER",
	"number":  "NUMBER",
	"boolean": "BOOLEAN",
}

// geminiAllowedKeywords is the keep-list of schema keywords Gemini's
// responseSchema accepts (after type normalization and $ref inlining).
// Anything not here is dropped during transformation.
var geminiAllowedKeywords = map[string]struct{}{
	"type": {}, "format": {}, "description": {}, "nullable": {},
	"enum": {}, "properties": {}, "required": {}, "items": {},
	"minItems": {}, "maxItems": {}, "minimum": {}, "maximum": {},
	"minLength": {}, "maxLength": {}, "pattern": {}, "propertyOrdering": {},
	"anyOf": {},
}

// transformSchema converts a JSON Schema draft-07 document into the Gemini
// Schema subset, returning a compact json.RawMessage suitable for
// generationConfig.responseSchema. The returned error is a plain error; the
// caller wraps it as LLMErrorMalformedRequest.
func transformSchema(raw json.RawMessage) (json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, errors.New("empty schema")
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // preserve integer literals (minItems, maximum, ...)
	var root any
	if err := dec.Decode(&root); err != nil {
		return nil, fmt.Errorf("schema is not valid JSON: %w", err)
	}

	rootObj, ok := root.(map[string]any)
	if !ok {
		return nil, errors.New("schema root must be a JSON object")
	}

	// $defs / definitions feed $ref resolution but are themselves stripped.
	defs := map[string]map[string]any{}
	collectDefs(rootObj, "$defs", defs)
	collectDefs(rootObj, "definitions", defs)

	out, err := transformNode(rootObj, defs, map[string]bool{}, 0)
	if err != nil {
		return nil, err
	}

	encoded, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("re-encode transformed schema: %w", err)
	}
	return encoded, nil
}

// collectDefs records the named subschemas under key ("$defs"|"definitions")
// for later $ref resolution. Non-object entries are ignored.
func collectDefs(root map[string]any, key string, into map[string]map[string]any) {
	block, ok := root[key].(map[string]any)
	if !ok {
		return
	}
	for name, v := range block {
		if obj, ok := v.(map[string]any); ok {
			into[name] = obj
		}
	}
}

// transformNode recursively rewrites one schema node. visited tracks the
// active $ref resolution chain for cycle detection; depth bounds recursion.
func transformNode(node map[string]any, defs map[string]map[string]any, visited map[string]bool, depth int) (map[string]any, error) {
	if depth > maxSchemaDepth {
		return nil, fmt.Errorf("schema exceeds max nesting depth %d (possible $ref cycle)", maxSchemaDepth)
	}

	// $ref is resolved first and replaces the node entirely (draft-07 pre-2019
	// $ref semantics: sibling keywords are ignored).
	if ref, ok := node["$ref"].(string); ok {
		name, err := refName(ref)
		if err != nil {
			return nil, err
		}
		if visited[name] {
			return nil, fmt.Errorf("recursive $ref %q is not representable in Gemini schema", ref)
		}
		target, ok := defs[name]
		if !ok {
			return nil, fmt.Errorf("$ref %q does not resolve to a known $defs/definitions entry", ref)
		}
		visited[name] = true
		resolved, err := transformNode(target, defs, visited, depth+1)
		visited[name] = false
		return resolved, err
	}

	out := make(map[string]any, len(node))

	// Type normalization (lowercase→UPPERCASE, X|null union → nullable).
	if rawType, present := node["type"]; present {
		typ, nullable, err := normalizeType(rawType)
		if err != nil {
			return nil, err
		}
		out["type"] = typ
		if nullable {
			out["nullable"] = true
		}
	}

	// const → single-value enum (Gemini has no const). Gemini requires a
	// `type` on every schema node, so when the draft-07 author wrote a bare
	// `const` with no `type` (legal in draft-07) we infer the type from the
	// const value's JSON kind — otherwise Gemini 400s a typeless enum node
	// (code-reviewer M1).
	if c, ok := node["const"]; ok {
		out["enum"] = []any{c}
		if _, hasType := out["type"]; !hasType {
			if inferred, ok := inferGeminiType(c); ok {
				out["type"] = inferred
			}
		}
	}

	for key, val := range node {
		switch key {
		case "$ref", "type", "const":
			// handled above / intentionally dropped
			continue
		case "$schema", "$id", "$comment", "$defs", "definitions",
			"additionalProperties", "patternProperties", "unevaluatedProperties",
			"default", "examples", "title", "$anchor", "if", "then", "else",
			"not", "dependentSchemas", "dependencies":
			// Not representable / not accepted by Gemini responseSchema — drop.
			continue
		case "properties":
			props, ok := val.(map[string]any)
			if !ok {
				return nil, errors.New("`properties` must be an object")
			}
			tp := make(map[string]any, len(props))
			for pname, pval := range props {
				child, err := asSchemaObject(pval, "properties."+pname)
				if err != nil {
					return nil, err
				}
				tc, err := transformNode(child, defs, visited, depth+1)
				if err != nil {
					return nil, err
				}
				tp[pname] = tc
			}
			out["properties"] = tp
		case "items":
			child, err := asSchemaObject(val, "items")
			if err != nil {
				// Tuple-form items ([]schema) is not representable in Gemini.
				return nil, fmt.Errorf("`items` must be a single schema object (tuple items unsupported): %w", err)
			}
			tc, err := transformNode(child, defs, visited, depth+1)
			if err != nil {
				return nil, err
			}
			out["items"] = tc
		case "anyOf", "oneOf":
			arr, err := transformSchemaList(val, defs, visited, depth, key)
			if err != nil {
				return nil, err
			}
			// oneOf has no Gemini equivalent; anyOf is the closest accepted
			// keyword and preserves the "one of these shapes" guidance.
			out["anyOf"] = arr
		case "allOf":
			list, ok := val.([]any)
			if !ok || len(list) != 1 {
				return nil, errors.New("`allOf` is only supported with exactly one subschema (merge is not representable)")
			}
			child, err := asSchemaObject(list[0], "allOf[0]")
			if err != nil {
				return nil, err
			}
			tc, err := transformNode(child, defs, visited, depth+1)
			if err != nil {
				return nil, err
			}
			// Inline the single allOf member onto this node WITHOUT clobbering
			// sibling keywords the outer node already contributed (type,
			// nullable, enum, description). Outer-node keywords win;
			// the allOf child only fills gaps — e.g. supplies `type` when the
			// outer node has none (code-reviewer M2).
			for k, v := range tc {
				if _, exists := out[k]; !exists {
					out[k] = v
				}
			}
		default:
			if _, allowed := geminiAllowedKeywords[key]; allowed {
				out[key] = val
			}
			// else: unknown keyword, silently dropped (forward-compatible).
		}
	}

	return out, nil
}

// transformSchemaList transforms each element of an anyOf/oneOf array.
func transformSchemaList(val any, defs map[string]map[string]any, visited map[string]bool, depth int, key string) ([]any, error) {
	list, ok := val.([]any)
	if !ok || len(list) == 0 {
		return nil, fmt.Errorf("`%s` must be a non-empty array of schemas", key)
	}
	out := make([]any, 0, len(list))
	for i, item := range list {
		child, err := asSchemaObject(item, fmt.Sprintf("%s[%d]", key, i))
		if err != nil {
			return nil, err
		}
		tc, err := transformNode(child, defs, visited, depth+1)
		if err != nil {
			return nil, err
		}
		out = append(out, tc)
	}
	return out, nil
}

// inferGeminiType derives a Gemini UPPERCASE type token from a concrete
// const/enum value's JSON kind, used only when a draft-07 author wrote a
// `const` with no accompanying `type` (code-reviewer M1). Numbers are decoded
// as json.Number (transformSchema uses dec.UseNumber); NUMBER is a valid
// Gemini type for any numeric literal (an integer is a valid NUMBER). A null
// const has no representable Gemini type → (",", false), caller leaves the
// node typeless rather than emitting a wrong type.
func inferGeminiType(v any) (string, bool) {
	switch v.(type) {
	case string:
		return "STRING", true
	case bool:
		return "BOOLEAN", true
	case json.Number, float64:
		return "NUMBER", true
	case map[string]any:
		return "OBJECT", true
	case []any:
		return "ARRAY", true
	default:
		return "", false
	}
}

// normalizeType converts a draft-07 `type` (string or array) into Gemini's
// UPPERCASE token plus a nullable flag. An array union is accepted only as
// exactly one concrete type optionally combined with "null"; anything richer
// is not representable.
func normalizeType(raw any) (typ string, nullable bool, err error) {
	switch v := raw.(type) {
	case string:
		mapped, ok := geminiTypeMap[strings.ToLower(v)]
		if !ok {
			return "", false, fmt.Errorf("unsupported schema type %q", v)
		}
		return mapped, false, nil
	case []any:
		var concrete []string
		for _, e := range v {
			s, ok := e.(string)
			if !ok {
				return "", false, errors.New("`type` array entries must be strings")
			}
			if strings.ToLower(s) == "null" {
				nullable = true
				continue
			}
			concrete = append(concrete, s)
		}
		if len(concrete) != 1 {
			return "", false, fmt.Errorf("`type` union with %d non-null members is not representable in Gemini schema (only X | null is)", len(concrete))
		}
		mapped, ok := geminiTypeMap[strings.ToLower(concrete[0])]
		if !ok {
			return "", false, fmt.Errorf("unsupported schema type %q", concrete[0])
		}
		return mapped, nullable, nil
	default:
		return "", false, errors.New("`type` must be a string or an array of strings")
	}
}

// asSchemaObject coerces a JSON value that must be a schema object into a
// map. A bare `true`/`false` (draft-07 boolean schema) or any non-object is
// rejected with a context-qualified error.
func asSchemaObject(v any, where string) (map[string]any, error) {
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be a schema object", where)
	}
	return obj, nil
}

// refName extracts the definition name from a local JSON Pointer $ref of the
// form "#/$defs/Name" or "#/definitions/Name". Remote / non-local refs are not
// representable.
func refName(ref string) (string, error) {
	const (
		defsPrefix    = "#/$defs/"
		legacyPrefix  = "#/definitions/"
	)
	switch {
	case strings.HasPrefix(ref, defsPrefix):
		return ref[len(defsPrefix):], nil
	case strings.HasPrefix(ref, legacyPrefix):
		return ref[len(legacyPrefix):], nil
	default:
		return "", fmt.Errorf("$ref %q must be a local #/$defs/ or #/definitions/ pointer", ref)
	}
}
