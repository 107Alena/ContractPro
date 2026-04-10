package model

import "testing"

func TestMapUOMError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       []byte
		expected   ErrorCode
	}{
		{
			name:       "5xx returns AUTH_SERVICE_UNAVAILABLE",
			statusCode: 500,
			body:       []byte(`{"error": "internal"}`),
			expected:   ErrAuthServiceUnavailable,
		},
		{
			name:       "502 returns AUTH_SERVICE_UNAVAILABLE",
			statusCode: 502,
			body:       nil,
			expected:   ErrAuthServiceUnavailable,
		},
		{
			name:       "503 returns AUTH_SERVICE_UNAVAILABLE",
			statusCode: 503,
			body:       []byte(`service unavailable`),
			expected:   ErrAuthServiceUnavailable,
		},
		{
			name:       "401 INVALID_CREDENTIALS",
			statusCode: 401,
			body:       []byte(`{"code": "INVALID_CREDENTIALS"}`),
			expected:   ErrInvalidCredentials,
		},
		{
			name:       "401 TOKEN_EXPIRED",
			statusCode: 401,
			body:       []byte(`{"code": "TOKEN_EXPIRED"}`),
			expected:   ErrRefreshTokenExpired,
		},
		{
			name:       "401 TOKEN_REVOKED",
			statusCode: 401,
			body:       []byte(`{"code": "TOKEN_REVOKED"}`),
			expected:   ErrTokenRevoked,
		},
		{
			name:       "401 unknown code defaults to INVALID_CREDENTIALS",
			statusCode: 401,
			body:       []byte(`{"code": "SOMETHING_ELSE"}`),
			expected:   ErrInvalidCredentials,
		},
		{
			name:       "401 invalid JSON defaults to INVALID_CREDENTIALS",
			statusCode: 401,
			body:       []byte(`not json`),
			expected:   ErrInvalidCredentials,
		},
		{
			name:       "401 empty body defaults to INVALID_CREDENTIALS",
			statusCode: 401,
			body:       nil,
			expected:   ErrInvalidCredentials,
		},
		{
			name:       "400 returns VALIDATION_ERROR",
			statusCode: 400,
			body:       []byte(`{"code": "VALIDATION_ERROR"}`),
			expected:   ErrValidationError,
		},
		{
			name:       "403 returns VALIDATION_ERROR",
			statusCode: 403,
			body:       nil,
			expected:   ErrValidationError,
		},
		{
			name:       "404 returns VALIDATION_ERROR",
			statusCode: 404,
			body:       nil,
			expected:   ErrValidationError,
		},
		{
			name:       "409 returns VALIDATION_ERROR",
			statusCode: 409,
			body:       nil,
			expected:   ErrValidationError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MapUOMError(tt.statusCode, tt.body)
			if got != tt.expected {
				t.Errorf("MapUOMError(%d, %q) = %q, want %q", tt.statusCode, string(tt.body), got, tt.expected)
			}
		})
	}
}

func TestNewUOMErrorCodes_InCatalog(t *testing.T) {
	codes := []ErrorCode{
		ErrAuthServiceUnavailable,
		ErrInvalidCredentials,
		ErrTokenRevoked,
		ErrRefreshTokenExpired,
	}

	for _, code := range codes {
		t.Run(string(code), func(t *testing.T) {
			entry, ok := LookupError(code)
			if !ok {
				t.Errorf("ErrorCode %q not found in catalog", code)
			}
			if entry.HTTPStatus == 0 {
				t.Errorf("ErrorCode %q has no HTTP status", code)
			}
			if entry.Message == "" {
				t.Errorf("ErrorCode %q has no message", code)
			}
		})
	}
}
