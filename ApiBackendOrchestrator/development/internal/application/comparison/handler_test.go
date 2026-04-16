package comparison

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"contractpro/api-orchestrator/internal/egress/commandpub"
	"contractpro/api-orchestrator/internal/egress/dmclient"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"
)

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

type mockDM struct {
	getVerFn   func(ctx context.Context, documentID, versionID string) (*dmclient.DocumentVersionWithArtifacts, error)
	getDiffFn  func(ctx context.Context, documentID, baseVersionID, targetVersionID string) (*dmclient.VersionDiff, error)
	getVerCalls  []getVerCall
	getDiffCalls []getDiffCall
}

type getVerCall struct {
	DocumentID string
	VersionID  string
}

type getDiffCall struct {
	DocumentID      string
	BaseVersionID   string
	TargetVersionID string
}

func (m *mockDM) GetVersion(ctx context.Context, documentID, versionID string) (*dmclient.DocumentVersionWithArtifacts, error) {
	m.getVerCalls = append(m.getVerCalls, getVerCall{DocumentID: documentID, VersionID: versionID})
	if m.getVerFn != nil {
		return m.getVerFn(ctx, documentID, versionID)
	}
	return stubVersionWithArtifacts("FULLY_READY"), nil
}

func (m *mockDM) GetDiff(ctx context.Context, documentID, baseVersionID, targetVersionID string) (*dmclient.VersionDiff, error) {
	m.getDiffCalls = append(m.getDiffCalls, getDiffCall{DocumentID: documentID, BaseVersionID: baseVersionID, TargetVersionID: targetVersionID})
	if m.getDiffFn != nil {
		return m.getDiffFn(ctx, documentID, baseVersionID, targetVersionID)
	}
	return stubDiff(), nil
}

type mockCmdPub struct {
	publishFn func(ctx context.Context, cmd commandpub.CompareVersionsCommand) error
	calls     []commandpub.CompareVersionsCommand
}

