package feedback

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"contractpro/api-orchestrator/internal/egress/dmclient"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"
)

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

type mockDMClient struct {
	mu            sync.Mutex
	getVersionFn  func(ctx context.Context, documentID, versionID string) (*dmclient.DocumentVersionWithArtifacts, error)
	getVersionCalls []getVersionCall
}

type getVersionCall struct {
	DocumentID string
	VersionID  string
}

func (m *mockDMClient) GetVersion(ctx context.Context, documentID, versionID string) (*dmclient.DocumentVersionWithArtifacts, error) {
	m.mu.Lock()
	m.getVersionCalls = append(m.getVersionCalls, getVersionCall{
		DocumentID: documentID,
		VersionID:  versionID,
	})
	m.mu.Unlock()
	if m.getVersionFn != nil {
		return m.getVersionFn(ctx, documentID, versionID)
	}
	return &dmclient.DocumentVersionWithArtifacts{
		DocumentVersion: dmclient.DocumentVersion{
			VersionID:      versionID,
			ArtifactStatus: "FULLY_READY",
		},
	}, nil
}

type mockKVStore struct {
	mu       sync.Mutex
	setFn    func(ctx context.Context, key string, value string, ttl time.Duration) error
	setCalls []setCall
}

type setCall struct {
	Key   string
	Value string
	TTL   time.Duration
}

func (m *mockKVStore) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	m.mu.Lock()
	m.setCalls = append(m.setCalls, setCall{Key: key, Value: value, TTL: ttl})
	m.mu.Unlock()
	if m.setFn != nil {
		return m.setFn(ctx, key, value, ttl)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestHandler(dm DMClient, kv KVStore) *Handler {
	log := logger.NewLogger("error")
	return NewHandler(dm, kv, log)
}

func makeRequest(t *testing.T, contractID, versionID string, body string) *http.Request {
	t.Helper()
	path := fmt.Sprintf("/api/v1/contracts/%s/versions/%s/feedback", contractID, versionID)
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	ctx := auth.WithAuthContext(r.Context(), auth.AuthContext{
		UserID:         "user-001",
		OrganizationID: "org-001",
		Role:           auth.RoleLawyer,
		TokenID:        "token-001",
	})

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("contract_id", contractID)
	rctx.URLParams.Add("version_id", versionID)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)

	return r.WithContext(ctx)
}

func makeRequestWithRole(t *testing.T, contractID, versionID string, body string, role auth.Role) *http.Request {
	t.Helper()
	path := fmt.Sprintf("/api/v1/contracts/%s/versions/%s/feedback", contractID, versionID)
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	ctx := auth.WithAuthContext(r.Context(), auth.AuthContext{
		UserID:         "user-001",
		OrganizationID: "org-001",
		Role:           role,
		TokenID:        "token-001",
	})

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("contract_id", contractID)
	rctx.URLParams.Add("version_id", versionID)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)

	return r.WithContext(ctx)
}

func makeRequestNoAuth(t *testing.T, contractID, versionID string, body string) *http.Request {
	t.Helper()
	path := fmt.Sprintf("/api/v1/contracts/%s/versions/%s/feedback", contractID, versionID)
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("contract_id", contractID)
	rctx.URLParams.Add("version_id", versionID)
	ctx := context.WithValue(r.Context(), chi.RouteCtxKey, rctx)

	return r.WithContext(ctx)
}

func decodeJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to decode response body: %v\nbody: %s", err, w.Body.String())
	}
	return result
}

// ---------------------------------------------------------------------------
// Test constants
// ---------------------------------------------------------------------------

const (
	validContractID = "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	validVersionID  = "b2c3d4e5-f6a7-8901-bcde-f12345678901"
)

var validBody = `{"is_useful": true, "comment": "Полезный анализ контракта."}`

// ---------------------------------------------------------------------------
// HandleSubmit — happy path
// ---------------------------------------------------------------------------

