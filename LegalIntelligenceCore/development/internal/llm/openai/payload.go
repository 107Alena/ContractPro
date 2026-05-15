package openai

import (
	"encoding/json"
	"fmt"

	"contractpro/legal-intelligence-core/internal/domain/port"
)

// responsesRequest is the wire-shape POSTed to /v1/responses.
// Field ordering follows the public OpenAI Responses API documentation.
//
// There is no `stop`/`stop_sequences` field — the Responses API does not
// support one (see package doc deviation note). System is carried as the first
// `input` element with role "developer" (arch §1.4), NOT the `instructions`
// field, per the LIC-TASK-015 acceptance criteria.
//
// Text is set ONLY when the caller wants JSON: a JSONSchema produces a strict
// json_schema format, a bare JSONMode produces json_object. When neither is
// set Text is nil and omitempty drops the key (free-form output — unused by
// LIC v1 agents but kept faithful to the port contract).
type responsesRequest struct {
	Model           string             `json:"model"`
	Input           []responsesMessage `json:"input"`
	MaxOutputTokens int                `json:"max_output_tokens"`
	Temperature     float64            `json:"temperature"`
	Text            *responsesText     `json:"text,omitempty"`
}

// responsesMessage is one {role, content} element of the Responses API `input`
// array. content is sent as a scalar string — multi-part content (images,
// files) is not part of the LIC v1 wire format (llm-provider-abstraction.md
// §1.6 multimodal-not-in-v1).
type responsesMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// responsesText is the `text` request object. Format selects the output
// shaping: json_schema (strict structured outputs) or json_object (JSON-only,
// no schema). Free-form output omits the whole `text` object.
type responsesText struct {
	Format responsesFormat `json:"format"`
}

// responsesFormat is the FLATTENED structured-output descriptor specific to
// the Responses API (code-architect Q2): unlike Chat Completions'
// `response_format:{type:"json_schema", json_schema:{name,strict,schema}}`,
// the Responses API puts Name / Strict / Schema directly inside `format`
// alongside Type. Name / Strict / Schema are emitted only for the json_schema
// variant (omitempty) so the json_object variant stays `{"type":"json_object"}`.
//
// INVARIANT (golang-pro S1): `Strict` carries `omitempty`, so a `false` value
// is dropped on the wire and is therefore NOT representable. This is
// deliberate and load-bearing: LIC v1 only ever wants strict structured
// outputs (the json_schema branch always sets Strict=true) or schemaless
// json_object (which must not carry `strict` at all). A future maintainer who
// needs a non-strict json_schema MUST switch `Strict` to `*bool` rather than
// relying on this struct — silently emitting `strict:false` via this field is
// impossible by construction and a regression would be caught by
// TestBuildRequestPayload_JSONModeWithoutSchema_EmitsJSONObject (exact-string
// assertion) and the golden-shape test.
type responsesFormat struct {
	Type   string          `json:"type"`
	Name   string          `json:"name,omitempty"`
	Strict bool            `json:"strict,omitempty"`
	Schema json.RawMessage `json:"schema,omitempty"`
}

// schemaName is the fixed identifier sent in text.format.name when a
// JSONSchema accompanies a CompletionRequest. OpenAI constrains this to the
// charset ^[a-zA-Z0-9_-]+$ (asserted by TestSchemaName_MatchesOpenAICharset).
// The value mirrors the Claude sibling's virtualToolName so golden tests and
// cross-provider behaviour stay aligned — agent identity lives in OTel span
// attributes, not on the wire (code-architect Q3/Q9).
const schemaName = "return_analysis_result"

// formatTypeJSONSchema / formatTypeJSONObject are the two text.format.type
// values LIC v1 emits.
const (
	formatTypeJSONSchema = "json_schema"
	formatTypeJSONObject = "json_object"
)

// buildRequestPayload assembles the responsesRequest for a single Complete
// call. It validates PriorTurns ordering, places System as the leading
// developer message (only when non-empty — HealthCheck sends no System), and
// selects the structured-output format from JSONSchema / JSONMode.
//
// Returns LLMErrorMalformedRequest on impossible inputs (e.g. a Turn with a
// non-user/assistant role) — this defends against misuse from callers other
// than the Router and surfaces the adapter-invariant before any wire I/O is
// spent (mirrors claude.buildRequestPayload — code-architect Q3).
func buildRequestPayload(req port.CompletionRequest, model string) (*responsesRequest, error) {
	for i, t := range req.PriorTurns {
		if !t.Role.IsValid() {
			return nil, port.NewLLMProviderError(port.LLMErrorMalformedRequest,
				fmt.Errorf("openai: PriorTurns[%d] has invalid role %q (must be user|assistant)", i, t.Role))
		}
	}

	// developer (system) + PriorTurns + the current user turn.
	input := make([]responsesMessage, 0, len(req.PriorTurns)+2)
	if req.System != "" {
		input = append(input, responsesMessage{
			Role:    "developer",
			Content: req.System,
		})
	}
	for _, t := range req.PriorTurns {
		input = append(input, responsesMessage{
			Role:    string(t.Role),
			Content: t.Content,
		})
	}
	input = append(input, responsesMessage{
		Role:    string(port.RoleUser),
		Content: req.User,
	})

	payload := &responsesRequest{
		Model:           model,
		Input:           input,
		MaxOutputTokens: req.MaxTokens,
		Temperature:     req.Temperature,
	}

	switch {
	case len(req.JSONSchema) > 0:
		payload.Text = &responsesText{Format: responsesFormat{
			Type:   formatTypeJSONSchema,
			Name:   schemaName,
			Strict: true,
			Schema: req.JSONSchema,
		}}
	case req.JSONMode:
		payload.Text = &responsesText{Format: responsesFormat{
			Type: formatTypeJSONObject,
		}}
	}

	return payload, nil
}
