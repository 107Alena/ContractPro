package consumer

import (
	"fmt"
	"strings"

	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
)

// validateProcessDocumentCommand checks that all required fields are present
// in a ProcessDocumentCommand. Returns a single aggregated validation error
// listing every missing field, or nil if the command is valid.
func validateProcessDocumentCommand(cmd model.ProcessDocumentCommand) error {
	var missing []string

	if strings.TrimSpace(cmd.JobID) == "" {
		missing = append(missing, "job_id")
	}
	if strings.TrimSpace(cmd.DocumentID) == "" {
		missing = append(missing, "document_id")
	}
	if strings.TrimSpace(cmd.FileURL) == "" {
		missing = append(missing, "file_url")
	}

	if len(missing) > 0 {
		return port.NewValidationError(
			fmt.Sprintf("command validation failed: missing fields: %s", strings.Join(missing, ", ")),
		)
	}
	return nil
}

// validateCompareVersionsCommand checks that all required fields are present
// in a CompareVersionsCommand. Returns a single aggregated validation error
// listing every missing field, or nil if the command is valid.
func validateCompareVersionsCommand(cmd model.CompareVersionsCommand) error {
	var missing []string

	if strings.TrimSpace(cmd.JobID) == "" {
		missing = append(missing, "job_id")
	}
	if strings.TrimSpace(cmd.DocumentID) == "" {
		missing = append(missing, "document_id")
	}
	if strings.TrimSpace(cmd.BaseVersionID) == "" {
		missing = append(missing, "base_version_id")
	}
	if strings.TrimSpace(cmd.TargetVersionID) == "" {
		missing = append(missing, "target_version_id")
	}

	if len(missing) > 0 {
		return port.NewValidationError(
			fmt.Sprintf("command validation failed: missing fields: %s", strings.Join(missing, ", ")),
		)
	}
	return nil
}