func TestHandleSubmit_Success_Returns201(t *testing.T) {
	dm := &mockDMClient{}
	kv := &mockKVStore{}
	h := newTestHandler(dm, kv)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, validBody)

	h.HandleSubmit().ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d; body: %s", w.Code, w.Body.String())
	}

	body := decodeJSON(t, w)
	if body["feedback_id"] == nil || body["feedback_id"] == "" {
		t.Error("expected non-empty feedback_id")
	}
	if body["created_at"] == nil || body["created_at"] == "" {
		t.Error("expected non-empty created_at")
	}

	// Verify created_at is RFC3339.
	createdAt, ok := body["created_at"].(string)
	if !ok {
		t.Fatal("created_at is not a string")
	}
	if _, err := time.Parse(time.RFC3339, createdAt); err != nil {
		t.Errorf("created_at is not RFC3339: %s", createdAt)
	}

	// Verify Content-Type.
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type=application/json, got %s", ct)
	}
}

func TestHandleSubmit_IsUsefulFalse_Returns201(t *testing.T) {
	dm := &mockDMClient{}
	kv := &mockKVStore{}
	h := newTestHandler(dm, kv)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, `{"is_useful": false}`)

	h.HandleSubmit().ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d; body: %s", w.Code, w.Body.String())
	}

	// Verify stored record has is_useful=false.
	kv.mu.Lock()
	calls := kv.setCalls
	kv.mu.Unlock()
	if len(calls) != 1 {
		t.Fatalf("expected 1 KV Set call, got %d", len(calls))
	}
	var record feedbackRecord
	if err := json.Unmarshal([]byte(calls[0].Value), &record); err != nil {
		t.Fatalf("failed to unmarshal stored record: %v", err)
	}
	if record.IsUseful != false {
		t.Error("expected is_useful=false in stored record")
	}
}

func TestHandleSubmit_WithoutComment_Returns201(t *testing.T) {
	dm := &mockDMClient{}
	kv := &mockKVStore{}
	h := newTestHandler(dm, kv)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, `{"is_useful": true}`)

	h.HandleSubmit().ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// HandleSubmit — auth
// ---------------------------------------------------------------------------

func TestHandleSubmit_NoAuth_Returns401(t *testing.T) {
	dm := &mockDMClient{}
	kv := &mockKVStore{}
	h := newTestHandler(dm, kv)

	w := httptest.NewRecorder()
	r := makeRequestNoAuth(t, validContractID, validVersionID, validBody)

	h.HandleSubmit().ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
	body := decodeJSON(t, w)
	if body["error_code"] != "AUTH_TOKEN_MISSING" {
		t.Errorf("expected AUTH_TOKEN_MISSING, got %s", body["error_code"])
	}
}

// ---------------------------------------------------------------------------
// HandleSubmit — all roles allowed
// ---------------------------------------------------------------------------

