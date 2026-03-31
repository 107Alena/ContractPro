package model

import (
	"encoding/json"
	"time"
)

// AuditAction represents the type of action recorded in the audit trail.
type AuditAction string

const (
	AuditActionDocumentCreated       AuditAction = "DOCUMENT_CREATED"
	AuditActionVersionCreated        AuditAction = "VERSION_CREATED"
	AuditActionArtifactSaved         AuditAction = "ARTIFACT_SAVED"
	AuditActionArtifactRead          AuditAction = "ARTIFACT_READ"
	AuditActionDiffSaved             AuditAction = "DIFF_SAVED"
	AuditActionDocumentArchived      AuditAction = "DOCUMENT_ARCHIVED"
	AuditActionDocumentDeleted       AuditAction = "DOCUMENT_DELETED"
	AuditActionArtifactStatusChanged AuditAction = "ARTIFACT_STATUS_CHANGED"
	AuditActionVersionFinalized      AuditAction = "VERSION_FINALIZED"
)

// AllAuditActions returns all valid audit actions.
var AllAuditActions = []AuditAction{
	AuditActionDocumentCreated,
	AuditActionVersionCreated,
	AuditActionArtifactSaved,
	AuditActionArtifactRead,
	AuditActionDiffSaved,
	AuditActionDocumentArchived,
	AuditActionDocumentDeleted,
	AuditActionArtifactStatusChanged,
	AuditActionVersionFinalized,
}

// ActorType represents the type of actor that performed an action.
type ActorType string

const (
	ActorTypeUser   ActorType = "USER"
	ActorTypeSystem ActorType = "SYSTEM"
	ActorTypeDomain ActorType = "DOMAIN"
)

// AllActorTypes returns all valid actor types.
var AllActorTypes = []ActorType{
	ActorTypeUser,
	ActorTypeSystem,
	ActorTypeDomain,
}

// AuditRecord is an append-only log entry for significant actions on documents and versions.
type AuditRecord struct {
	AuditID        string          `json:"audit_id"`
	OrganizationID string          `json:"organization_id"`
	DocumentID     string          `json:"document_id,omitempty"`
	VersionID      string          `json:"version_id,omitempty"`
	Action         AuditAction     `json:"action"`
	ActorType      ActorType       `json:"actor_type"`
	ActorID        string          `json:"actor_id"`
	JobID          string          `json:"job_id,omitempty"`
	CorrelationID  string          `json:"correlation_id,omitempty"`
	Details        json.RawMessage `json:"details,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
}

// NewAuditRecord creates a new AuditRecord with the current timestamp.
func NewAuditRecord(
	auditID, organizationID string,
	action AuditAction,
	actorType ActorType,
	actorID string,
) *AuditRecord {
	return &AuditRecord{
		AuditID:        auditID,
		OrganizationID: organizationID,
		Action:         action,
		ActorType:      actorType,
		ActorID:        actorID,
		CreatedAt:      time.Now().UTC(),
	}
}

// WithDocument sets the document context on the audit record.
func (r *AuditRecord) WithDocument(documentID string) *AuditRecord {
	r.DocumentID = documentID
	return r
}

// WithVersion sets the version context on the audit record.
func (r *AuditRecord) WithVersion(versionID string) *AuditRecord {
	r.VersionID = versionID
	return r
}

// WithJob sets the job context on the audit record.
func (r *AuditRecord) WithJob(jobID, correlationID string) *AuditRecord {
	r.JobID = jobID
	r.CorrelationID = correlationID
	return r
}

// WithDetails sets additional context on the audit record.
func (r *AuditRecord) WithDetails(details json.RawMessage) *AuditRecord {
	r.Details = details
	return r
}
