package gemini

import (
	"encoding/json"
	"fmt"

	"contractpro/legal-intelligence-core/internal/domain/port"
)

// generateContentRequest is the wire-shape POSTed to
// /v1beta/models/{model}:generateContent. Field ordering follows the public
// Gemini API documentation.
//
// SystemInstruction is a pointer with omitempty: when req.System is empty
// (e.g. HealthCheck) the key is dropped entirely rather than sent as
// {parts:[{text:""}]}, which Gemini rejects with 400 on some model versions
// (code-architect MUST-FIX #5). GenerationConfig is always present (it carries
// at least maxOutputTokens / temperature).
type generateContentRequest struct {
	SystemInstruction *geminiContent       `json:"systemInstruction,omitempty"`
	Contents          []geminiContent      `json:"contents"`
	GenerationConfig  *geminiGenerationCfg `json:"generationConfig"`
}

// geminiContent is one conversational turn (or the system instruction). Role
// is "user" | "model" for `contents` entries and omitted for
// systemInstruction (omitempty drops the empty string). Parts is always
// emitted; v1 sends a single text part (multimodal is not in the v1 wire
// format — llm-provider-abstraction.md §1.6).
type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

// geminiPart is one content part. v1 only ever emits text parts.
type geminiPart struct {
	Text string `json:"text"`
}

// geminiGenerationCfg is the `generationConfig` object. ResponseMimeType /
// ResponseSchema are set only when the caller wants JSON: a JSONSchema yields
// both (strict structured outputs), a bare JSONMode yields only
// ResponseMimeType="application/json" (JSON-only, no schema — arch §1.5
// table). StopSequences is forwarded (Gemini supports it, unlike the OpenAI
// Responses API — code-architect Q5).
//
// MaxOutputTokens / Temperature carry no omitempty: they are always sent
// explicitly (mirrors the OpenAI sibling) so a deliberate Temperature=0 or a
// caller-set token cap is never silently dropped.
type geminiGenerationCfg struct {
	MaxOutputTokens  int             `json:"maxOutputTokens"`
	Temperature      float64         `json:"temperature"`
	StopSequences    []string        `json:"stopSequences,omitempty"`
	ResponseMimeType string          `json:"responseMimeType,omitempty"`
	ResponseSchema   json.RawMessage `json:"responseSchema,omitempty"`
}

// responseMimeTypeJSON is the MIME type that switches Gemini into JSON output
// (with or without an accompanying responseSchema).
const responseMimeTypeJSON = "application/json"

// roleModel is Gemini's wire token for an assistant turn. Gemini uses "model"
// where the port uses RoleAssistant — this mapping lives entirely in the
// adapter and does not leak into the port contract
// (llm-provider-abstraction.md §1.1 / §1.4).
const roleModel = "model"

// buildRequestPayload assembles the generateContentRequest for a single
// Complete call. It validates PriorTurns ordering, places System in
// systemInstruction (only when non-empty — HealthCheck sends no System), maps
// roles (Assistant→"model"), and selects the structured-output config from
// JSONSchema / JSONMode.
//
// Returns LLMErrorMalformedRequest on impossible inputs (a Turn with a
// non-user/assistant role, or a JSONSchema that cannot be transformed into
// Gemini's schema dialect) — this defends against misuse from callers other
// than the Router and surfaces the adapter invariant before any wire I/O is
// spent (mirrors openai/claude buildRequestPayload — code-architect Q3).
func buildRequestPayload(req port.CompletionRequest) (*generateContentRequest, error) {
	for i, t := range req.PriorTurns {
		if !t.Role.IsValid() {
			return nil, port.NewLLMProviderError(port.LLMErrorMalformedRequest,
				fmt.Errorf("gemini: PriorTurns[%d] has invalid role %q (must be user|assistant)", i, t.Role))
		}
	}

	contents := make([]geminiContent, 0, len(req.PriorTurns)+1)
	for _, t := range req.PriorTurns {
		contents = append(contents, geminiContent{
			Role:  wireRole(t.Role),
			Parts: []geminiPart{{Text: t.Content}},
		})
	}
	contents = append(contents, geminiContent{
		Role:  wireRole(port.RoleUser),
		Parts: []geminiPart{{Text: req.User}},
	})

	genCfg := &geminiGenerationCfg{
		MaxOutputTokens: req.MaxTokens,
		Temperature:     req.Temperature,
		StopSequences:   req.StopSequences,
	}

	switch {
	case len(req.JSONSchema) > 0:
		// Transform JSON Schema draft-07 → Gemini's OpenAPI-3.0 Schema subset
		// (code-architect MUST-FIX #1). An un-transformable schema is a
		// programming error in the caller, surfaced as MALFORMED before any
		// wire I/O.
		schema, err := transformSchema(req.JSONSchema)
		if err != nil {
			return nil, port.NewLLMProviderError(port.LLMErrorMalformedRequest,
				fmt.Errorf("gemini: responseSchema: %w", err))
		}
		genCfg.ResponseMimeType = responseMimeTypeJSON
		genCfg.ResponseSchema = schema
	case req.JSONMode:
		genCfg.ResponseMimeType = responseMimeTypeJSON
	}

	payload := &generateContentRequest{
		Contents:         contents,
		GenerationConfig: genCfg,
	}
	if req.System != "" {
		payload.SystemInstruction = &geminiContent{
			Parts: []geminiPart{{Text: req.System}},
		}
	}

	return payload, nil
}

// wireRole maps a port Role onto Gemini's wire token. RoleAssistant→"model"
// (Gemini's convention); RoleUser→"user". The caller has already validated the
// role via Role.IsValid, so the default is unreachable for well-formed input
// and conservatively yields "user".
func wireRole(r port.Role) string {
	if r == port.RoleAssistant {
		return roleModel
	}
	return string(port.RoleUser)
}