func TestHandleSubmit_AllRolesAllowed(t *testing.T) {
	roles := []auth.Role{auth.RoleLawyer, auth.RoleBusinessUser, auth.RoleOrgAdmin}

	for _, role := range roles {
		t.Run(string(role), func(t *testing.T) {
			dm := &mockDMClient{}
			kv := &mockKVStore{}
			h := newTestHandler(dm, kv)

			w := httptest.NewRecorder()
			r := makeRequestWithRole(t, validContractID, validVersionID, validBody, role)

			h.HandleSubmit().ServeHTTP(w, r)

			if w.Code != http.StatusCreated {
				t.Errorf("expected 201 for role %s, got %d; body: %s", role, w.Code, w.Body.String())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// HandleSubmit — UUID validation
// ---------------------------------------------------------------------------

func TestHandleSubmit_InvalidContractID_Returns400(t *testing.T) {
	dm := &mockDMClient{}
	kv := &mockKVStore{}
	h := newTestHandler(dm, kv)

	w := httptest.NewRecorder()
	r := makeRequest(t, "not-a-uuid", validVersionID, validBody)

	h.HandleSubmit().ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	body := decodeJSON(t, w)
	if body["error_code"] != "VALIDATION_ERROR" {
		t.Errorf("expected VALIDATION_ERROR, got %s", body["error_code"])
	}
}

func TestHandleSubmit_InvalidVersionID_Returns400(t *testing.T) {
	dm := &mockDMClient{}
	kv := &mockKVStore{}
	h := newTestHandler(dm, kv)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, "not-a-uuid", validBody)

	h.HandleSubmit().ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleSubmit_EmptyContractID_Returns400(t *testing.T) {
	dm := &mockDMClient{}
	kv := &mockKVStore{}
	h := newTestHandler(dm, kv)

	w := httptest.NewRecorder()
	r := makeRequest(t, "", validVersionID, validBody)

	h.HandleSubmit().ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleSubmit_EmptyVersionID_Returns400(t *testing.T) {
	dm := &mockDMClient{}
	kv := &mockKVStore{}
	h := newTestHandler(dm, kv)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, "", validBody)

	h.HandleSubmit().ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// HandleSubmit — body validation
// ---------------------------------------------------------------------------

func TestHandleSubmit_InvalidJSON_Returns400(t *testing.T) {
	dm := &mockDMClient{}
	kv := &mockKVStore{}
	h := newTestHandler(dm, kv)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, "not json")

	h.HandleSubmit().ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	body := decodeJSON(t, w)
	if body["error_code"] != "VALIDATION_ERROR" {
		t.Errorf("expected VALIDATION_ERROR, got %s", body["error_code"])
	}
}

func TestHandleSubmit_EmptyBody_Returns400(t *testing.T) {
	dm := &mockDMClient{}
	kv := &mockKVStore{}
	h := newTestHandler(dm, kv)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, "")

	h.HandleSubmit().ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleSubmit_MissingIsUseful_Returns400(t *testing.T) {
	dm := &mockDMClient{}
	kv := &mockKVStore{}
	h := newTestHandler(dm, kv)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, `{"comment": "hello"}`)

	h.HandleSubmit().ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	body := decodeJSON(t, w)
	if body["error_code"] != "VALIDATION_ERROR" {
		t.Errorf("expected VALIDATION_ERROR, got %s", body["error_code"])
	}
}

func TestHandleSubmit_CommentTooLong_Returns400(t *testing.T) {
	dm := &mockDMClient{}
	kv := &mockKVStore{}
	h := newTestHandler(dm, kv)

	// Build a comment with 2001 runes.
	longComment := strings.Repeat("а", 2001)
	bodyJSON := fmt.Sprintf(`{"is_useful": true, "comment": %q}`, longComment)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, bodyJSON)

	h.HandleSubmit().ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
	body := decodeJSON(t, w)
	if body["error_code"] != "VALIDATION_ERROR" {
		t.Errorf("expected VALIDATION_ERROR, got %s", body["error_code"])
	}
}

func TestHandleSubmit_CommentExactly2000_Returns201(t *testing.T) {
	dm := &mockDMClient{}
	kv := &mockKVStore{}
	h := newTestHandler(dm, kv)

	// 2000 runes — exactly at the limit.
	comment := strings.Repeat("а", 2000)
	bodyJSON := fmt.Sprintf(`{"is_useful": true, "comment": %q}`, comment)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, bodyJSON)

	h.HandleSubmit().ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleSubmit_UnknownFields_Returns400(t *testing.T) {
	dm := &mockDMClient{}
	kv := &mockKVStore{}
	h := newTestHandler(dm, kv)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, `{"is_useful": true, "unknown_field": 123}`)

	h.HandleSubmit().ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleSubmit_CommentTrimmed(t *testing.T) {
	dm := &mockDMClient{}
	kv := &mockKVStore{}
	h := newTestHandler(dm, kv)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, `{"is_useful": true, "comment": "  trimmed comment  "}`)

	h.HandleSubmit().ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d", w.Code)
	}

	// Verify stored record has trimmed comment.
	kv.mu.Lock()
	calls := kv.setCalls
	kv.mu.Unlock()
	if len(calls) != 1 {
		t.Fatalf("expected 1 KV call, got %d", len(calls))
	}
	var record feedbackRecord
	if err := json.Unmarshal([]byte(calls[0].Value), &record); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if record.Comment != "trimmed comment" {
		t.Errorf("expected trimmed comment, got %q", record.Comment)
	}
}

func TestHandleSubmit_WhitespaceOnlyComment_TrimmedToEmpty(t *testing.T) {
	dm := &mockDMClient{}
	kv := &mockKVStore{}
	h := newTestHandler(dm, kv)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, `{"is_useful": true, "comment": "   "}`)

	h.HandleSubmit().ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d", w.Code)
	}

	kv.mu.Lock()
	calls := kv.setCalls
	kv.mu.Unlock()
	var record feedbackRecord
	if err := json.Unmarshal([]byte(calls[0].Value), &record); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if record.Comment != "" {
		t.Errorf("expected empty comment after trim, got %q", record.Comment)
	}
}