func (m *mockCmdPub) PublishCompareVersions(ctx context.Context, cmd commandpub.CompareVersionsCommand) error {
	m.calls = append(m.calls, cmd)
	if m.publishFn != nil {
		return m.publishFn(ctx, cmd)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Compile-time interface checks for mocks
// ---------------------------------------------------------------------------

var (
	_ DMClient         = (*mockDM)(nil)
	_ CommandPublisher = (*mockCmdPub)(nil)
)

// ---------------------------------------------------------------------------
// Test constants and helpers
// ---------------------------------------------------------------------------

const (
	testContractID  = "550e8400-e29b-41d4-a716-446655440000"
	testBaseVerID   = "660e8400-e29b-41d4-a716-446655440000"
	testTargetVerID = "770e8400-e29b-41d4-a716-446655440000"
)

func newTestHandler(dm *mockDM, pub *mockCmdPub) *Handler {
	h := NewHandler(dm, pub, logger.NewLogger("error"))
	counter := 0
	h.uuidGen = func() string {
		counter++
		return fmt.Sprintf("uuid-%03d", counter)
	}
	return h
}

func withAuthContext(r *http.Request) *http.Request {
	ctx := auth.WithAuthContext(r.Context(), auth.AuthContext{
		UserID:         "user-001",
		OrganizationID: "org-001",
		Role:           auth.RoleLawyer,
		TokenID:        "token-001",
	})
	return r.WithContext(ctx)
}

func withChiParams(r *http.Request, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func parseJSON(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v\nbody: %s", err, rr.Body.String())
	}
	return result
}

func assertValidationField(t *testing.T, result map[string]any, fieldName string) {
	t.Helper()
	details, ok := result["details"].(map[string]any)
	if !ok {
		t.Fatalf("details is not an object, got %T: %v", result["details"], result["details"])
	}
	fields, ok := details["fields"].([]any)
	if !ok || len(fields) == 0 {
		t.Fatalf("details.fields is empty or not an array")
	}
	for _, f := range fields {
		fm, _ := f.(map[string]any)
		if fm["field"] == fieldName {
			return
		}
	}
	t.Errorf("expected details.fields to contain field %q, got %v", fieldName, fields)
}

func stubVersionWithArtifacts(artifactStatus string) *dmclient.DocumentVersionWithArtifacts {
	return &dmclient.DocumentVersionWithArtifacts{
		DocumentVersion: dmclient.DocumentVersion{
			VersionID:          testBaseVerID,
			DocumentID:         testContractID,
			VersionNumber:      1,
			OriginType:         "UPLOAD",
			SourceFileKey:      "uploads/org-001/key.pdf",
			SourceFileName:     "contract.pdf",
			SourceFileSize:     1024000,
			SourceFileChecksum: "abc123",
			ArtifactStatus:     artifactStatus,
			CreatedByUserID:    "user-001",
			CreatedAt:          time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
		},
		Artifacts: []dmclient.ArtifactDescriptor{},
	}
}

func stubDiff() *dmclient.VersionDiff {
	return &dmclient.VersionDiff{
		DiffID:              "diff-001",
		DocumentID:          testContractID,
		BaseVersionID:       testBaseVerID,
		TargetVersionID:     testTargetVerID,
		TextDiffCount:       2,
		StructuralDiffCount: 1,
		TextDiffs: []dmclient.TextDiff{
			{Type: "modified", Path: "1.1", OldText: strPtr("old"), NewText: strPtr("new")},
			{Type: "added", Path: "2.1", NewText: strPtr("added text")},
		},
		StructuralDiffs: []dmclient.StructuralDiff{
			{Type: "added", NodeID: "node-1", NewValue: json.RawMessage(`{"title":"Новый раздел"}`)},
		},
		CreatedAt: time.Date(2026, 1, 20, 14, 0, 0, 0, time.UTC),
	}
}

func strPtr(s string) *string { return &s }

func dmHTTPError(op string, code int, body string) *dmclient.DMError {
	return &dmclient.DMError{
		Operation:  op,
		StatusCode: code,
		Body:       []byte(body),
		Retryable:  code >= 500,
	}
}

func compareBody(baseID, targetID string) *bytes.Buffer {
	b, _ := json.Marshal(CompareRequest{
		BaseVersionID:   baseID,
		TargetVersionID: targetID,
	})
	return bytes.NewBuffer(b)
}

// ---------------------------------------------------------------------------
// HandleCompare tests
// ---------------------------------------------------------------------------

func TestHandleCompare_Success(t *testing.T) {
	dm := &mockDM{}
	pub := &mockCmdPub{}
	h := newTestHandler(dm, pub)

	body := compareBody(testBaseVerID, testTargetVerID)
	r := httptest.NewRequest(http.MethodPost, "/compare", body)
	r.Header.Set("Content-Type", "application/json")
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	rr := httptest.NewRecorder()
	h.HandleCompare().ServeHTTP(rr, r)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rr.Code, rr.Body.String())
	}

	result := parseJSON(t, rr)
	if result["contract_id"] != testContractID {
		t.Errorf("contract_id = %v, want %v", result["contract_id"], testContractID)
	}
	if result["job_id"] != "uuid-002" {
		t.Errorf("job_id = %v, want uuid-002", result["job_id"])
	}
	if result["base_version_id"] != testBaseVerID {
		t.Errorf("base_version_id = %v", result["base_version_id"])
	}
	if result["target_version_id"] != testTargetVerID {
		t.Errorf("target_version_id = %v", result["target_version_id"])
	}
	if result["status"] != "COMPARISON_QUEUED" {
		t.Errorf("status = %v", result["status"])
	}
	if result["message"] != "Сравнение версий запущено." {
		t.Errorf("message = %v", result["message"])
	}

	// Verify correlation_id header.
	if corrID := rr.Header().Get("X-Correlation-Id"); corrID != "uuid-001" {
		t.Errorf("X-Correlation-Id = %q, want uuid-001", corrID)
	}

	// Verify DM calls: base then target.
	if len(dm.getVerCalls) != 2 {
		t.Fatalf("expected 2 GetVersion calls, got %d", len(dm.getVerCalls))
	}
	if dm.getVerCalls[0].VersionID != testBaseVerID {
		t.Errorf("first GetVersion should be base, got %s", dm.getVerCalls[0].VersionID)
	}
	if dm.getVerCalls[1].VersionID != testTargetVerID {
		t.Errorf("second GetVersion should be target, got %s", dm.getVerCalls[1].VersionID)
	}

	// Verify published command.
	if len(pub.calls) != 1 {
		t.Fatalf("expected 1 publish call, got %d", len(pub.calls))
	}
	cmd := pub.calls[0]
	if cmd.JobID != "uuid-002" {
		t.Errorf("cmd.JobID = %q", cmd.JobID)
	}
	if cmd.DocumentID != testContractID {
		t.Errorf("cmd.DocumentID = %q", cmd.DocumentID)
	}
	if cmd.OrganizationID != "org-001" {
		t.Errorf("cmd.OrganizationID = %q", cmd.OrganizationID)
	}
	if cmd.RequestedByUserID != "user-001" {
		t.Errorf("cmd.RequestedByUserID = %q", cmd.RequestedByUserID)
	}
	if cmd.BaseVersionID != testBaseVerID {
		t.Errorf("cmd.BaseVersionID = %q", cmd.BaseVersionID)
	}
	if cmd.TargetVersionID != testTargetVerID {
		t.Errorf("cmd.TargetVersionID = %q", cmd.TargetVersionID)
	}
}

