package consumer

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// validate.go implements the PINNED per-event validation pipeline (build-spec
// D9), the canonical-UUID rule (D10), the best-effort ID extractor for the
// invalid path (D12) and the single internal *model.DomainError that all
// failures collapse into (D15).
//
// Validation order (PINNED, build-spec D9), applied for every event:
//  1. json.Unmarshal(body, &dto) — on error ⇒ invalid (decode failure;
//     best-effort IDs via idProbe — D12).
//  2. required-present: every field in the event's REQUIRED set must be
//     present and, for string IDs, non-empty after strings.TrimSpace.
//  3. canonical-UUID-valid: every field in the event's UUID-checked subset
//     must pass isCanonicalUUID (D10).
//
// Any failure at step 1, 2 or 3 ⇒ a single *model.DomainError
// (model.NewDomainError(model.ErrCodeInvalidMessageSchema,
// model.StageReceived) — D15) whose wrapped cause lists every offending
// field. All-pass ⇒ the valid path.
//
// The REQUIRED set is the per-event projection of the maximal envelope
// (integration-contracts.md:121-141) onto each frozen events.go struct shape
// (R5): the struct shapes are the binding SSOT for which envelope fields
// actually exist on each event.

// idProbe is the tolerant flat decode target used to salvage forensic IDs for
// the invalid-message DLQ envelope (build-spec D12). It mirrors only the five
// best-effort envelope IDs (events.go:281-287). Decoding into it is allowed to
// fail (it is the same body that failed strict decode); whatever string
// fields populate are taken, then each is admitted into the envelope ONLY if
// it is a clean canonical UUID (D12 — a forged body must not inject arbitrary
// strings into PII-safe forensic IDs).
type idProbe struct {
	CorrelationID  string `json:"correlation_id"`
	JobID          string `json:"job_id"`
	DocumentID     string `json:"document_id"`
	VersionID      string `json:"version_id"`
	OrganizationID string `json:"organization_id"`
}

// probeIDs performs the tolerant decode and ignores the error by design
// (build-spec D12).
func probeIDs(body []byte) idProbe {
	var p idProbe
	_ = json.Unmarshal(body, &p)
	return p
}

// isCanonicalUUID reports whether s is a canonical 36-char hyphenated UUID
// (xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx). It does NOT enforce version==4: any
// valid UUID in canonical form is accepted (build-spec D10 — security.md does
// NOT mandate v4-strictness; integration-contracts.md:121 is a wire-shape
// hint, not a version constraint). It DOES reject urn:/braced/no-hyphen
// encodings: at exactly 36 bytes uuid.Parse takes only the canonical case-36
// branch (uuid.go:71-72,98-116), so len==36 && uuid.Parse(s)==nil is exactly
// the canonical-form check. uuid.Validate is deliberately NOT used (it would
// also pass the 32/38-char forms at their lengths).
func isCanonicalUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	if _, err := uuid.Parse(s); err != nil {
		return false
	}
	return true // len==36 + uuid.Parse ok ⇒ canonical hyphenated form
}

// fieldChecks accumulates the offending json-tag names across the two
// post-decode validation steps so a single DomainError can list every problem
// at once (build-spec D9/D15). Names are appended in each event's fixed,
// source-order check sequence, so the rendered message is deterministic and
// test-stable WITHOUT a sort (the D16 allowlist excludes "sort"; relying on
// the fixed append order is the hermetic, equally-deterministic choice).
type fieldChecks struct {
	missing []string // required-present failures (step 2)
	badUUID []string // canonical-UUID failures (step 3)
}

// required records a required-present field: present and, after trim,
// non-empty (build-spec D9 step 2).
func (fc *fieldChecks) required(jsonTag, value string) {
	if strings.TrimSpace(value) == "" {
		fc.missing = append(fc.missing, jsonTag)
	}
}

// canonicalUUID records a canonical-UUID-checked field (build-spec D9 step 3).
// A field that is empty was already reported by required(); only a non-empty
// but non-canonical value is reported here (avoids duplicate noise for a
// missing+invalid field).
func (fc *fieldChecks) canonicalUUID(jsonTag, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	if !isCanonicalUUID(value) {
		fc.badUUID = append(fc.badUUID, jsonTag)
	}
}

