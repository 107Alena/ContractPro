// Package promptbuilder assembles the final LLM message for every LIC agent:
// CompletionRequest.System (the baked agent prompt, passed in by the caller)
// and CompletionRequest.User (the XML envelope `<input>…</input>`).
//
// It is prompt-injection defence layer 2 (ADR-LIC-07, high-architecture.md
// §6.7.1, ai-agents-pipeline.md §0.3): EVERY user-controlled byte that enters
// the envelope is `&`/`<`/`>`-escaped first, so a literal `</contract_document>`
// or `<input>` planted in the contract body can never read as a block
// delimiter to the model. The Builder is the ONLY thing that can place
// structural (un-escaped) XML in the envelope — see Part.
//
// For agent 3 (Party Data Consistency) the Builder additionally performs the
// pre-LLM, LLM-free ИНН/ОГРН checksum validation (high-architecture.md
// §6.7.2) and emits the <validation_facts> ground-truth block.
//
// Hermetic leaf: stdlib + internal/domain/{model,port} only — exactly like
// internal/llm/cost. Prometheus is inverted behind the Recorder seam; the
// adapter over *metrics.CrossCutMetrics is wired in LIC-TASK-047. The Builder
// is immutable after construction and safe for concurrent use by the parallel
// errgroup agent pipeline.
package promptbuilder