// ---------------------------------------------------------------------------
// HandleSubmit — DM errors
// ---------------------------------------------------------------------------

func TestHandleSubmit_VersionNotFound_Returns404(t *testing.T) {
	dm := &mockDMClient{
		getVersionFn: func(_ context.Context, _, _ string) (*dmclient.DocumentVersionWithArtifacts, error) {
			return nil, &dmclient.DMError{
				Operation:  "GetVersion",
				StatusCode: 404,
				Body:       []byte(`{"error":"not found"}`),
			}
		},
	}
	kv := &mockKVStore{}
	h := newTestHandler(dm, kv)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, validBody)

	h.HandleSubmit().ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
	body := decodeJSON(t, w)
	if body["error_code"] != "VERSION_NOT_FOUND" {
		t.Errorf("expected VERSION_NOT_FOUND, got %s", body["error_code"])
	}
}

func TestHandleSubmit_DM5xx_Returns502(t *testing.T) {
	dm := &mockDMClient{
		getVersionFn: func(_ context.Context, _, _ string) (*dmclient.DocumentVersionWithArtifacts, error) {
			return nil, &dmclient.DMError{
				Operation:  "GetVersion",
				StatusCode: 500,
				Body:       []byte(`internal error`),
				Retryable:  true,
			}
		},
	}
	kv := &mockKVStore{}
	h := newTestHandler(dm, kv)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, validBody)

	h.HandleSubmit().ServeHTTP(w, r)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}
	body := decodeJSON(t, w)
	if body["error_code"] != "DM_UNAVAILABLE" {
		t.Errorf("expected DM_UNAVAILABLE, got %s", body["error_code"])
	}
}

func TestHandleSubmit_CircuitOpen_Returns502(t *testing.T) {
	dm := &mockDMClient{
		getVersionFn: func(_ context.Context, _, _ string) (*dmclient.DocumentVersionWithArtifacts, error) {
			return nil, dmclient.ErrCircuitOpen
		},
	}
	kv := &mockKVStore{}
	h := newTestHandler(dm, kv)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, validBody)

	h.HandleSubmit().ServeHTTP(w, r)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}
	body := decodeJSON(t, w)
	if body["error_code"] != "DM_UNAVAILABLE" {
		t.Errorf("expected DM_UNAVAILABLE, got %s", body["error_code"])
	}
}

func TestHandleSubmit_TransportError_Returns502(t *testing.T) {
	dm := &mockDMClient{
		getVersionFn: func(_ context.Context, _, _ string) (*dmclient.DocumentVersionWithArtifacts, error) {
			return nil, &dmclient.DMError{
				Operation: "GetVersion",
				Retryable: true,
				Cause:     errors.New("connection refused"),
			}
		},
	}
	kv := &mockKVStore{}
	h := newTestHandler(dm, kv)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, validBody)

	h.HandleSubmit().ServeHTTP(w, r)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}
}

func TestHandleSubmit_UnknownError_Returns500(t *testing.T) {
	dm := &mockDMClient{
		getVersionFn: func(_ context.Context, _, _ string) (*dmclient.DocumentVersionWithArtifacts, error) {
			return nil, errors.New("something unexpected")
		},
	}
	kv := &mockKVStore{}
	h := newTestHandler(dm, kv)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, validBody)

	h.HandleSubmit().ServeHTTP(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}
	body := decodeJSON(t, w)
	if body["error_code"] != "INTERNAL_ERROR" {
		t.Errorf("expected INTERNAL_ERROR, got %s", body["error_code"])
	}
}

