package model

import "testing"

func TestMapDMError(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		body         []byte
		resourceHint string
		want         ErrorCode
	}{
		// 5xx → DM_UNAVAILABLE
		{"DM 500", 500, nil, "", ErrDMUnavailable},
		{"DM 502", 502, nil, "", ErrDMUnavailable},
		{"DM 503", 503, nil, "", ErrDMUnavailable},
		{"DM 500 ignores body", 500, []byte(`{"code":"ANYTHING"}`), "", ErrDMUnavailable},
		{"DM 500 ignores hint", 500, nil, "version", ErrDMUnavailable},

		// 404 → based on resourceHint
		{"404 default", 404, nil, "", ErrDocumentNotFound},
		{"404 document", 404, nil, "document", ErrDocumentNotFound},
		{"404 version", 404, nil, "version", ErrVersionNotFound},
		{"404 artifact", 404, nil, "artifact", ErrArtifactNotFound},
		{"404 diff", 404, nil, "diff", ErrDiffNotFound},

		// 409 → based on DM code in body
		{"409 DOCUMENT_ARCHIVED", 409, []byte(`{"code":"DOCUMENT_ARCHIVED"}`), "", ErrDocumentArchived},
		{"409 ARCHIVED", 409, []byte(`{"code":"ARCHIVED"}`), "", ErrDocumentArchived},
		{"409 DOCUMENT_DELETED", 409, []byte(`{"code":"DOCUMENT_DELETED"}`), "", ErrDocumentDeleted},
		{"409 DELETED", 409, []byte(`{"code":"DELETED"}`), "", ErrDocumentDeleted},
		{"409 VERSION_STILL_PROCESSING", 409, []byte(`{"code":"VERSION_STILL_PROCESSING"}`), "", ErrVersionStillProcessing},
		{"409 PROCESSING", 409, []byte(`{"code":"PROCESSING"}`), "", ErrVersionStillProcessing},
		{"409 RESULTS_NOT_READY", 409, []byte(`{"code":"RESULTS_NOT_READY"}`), "", ErrResultsNotReady},
		{"409 unknown code", 409, []byte(`{"code":"UNKNOWN"}`), "", ErrInternalError},
		{"409 invalid json", 409, []byte(`not json`), "", ErrInternalError},
		{"409 empty body", 409, nil, "", ErrInternalError},

		// Other 4xx → INTERNAL_ERROR
		{"400", 400, nil, "", ErrInternalError},
		{"418 unexpected", 418, nil, "", ErrInternalError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MapDMError(tt.statusCode, tt.body, tt.resourceHint)
			if got != tt.want {
				t.Errorf("MapDMError(%d, %q, %q) = %q, want %q",
					tt.statusCode, tt.body, tt.resourceHint, got, tt.want)
			}
		})
	}
}