func TestHandleCompare_NoAuth(t *testing.T) {
	h := newTestHandler(&mockDM{}, &mockCmdPub{})

	body := compareBody(testBaseVerID, testTargetVerID)
	r := httptest.NewRequest(http.MethodPost, "/compare", body)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	rr := httptest.NewRecorder()
	h.HandleCompare().ServeHTTP(rr, r)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestHandleCompare_InvalidContractID(t *testing.T) {
	h := newTestHandler(&mockDM{}, &mockCmdPub{})

	body := compareBody(testBaseVerID, testTargetVerID)
	r := httptest.NewRequest(http.MethodPost, "/compare", body)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": "not-a-uuid"})

	rr := httptest.NewRecorder()
	h.HandleCompare().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
	result := parseJSON(t, rr)
	if result["error_code"] != "VALIDATION_ERROR" {
		t.Errorf("error_code = %v", result["error_code"])
	}
}

func TestHandleCompare_MalformedJSON(t *testing.T) {
	h := newTestHandler(&mockDM{}, &mockCmdPub{})

	r := httptest.NewRequest(http.MethodPost, "/compare", strings.NewReader("{invalid"))
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	rr := httptest.NewRecorder()
	h.HandleCompare().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestHandleCompare_EmptyBaseVersionID(t *testing.T) {
	h := newTestHandler(&mockDM{}, &mockCmdPub{})

	body := compareBody("", testTargetVerID)
	r := httptest.NewRequest(http.MethodPost, "/compare", body)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	rr := httptest.NewRecorder()
	h.HandleCompare().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
	result := parseJSON(t, rr)
	assertValidationField(t, result, "base_version_id")
}

func TestHandleCompare_EmptyTargetVersionID(t *testing.T) {
	h := newTestHandler(&mockDM{}, &mockCmdPub{})

	body := compareBody(testBaseVerID, "")
	r := httptest.NewRequest(http.MethodPost, "/compare", body)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	rr := httptest.NewRecorder()
	h.HandleCompare().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
	result := parseJSON(t, rr)
	assertValidationField(t, result, "target_version_id")
}

func TestHandleCompare_InvalidUUIDBaseVersion(t *testing.T) {
	h := newTestHandler(&mockDM{}, &mockCmdPub{})

	body := compareBody("not-uuid", testTargetVerID)
	r := httptest.NewRequest(http.MethodPost, "/compare", body)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	rr := httptest.NewRecorder()
	h.HandleCompare().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestHandleCompare_SameVersionIDs(t *testing.T) {
	h := newTestHandler(&mockDM{}, &mockCmdPub{})

	body := compareBody(testBaseVerID, testBaseVerID)
	r := httptest.NewRequest(http.MethodPost, "/compare", body)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	rr := httptest.NewRecorder()
	h.HandleCompare().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
	result := parseJSON(t, rr)
	assertValidationField(t, result, "target_version_id")
}

func TestHandleCompare_BaseVersionNotFound(t *testing.T) {
	dm := &mockDM{
		getVerFn: func(_ context.Context, _, versionID string) (*dmclient.DocumentVersionWithArtifacts, error) {
			if versionID == testBaseVerID {
				return nil, dmHTTPError("GetVersion", 404, `{"error_code":"VERSION_NOT_FOUND"}`)
			}
			return stubVersionWithArtifacts("FULLY_READY"), nil
		},
	}
	h := newTestHandler(dm, &mockCmdPub{})

	body := compareBody(testBaseVerID, testTargetVerID)
	r := httptest.NewRequest(http.MethodPost, "/compare", body)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	rr := httptest.NewRecorder()
	h.HandleCompare().ServeHTTP(rr, r)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", rr.Code, rr.Body.String())
	}

	// Target should not be checked after base fails.
	if len(dm.getVerCalls) != 1 {
		t.Errorf("expected only 1 GetVersion call (base), got %d", len(dm.getVerCalls))
	}
}

func TestHandleCompare_TargetVersionNotFound(t *testing.T) {
	dm := &mockDM{
		getVerFn: func(_ context.Context, _, versionID string) (*dmclient.DocumentVersionWithArtifacts, error) {
			if versionID == testTargetVerID {
				return nil, dmHTTPError("GetVersion", 404, `{"error_code":"VERSION_NOT_FOUND"}`)
			}
			return stubVersionWithArtifacts("FULLY_READY"), nil
		},
	}
	h := newTestHandler(dm, &mockCmdPub{})

	body := compareBody(testBaseVerID, testTargetVerID)
	r := httptest.NewRequest(http.MethodPost, "/compare", body)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	rr := httptest.NewRecorder()
	h.HandleCompare().ServeHTTP(rr, r)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}

	// Both base and target should be checked.
	if len(dm.getVerCalls) != 2 {
		t.Errorf("expected 2 GetVersion calls, got %d", len(dm.getVerCalls))
	}
}

func TestHandleCompare_BaseVersionStillProcessing(t *testing.T) {
	processingStatuses := []string{
		"PENDING_UPLOAD", "PENDING_PROCESSING", "PROCESSING_IN_PROGRESS",
		"ARTIFACTS_READY", "ANALYSIS_IN_PROGRESS", "ANALYSIS_READY",
		"REPORTS_IN_PROGRESS",
	}

	for _, status := range processingStatuses {
		t.Run(status, func(t *testing.T) {
			dm := &mockDM{
				getVerFn: func(_ context.Context, _, versionID string) (*dmclient.DocumentVersionWithArtifacts, error) {
					if versionID == testBaseVerID {
						return stubVersionWithArtifacts(status), nil
					}
					return stubVersionWithArtifacts("FULLY_READY"), nil
				},
			}
			h := newTestHandler(dm, &mockCmdPub{})

			body := compareBody(testBaseVerID, testTargetVerID)
			r := httptest.NewRequest(http.MethodPost, "/compare", body)
			r = withAuthContext(r)
			r = withChiParams(r, map[string]string{"contract_id": testContractID})

			rr := httptest.NewRecorder()
			h.HandleCompare().ServeHTTP(rr, r)

			if rr.Code != http.StatusConflict {
				t.Errorf("status %s: expected 409, got %d", status, rr.Code)
			}
			result := parseJSON(t, rr)
			if result["error_code"] != "VERSION_STILL_PROCESSING" {
				t.Errorf("error_code = %v", result["error_code"])
			}
		})
	}
}

func TestHandleCompare_TargetVersionStillProcessing(t *testing.T) {
	dm := &mockDM{
		getVerFn: func(_ context.Context, _, versionID string) (*dmclient.DocumentVersionWithArtifacts, error) {
			if versionID == testTargetVerID {
				return stubVersionWithArtifacts("PROCESSING_IN_PROGRESS"), nil
			}
			return stubVersionWithArtifacts("FULLY_READY"), nil
		},
	}
	h := newTestHandler(dm, &mockCmdPub{})

	body := compareBody(testBaseVerID, testTargetVerID)
	r := httptest.NewRequest(http.MethodPost, "/compare", body)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	rr := httptest.NewRecorder()
	h.HandleCompare().ServeHTTP(rr, r)

	if rr.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", rr.Code)
	}
	result := parseJSON(t, rr)
	details, _ := result["details"].(string)
	if !strings.Contains(details, "Целевая") {
		t.Errorf("expected details to mention target version, got %q", details)
	}
}