// canonicalUUIDIfPresent is the conditional variant for an omitempty UUID
// field (e.g. VersionCreated.job_id — events.go:69): UUID-checked ONLY IF
// present and non-empty (build-spec D9, the VersionCreated row).
func (fc *fieldChecks) canonicalUUIDIfPresent(jsonTag, value string) {
	if strings.TrimSpace(value) == "" {
		return // omitempty + absent ⇒ not an error
	}
	if !isCanonicalUUID(value) {
		fc.badUUID = append(fc.badUUID, jsonTag)
	}
}

// err returns the single internal *model.DomainError (build-spec D15) listing
// every offending field (in fixed source-order, no sort — D16 allowlist), or
// nil if all checks passed. The DomainError uses
// model.ErrCodeInvalidMessageSchema (registered, retryable=false,
// userMessage="" ⇒ IsPublishableToOrchestrator()==false) and
// model.StageReceived (the receive/ingress stage — the message was received
// and rejected before any pipeline stage). It is internal to 039: it only
// feeds LICDLQEnvelope.ErrorMessage and the Error log line; no status event
// is published from 039.
func (fc *fieldChecks) err() *model.DomainError {
	if len(fc.missing) == 0 && len(fc.badUUID) == 0 {
		return nil
	}
	var parts []string
	if len(fc.missing) > 0 {
		parts = append(parts, "missing/empty: "+strings.Join(fc.missing, ", "))
	}
	if len(fc.badUUID) > 0 {
		parts = append(parts, "non-canonical UUID: "+strings.Join(fc.badUUID, ", "))
	}
	return newSchemaError(strings.Join(parts, "; "))
}

// newSchemaError builds the single internal *model.DomainError for any
// validation failure (build-spec D15). reason is the sanitized field list —
// it NEVER contains raw body bytes / payload content (PII —
// integration-contracts.md:319).
func newSchemaError(reason string) *model.DomainError {
	return model.NewDomainError(model.ErrCodeInvalidMessageSchema, model.StageReceived).
		WithCause(fmt.Errorf("invalid inbound message: %s", reason))
}

// decodeFailed builds the schema error for a hard json.Unmarshal failure
// (build-spec D9 step 1). The decode error text is the json package's own
// message (a structural parse description, e.g. "unexpected end of JSON
// input") — never the raw body.
func decodeFailed(jerr error) *model.DomainError {
	return newSchemaError(fmt.Sprintf("json decode error: %v", jerr))
}

// ----------------------------------------------------------------------------
// Per-event decode + validate. Each returns (typed DTO, RequestIDs, *err).
// On verr != nil the DTO/RequestIDs are zero and the caller takes the invalid
// path (best-effort IDs come from idProbe, NOT these — build-spec D12). The
// RequestIDs population follows the build-spec D11 per-event table (only
// fields that exist on the DTO).
// ----------------------------------------------------------------------------

func decodeVersionArtifactsReady(body []byte) (port.VersionProcessingArtifactsReady, RequestIDs, *model.DomainError) {
	var evt port.VersionProcessingArtifactsReady
	if err := json.Unmarshal(body, &evt); err != nil {
		return evt, RequestIDs{}, decodeFailed(err)
	}
	var fc fieldChecks
	// REQUIRED present: correlation_id, timestamp, document_id, version_id,
	// organization_id, job_id, created_by_user_id (events.go:42-53;
	// created_by_user_id no omitempty — events.go:52; integration-contracts
	// .md:138 "required").
	fc.required("correlation_id", evt.CorrelationID)
	fc.required("timestamp", evt.Timestamp)
	fc.required("document_id", evt.DocumentID)
	fc.required("version_id", evt.VersionID)
	fc.required("organization_id", evt.OrganizationID)
	fc.required("job_id", evt.JobID)
	fc.required("created_by_user_id", evt.CreatedByUserID)
	// UUID-checked: correlation_id, document_id, version_id, organization_id,
	// job_id, created_by_user_id.
	fc.canonicalUUID("correlation_id", evt.CorrelationID)
	fc.canonicalUUID("document_id", evt.DocumentID)
	fc.canonicalUUID("version_id", evt.VersionID)
	fc.canonicalUUID("organization_id", evt.OrganizationID)
	fc.canonicalUUID("job_id", evt.JobID)
	fc.canonicalUUID("created_by_user_id", evt.CreatedByUserID)
	if verr := fc.err(); verr != nil {
		return port.VersionProcessingArtifactsReady{}, RequestIDs{}, verr
	}
	ids := RequestIDs{
		CorrelationID:   evt.CorrelationID,
		JobID:           evt.JobID,
		DocumentID:      evt.DocumentID,
		VersionID:       evt.VersionID,
		OrganizationID:  evt.OrganizationID,
		CreatedByUserID: evt.CreatedByUserID,
	}
	return evt, ids, nil
}

