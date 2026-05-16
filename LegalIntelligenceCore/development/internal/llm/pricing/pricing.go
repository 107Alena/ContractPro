// Package pricing loads and evaluates the per-model LLM price table that
// gates lic_llm_cost_usd_total and the LICCostSpike alert
// (llm-provider-abstraction.md §4.1, configuration.md §2.15).
//
// It is deliberately pure data: it imports only the standard library and
// go.yaml.in/yaml/v2 (the YAML module already pinned in go.sum as a
// prometheus/common transitive dep — see CLAUDE.md "Dependency choice").
// internal/llm/cost depends on this package; the edge is acyclic and the
// Table is read-only after Load, so a single Table is safe to share across
// the parallel agent pipeline.
package pricing

import (
	"fmt"
	"math"
	"os"
	"sort"

	yaml "go.yaml.in/yaml/v2"
)

// tokensPerMillion is the SSOT divisor: prices are quoted per 1,000,000
// tokens (llm-provider-abstraction.md §4.1). Named so the formula and the
// docs reference one symbol, not a literal that can drift.
const tokensPerMillion = 1_000_000

// ModelPricing is one model's USD price per 1,000,000 tokens, split into the
// three billable categories. CachedInputPerMTokenUSD is broken out because
// Anthropic prompt-cache hits bill at ~10% of the normal input rate; folding
// it into the input rate would over-bill cache-hit requests up to 10× and
// trip a false LICCostSpike (llm-provider-abstraction.md §4.1,
// observability.md §3.4).
type ModelPricing struct {
	InputPerMTokenUSD       float64
	CachedInputPerMTokenUSD float64
	OutputPerMTokenUSD      float64

	// CachedRateDefaulted is true when the YAML omitted
	// cached_input_per_m_token_usd and Load set it to 0.0 explicitly. It is
	// surfaced (not left as an implicit Go zero value) so app-wiring
	// (LIC-TASK-047) can log "model X: cached rate defaulted to 0.0": an
	// absent key on a cache-billed provider is a silent under-bill and must
	// be observable (code-architect MF-5).
	CachedRateDefaulted bool
}

// Table maps a concrete model id (e.g. "claude-sonnet-4-6") to its pricing.
// It is read-only after Load; callers MUST NOT mutate it. The cost Tracker
// shares one Table across the parallel errgroup pipeline — concurrent reads
// of a never-written map are race-free; a write would not be.
type Table map[string]ModelPricing

// CostUSD applies the SSOT formula (llm-provider-abstraction.md §4.1):
//
//	cost = (input·Input + cached·CachedInput + output·Output) / 1_000_000
//
// Each term is promoted to float64 before the multiply so a large int token
// count cannot overflow an int intermediate (code-architect MF-4). Summing
// the three products before the single divide minimises rounding; the
// precision contract is a relative error of a few ULPs (~1e-15), orders of
// magnitude below any plausible LICCostSpike threshold — the test asserts
// the formula to 1e-12, which is the contract, not float noise. Load
// rejects non-finite rates so NaN/±Inf can never reach this multiply and
// silently defeat the alert (golang-pro Y1). Negative token counts are
// clamped to 0: an upstream adapter bug must never yield a negative cost,
// and a negative downstream would panic a Prometheus counter Add.
//
// known is false when model is absent from the table; usd is then 0.0. The
// caller (cost.Tracker) still records tokens/latency/calls and emits a
// distinct unknown-model signal — a missing price never drops the request
// nor errors.
func (t Table) CostUSD(model string, input, cached, output int) (usd float64, known bool) {
	p, ok := t[model]
	if !ok {
		return 0, false
	}
	usd = (float64(nonNeg(input))*p.InputPerMTokenUSD +
		float64(nonNeg(cached))*p.CachedInputPerMTokenUSD +
		float64(nonNeg(output))*p.OutputPerMTokenUSD) / tokensPerMillion
	return usd, true
}

