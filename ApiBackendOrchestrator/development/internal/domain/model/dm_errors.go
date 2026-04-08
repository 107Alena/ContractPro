package model

import (
	"encoding/json"
	"net/http"
)

// dmErrorBody represents the minimal error body returned by the DM service.
type dmErrorBody struct {
	Code string `json:"code"`
}

// MapDMError maps a DM HTTP response (status code + body) to the appropriate
// orchestrator ErrorCode. This function implements the downstream error mapping
// rules from error-handling.md section 2.3.
//
// The resourceHint parameter disambiguates 404 responses: "version" maps to
// VERSION_NOT_FOUND, "artifact" to ARTIFACT_NOT_FOUND, "diff" to DIFF_NOT_FOUND,
// anything else to DOCUMENT_NOT_FOUND.
//
// For 5xx responses, the function returns DM_UNAVAILABLE regardless of the
// response body. The caller is responsible for retry logic; this function
// only performs the final mapping after retries are exhausted.
func MapDMError(statusCode int, body []byte, resourceHint string) ErrorCode {
	switch {
	case statusCode >= 500:
		return ErrDMUnavailable

	case statusCode == http.StatusNotFound:
		return mapDMNotFound(resourceHint)

	case statusCode == http.StatusConflict:
		return mapDMConflict(body)

	default:
		// DM 400 or other 4xx: treated as internal error because it likely
		// indicates a bug in the orchestrator's request construction.
		// The architecture spec (section 2.3) mentions analyzing whether a
		// DM 400 was caused by user data, but for v1 we conservatively
		// treat all DM 400s as internal errors.
		return ErrInternalError
	}
}

func mapDMNotFound(resourceHint string) ErrorCode {
	switch resourceHint {
	case "version":
		return ErrVersionNotFound
	case "artifact":
		return ErrArtifactNotFound
	case "diff":
		return ErrDiffNotFound
	default:
		return ErrDocumentNotFound
	}
}

func mapDMConflict(body []byte) ErrorCode {
	var dmErr dmErrorBody
	if err := json.Unmarshal(body, &dmErr); err != nil {
		return ErrInternalError
	}

	switch dmErr.Code {
	case "DOCUMENT_ARCHIVED", "ARCHIVED":
		return ErrDocumentArchived
	case "DOCUMENT_DELETED", "DELETED":
		return ErrDocumentDeleted
	case "VERSION_STILL_PROCESSING", "PROCESSING":
		return ErrVersionStillProcessing
	case "RESULTS_NOT_READY":
		return ErrResultsNotReady
	default:
		return ErrInternalError
	}
}
