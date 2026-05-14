package claude

import (
	"encoding/json"
	"fmt"

	"contractpro/legal-intelligence-core/internal/domain/port"
)

// anthropicRequest is the wire-shape POSTed to /v1/messages.
// Field ordering follows the public Anthropic Messages API documentation.
//
// System uses the array form even when there is a single block so the
// cache_control toggle does not require two branches in the marshalling path
// (code-architect Q9). When req.System is empty the slice is nil and
// omitempty drops the key entirely.
//
// Tool / ToolChoice are set ONLY when the caller passed a JSONSchema; the
// adapter emits a single virtual tool whose input_schema mirrors the agent
// schema, paired with tool_choice forcing the model to call it
// (llm-provider-abstraction.md §1.5).
type anthropicRequest struct {
	Model         string                 `json:"model"`
	System        []systemBlock          `json:"system,omitempty"`
	Messages      []anthropicMessage     `json:"messages"`
	MaxTokens     int                    `json:"max_tokens"`
	Temperature   float64                `json:"temperature"`
	StopSequences []string               `json:"stop_sequences,omitempty"`
	Tools         []anthropicTool        `json:"tools,omitempty"`
	ToolChoice    *anthropicToolChoice   `json:"tool_choice,omitempty"`
}

// systemBlock is one element of the Messages-API system array. The
// cache_control marker is an Anthropic-private extension; when omitted the
// system text is billed at full rate, when set the call participates in the
// ephemeral prompt cache (llm-provider-abstraction.md §5.1).
type systemBlock struct {
	Type         string             `json:"type"`
	Text         string             `json:"text"`
	CacheControl *cacheControlBlock `json:"cache_control,omitempty"`
}

// cacheControlBlock is the typed cache hint. v1 only emits ephemeral (5-min
// TTL, default Anthropic semantics).
type cacheControlBlock struct {
	Type string `json:"type"`
}

// anthropicMessage is the {role, content} pair Anthropic's chat completion
// API consumes. content is sent as a scalar string — multi-block content
// (images, tool_use) is not part of the LIC v1 wire format.
type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// anthropicTool is the virtual-tool envelope used to force JSON Schema
// compliance via Anthropic's tool_use mechanism. Name is fixed to
// virtualToolName so golden tests stay stable across agents — agent identity
// is captured in OTel span attributes, not on the wire (code-architect Q3).
type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// anthropicToolChoice forces the model to invoke a specific named tool. We
// always pin to virtualToolName so the response shape is deterministic and
// response.go can demand a tool_use block named exactly that.
type anthropicToolChoice struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

// virtualToolName is the fixed identifier of the single Anthropic tool we
// register when a JSONSchema accompanies a CompletionRequest. The name is
// descriptive enough to guide the model's behaviour while remaining stable
// across all 9 LIC agents.
const virtualToolName = "return_analysis_result"

// virtualToolDescription is sent to Anthropic alongside the tool definition;
// it communicates intent to the model without disclosing agent identity.
const virtualToolDescription = "Returns the structured analysis result conforming to the provided schema."

// buildRequestPayload assembles the anthropicRequest for a single Complete
// call. It validates PriorTurns ordering, applies prompt-cache control if
// enabled, and emits the virtual tool / tool_choice pair when the caller
// requested strict structured outputs.
//
// Returns LLMErrorMalformedRequest on impossible inputs (e.g. a Turn with a
// non-user/assistant role) — this defends against misuse from callers other
// than the Router (code-architect Q8) and surfaces the adapter-invariant
// before any wire I/O is spent.
func buildRequestPayload(req port.CompletionRequest, model string, promptCacheEnabled bool) (*anthropicRequest, error) {
	for i, t := range req.PriorTurns {
		if !t.Role.IsValid() {
			return nil, port.NewLLMProviderError(port.LLMErrorMalformedRequest,
				fmt.Errorf("claude: PriorTurns[%d] has invalid role %q (must be user|assistant)", i, t.Role))
		}
	}

	messages := make([]anthropicMessage, 0, len(req.PriorTurns)+1)
	for _, t := range req.PriorTurns {
		messages = append(messages, anthropicMessage{
			Role:    string(t.Role),
			Content: t.Content,
		})
	}
	messages = append(messages, anthropicMessage{
		Role:    string(port.RoleUser),
		Content: req.User,
	})

	payload := &anthropicRequest{
		Model:         model,
		Messages:      messages,
		MaxTokens:     req.MaxTokens,
		Temperature:   req.Temperature,
		StopSequences: req.StopSequences,
	}

	if req.System != "" {
		payload.System = buildSystemBlocks(req.System, promptCacheEnabled)
	}

	if len(req.JSONSchema) > 0 {
		payload.Tools = []anthropicTool{{
			Name:        virtualToolName,
			Description: virtualToolDescription,
			InputSchema: req.JSONSchema,
		}}
		payload.ToolChoice = &anthropicToolChoice{
			Type: "tool",
			Name: virtualToolName,
		}
	}

	return payload, nil
}

// buildSystemBlocks emits the single-block system array. Always-array form
// is intentional: it lets the cache toggle be a one-field flip instead of a
// scalar-vs-array shape switch (code-architect Q9).
func buildSystemBlocks(systemText string, cacheEnabled bool) []systemBlock {
	block := systemBlock{Type: "text", Text: systemText}
	if cacheEnabled {
		block.CacheControl = &cacheControlBlock{Type: "ephemeral"}
	}
	return []systemBlock{block}
}