// yamlEntry decodes one model's mapping. Input/Output are pointers so a
// missing required key is distinguishable from a literal 0.0; Cached is a
// pointer so "absent" (→ explicit 0.0 + CachedRateDefaulted) is
// distinguishable from "0.0 by intent" (code-architect MF-5).
type yamlEntry struct {
	Input  *float64 `yaml:"input_per_m_token_usd"`
	Cached *float64 `yaml:"cached_input_per_m_token_usd"`
	Output *float64 `yaml:"output_per_m_token_usd"`
}

// Load reads, parses and validates the pricing YAML at path. It returns a
// typed, path-wrapped error on any of: file unreadable, malformed YAML, an
// unknown key (strict decode — a typo in a money-critical file must fail
// loud, not silently zero a rate), no models, an empty model key, a missing
// required input/output rate, or any negative rate. It never panics and has
// no fallback table.
//
// Forward requirement (LIC-TASK-047): missing/invalid pricing MUST be a
// fatal startup error. A silently empty table bills every call at $0 and
// hides every spike from LICCostSpike — worse than failing to start
// (code-architect OQ-6; mirrors ratelimit.NewLimiter / kvstore.NewClient
// fail-fast).
func Load(path string) (Table, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("pricing: read %q: %w", path, err)
	}

	var doc map[string]yamlEntry
	if err := yaml.UnmarshalStrict(raw, &doc); err != nil {
		return nil, fmt.Errorf("pricing: parse %q: %w", path, err)
	}
	if len(doc) == 0 {
		return nil, fmt.Errorf("pricing: %q contains no models", path)
	}

	// Iterate models in sorted order so that, when a pricing file has more
	// than one defect, the *same* error surfaces every run — a money-critical
	// file is debugged against a stable message, not a map-order lottery
	// (golang-pro E1).
	models := make([]string, 0, len(doc))
	for mdl := range doc {
		models = append(models, mdl)
	}
	sort.Strings(models)

	table := make(Table, len(doc))
	for _, mdl := range models {
		e := doc[mdl]
		if mdl == "" {
			return nil, fmt.Errorf("pricing: %q contains an empty model key", path)
		}
		if e.Input == nil {
			return nil, fmt.Errorf("pricing: %q model %q: missing input_per_m_token_usd", path, mdl)
		}
		if e.Output == nil {
			return nil, fmt.Errorf("pricing: %q model %q: missing output_per_m_token_usd", path, mdl)
		}

		mp := ModelPricing{
			InputPerMTokenUSD:  *e.Input,
			OutputPerMTokenUSD: *e.Output,
		}
		if e.Cached == nil {
			mp.CachedRateDefaulted = true // explicit 0.0 (code-architect MF-5)
		} else {
			mp.CachedInputPerMTokenUSD = *e.Cached
		}

		// Reject negative AND non-finite rates. NaN < 0 is false and
		// +Inf < 0 is false, so a bare sign check would let NaN/±Inf through
		// → every CostUSD returns NaN/Inf, silently defeating LICCostSpike or
		// poisoning a Prometheus counter. A money-critical loader that chose
		// strict decode to fail loud must reject these too (golang-pro Y1).
		for _, r := range [...]struct {
			name string
			v    float64
		}{
			{"input_per_m_token_usd", mp.InputPerMTokenUSD},
			{"cached_input_per_m_token_usd", mp.CachedInputPerMTokenUSD},
			{"output_per_m_token_usd", mp.OutputPerMTokenUSD},
		} {
			if r.v < 0 || math.IsNaN(r.v) || math.IsInf(r.v, 0) {
				return nil, fmt.Errorf("pricing: %q model %q: %s must be a finite value >= 0, got %v",
					path, mdl, r.name, r.v)
			}
		}
		table[mdl] = mp
	}
	return table, nil
}

// nonNeg clamps a token count to >= 0. Deliberately duplicated, not shared,
// with cost.nonNeg: hermeticity keeps "pure data" pricing free of an
// exported util surface, and a 4-line pure clamp has near-zero drift risk.
// The two copies MUST stay behaviourally identical — cost clamps the token
// counters it reports, this clamps the cost it computes; on a negative input
// both must agree on 0 (golang-pro D1/N1).
func nonNeg(n int) int {
	if n < 0 {
		return 0
	}
	return n
}
