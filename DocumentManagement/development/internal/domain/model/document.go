package model

import "time"

// DocumentStatus represents the lifecycle status of a document.
type DocumentStatus string

const (
	DocumentStatusActive   DocumentStatus = "ACTIVE"
	DocumentStatusArchived DocumentStatus = "ARCHIVED"
	DocumentStatusDeleted  DocumentStatus = "DELETED"
)

// AllDocumentStatuses returns all valid document statuses.
var AllDocumentStatuses = []DocumentStatus{
	DocumentStatusActive,
	DocumentStatusArchived,
	DocumentStatusDeleted,
}

// validDocumentTransitions defines allowed status transitions.
var validDocumentTransitions = map[DocumentStatus][]DocumentStatus{
	DocumentStatusActive:   {DocumentStatusArchived, DocumentStatusDeleted},
	DocumentStatusArchived: {DocumentStatusDeleted},
	DocumentStatusDeleted:  {},
}

// ValidateDocumentTransition checks whether the transition from → to is allowed.
func ValidateDocumentTransition(from, to DocumentStatus) bool {
	targets, ok := validDocumentTransitions[from]
	if !ok {
		return false
	}
	for _, t := range targets {
		if t == to {
			return true
		}
	}
	return false
}

// IsTerminal returns true if the status has no outgoing transitions.
func (s DocumentStatus) IsTerminal() bool {
	targets, ok := validDocumentTransitions[s]
	return !ok || len(targets) == 0
}

// Document is the root aggregate representing a contract in the system.
type Document struct {
	DocumentID       string         `json:"document_id"`
	OrganizationID   string         `json:"organization_id"`
	Title            string         `json:"title"`
	CurrentVersionID string         `json:"current_version_id,omitempty"`
	Status           DocumentStatus `json:"status"`
	CreatedByUserID  string         `json:"created_by_user_id"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
	DeletedAt        *time.Time     `json:"deleted_at,omitempty"`
}

// NewDocument creates a new Document in ACTIVE status.
func NewDocument(documentID, organizationID, title, createdByUserID string) *Document {
	now := time.Now().UTC()
	return &Document{
		DocumentID:      documentID,
		OrganizationID:  organizationID,
		Title:           title,
		Status:          DocumentStatusActive,
		CreatedByUserID: createdByUserID,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

// TransitionTo transitions the document to a new status if the transition is valid.
func (d *Document) TransitionTo(newStatus DocumentStatus) bool {
	if !ValidateDocumentTransition(d.Status, newStatus) {
		return false
	}
	d.Status = newStatus
	d.UpdatedAt = time.Now().UTC()
	if newStatus == DocumentStatusDeleted {
		now := time.Now().UTC()
		d.DeletedAt = &now
	}
	return true
}
