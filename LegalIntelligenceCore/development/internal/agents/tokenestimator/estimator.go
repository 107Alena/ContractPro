// Package tokenestimator is the LIC Token Estimator (LIC-TASK-021,
// high-architecture.md §6.7 / ASSUMPTION-LIC-12, ai-agents-pipeline.md
// §INPUT_TRUNCATED warning schema). It is the SOLE source of the
// TruncatedBytes/TotalBytes counts that the Pipeline Orchestrator
// (LIC-TASK-036) forwards to aggregator.Input.Truncation for the
// INPUT_TRUNCATED warning render (aggregator/CLAUDE.md forward note #2), AND
// the real implementation of base.TokenEstimator via Fit (base/seams.go
// :135-137) — the LIC-TASK-047 wiring injects an *Estimator into
// base.Deps.Estimator.
//
// Hermeticity. Like every internal/agents|llm/* sibling this package imports
// ONLY stdlib + internal/domain/{model,port}. NO internal/config (Config is
// ctor-injected by app-wiring — the aggregator/agents-* precedent), NO
// internal/infra/* (no telemetry seam — pure compute), NO internal/agents/base
// (that import would invert the LIC-TASK-047 wiring direction — the
// *Estimator is INJECTED into base, not the other way round; the base.go MF-3
// contract is satisfied STRUCTURALLY by Fit's signature). TestHermeticImports
// pins this against the exact two-entry allowlist {model, port}.
//
// Concurrency. *Estimator is immutable after NewEstimator (Config is a value;
// no per-call mutable state); one *Estimator is shared by the parallel
// errgroup pipeline without locking. Pinned by
// TestEstimator_ConcurrentRaceClean (-race, 32 goroutines).
//
// Decisions (see CLAUDE.md): D1 wire-shape parity with aggregator (separate
// TruncationInfo declaration so the 036 adapter avoids an aggregator import);
// D2 fail-fast Config validation via errors.Join (the
// scoring.ScoringConfig/aggregator.Config precedent); D3 rune-aware slicing
// (UTF-8/Cyrillic safety); D4 conservative ceiling rounding (a returned
// est<=maxTokens guarantees true tokens<=maxTokens under any provider's
// actual tokeniser within the heuristic accuracy); D5 byte-parity with
// orchestrator.go:393-403 for CheckIngestSize (the LIC-TASK-036 forward-note
// #5 enabler); D-NO-MARKER no join marker in Truncate (the spec is silent +
// any marker would inflate the post-truncation estimate above maxTokens).
package tokenestimator