func decodeVersionCreated(body []byte) (port.VersionCreated, RequestIDs, *model.DomainError) {
	var evt port.VersionCreated
	if err := json.Unmarshal(body, &evt); err != nil {
		return evt, RequestIDs{}, decodeFailed(err)
	}
	var fc fieldChecks
	// REQUIRED present: correlation_id, timestamp, document_id, version_id,
	// organization_id, created_by_user_id (events.go:60-71; created_by_user_id
	// no omitempty — events.go:70). job_id is json:"job_id,omitempty"
	// (events.go:69) ⇒ NOT required.
	fc.required("correlation_id", evt.CorrelationID)
	fc.required("timestamp", evt.Timestamp)
	fc.required("document_id", evt.DocumentID)
	fc.required("version_id", evt.VersionID)
	fc.required("organization_id", evt.OrganizationID)
	fc.required("created_by_user_id", evt.CreatedByUserID)
	// UUID-checked: correlation_id, document_id, version_id, organization_id,
	// created_by_user_id; job_id UUID-checked ONLY IF present and non-empty
	// (conditional — build-spec D9 VersionCreated row).
	fc.canonicalUUID("correlation_id", evt.CorrelationID)
	fc.canonicalUUID("document_id", evt.DocumentID)
	fc.canonicalUUID("version_id", evt.VersionID)
	fc.canonicalUUID("organization_id", evt.OrganizationID)
	fc.canonicalUUID("created_by_user_id", evt.CreatedByUserID)
	fc.canonicalUUIDIfPresent("job_id", evt.JobID)
	if verr := fc.err(); verr != nil {
		return port.VersionCreated{}, RequestIDs{}, verr
	}
	ids := RequestIDs{
		CorrelationID:   evt.CorrelationID,
		JobID:           evt.JobID, // if present (omitempty) — left "" otherwise
		DocumentID:      evt.DocumentID,
		VersionID:       evt.VersionID,
		OrganizationID:  evt.OrganizationID,
		CreatedByUserID: evt.CreatedByUserID,
	}
	return evt, ids, nil
}

func decodeArtifactsProvided(body []byte) (port.ArtifactsProvided, RequestIDs, *model.DomainError) {
	var evt port.ArtifactsProvided
	if err := json.Unmarshal(body, &evt); err != nil {
		return evt, RequestIDs{}, decodeFailed(err)
	}
	var fc fieldChecks
	// REQUIRED present: correlation_id, timestamp, job_id, document_id,
	// version_id (events.go:81-91). NO organization_id field exists on this
	// struct — MUST NOT be required (R5).
	fc.required("correlation_id", evt.CorrelationID)
	fc.required("timestamp", evt.Timestamp)
	fc.required("job_id", evt.JobID)
	fc.required("document_id", evt.DocumentID)
	fc.required("version_id", evt.VersionID)
	// UUID-checked: correlation_id, job_id, document_id, version_id.
	fc.canonicalUUID("correlation_id", evt.CorrelationID)
	fc.canonicalUUID("job_id", evt.JobID)
	fc.canonicalUUID("document_id", evt.DocumentID)
	fc.canonicalUUID("version_id", evt.VersionID)
	if verr := fc.err(); verr != nil {
		return port.ArtifactsProvided{}, RequestIDs{}, verr
	}
	ids := RequestIDs{
		CorrelationID: evt.CorrelationID,
		JobID:         evt.JobID,
		DocumentID:    evt.DocumentID,
		VersionID:     evt.VersionID,
		// OrganizationID / CreatedByUserID: no field on this struct — "".
	}
	return evt, ids, nil
}