func TestHandleSubmit_ContextCanceled_Returns502(t *testing.T) {
	dm := &mockDMClient{
		getVersionFn: func(_ context.Context, _, _ string) (*dmclient.DocumentVersionWithArtifacts, error) {
			return nil, context.Canceled
		},
	}
	kv := &mockKVStore{}
	h := newTestHandler(dm, kv)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, validBody)

	h.HandleSubmit().ServeHTTP(w, r)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}
}

func TestHandleSubmit_DeadlineExceeded_Returns502(t *testing.T) {
	dm := &mockDMClient{
		getVersionFn: func(_ context.Context, _, _ string) (*dmclient.DocumentVersionWithArtifacts, error) {
			return nil, context.DeadlineExceeded
		},
	}
	kv := &mockKVStore{}
	h := newTestHandler(dm, kv)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, validBody)

	h.HandleSubmit().ServeHTTP(w, r)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// HandleSubmit — Redis failure (non-critical)
// ---------------------------------------------------------------------------

func TestHandleSubmit_RedisFailure_StillReturns201(t *testing.T) {
	dm := &mockDMClient{}
	kv := &mockKVStore{
		setFn: func(_ context.Context, _ string, _ string, _ time.Duration) error {
			return errors.New("redis connection refused")
		},
	}
	h := newTestHandler(dm, kv)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, validBody)

	h.HandleSubmit().ServeHTTP(w, r)

	// Redis failure is non-critical — still 201.
	if w.Code != http.StatusCreated {
		t.Errorf("expected 201 even with Redis failure, got %d; body: %s", w.Code, w.Body.String())
	}

	body := decodeJSON(t, w)
	if body["feedback_id"] == nil || body["feedback_id"] == "" {
		t.Error("expected non-empty feedback_id despite Redis failure")
	}
}

// ---------------------------------------------------------------------------
// HandleSubmit — data integrity
// ---------------------------------------------------------------------------

func TestHandleSubmit_StoredRecordFields(t *testing.T) {
	dm := &mockDMClient{}
	kv := &mockKVStore{}
	h := newTestHandler(dm, kv)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, `{"is_useful": true, "comment": "Great analysis"}`)

	h.HandleSubmit().ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}

	kv.mu.Lock()
	calls := kv.setCalls
	kv.mu.Unlock()
	if len(calls) != 1 {
		t.Fatalf("expected 1 KV Set call, got %d", len(calls))
	}

	var record feedbackRecord
	if err := json.Unmarshal([]byte(calls[0].Value), &record); err != nil {
		t.Fatalf("failed to unmarshal stored record: %v", err)
	}

	if record.ContractID != validContractID {
		t.Errorf("expected contract_id=%s, got %s", validContractID, record.ContractID)
	}
	if record.VersionID != validVersionID {
		t.Errorf("expected version_id=%s, got %s", validVersionID, record.VersionID)
	}
	if record.OrganizationID != "org-001" {
		t.Errorf("expected organization_id=org-001, got %s", record.OrganizationID)
	}
	if record.UserID != "user-001" {
		t.Errorf("expected user_id=user-001, got %s", record.UserID)
	}
	if record.IsUseful != true {
		t.Error("expected is_useful=true")
	}
	if record.Comment != "Great analysis" {
		t.Errorf("expected comment='Great analysis', got %q", record.Comment)
	}
	if record.FeedbackID == "" {
		t.Error("expected non-empty feedback_id in record")
	}
	if _, err := time.Parse(time.RFC3339, record.CreatedAt); err != nil {
		t.Errorf("created_at is not RFC3339: %s", record.CreatedAt)
	}
}

func TestHandleSubmit_RedisTTL(t *testing.T) {
	dm := &mockDMClient{}
	kv := &mockKVStore{}
	h := newTestHandler(dm, kv)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, validBody)

	h.HandleSubmit().ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}

	kv.mu.Lock()
	calls := kv.setCalls
	kv.mu.Unlock()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}

	expectedTTL := 30 * 24 * time.Hour
	if calls[0].TTL != expectedTTL {
		t.Errorf("expected TTL=%v, got %v", expectedTTL, calls[0].TTL)
	}
}