import (
	"errors"
	"fmt"
	"math"

	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// DefaultCharsPerToken is the empirical RU heuristic (1 token ≈ 3.5 RU chars
// — tasks.json LIC-TASK-021 acceptance criterion). A Config with
// CharsPerToken==0 is treated as "use this default" (Config.validate fills
// the field in place; see D2).
const DefaultCharsPerToken = 3.5

// Config holds the four budget parameters. All fields except CharsPerToken
// are required (>0). CharsPerToken==0 is interpreted as "use
// DefaultCharsPerToken" — Config.validate fills it in. Validated fail-fast
// by NewEstimator.
type Config struct {
	// MaxInputTokens is the global per-version EXTRACTED_TEXT truncation
	// budget (LIC_MAX_INPUT_TOKENS, default 150000, ASSUMPTION-LIC-12).
	// Consumed by TruncateToInputBudget; the LIC-TASK-036 call site uses it
	// for the §6.7 per-artifact upstream truncation.
	MaxInputTokens int
	// MaxAgentInputTokens is the per-agent assembled-request budget
	// (LIC_MAX_AGENT_INPUT_TOKENS, default 120000). Consumed by Fit (the
	// base.TokenEstimator seam). By construction
	// MaxAgentInputTokens <= MaxInputTokens.
	MaxAgentInputTokens int
	// MaxIngestedBytes is the hard cap on aggregate artifact byte size
	// (LIC_MAX_INGESTED_BYTES, default 10 MiB). Enforced by
	// CheckIngestSize — byte-parity with orchestrator.go:393-403 (D5).
	// Declared as Go int (not int64) to match the inline form at
	// pipeline.Config.MaxIngestedBytes today; reflect.DeepEqual over
	// .Attributes distinguishes int(5) from int64(5), so the type must
	// match exactly. The env var source (scoring.ScoringConfig) carries
	// int64; the LIC-TASK-047 wiring narrows to int (the value is bounded
	// by NFR §1.1 to ≤ 10 MiB, well below int32 max).
	MaxIngestedBytes int
	// CharsPerToken is the token-density heuristic; 0 ⇒ DefaultCharsPerToken
	// (3.5). Must be >= 1.0 (a sub-1.0 heuristic would explode the estimate
	// and is meaningless for any real tokeniser).
	CharsPerToken float64
}

// validate fails-fast on the same bounds the global scoring.ScoringConfig
// already validates (the aggregator/stages errors.Join precedent) PLUS the
// local CharsPerToken default fill-in. Mutates the receiver to apply the
// default (note the pointer receiver — same pattern as
// aggregator.Config.validate, but with the addition of the default fill).
// Returns errors.Join of all violations so the caller sees every offending
// field, not just the first.
func (c *Config) validate() error {
	var errs []error
	if c.MaxInputTokens < 1 {
		errs = append(errs, fmt.Errorf("tokenestimator: Config.MaxInputTokens must be >= 1, got %d", c.MaxInputTokens))
	}
	if c.MaxAgentInputTokens < 1 {
		errs = append(errs, fmt.Errorf("tokenestimator: Config.MaxAgentInputTokens must be >= 1, got %d", c.MaxAgentInputTokens))
	}
	if c.MaxAgentInputTokens > c.MaxInputTokens {
		errs = append(errs, fmt.Errorf("tokenestimator: Config.MaxAgentInputTokens (%d) must be <= Config.MaxInputTokens (%d)", c.MaxAgentInputTokens, c.MaxInputTokens))
	}
	if c.MaxIngestedBytes < 1 {
		errs = append(errs, fmt.Errorf("tokenestimator: Config.MaxIngestedBytes must be >= 1, got %d", c.MaxIngestedBytes))
	}
	if c.CharsPerToken == 0 {
		c.CharsPerToken = DefaultCharsPerToken
	} else if c.CharsPerToken < 1.0 {
		errs = append(errs, fmt.Errorf("tokenestimator: Config.CharsPerToken must be >= 1.0 (or 0 for the default %v), got %v", DefaultCharsPerToken, c.CharsPerToken))
	}
	return errors.Join(errs...)
}

// Estimator is the immutable token-estimation runner. Construct via
// NewEstimator; all methods are safe for concurrent use over a single
// receiver.
type Estimator struct {
	cfg Config
}

// NewEstimator constructs an Estimator from cfg. Fails fast (returns the
// cfg.validate() errors.Join verbatim, no wrap) on any invalid field; the
// caller gets every violation in one error. Stutter-free NewTypeName per
// feedback_constructors.md.
func NewEstimator(cfg Config) (*Estimator, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &Estimator{cfg: cfg}, nil
}

// EstimateTokens returns ⌈len([]rune(text)) / CharsPerToken⌉. Operates on
// runes (UTF-8), not bytes — Cyrillic characters are 2 bytes each and a
// byte-based estimate would double-count them. The ceiling rounding is
// conservative by design (D4): a returned est<=maxTokens guarantees true
// tokens<=maxTokens for any provider's real tokeniser within the heuristic
// accuracy.
func (e *Estimator) EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	runes := len([]rune(text))
	return int(math.Ceil(float64(runes) / e.cfg.CharsPerToken))
}

// TruncationInfo is the byte-count pair forwarded by the Pipeline
// Orchestrator (LIC-TASK-036) to aggregator.Input.Truncation. Same wire
// shape as aggregator.TruncationInfo; declared here so the orchestrator can
// adapt a *tokenestimator.TruncationInfo directly to
// *aggregator.TruncationInfo without re-importing aggregator from this
// hermetic package (the D1 reconciliation — aggregator owns its own wire
// shape; this package owns the producer-side one).
type TruncationInfo struct {
	// TruncatedBytes is the byte count removed from the middle by Truncate
	// (== len(original) - len(truncated_output)). Always >= 1 when info is
	// non-nil — the defensive fallback returns (text, nil) when the budget
	// would not actually drop any bytes (D-DEFENSIVE).
	TruncatedBytes int
	// TotalBytes is the byte count of the original text.
	TotalBytes int
}

