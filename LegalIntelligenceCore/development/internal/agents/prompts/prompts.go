// Package prompts embeds and serves the nine LIC agent system prompts.
//
// The *.txt files are copied byte-for-byte from ai-agents-pipeline.md §1–9
// (Russian legal-domain text — the SSOT). Each is the System message of one
// agent's LLM call; system-prompt-only caching is the documented policy
// (llm-provider-abstraction.md §5), so the text must be returned verbatim.
//
// Hermetic: imports only the standard library plus internal/domain/model
// (for the typed AgentID key — the loader API is keyed by it, so the import
// is necessary, not a smell; cf. internal/llm/pricing which avoids model
// only because it has no domain identifier). embed.FS content is fixed at
// build time; a missing/empty/extra asset is surfaced by Validate as a
// deterministic error that app-wiring (LIC-TASK-024/047) MUST treat as a
// fatal startup error — the same fail-loud contract as pricing.Load. This
// is an embedded/external-data loader, hence Validate-returns-error, NOT an
// init() panic (init()-panic is reserved for invariants over compiled-in Go
// constants, e.g. domain/model error_codes.go; code-architect Q4).
package prompts

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"contractpro/legal-intelligence-core/internal/domain/model"
)

// promptFS holds the 9 system-prompt texts. It is unexported so callers
// cannot bypass the AgentID-keyed LoadPrompt and read arbitrary files.
//
//go:embed *.txt
var promptFS embed.FS

// ext is the embedded prompt file extension. The //go:embed glob above must
// stay in sync with it; both are stated once so neither drifts.
const ext = ".txt"

// basenames is the single source of truth mapping each of the 9 AgentIDs to
// its embedded file basename (without extension). It is explicit, NOT derived
// from the AgentID wire string: the house style encodes SSOT as enumerated
// tables checked by Validate against model.AllAgentIDs() (cf. domain/model
// error_codes.go errorCatalog, agent.go agentIDSet), not as a string
// transform that merely "happens to hold" today (code-architect Q2).
var basenames = map[model.AgentID]string{
	model.AgentTypeClassifier:      "type_classifier",
	model.AgentKeyParams:           "key_params",
	model.AgentPartyConsistency:    "party_consistency",
	model.AgentMandatoryConditions: "mandatory_conditions",
	model.AgentRiskDetection:       "risk_detection",
	model.AgentRecommendation:      "recommendation",
	model.AgentSummary:             "summary",
	model.AgentDetailedReport:      "detailed_report",
	model.AgentRiskDelta:           "risk_delta",
}

// LoadPrompt returns the verbatim system-prompt text for id. It errors when
// id is not one of the 9 agents, or the embedded file is missing/empty —
// it never panics and never trims: the prompt is legal-domain SSOT and a
// non-empty length is a floor, not a content check.
func LoadPrompt(id model.AgentID) (string, error) {
	return loadPrompt(promptFS, basenames, id)
}

// loadPrompt is the FS-injectable core of LoadPrompt. fs.ReadFile works for
// both embed.FS and testing/fstest.MapFS, so the deterministic error
// contract can be exercised directly in tests without a fake API surface.
func loadPrompt(fsys fs.FS, names map[model.AgentID]string, id model.AgentID) (string, error) {
	base, ok := names[id]
	if !ok {
		return "", fmt.Errorf("prompts: unknown agent id %q", id)
	}
	b, err := fs.ReadFile(fsys, base+ext)
	if err != nil {
		return "", fmt.Errorf("prompts: agent %s: read %s%s: %w", id, base, ext, err)
	}
	if len(b) == 0 {
		return "", fmt.Errorf("prompts: agent %s: %s%s is empty", id, base, ext)
	}
	return string(b), nil
}

// Validate asserts the embedded set is exactly the 9 mapped prompts: every
// AgentID maps to a clean basename resolving to a non-empty file, and there
// is no extra/orphan *.txt file (strict, mirroring pricing's fail-loud
// decode). Errors are aggregated via errors.Join in deterministic order —
// per-agent first in pipeline order (model.AllAgentIDs()), then orphan files
// sorted by name — so a multi-defect build fails with the same message every
// run (pricing E1). app-wiring MUST treat a non-nil result as fatal.
func Validate() error {
	return validate(promptFS, basenames)
}

func validate(fsys fs.FS, names map[model.AgentID]string) error {
	var errs []error

	for _, id := range model.AllAgentIDs() {
		base, ok := names[id]
		if !ok {
			errs = append(errs, fmt.Errorf("prompts: agent %s: no basename mapped", id))
			continue
		}
		if err := validBasename(base); err != nil {
			errs = append(errs, fmt.Errorf("prompts: agent %s: invalid basename %q: %w", id, base, err))
			continue
		}
		if _, err := loadPrompt(fsys, names, id); err != nil {
			errs = append(errs, err)
		}
	}

	mapped := make(map[string]struct{}, len(names))
	for _, b := range names {
		mapped[b] = struct{}{}
	}
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		errs = append(errs, fmt.Errorf("prompts: read embed dir: %w", err))
	} else {
		var orphans []string
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ext) {
				continue
			}
			if _, ok := mapped[strings.TrimSuffix(name, ext)]; !ok {
				orphans = append(orphans, name)
			}
		}
		sort.Strings(orphans)
		for _, o := range orphans {
			errs = append(errs, fmt.Errorf("prompts: orphan embedded file %q (no AgentID maps to it)", o))
		}
	}

	return errors.Join(errs...)
}

// validBasename rejects a basenames-table value that is not a single clean
// path segment. The table is hand-maintained SSOT; a stray "sub/x", "../x"
// or ".hidden" must fail loud at Validate (a deterministic startup error),
// not resolve a different embedded path silently — the same reason pricing
// rejects an empty model key. Deliberately duplicated, not shared, with
// schemas.validBasename: hermeticity keeps these leaf packages free of a
// shared util surface and the rule is four trivial guards.
func validBasename(b string) error {
	switch {
	case b == "":
		return errors.New("empty")
	case strings.HasPrefix(b, "."):
		return errors.New("must not start with '.'")
	case strings.ContainsAny(b, `/\`):
		return errors.New("must not contain a path separator")
	case strings.Contains(b, ".."):
		return errors.New("must not contain '..'")
	default:
		return nil
	}
}