func TestHandleSubmit_RedisKeyFormat(t *testing.T) {
	dm := &mockDMClient{}
	kv := &mockKVStore{}
	h := newTestHandler(dm, kv)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, validBody)

	h.HandleSubmit().ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}

	kv.mu.Lock()
	calls := kv.setCalls
	kv.mu.Unlock()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}

	key := calls[0].Key
	prefix := "feedback:org-001:" + validVersionID + ":"
	if !strings.HasPrefix(key, prefix) {
		t.Errorf("expected key to start with %q, got %q", prefix, key)
	}
}

func TestHandleSubmit_DMCalledWithCorrectArgs(t *testing.T) {
	dm := &mockDMClient{}
	kv := &mockKVStore{}
	h := newTestHandler(dm, kv)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, validBody)

	h.HandleSubmit().ServeHTTP(w, r)

	dm.mu.Lock()
	calls := dm.getVersionCalls
	dm.mu.Unlock()
	if len(calls) != 1 {
		t.Fatalf("expected 1 DM call, got %d", len(calls))
	}
	if calls[0].DocumentID != validContractID {
		t.Errorf("expected documentID=%s, got %s", validContractID, calls[0].DocumentID)
	}
	if calls[0].VersionID != validVersionID {
		t.Errorf("expected versionID=%s, got %s", validVersionID, calls[0].VersionID)
	}
}

func TestHandleSubmit_ResponseFeedbackIDMatchesStoredRecord(t *testing.T) {
	dm := &mockDMClient{}
	kv := &mockKVStore{}
	h := newTestHandler(dm, kv)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, validBody)

	h.HandleSubmit().ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}

	// Get feedback_id from response.
	respBody := decodeJSON(t, w)
	respFeedbackID := respBody["feedback_id"].(string)

	// Get feedback_id from stored record.
	kv.mu.Lock()
	calls := kv.setCalls
	kv.mu.Unlock()
	var record feedbackRecord
	if err := json.Unmarshal([]byte(calls[0].Value), &record); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if respFeedbackID != record.FeedbackID {
		t.Errorf("response feedback_id=%s doesn't match stored=%s", respFeedbackID, record.FeedbackID)
	}
}

// ---------------------------------------------------------------------------
// HandleSubmit — response format
// ---------------------------------------------------------------------------

func TestHandleSubmit_ErrorResponseFormat(t *testing.T) {
	dm := &mockDMClient{}
	kv := &mockKVStore{}
	h := newTestHandler(dm, kv)

	w := httptest.NewRecorder()
	r := makeRequest(t, "invalid", validVersionID, validBody)

	h.HandleSubmit().ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type=application/json, got %s", ct)
	}

	body := decodeJSON(t, w)
	for _, field := range []string{"error_code", "message"} {
		if _, ok := body[field]; !ok {
			t.Errorf("missing field %q in error response", field)
		}
	}
}

// ---------------------------------------------------------------------------
// feedbackKey
// ---------------------------------------------------------------------------

