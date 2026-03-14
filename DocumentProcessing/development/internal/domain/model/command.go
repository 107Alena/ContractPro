package model

// ProcessDocumentCommand is the command received from the broker
// to initiate document processing (ProcessDocumentRequested).
type ProcessDocumentCommand struct {
	JobID      string `json:"job_id"`
	DocumentID string `json:"document_id"`
	FileURL    string `json:"file_url"`
	OrgID      string `json:"organization_id,omitempty"`
	UserID     string `json:"requested_by_user_id,omitempty"`
	FileName   string `json:"file_name,omitempty"`
	FileSize   int64  `json:"file_size,omitempty"`
	MimeType   string `json:"mime_type,omitempty"`
	Checksum   string `json:"checksum,omitempty"`
}

// CompareVersionsCommand is the command received from the broker
// to initiate version comparison (CompareDocumentVersionsRequested).
type CompareVersionsCommand struct {
	JobID           string `json:"job_id"`
	DocumentID      string `json:"document_id"`
	BaseVersionID   string `json:"base_version_id"`
	TargetVersionID string `json:"target_version_id"`
	OrgID           string `json:"organization_id,omitempty"`
	UserID          string `json:"requested_by_user_id,omitempty"`
}