// Truncate applies the §6.7 head-60% / tail-40% rule when
// EstimateTokens(text) > maxTokens. Returns (text, nil) when no truncation
// is needed OR when maxTokens is too small to fit a meaningful head+tail
// split (the defensive fallback — preserves the invariant that a non-nil
// TruncationInfo always carries TruncatedBytes>=1, matching the
// INPUT_TRUNCATED.truncated_bytes minimum:1 schema in
// ai-agents-pipeline.md:1362).
//
// Rune-aware: slicing is on []rune so multi-byte (Cyrillic) boundaries are
// safe — never index text[i] for slicing here. No join marker is inserted
// (D-NO-MARKER: the spec is silent + a marker would inflate the
// post-truncation estimate above maxTokens).
func (e *Estimator) Truncate(text string, maxTokens int) (string, *TruncationInfo) {
	// 1. No-op fast path.
	if e.EstimateTokens(text) <= maxTokens {
		return text, nil
	}

	// 2. Head-60 / Tail-40 split on TOKENS, then convert to runes.
	headTokens := maxTokens * 60 / 100 // floor(.6*maxTokens), integer arithmetic
	tailTokens := maxTokens - headTokens
	headRunes := int(math.Floor(float64(headTokens) * e.cfg.CharsPerToken))
	tailRunes := int(math.Floor(float64(tailTokens) * e.cfg.CharsPerToken))

	// 3. Defensive fallback (D-DEFENSIVE): if the budget is so small that
	//    the head+tail split would not actually drop runes, OR either side
	//    is <=0, return (text, nil) — keep the "non-nil info ⇒
	//    TruncatedBytes>=1" invariant.
	runes := []rune(text)
	total := len(runes)
	if headRunes <= 0 || tailRunes <= 0 || headRunes+tailRunes >= total {
		return text, nil
	}

	// 4. Build the head+tail concatenation; record byte deltas on the
	//    ORIGINAL string (TotalBytes == len(text), TruncatedBytes computed
	//    against the assembled output).
	truncated := string(runes[:headRunes]) + string(runes[total-tailRunes:])
	return truncated, &TruncationInfo{
		TruncatedBytes: len(text) - len(truncated),
		TotalBytes:     len(text),
	}
}

// TruncateToInputBudget is a convenience wrapper around
// Truncate(text, cfg.MaxInputTokens). The LIC-TASK-036 call site uses this
// directly on per-version EXTRACTED_TEXT (§6.7 per-artifact upstream
// truncation, BEFORE the per-agent Spec.Parts/promptbuilder.Build step —
// see base/CLAUDE.md MF-3 on why envelope-time truncation is forbidden).
func (e *Estimator) TruncateToInputBudget(text string) (string, *TruncationInfo) {
	return e.Truncate(text, e.cfg.MaxInputTokens)
}

// CheckIngestSize sums len(raw) across all artifacts and returns
// DOCUMENT_TOO_LARGE when the total exceeds cfg.MaxIngestedBytes. The
// returned DomainError is BYTE-IDENTICAL on the wire to the inline form at
// internal/application/pipeline/orchestrator.go:393-403 (D5 / D8 in
// pipeline/CLAUDE.md): the LIC-TASK-036 forward-note #5 enabler — when 036
// later delegates this check to the Token Estimator, observable behaviour
// (Code, Stage, Retryable, Attributes incl. their Go types) does not
// change.
//
// Wire shape:
//   - Code:       model.ErrCodeDocumentTooLarge
//   - Stage:      model.StageArtifactsReceived
//   - Retryable:  false (explicit, never inherit the catalog default)
//   - Attributes: {"ingested_bytes": <Go int sum of len(raw)>,
//     "limit":          <Go int == cfg.MaxIngestedBytes>}
//
// Returns nil when within budget OR when artifacts is nil/empty. The
// comparison is strict ('>'), so total == limit passes (matches
// orchestrator.go line 398).
func (e *Estimator) CheckIngestSize(artifacts model.InputArtifactsCompact) error {
	total := 0
	for _, raw := range artifacts {
		total += len(raw)
	}
	if total > e.cfg.MaxIngestedBytes {
		return model.NewDomainError(model.ErrCodeDocumentTooLarge, model.StageArtifactsReceived).
			WithRetryable(false).
			WithAttribute("ingested_bytes", total).
			WithAttribute("limit", e.cfg.MaxIngestedBytes)
	}
	return nil
}

// Fit implements base.TokenEstimator (base/seams.go:135-137). It sums runes
// over req.System + req.User + sum(t.Content for t in req.PriorTurns) and
// returns (⌈runes/CharsPerToken⌉, est > cfg.MaxAgentInputTokens). The
// comparison is strict ('>'), so est == MaxAgentInputTokens is NOT
// over-budget.
//
// NEVER mutates req — the seam contract (base/CLAUDE.md MF-3) makes
// envelope corruption a type-level impossibility (no request is returned to
// mutate). port.CompletionRequest is passed by value; PriorTurns is read
// but never re-sliced/written. The §6.7 head-60/tail-40 truncation of
// EXTRACTED_TEXT happens UPSTREAM via TruncateToInputBudget on
// model.AgentInput artifacts — truncating the already-escaped
// <input>…</input> envelope here would slice an XML entity and defeat
// prompt-injection defence layer 2.
func (e *Estimator) Fit(req port.CompletionRequest) (estInputTokens int, overBudget bool) {
	runes := len([]rune(req.System)) + len([]rune(req.User))
	for _, t := range req.PriorTurns {
		runes += len([]rune(t.Content))
	}
	if runes == 0 {
		return 0, false
	}
	est := int(math.Ceil(float64(runes) / e.cfg.CharsPerToken))
	return est, est > e.cfg.MaxAgentInputTokens
}