func TestHandleCompare_TerminalStatusesAllowed(t *testing.T) {
	terminalStatuses := []string{"FULLY_READY", "PARTIALLY_AVAILABLE", "PROCESSING_FAILED", "REJECTED"}

	for _, status := range terminalStatuses {
		t.Run(status, func(t *testing.T) {
			dm := &mockDM{
				getVerFn: func(_ context.Context, _, _ string) (*dmclient.DocumentVersionWithArtifacts, error) {
					return stubVersionWithArtifacts(status), nil
				},
			}
			pub := &mockCmdPub{}
			h := newTestHandler(dm, pub)

			body := compareBody(testBaseVerID, testTargetVerID)
			r := httptest.NewRequest(http.MethodPost, "/compare", body)
			r = withAuthContext(r)
			r = withChiParams(r, map[string]string{"contract_id": testContractID})

			rr := httptest.NewRecorder()
			h.HandleCompare().ServeHTTP(rr, r)

			if rr.Code != http.StatusAccepted {
				t.Errorf("status %s: expected 202, got %d: %s", status, rr.Code, rr.Body.String())
			}
			if len(pub.calls) != 1 {
				t.Errorf("expected 1 publish, got %d", len(pub.calls))
			}
		})
	}
}

func TestHandleCompare_DM5xx(t *testing.T) {
	dm := &mockDM{
		getVerFn: func(_ context.Context, _, _ string) (*dmclient.DocumentVersionWithArtifacts, error) {
			return nil, dmHTTPError("GetVersion", 500, "internal error")
		},
	}
	h := newTestHandler(dm, &mockCmdPub{})

	body := compareBody(testBaseVerID, testTargetVerID)
	r := httptest.NewRequest(http.MethodPost, "/compare", body)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	rr := httptest.NewRecorder()
	h.HandleCompare().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", rr.Code)
	}
}

