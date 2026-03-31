package consumer

import (
	"fmt"
	"strings"
	"unicode"

	"contractpro/document-processing/internal/domain/model"
	"contractpro/document-processing/internal/domain/port"
)

// sanitizeString removes dangerous characters from untrusted input:
//   - null bytes (\x00) and percent-encoded null bytes (%00)
//   - C0 control characters (0x01-0x08, 0x0B, 0x0C, 0x0E-0x1F) except \t, \n, \r
//   - C1 control characters (0x80-0x9F)
//   - path traversal sequences (../, ..\)
//   - leading/trailing whitespace
func sanitizeString(s string) string {
	// Remove percent-encoded null bytes first (before other processing).
	s = strings.ReplaceAll(s, "%00", "")

	// Remove path traversal sequences in a loop until stable,
	// to handle nested patterns like "....//".
	for {
		replaced := strings.ReplaceAll(s, "../", "")
		replaced = strings.ReplaceAll(replaced, "..\\", "")
		if replaced == s {
			break
		}
		s = replaced
	}

	// Remove null bytes and dangerous control characters.
	s = strings.Map(func(r rune) rune {
		// Remove null byte.
		if r == 0x00 {
			return -1
		}
		// Allow common whitespace: \t (0x09), \n (0x0A), \r (0x0D).
		if r == '\t' || r == '\n' || r == '\r' {
			return r
		}
		// Remove C0 control characters (0x01-0x1F).
		if r >= 0x01 && r <= 0x1F {
			return -1
		}
		// Remove C1 control characters (0x80-0x9F).
		if r >= 0x80 && r <= 0x9F && !unicode.IsPrint(r) {
			return -1
		}
		return r
	}, s)

	return strings.TrimSpace(s)
}

// sanitizeProcessDocumentCommand sanitizes all string fields in-place,
// removing dangerous characters at the trust boundary.
func sanitizeProcessDocumentCommand(cmd *model.ProcessDocumentCommand) {
	cmd.JobID = sanitizeString(cmd.JobID)
	cmd.DocumentID = sanitizeString(cmd.DocumentID)
	cmd.VersionID = sanitizeString(cmd.VersionID)
	cmd.FileURL = sanitizeString(cmd.FileURL)
	cmd.OrgID = sanitizeString(cmd.OrgID)
	cmd.UserID = sanitizeString(cmd.UserID)
	cmd.FileName = sanitizeString(cmd.FileName)
	cmd.MimeType = sanitizeString(cmd.MimeType)
	cmd.Checksum = sanitizeString(cmd.Checksum)
}

// sanitizeCompareVersionsCommand sanitizes all string fields in-place,
// removing dangerous characters at the trust boundary.
func sanitizeCompareVersionsCommand(cmd *model.CompareVersionsCommand) {
	cmd.JobID = sanitizeString(cmd.JobID)
	cmd.DocumentID = sanitizeString(cmd.DocumentID)
	cmd.BaseVersionID = sanitizeString(cmd.BaseVersionID)
	cmd.TargetVersionID = sanitizeString(cmd.TargetVersionID)
	cmd.OrgID = sanitizeString(cmd.OrgID)
	cmd.UserID = sanitizeString(cmd.UserID)
}

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
	if strings.TrimSpace(cmd.VersionID) == "" {
		missing = append(missing, "version_id")
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