func decodePersisted(body []byte) (port.LegalAnalysisArtifactsPersisted, RequestIDs, *model.DomainError) {
	var evt port.LegalAnalysisArtifactsPersisted
	if err := json.Unmarshal(body, &evt); err != nil {
		return evt, RequestIDs{}, decodeFailed(err)
	}
	var fc fieldChecks
	// REQUIRED present: correlation_id, timestamp, job_id, document_id — only
	// these 4 fields exist (events.go:97-102); no version_id/organization_id.
	fc.required("correlation_id", evt.CorrelationID)
	fc.required("timestamp", evt.Timestamp)
	fc.required("job_id", evt.JobID)
	fc.required("document_id", evt.DocumentID)
	// UUID-checked: correlation_id, job_id, document_id.
	fc.canonicalUUID("correlation_id", evt.CorrelationID)
	fc.canonicalUUID("job_id", evt.JobID)
	fc.canonicalUUID("document_id", evt.DocumentID)
	if verr := fc.err(); verr != nil {
		return port.LegalAnalysisArtifactsPersisted{}, RequestIDs{}, verr
	}
	ids := RequestIDs{
		CorrelationID: evt.CorrelationID,
		JobID:         evt.JobID,
		DocumentID:    evt.DocumentID,
	}
	return evt, ids, nil
}

func decodePersistFailed(body []byte) (port.LegalAnalysisArtifactsPersistFailed, RequestIDs, *model.DomainError) {
	var evt port.LegalAnalysisArtifactsPersistFailed
	if err := json.Unmarshal(body, &evt); err != nil {
		return evt, RequestIDs{}, decodeFailed(err)
	}
	var fc fieldChecks
	// REQUIRED present: correlation_id, timestamp, job_id, document_id.
	// error_message is present-but-not-an-ID (events.go:114, no omitempty —
	// required-present, NOT UUID-checked). error_code is omitempty
	// (events.go:113) ⇒ not required. is_retryable is a bool ⇒ not
	// required-present.
	fc.required("correlation_id", evt.CorrelationID)
	fc.required("timestamp", evt.Timestamp)
	fc.required("job_id", evt.JobID)
	fc.required("document_id", evt.DocumentID)
	fc.required("error_message", evt.ErrorMessage)
	// UUID-checked: correlation_id, job_id, document_id.
	fc.canonicalUUID("correlation_id", evt.CorrelationID)
	fc.canonicalUUID("job_id", evt.JobID)
	fc.canonicalUUID("document_id", evt.DocumentID)
	if verr := fc.err(); verr != nil {
		return port.LegalAnalysisArtifactsPersistFailed{}, RequestIDs{}, verr
	}
	ids := RequestIDs{
		CorrelationID: evt.CorrelationID,
		JobID:         evt.JobID,
		DocumentID:    evt.DocumentID,
	}
	return evt, ids, nil
}

func decodeUserConfirmedType(body []byte) (port.UserConfirmedType, RequestIDs, *model.DomainError) {
	var evt port.UserConfirmedType
	if err := json.Unmarshal(body, &evt); err != nil {
		return evt, RequestIDs{}, decodeFailed(err)
	}
	var fc fieldChecks
	// REQUIRED present: correlation_id, timestamp, job_id, document_id,
	// version_id, organization_id, contract_type (events.go:128-137;
	// contract_type no omitempty — events.go:135). user_id is omitempty
	// (events.go:136) ⇒ not required.
	fc.required("correlation_id", evt.CorrelationID)
	fc.required("timestamp", evt.Timestamp)
	fc.required("job_id", evt.JobID)
	fc.required("document_id", evt.DocumentID)
	fc.required("version_id", evt.VersionID)
	fc.required("organization_id", evt.OrganizationID)
	fc.required("contract_type", evt.ContractType)
	// UUID-checked: correlation_id, job_id, document_id, version_id,
	// organization_id. NOT contract_type — it is ^[A-Z_]{1,32}$, not a UUID;
	// the whitelist/regex check is 040/manager-owned per security.md §11.2
	// (R6); 039 only requires it present-and-non-empty.
	fc.canonicalUUID("correlation_id", evt.CorrelationID)
	fc.canonicalUUID("job_id", evt.JobID)
	fc.canonicalUUID("document_id", evt.DocumentID)
	fc.canonicalUUID("version_id", evt.VersionID)
	fc.canonicalUUID("organization_id", evt.OrganizationID)
	if verr := fc.err(); verr != nil {
		return port.UserConfirmedType{}, RequestIDs{}, verr
	}
	ids := RequestIDs{
		CorrelationID:  evt.CorrelationID,
		JobID:          evt.JobID,
		DocumentID:     evt.DocumentID,
		VersionID:      evt.VersionID,
		OrganizationID: evt.OrganizationID,
		// CreatedByUserID: user_id is a *confirmer*, NOT created_by_user_id;
		// RequestIDs has no confirmer field by design (build-spec D11 — 040 /
		// the manager owns confirmer audit per security.md §11.2 / R6).
	}
	return evt, ids, nil
}