func TestFeedbackKey(t *testing.T) {
	tests := []struct {
		orgID      string
		versionID  string
		feedbackID string
		want       string
	}{
		{"org-1", "ver-1", "fb-1", "feedback:org-1:ver-1:fb-1"},
		{"org-2", "ver-2", "fb-2", "feedback:org-2:ver-2:fb-2"},
		{"", "ver-3", "fb-3", "feedback::ver-3:fb-3"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := feedbackKey(tt.orgID, tt.versionID, tt.feedbackID)
			if got != tt.want {
				t.Errorf("feedbackKey(%q, %q, %q) = %q, want %q",
					tt.orgID, tt.versionID, tt.feedbackID, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// validate
// ---------------------------------------------------------------------------

func TestValidate(t *testing.T) {
	boolTrue := true
	boolFalse := false

	tests := []struct {
		name    string
		req     FeedbackRequest
		wantOK  bool
	}{
		{"valid_true", FeedbackRequest{IsUseful: &boolTrue, Comment: "good"}, true},
		{"valid_false", FeedbackRequest{IsUseful: &boolFalse}, true},
		{"valid_empty_comment", FeedbackRequest{IsUseful: &boolTrue, Comment: ""}, true},
		{"valid_2000_chars", FeedbackRequest{IsUseful: &boolTrue, Comment: strings.Repeat("x", 2000)}, true},
		{"nil_is_useful", FeedbackRequest{IsUseful: nil}, false},
		{"comment_2001", FeedbackRequest{IsUseful: &boolTrue, Comment: strings.Repeat("x", 2001)}, false},
		{"unicode_2000", FeedbackRequest{IsUseful: &boolTrue, Comment: strings.Repeat("ы", 2000)}, true},
		{"unicode_2001", FeedbackRequest{IsUseful: &boolTrue, Comment: strings.Repeat("ы", 2001)}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			verr := tt.req.validate()
			gotOK := verr == nil
			if gotOK != tt.wantOK {
				t.Errorf("validate() ok=%v, want %v", gotOK, tt.wantOK)
			}
			if !gotOK && verr != nil && len(verr.Details.Fields) == 0 {
				t.Error("expected non-empty fields when validation fails")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Interface compliance
// ---------------------------------------------------------------------------

func TestInterfaceCompliance(t *testing.T) {
	var _ DMClient = (*mockDMClient)(nil)
	var _ DMClient = (*dmclient.Client)(nil)
	var _ KVStore = (*mockKVStore)(nil)
}

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

func TestNewHandler(t *testing.T) {
	log := logger.NewLogger("error")
	dm := &mockDMClient{}
	kv := &mockKVStore{}
	h := NewHandler(dm, kv, log)
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
	if h.dm == nil {
		t.Error("expected non-nil dm")
	}
	if h.kv == nil {
		t.Error("expected non-nil kv")
	}
	if h.log == nil {
		t.Error("expected non-nil logger")
	}
}

// ---------------------------------------------------------------------------
// Concurrent safety
// ---------------------------------------------------------------------------

func TestHandleSubmit_ConcurrentSafety(t *testing.T) {
	dm := &mockDMClient{}
	kv := &mockKVStore{}
	h := newTestHandler(dm, kv)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			r := makeRequest(t, validContractID, validVersionID, validBody)
			h.HandleSubmit().ServeHTTP(w, r)
			if w.Code != http.StatusCreated {
				t.Errorf("expected 201, got %d", w.Code)
			}
		}()
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// No DM call for invalid requests
// ---------------------------------------------------------------------------

func TestHandleSubmit_NoDMCallOnValidationFailure(t *testing.T) {
	dm := &mockDMClient{}
	kv := &mockKVStore{}
	h := newTestHandler(dm, kv)

	// Invalid contract_id.
	w := httptest.NewRecorder()
	r := makeRequest(t, "invalid", validVersionID, validBody)
	h.HandleSubmit().ServeHTTP(w, r)

	// Missing is_useful.
	w2 := httptest.NewRecorder()
	r2 := makeRequest(t, validContractID, validVersionID, `{"comment": "hello"}`)
	h.HandleSubmit().ServeHTTP(w2, r2)

	dm.mu.Lock()
	calls := len(dm.getVersionCalls)
	dm.mu.Unlock()
	if calls != 0 {
		t.Errorf("expected 0 DM calls for invalid requests, got %d", calls)
	}
}

// ---------------------------------------------------------------------------
// No KV call when DM fails
// ---------------------------------------------------------------------------

func TestHandleSubmit_NoKVCallWhenDMFails(t *testing.T) {
	dm := &mockDMClient{
		getVersionFn: func(_ context.Context, _, _ string) (*dmclient.DocumentVersionWithArtifacts, error) {
			return nil, &dmclient.DMError{
				Operation:  "GetVersion",
				StatusCode: 404,
				Body:       []byte(`not found`),
			}
		},
	}
	kv := &mockKVStore{}
	h := newTestHandler(dm, kv)

	w := httptest.NewRecorder()
	r := makeRequest(t, validContractID, validVersionID, validBody)
	h.HandleSubmit().ServeHTTP(w, r)

	kv.mu.Lock()
	calls := len(kv.setCalls)
	kv.mu.Unlock()
	if calls != 0 {
		t.Errorf("expected 0 KV calls when DM fails, got %d", calls)
	}
}
