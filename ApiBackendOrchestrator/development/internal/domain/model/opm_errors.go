package model

import "net/http"

// MapOPMError maps an OPM HTTP response (status code + body) to the appropriate
// orchestrator ErrorCode. This function implements the downstream error mapping
// for the Organization Policy Management service.
//
// The resourceHint parameter disambiguates 404 responses: "policy" maps to
// POLICY_NOT_FOUND, "checklist" to CHECKLIST_NOT_FOUND.
//
// Mapping:
//   - 5xx → OPM_UNAVAILABLE (OPM is down after retries)
//   - 404 → POLICY_NOT_FOUND or CHECKLIST_NOT_FOUND (based on resourceHint)
//   - 400 → VALIDATION_ERROR (user sent invalid data through the proxy)
//   - Other 4xx → INTERNAL_ERROR (unexpected; orchestrator handles auth/RBAC)
func MapOPMError(statusCode int, body []byte, resourceHint string) ErrorCode {
	switch {
	case statusCode >= 500:
		return ErrOPMUnavailable

	case statusCode == http.StatusNotFound:
		return mapOPMNotFound(resourceHint)

	case statusCode == http.StatusBadRequest:
		return ErrValidationError

	default:
		// Other 4xx (401, 403, 409, etc.) from OPM are unexpected — the
		// orchestrator already handles auth and RBAC. Treat as internal error.
		return ErrInternalError
	}
}

func mapOPMNotFound(resourceHint string) ErrorCode {
	switch resourceHint {
	case "policy":
		return ErrPolicyNotFound
	case "checklist":
		return ErrChecklistNotFound
	default:
		return ErrInternalError
	}
}
