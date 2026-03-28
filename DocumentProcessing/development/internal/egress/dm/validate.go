package dm

import (
	"fmt"
	"strings"

	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
)

// validateArtifactsPersisted checks that all required fields are present.
func validateArtifactsPersisted(event model.DocumentProcessingArtifactsPersisted) error {
	var missing []string
	if strings.TrimSpace(event.JobID) == "" {
		missing = append(missing, "job_id")
	}
	if strings.TrimSpace(event.DocumentID) == "" {
		missing = append(missing, "document_id")
	}
	if len(missing) > 0 {
		return port.NewValidationError(
			fmt.Sprintf("artifacts persisted event: missing fields: %s", strings.Join(missing, ", ")),
		)
	}
	return nil
}

// validateArtifactsPersistFailed checks that all required fields are present.
func validateArtifactsPersistFailed(event model.DocumentProcessingArtifactsPersistFailed) error {
	var missing []string
	if strings.TrimSpace(event.JobID) == "" {
		missing = append(missing, "job_id")
	}
	if strings.TrimSpace(event.DocumentID) == "" {
		missing = append(missing, "document_id")
	}
	if strings.TrimSpace(event.ErrorMessage) == "" {
		missing = append(missing, "error_message")
	}
	if len(missing) > 0 {
		return port.NewValidationError(
			fmt.Sprintf("artifacts persist failed event: missing fields: %s", strings.Join(missing, ", ")),
		)
	}
	return nil
}

// validateSemanticTreeProvided checks that all required fields are present.
// correlation_id is required because it is the routing key for the registry.
func validateSemanticTreeProvided(event model.SemanticTreeProvided) error {
	var missing []string
	if strings.TrimSpace(event.JobID) == "" {
		missing = append(missing, "job_id")
	}
	if strings.TrimSpace(event.DocumentID) == "" {
		missing = append(missing, "document_id")
	}
	if strings.TrimSpace(event.VersionID) == "" {
		missing = append(missing, "version_id")
	}
	if strings.TrimSpace(event.CorrelationID) == "" {
		missing = append(missing, "correlation_id")
	}
	if len(missing) > 0 {
		return port.NewValidationError(
			fmt.Sprintf("semantic tree provided event: missing fields: %s", strings.Join(missing, ", ")),
		)
	}
	return nil
}

// validateDiffPersisted checks that all required fields are present.
// correlation_id is required because it is the routing key for the registry.
func validateDiffPersisted(event model.DocumentVersionDiffPersisted) error {
	var missing []string
	if strings.TrimSpace(event.JobID) == "" {
		missing = append(missing, "job_id")
	}
	if strings.TrimSpace(event.DocumentID) == "" {
		missing = append(missing, "document_id")
	}
	if strings.TrimSpace(event.CorrelationID) == "" {
		missing = append(missing, "correlation_id")
	}
	if len(missing) > 0 {
		return port.NewValidationError(
			fmt.Sprintf("diff persisted event: missing fields: %s", strings.Join(missing, ", ")),
		)
	}
	return nil
}

// validateDiffPersistFailed checks that all required fields are present.
// correlation_id is required because it is the routing key for the registry.
func validateDiffPersistFailed(event model.DocumentVersionDiffPersistFailed) error {
	var missing []string
	if strings.TrimSpace(event.JobID) == "" {
		missing = append(missing, "job_id")
	}
	if strings.TrimSpace(event.DocumentID) == "" {
		missing = append(missing, "document_id")
	}
	if strings.TrimSpace(event.CorrelationID) == "" {
		missing = append(missing, "correlation_id")
	}
	if strings.TrimSpace(event.ErrorMessage) == "" {
		missing = append(missing, "error_message")
	}
	if len(missing) > 0 {
		return port.NewValidationError(
			fmt.Sprintf("diff persist failed event: missing fields: %s", strings.Join(missing, ", ")),
		)
	}
	return nil
}
