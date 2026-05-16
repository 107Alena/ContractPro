// Package schemas embeds and serves the nine LIC agent output JSON Schemas.
//
// The *.json files are copied byte-for-byte from ai-agents-pipeline.md §1–9
// (the SSOT). They drive the Schema Validator + 1-shot repair loop
// (LIC-TASK-023): LoadSchema returns the verbatim bytes so the downstream
// JSON-Schema library sees the exact document — re-marshalling through
// encoding/json would reorder keys and could alter number formatting.
//
// Scope boundary (forward requirement). TASK-020 asserts each schema is
// well-formed JSON, pins "$schema" to draft-07 by exact string equality, and
// checks a minimal top-level shape (title + a valid draft-07 root "type",
// which may legitimately be "array" — Recommendations — not only "object").
// Full JSON-Schema meta-schema conformance and instance validation are owned
// by LIC-TASK-023, which adds the real JSON-Schema library; pulling that lib
// in here would duplicate that dependency decision and break hermeticity.
//
// Hermetic: stdlib only + internal/domain/model (typed AgentID key). A
// missing/empty/extra/malformed asset is surfaced by Validate as a
// deterministic error that app-wiring (LIC-TASK-023/024/047) MUST treat as
// fatal — the fail-loud contract of pricing.Load; Validate-returns-error,
// not init()-panic (embedded data, not compiled-in constants; code-architect
// Q4).
package schemas

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"contractpro/legal-intelligence-core/internal/domain/model"
)

// schemaFS holds the 9 output schemas. Unexported so callers cannot bypass
// the AgentID-keyed LoadSchema and read arbitrary files.
//
//go:embed *.json
var schemaFS embed.FS

// ext is the embedded schema file extension; kept in sync with the //go:embed
// glob above.
const ext = ".json"

// draft07URI is the exact, canonical draft-07 meta-schema identifier. Every
// embedded schema's "$schema" must equal this string byte-for-byte — any
// other value (draft/2020-12, trailing-slash variants, …) is rejected loud,
// the same strictness pricing applies to its YAML keys (code-architect Q3).
const draft07URI = "http://json-schema.org/draft-07/schema#"

// draft07TypeNames is the closed set of draft-07 primitive type names a
// top-level "type" may name (as a bare string or inside an array).
var draft07TypeNames = map[string]struct{}{
	"null": {}, "boolean": {}, "object": {}, "array": {},
	"number": {}, "string": {}, "integer": {},
}

// basenames is the single source of truth mapping each AgentID to its
// embedded schema basename — explicit, not derived (see prompts.basenames
// rationale; code-architect Q2).
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

// LoadSchema returns the verbatim JSON-Schema bytes for id. It errors when
// id is not one of the 9 agents, the embedded file is missing/empty, or its
// content is not well-formed JSON (a corrupt schema reaching the validator
// is a definite bug — fail loud, like pricing's strict decode). The bytes
// are returned unchanged; the caller MUST NOT assume canonical key order.
func LoadSchema(id model.AgentID) ([]byte, error) {
	return loadSchema(schemaFS, basenames, id)
}

// loadSchema is the FS-injectable core of LoadSchema (see prompts.loadPrompt
// for the testability rationale).
func loadSchema(fsys fs.FS, names map[model.AgentID]string, id model.AgentID) ([]byte, error) {
	base, ok := names[id]
	if !ok {
		return nil, fmt.Errorf("schemas: unknown agent id %q", id)
	}
	b, err := fs.ReadFile(fsys, base+ext)
	if err != nil {
		return nil, fmt.Errorf("schemas: agent %s: read %s%s: %w", id, base, ext, err)
	}
	if len(b) == 0 {
		return nil, fmt.Errorf("schemas: agent %s: %s%s is empty", id, base, ext)
	}
	if !json.Valid(b) {
		return nil, fmt.Errorf("schemas: agent %s: %s%s is not well-formed JSON", id, base, ext)
	}
	return b, nil
}

// schemaHead is the minimal top-level shape Validate inspects. RawMessage
// for Type because draft-07 allows a string ("object") or an array
// (["string","null"]).
type schemaHead struct {
	Schema string          `json:"$schema"`
	Title  string          `json:"title"`
	Type   json.RawMessage `json:"type"`
}

// Validate asserts the embedded set is exactly the 9 mapped schemas and that
// each maps to a clean basename, is well-formed JSON, pins "$schema" to
// draft-07, and has a non-empty "title" plus a valid draft-07 root "type".
// Errors are aggregated via errors.Join in deterministic order — per-agent
// first (pipeline order), then orphan files sorted — so a multi-defect build
// fails identically every run (pricing E1). app-wiring MUST treat a non-nil
// result as fatal.
func Validate() error {
	return validate(schemaFS, basenames)
}

func validate(fsys fs.FS, names map[model.AgentID]string) error {
	var errs []error

	for _, id := range model.AllAgentIDs() {
		base, ok := names[id]
		if !ok {
			errs = append(errs, fmt.Errorf("schemas: agent %s: no basename mapped", id))
			continue
		}
		if err := validBasename(base); err != nil {
			errs = append(errs, fmt.Errorf("schemas: agent %s: invalid basename %q: %w", id, base, err))
			continue
		}
		b, err := loadSchema(fsys, names, id)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		var h schemaHead
		if err := json.Unmarshal(b, &h); err != nil {
			errs = append(errs, fmt.Errorf("schemas: agent %s: decode top level: %w", id, err))
			continue
		}
		if h.Schema != draft07URI {
			errs = append(errs, fmt.Errorf("schemas: agent %s: $schema must be %q, got %q", id, draft07URI, h.Schema))
		}
		if strings.TrimSpace(h.Title) == "" {
			errs = append(errs, fmt.Errorf("schemas: agent %s: missing non-empty top-level title", id))
		}
		if err := validTopLevelType(h.Type); err != nil {
			errs = append(errs, fmt.Errorf("schemas: agent %s: %w", id, err))
		}
	}

	mapped := make(map[string]struct{}, len(names))
	for _, b := range names {
		mapped[b] = struct{}{}
	}
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		errs = append(errs, fmt.Errorf("schemas: read embed dir: %w", err))
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
			errs = append(errs, fmt.Errorf("schemas: orphan embedded file %q (no AgentID maps to it)", o))
		}
	}

	return errors.Join(errs...)
}

// validBasename rejects a basenames-table value that is not a single clean
// path segment (see prompts.validBasename for the rationale; deliberately
// duplicated to keep both leaf packages hermetic).
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

// validTopLevelType accepts a draft-07 root "type": a single primitive name
// or a non-empty array of them. It deliberately does NOT require "object" —
// the Recommendations schema is root "array" (ai-agents-pipeline.md §6) and
// must validate (code-architect must-fix #1).
func validTopLevelType(raw json.RawMessage) error {
	if len(raw) == 0 {
		return errors.New(`missing top-level "type"`)
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if _, ok := draft07TypeNames[s]; !ok {
			return fmt.Errorf(`top-level "type" %q is not a draft-07 primitive type`, s)
		}
		return nil
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		if len(arr) == 0 {
			return errors.New(`top-level "type" array is empty`)
		}
		for _, t := range arr {
			if _, ok := draft07TypeNames[t]; !ok {
				return fmt.Errorf(`top-level "type" array contains non-draft-07 value %q`, t)
			}
		}
		return nil
	}
	return errors.New(`top-level "type" must be a string or array of strings`)
}
