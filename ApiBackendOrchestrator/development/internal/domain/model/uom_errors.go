package model

import (
	"encoding/json"
	"net/http"
)

// uomErrorBody represents the minimal error body returned by the UOM service.
type uomErrorBody struct {
	Code string `json:"code"`
}

// MapUOMError maps a UOM HTTP response (status code + body) to the appropriate
// orchestrator ErrorCode. This function implements the downstream error mapping
// rules from error-handling.md section 4.6.
//
// Mapping:
//   - 5xx → AUTH_SERVICE_UNAVAILABLE (UOM is down after retries)
//   - 401 → INVALID_CREDENTIALS, TOKEN_REVOKED, or REFRESH_TOKEN_EXPIRED
//     (parsed from UOM error body code)
//   - Other 4xx → VALIDATION_ERROR (bad request from client)
func MapUOMError(statusCode int, body []byte) ErrorCode {
	switch {
	case statusCode >= 500:
		return ErrAuthServiceUnavailable

	case statusCode == http.StatusUnauthorized:
		return mapUOM401(body)

	default:
		return ErrValidationError
	}
}

func mapUOM401(body []byte) ErrorCode {
	var uomErr uomErrorBody
	if err := json.Unmarshal(body, &uomErr); err != nil {
		return ErrInvalidCredentials
	}

	switch uomErr.Code {
	case "INVALID_CREDENTIALS":
		return ErrInvalidCredentials
	case "TOKEN_EXPIRED":
		return ErrRefreshTokenExpired
	case "TOKEN_REVOKED":
		return ErrTokenRevoked
	default:
		return ErrInvalidCredentials
	}
}