func TestHandleCompare_DMCircuitOpen(t *testing.T) {
	dm := &mockDM{
		getVerFn: func(_ context.Context, _, _ string) (*dmclient.DocumentVersionWithArtifacts, error) {
			return nil, fmt.Errorf("wrapped: %w", dmclient.ErrCircuitOpen)
		},
	}
	h := newTestHandler(dm, &mockCmdPub{})

	body := compareBody(testBaseVerID, testTargetVerID)
	r := httptest.NewRequest(http.MethodPost, "/compare", body)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	rr := httptest.NewRecorder()
	h.HandleCompare().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", rr.Code)
	}
}

func TestHandleCompare_BrokerFailure(t *testing.T) {
	pub := &mockCmdPub{
		publishFn: func(_ context.Context, _ commandpub.CompareVersionsCommand) error {
			return errors.New("broker down")
		},
	}
	h := newTestHandler(&mockDM{}, pub)

	body := compareBody(testBaseVerID, testTargetVerID)
	r := httptest.NewRequest(http.MethodPost, "/compare", body)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	rr := httptest.NewRecorder()
	h.HandleCompare().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", rr.Code)
	}
	result := parseJSON(t, rr)
	if result["error_code"] != "BROKER_UNAVAILABLE" {
		t.Errorf("error_code = %v", result["error_code"])
	}
}