import (
	"fmt"
	"strings"

	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// PartyKind is the typed `type` label of lic_party_validation_total. It is a
// LOCAL mirror of metrics.PartyValidationType (kept here, not imported, to
// preserve hermeticity); TestPartyKind_WireStringsPinned guards it against
// SSOT drift (observability.md §3.9 / high-architecture.md §6.7.2). Callers
// MUST pass only the constants so a typo is a compile error, not a bad label.
type PartyKind string

const (
	PartyKindINN  PartyKind = "inn"
	PartyKindOGRN PartyKind = "ogrn"
)

// Recorder is the metrics seam. Single method, fired once per identifier
// ACTUALLY present and checked → lic_party_validation_total{type,valid}.
// An absent INN/OGRN is NOT recorded: the metric is a data-quality signal
// (§6.7.2) and "absent" is not "invalid" — counting absent as valid=false
// would poison it (code-architect MF-5d). The LIC-TASK-047 adapter binds
// this to metrics.CrossCutMetrics.PartyValidationTotal via
// metrics.PartyValidationType + metrics.BoolLabel.
type Recorder interface {
	RecordPartyValidation(kind PartyKind, valid bool)
}

// noopRecorder is the zero-dependency default so the Builder is usable in
// tests and before LIC-TASK-047 wires Prometheus, without a nil check per
// call (mirrors cost.noopRecorder / ratelimit.noopObserver).
type noopRecorder struct{}

func (noopRecorder) RecordPartyValidation(PartyKind, bool) {}

var _ Recorder = noopRecorder{}

// Part is one envelope block. It is a CLOSED type: code in other packages can
// only obtain a Part from Content (a tag + user content that is ALWAYS
// escaped) or from a Builder method that returns a Builder-formed structural
// block (ValidationFacts). The unexported fields make "no caller can inject
// un-escaped structural XML" a *type* guarantee rather than a discipline
// (code-architect MF-2). The zero Part is invalid and Build rejects it.
type Part struct {
	tag     string // content block: wrapping element name; "" for a minted block
	content string // content block: raw user-controlled text (escaped on Build)
	xml     string // minted block: complete structural XML, inserted verbatim
	minted  bool   // true ⇒ Builder-formed; only Builder methods set this
}

// Content returns an escaped content block that Build renders as
// <tag>escapeText(content)</tag>. tag must match ^[a-z_]{1,32}$ (validated
// in Build); content is treated as untrusted.
func Content(tag, content string) Part {
	return Part{tag: tag, content: content}
}

// Builder is immutable after NewBuilder; the only field is the metrics seam.
type Builder struct {
	rec Recorder
}

// NewBuilder returns a Builder. A nil rec is replaced by a no-op so callers
// and tests never need a per-call nil check (stutter-free NewTypeName per the
// codebase-wide convention / feedback_constructors.md).
func NewBuilder(rec Recorder) *Builder {
	if rec == nil {
		rec = noopRecorder{}
	}
	return &Builder{rec: rec}
}

// Build assembles the provider-agnostic request. System is the baked agent
// prompt (passed in — prompt loading is the caller's job so this package
// stays hermetic and envelope-structure-agnostic). User is
// `<input>{parts}</input>`: every content Part is `<tag>`-wrapped with its
// content escaped; every minted Part is inserted verbatim. Only AgentID,
// System and User are set — Model/MaxTokens/Temperature/JSONSchema belong to
// the agent/router layer (high-architecture.md §6.7).
//
// Fail-fast (deterministic error, never panic) on caller/programming errors:
// empty system, no parts, an invalid or duplicate content tag, an empty
// minted block. A bad tag is a programming error, not user data, so erroring
// (not escaping) is correct (code-architect NTH-1).
func (b *Builder) Build(agentID model.AgentID, system string, parts []Part) (port.CompletionRequest, error) {
	if system == "" {
		return port.CompletionRequest{}, fmt.Errorf("promptbuilder: agent %q: empty system prompt", agentID)
	}
	if len(parts) == 0 {
		return port.CompletionRequest{}, fmt.Errorf("promptbuilder: agent %q: no envelope parts", agentID)
	}

	var sb strings.Builder
	sb.WriteString("<input>")
	seen := make(map[string]struct{}, len(parts))
	for i, p := range parts {
		if p.minted {
			if p.xml == "" {
				return port.CompletionRequest{}, fmt.Errorf("promptbuilder: agent %q: part %d is an empty minted block", agentID, i)
			}
			sb.WriteString(p.xml)
			continue
		}
		if !validTag(p.tag) {
			return port.CompletionRequest{}, fmt.Errorf("promptbuilder: agent %q: part %d invalid tag %q (want ^[a-z_]{1,32}$)", agentID, i, p.tag)
		}
		if _, dup := seen[p.tag]; dup {
			return port.CompletionRequest{}, fmt.Errorf("promptbuilder: agent %q: duplicate envelope tag %q", agentID, p.tag)
		}
		seen[p.tag] = struct{}{}
		sb.WriteString("<")
		sb.WriteString(p.tag)
		sb.WriteString(">")
		sb.WriteString(escapeText(p.content))
		sb.WriteString("</")
		sb.WriteString(p.tag)
		sb.WriteString(">")
	}
	sb.WriteString("</input>")

	return port.CompletionRequest{
		AgentID: agentID,
		System:  system,
		User:    sb.String(),
	}, nil
}

// validTag enforces ^[a-z_]{1,32}$ by hand (no regexp on the hot path;
// mirrors prompts.validBasename's manual-guard precedent). Tag names are
// Builder/caller-controlled structural strings, never user data.
func validTag(t string) bool {
	if len(t) == 0 || len(t) > 32 {
		return false
	}
	for i := 0; i < len(t); i++ {
		if c := t[i]; c != '_' && (c < 'a' || c > 'z') {
			return false
		}
	}
	return true
}

// Party is one contracting party's raw identifiers as extracted upstream
// (agent-2 party_roles / DOCUMENT_STRUCTURE.party_details). All three fields
// are user-controlled; ValidationFacts treats them as untrusted (attribute
// escaping + digit-gated checksums).
type Party struct {
	Name string
	INN  string // empty ⇒ "not present" (not "invalid")
	OGRN string // empty ⇒ "not present" (not "invalid")
}

// PartyValidation is the deterministic per-party outcome, returned so
// agent-3's future Run / cross-agent verification can use it as ground truth
// without re-parsing the XML (high-architecture.md §6.7.2). The *Present
// flags make "absent vs invalid" explicit to that caller (code-architect
// NTH-2 / MF-5d).
type PartyValidation struct {
	Name string

	// INN/OGRN are the trimmed identifiers ("" when absent); *Present make
	// "absent" distinguishable from "invalid"; EntityType is one of
	// LEGAL_ENTITY | INDIVIDUAL_ENTREPRENEUR | null.
	INNPresent bool
	INN        string
	INNValid   bool

	OGRNPresent bool
	OGRN        string
	OGRNValid   bool
	EntityType  string
}

// ValidationFacts performs the pre-LLM, LLM-free ИНН/ОГРН checksum validation
// for agent 3 and returns (1) a Builder-minted <validation_facts> Part for
// the envelope and (2) the structured results.
//
// The element shape is the <inn_check>/<ogrn_check> form the agent-3 system
// prompt actually consumes (internal/agents/prompts/party_consistency.txt,
// ai-agents-pipeline.md §3) — NOT the illustrative flat <party> form in
// high-architecture.md §6.7.2. The prompt is what the model reads, so the
// prompt is SSOT (code-architect-confirmed). name/inn/ogrn are
// attribute-escaped (user-controlled); valid/entity_type come from closed
// const sets and are emitted as-is.
//
// lic_party_validation_total{type,valid} fires once per identifier ACTUALLY
// present; an absent INN/OGRN is neither rendered nor counted.
func (b *Builder) ValidationFacts(parties []Party) (Part, []PartyValidation) {
	var sb strings.Builder
	sb.WriteString("<validation_facts>")
	results := make([]PartyValidation, 0, len(parties))

	for _, p := range parties {
		name := strings.TrimSpace(p.Name)
		inn := strings.TrimSpace(p.INN)
		ogrn := strings.TrimSpace(p.OGRN)

		// pv.Name keeps the FULL trimmed name: it is in-process Go data for
		// agent-3 ground-truth correlation, NOT a delimiter-vulnerable prompt
		// string. Only the model-facing XML attribute is rune-capped.
		pv := PartyValidation{Name: name, EntityType: EntityNull}
		attrName := escapeAttr(capRunes(name, maxPartyNameRunes))

		if inn != "" {
			pv.INNPresent = true
			pv.INN = inn
			pv.INNValid = ValidateINN(inn)
			b.rec.RecordPartyValidation(PartyKindINN, pv.INNValid)
			sb.WriteString(`<inn_check name="`)
			sb.WriteString(attrName)
			sb.WriteString(`" inn="`)
			sb.WriteString(escapeAttr(inn))
			sb.WriteString(`" valid="`)
			sb.WriteString(boolAttr(pv.INNValid))
			sb.WriteString(`" />`)
		}
		if ogrn != "" {
			pv.OGRNPresent = true
			pv.OGRN = ogrn
			pv.OGRNValid, pv.EntityType = ValidateOGRN(ogrn)
			b.rec.RecordPartyValidation(PartyKindOGRN, pv.OGRNValid)
			sb.WriteString(`<ogrn_check name="`)
			sb.WriteString(attrName)
			sb.WriteString(`" ogrn="`)
			sb.WriteString(escapeAttr(ogrn))
			sb.WriteString(`" valid="`)
			sb.WriteString(boolAttr(pv.OGRNValid))
			sb.WriteString(`" entity_type="`)
			sb.WriteString(safeEntityType(pv.EntityType))
			sb.WriteString(`" />`)
		}

		results = append(results, pv)
	}

	sb.WriteString("</validation_facts>")
	return Part{xml: sb.String(), minted: true}, results
}

// maxPartyNameRunes bounds the party `name` attribute. The block is
// model-trusted ground truth (§6.7.2) and `name` is fully document-derived;
// a real Russian legal-entity name is well under this, so capping cannot
// corrupt a legitimate name but denies an attacker a paragraph-long
// pseudo-instruction smuggled into the trusted block (security-engineer
// HIGH-2, cheap in-scope defence-in-depth — the residual social-attribution
// risk is recorded for the agent-3 prompt / report layer).
const maxPartyNameRunes = 256

// capRunes truncates s to at most n runes (rune-safe, never splits a
// multi-byte Cyrillic codepoint). No truncation marker is appended — that
// would inject Builder-authored bytes into a user field.
func capRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// boolAttr is the XML attribute spelling of a bool. Kept local (not
// metrics.BoolLabel) so the package stays hermetic; the values happen to
// coincide with the Prometheus label spelling, which is asserted by test.
func boolAttr(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
