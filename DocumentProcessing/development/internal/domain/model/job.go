package model

import "time"

// JobMeta contains fields shared between ProcessingJob and ComparisonJob.
type JobMeta struct {
	JobID     string    `json:"job_id"`
	Status    JobStatus `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ProcessingJob represents a document processing task.
type ProcessingJob struct {
	JobMeta
	DocumentID string          `json:"document_id"`
	VersionID  string          `json:"version_id,omitempty"`
	FileURL    string          `json:"file_url"`
	Stage      ProcessingStage `json:"stage"`
	FileName   string          `json:"file_name,omitempty"`
	FileSize   int64           `json:"file_size,omitempty"`
	MimeType   string          `json:"mime_type,omitempty"`
	Checksum   string          `json:"checksum,omitempty"`
	OrgID      string          `json:"organization_id,omitempty"`
	UserID     string          `json:"requested_by_user_id,omitempty"`
}

// ComparisonJob represents a document version comparison task.
type ComparisonJob struct {
	JobMeta
	DocumentID      string          `json:"document_id"`
	BaseVersionID   string          `json:"base_version_id"`
	TargetVersionID string          `json:"target_version_id"`
	Stage           ComparisonStage `json:"stage"`
	OrgID           string          `json:"organization_id,omitempty"`
	UserID          string          `json:"requested_by_user_id,omitempty"`
}

// TransitionTo validates and performs a status transition.
// On success, updates Status and UpdatedAt. Returns an error if the transition is invalid.
func (m *JobMeta) TransitionTo(newStatus JobStatus) error {
	if err := ValidateTransition(m.Status, newStatus); err != nil {
		return err
	}
	m.Status = newStatus
	m.UpdatedAt = time.Now().UTC()
	return nil
}

// GetJobMeta returns a pointer to the embedded JobMeta.
func (j *ProcessingJob) GetJobMeta() *JobMeta { return &j.JobMeta }

// GetDocumentID returns the document ID associated with this job.
func (j *ProcessingJob) GetDocumentID() string { return j.DocumentID }

// GetStage returns the current processing stage as a string.
func (j *ProcessingJob) GetStage() string { return string(j.Stage) }

// GetOrgID returns the organization ID associated with this job.
func (j *ProcessingJob) GetOrgID() string { return j.OrgID }

// GetJobMeta returns a pointer to the embedded JobMeta.
func (j *ComparisonJob) GetJobMeta() *JobMeta { return &j.JobMeta }

// GetDocumentID returns the document ID associated with this job.
func (j *ComparisonJob) GetDocumentID() string { return j.DocumentID }

// GetStage returns the current comparison stage as a string.
func (j *ComparisonJob) GetStage() string { return string(j.Stage) }

// GetOrgID returns the organization ID associated with this job.
func (j *ComparisonJob) GetOrgID() string { return j.OrgID }

// NewProcessingJob creates a new ProcessingJob in QUEUED status.
func NewProcessingJob(jobID, documentID, fileURL string) *ProcessingJob {
	now := time.Now().UTC()
	return &ProcessingJob{
		JobMeta: JobMeta{
			JobID:     jobID,
			Status:    StatusQueued,
			CreatedAt: now,
			UpdatedAt: now,
		},
		DocumentID: documentID,
		FileURL:    fileURL,
	}
}

// NewComparisonJob creates a new ComparisonJob in QUEUED status.
func NewComparisonJob(jobID, documentID, baseVersionID, targetVersionID string) *ComparisonJob {
	now := time.Now().UTC()
	return &ComparisonJob{
		JobMeta: JobMeta{
			JobID:     jobID,
			Status:    StatusQueued,
			CreatedAt: now,
			UpdatedAt: now,
		},
		DocumentID:      documentID,
		BaseVersionID:   baseVersionID,
		TargetVersionID: targetVersionID,
	}
}