func TestHandleCompare_PublishedCommandFields(t *testing.T) {
	pub := &mockCmdPub{}
	h := newTestHandler(&mockDM{}, pub)

	body := compareBody(testBaseVerID, testTargetVerID)
	r := httptest.NewRequest(http.MethodPost, "/compare", body)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	rr := httptest.NewRecorder()
	h.HandleCompare().ServeHTTP(rr, r)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rr.Code)
	}

	if len(pub.calls) != 1 {
		t.Fatalf("expected 1 publish, got %d", len(pub.calls))
	}

	cmd := pub.calls[0]
	if cmd.JobID != "uuid-002" {
		t.Errorf("JobID = %q, want uuid-002", cmd.JobID)
	}
	if cmd.DocumentID != testContractID {
		t.Errorf("DocumentID = %q", cmd.DocumentID)
	}
	if cmd.OrganizationID != "org-001" {
		t.Errorf("OrganizationID = %q", cmd.OrganizationID)
	}
	if cmd.RequestedByUserID != "user-001" {
		t.Errorf("RequestedByUserID = %q", cmd.RequestedByUserID)
	}
	if cmd.BaseVersionID != testBaseVerID {
		t.Errorf("BaseVersionID = %q", cmd.BaseVersionID)
	}
	if cmd.TargetVersionID != testTargetVerID {
		t.Errorf("TargetVersionID = %q", cmd.TargetVersionID)
	}
}

func TestHandleCompare_ResponseContentType(t *testing.T) {
	h := newTestHandler(&mockDM{}, &mockCmdPub{})

	body := compareBody(testBaseVerID, testTargetVerID)
	r := httptest.NewRequest(http.MethodPost, "/compare", body)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{"contract_id": testContractID})

	rr := httptest.NewRecorder()
	h.HandleCompare().ServeHTTP(rr, r)

	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
}

// ---------------------------------------------------------------------------
// HandleGetDiff tests
// ---------------------------------------------------------------------------

func TestHandleGetDiff_Success(t *testing.T) {
	dm := &mockDM{}
	h := newTestHandler(dm, &mockCmdPub{})

	r := httptest.NewRequest(http.MethodGet, "/diff", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{
		"contract_id":       testContractID,
		"base_version_id":   testBaseVerID,
		"target_version_id": testTargetVerID,
	})

	rr := httptest.NewRecorder()
	h.HandleGetDiff().ServeHTTP(rr, r)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	result := parseJSON(t, rr)
	if result["diff_id"] != "diff-001" {
		t.Errorf("diff_id = %v", result["diff_id"])
	}
	if result["document_id"] != testContractID {
		t.Errorf("document_id = %v", result["document_id"])
	}
	if result["base_version_id"] != testBaseVerID {
		t.Errorf("base_version_id = %v", result["base_version_id"])
	}
	if result["target_version_id"] != testTargetVerID {
		t.Errorf("target_version_id = %v", result["target_version_id"])
	}

	textCount, ok := result["text_diff_count"].(float64)
	if !ok || int(textCount) != 2 {
		t.Errorf("text_diff_count = %v", result["text_diff_count"])
	}

	structCount, ok := result["structural_diff_count"].(float64)
	if !ok || int(structCount) != 1 {
		t.Errorf("structural_diff_count = %v", result["structural_diff_count"])
	}

	// Verify DM call parameters.
	if len(dm.getDiffCalls) != 1 {
		t.Fatalf("expected 1 GetDiff call, got %d", len(dm.getDiffCalls))
	}
	call := dm.getDiffCalls[0]
	if call.DocumentID != testContractID || call.BaseVersionID != testBaseVerID || call.TargetVersionID != testTargetVerID {
		t.Errorf("GetDiff called with %+v", call)
	}
}

func TestHandleGetDiff_NoAuth(t *testing.T) {
	h := newTestHandler(&mockDM{}, &mockCmdPub{})

	r := httptest.NewRequest(http.MethodGet, "/diff", nil)
	r = withChiParams(r, map[string]string{
		"contract_id":       testContractID,
		"base_version_id":   testBaseVerID,
		"target_version_id": testTargetVerID,
	})

	rr := httptest.NewRecorder()
	h.HandleGetDiff().ServeHTTP(rr, r)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestHandleGetDiff_InvalidContractID(t *testing.T) {
	h := newTestHandler(&mockDM{}, &mockCmdPub{})

	r := httptest.NewRequest(http.MethodGet, "/diff", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{
		"contract_id":       "bad",
		"base_version_id":   testBaseVerID,
		"target_version_id": testTargetVerID,
	})

	rr := httptest.NewRecorder()
	h.HandleGetDiff().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestHandleGetDiff_InvalidBaseVersionID(t *testing.T) {
	h := newTestHandler(&mockDM{}, &mockCmdPub{})

	r := httptest.NewRequest(http.MethodGet, "/diff", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{
		"contract_id":       testContractID,
		"base_version_id":   "bad-uuid",
		"target_version_id": testTargetVerID,
	})

	rr := httptest.NewRecorder()
	h.HandleGetDiff().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestHandleGetDiff_InvalidTargetVersionID(t *testing.T) {
	h := newTestHandler(&mockDM{}, &mockCmdPub{})

	r := httptest.NewRequest(http.MethodGet, "/diff", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{
		"contract_id":       testContractID,
		"base_version_id":   testBaseVerID,
		"target_version_id": "xyz",
	})

	rr := httptest.NewRecorder()
	h.HandleGetDiff().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestHandleGetDiff_NotFound(t *testing.T) {
	dm := &mockDM{
		getDiffFn: func(_ context.Context, _, _, _ string) (*dmclient.VersionDiff, error) {
			return nil, dmHTTPError("GetDiff", 404, `{"error_code":"DIFF_NOT_FOUND"}`)
		},
	}
	h := newTestHandler(dm, &mockCmdPub{})

	r := httptest.NewRequest(http.MethodGet, "/diff", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{
		"contract_id":       testContractID,
		"base_version_id":   testBaseVerID,
		"target_version_id": testTargetVerID,
	})

	rr := httptest.NewRecorder()
	h.HandleGetDiff().ServeHTTP(rr, r)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
	result := parseJSON(t, rr)
	if result["error_code"] != "DIFF_NOT_FOUND" {
		t.Errorf("error_code = %v", result["error_code"])
	}
}

func TestHandleGetDiff_DM5xx(t *testing.T) {
	dm := &mockDM{
		getDiffFn: func(_ context.Context, _, _, _ string) (*dmclient.VersionDiff, error) {
			return nil, dmHTTPError("GetDiff", 500, "internal")
		},
	}
	h := newTestHandler(dm, &mockCmdPub{})

	r := httptest.NewRequest(http.MethodGet, "/diff", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{
		"contract_id":       testContractID,
		"base_version_id":   testBaseVerID,
		"target_version_id": testTargetVerID,
	})

	rr := httptest.NewRecorder()
	h.HandleGetDiff().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", rr.Code)
	}
}

func TestHandleGetDiff_DMCircuitOpen(t *testing.T) {
	dm := &mockDM{
		getDiffFn: func(_ context.Context, _, _, _ string) (*dmclient.VersionDiff, error) {
			return nil, fmt.Errorf("wrapped: %w", dmclient.ErrCircuitOpen)
		},
	}
	h := newTestHandler(dm, &mockCmdPub{})

	r := httptest.NewRequest(http.MethodGet, "/diff", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{
		"contract_id":       testContractID,
		"base_version_id":   testBaseVerID,
		"target_version_id": testTargetVerID,
	})

	rr := httptest.NewRecorder()
	h.HandleGetDiff().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", rr.Code)
	}
}

func TestHandleGetDiff_DMTransportError(t *testing.T) {
	dm := &mockDM{
		getDiffFn: func(_ context.Context, _, _, _ string) (*dmclient.VersionDiff, error) {
			return nil, &dmclient.DMError{Operation: "GetDiff", Retryable: true}
		},
	}
	h := newTestHandler(dm, &mockCmdPub{})

	r := httptest.NewRequest(http.MethodGet, "/diff", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{
		"contract_id":       testContractID,
		"base_version_id":   testBaseVerID,
		"target_version_id": testTargetVerID,
	})

	rr := httptest.NewRecorder()
	h.HandleGetDiff().ServeHTTP(rr, r)

	if rr.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", rr.Code)
	}
}

func TestHandleGetDiff_ContentType(t *testing.T) {
	h := newTestHandler(&mockDM{}, &mockCmdPub{})

	r := httptest.NewRequest(http.MethodGet, "/diff", nil)
	r = withAuthContext(r)
	r = withChiParams(r, map[string]string{
		"contract_id":       testContractID,
		"base_version_id":   testBaseVerID,
		"target_version_id": testTargetVerID,
	})

	rr := httptest.NewRecorder()
	h.HandleGetDiff().ServeHTTP(rr, r)

	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
}

// ---------------------------------------------------------------------------
// isStillProcessing tests
// ---------------------------------------------------------------------------

func TestIsStillProcessing(t *testing.T) {
	tests := []struct {
		status string
		want   bool
	}{
		{"FULLY_READY", false},
		{"PARTIALLY_AVAILABLE", false},
		{"PROCESSING_FAILED", false},
		{"REJECTED", false},
		{"PENDING_UPLOAD", true},
		{"PENDING_PROCESSING", true},
		{"PROCESSING_IN_PROGRESS", true},
		{"ARTIFACTS_READY", true},
		{"ANALYSIS_IN_PROGRESS", true},
		{"ANALYSIS_READY", true},
		{"REPORTS_IN_PROGRESS", true},
		{"UNKNOWN_STATUS", true},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			if got := isStillProcessing(tt.status); got != tt.want {
				t.Errorf("isStillProcessing(%q) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// validate tests
// ---------------------------------------------------------------------------

func TestCompareRequest_Validate(t *testing.T) {
	tests := []struct {
		name    string
		req     CompareRequest
		wantOK  bool
		wantMsg string
	}{
		{
			name:   "valid",
			req:    CompareRequest{BaseVersionID: testBaseVerID, TargetVersionID: testTargetVerID},
			wantOK: true,
		},
		{
			name:    "empty base",
			req:     CompareRequest{BaseVersionID: "", TargetVersionID: testTargetVerID},
			wantMsg: "base_version_id",
		},
		{
			name:    "empty target",
			req:     CompareRequest{BaseVersionID: testBaseVerID, TargetVersionID: ""},
			wantMsg: "target_version_id",
		},
		{
			name:    "invalid base UUID",
			req:     CompareRequest{BaseVersionID: "not-a-uuid", TargetVersionID: testTargetVerID},
			wantMsg: "base_version_id",
		},
		{
			name:    "invalid target UUID",
			req:     CompareRequest{BaseVersionID: testBaseVerID, TargetVersionID: "bad"},
			wantMsg: "target_version_id",
		},
		{
			name:    "same IDs",
			req:     CompareRequest{BaseVersionID: testBaseVerID, TargetVersionID: testBaseVerID},
			wantMsg: "target_version_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			verr := tt.req.validate()
			gotOK := verr == nil
			if gotOK != tt.wantOK {
				t.Errorf("validate() ok = %v, want %v", gotOK, tt.wantOK)
			}
			if tt.wantMsg != "" && verr != nil {
				found := false
				for _, f := range verr.Details.Fields {
					if strings.Contains(f.Field, tt.wantMsg) || strings.Contains(f.Message, tt.wantMsg) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("validate() fields = %+v, want field containing %q", verr.Details.Fields, tt.wantMsg)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// handleDMError tests
// ---------------------------------------------------------------------------

func TestHandleDMError_TransportError(t *testing.T) {
	h := newTestHandler(&mockDM{}, &mockCmdPub{})

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = withAuthContext(r)
	rr := httptest.NewRecorder()

	h.handleDMError(r.Context(), rr, r, &dmclient.DMError{Operation: "test"}, "test", "diff")

	if rr.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", rr.Code)
	}
}

func TestHandleDMError_UnexpectedError(t *testing.T) {
	h := newTestHandler(&mockDM{}, &mockCmdPub{})

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = withAuthContext(r)
	rr := httptest.NewRecorder()

	h.handleDMError(r.Context(), rr, r, errors.New("mystery error"), "test", "diff")

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Interface compliance
// ---------------------------------------------------------------------------

func TestInterfaceCompliance(t *testing.T) {
	var _ DMClient = (*dmclient.Client)(nil)
	var _ CommandPublisher = (*commandpub.Publisher)(nil)
}
