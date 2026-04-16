package confirmtype

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"contractpro/api-orchestrator/internal/application/statustracker"
	"contractpro/api-orchestrator/internal/infra/kvstore"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"
)

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

type mockTracker struct {
	err error
}

func (m *mockTracker) ConfirmType(_ context.Context, _, _, _ string) error {
	return m.err
}

type mockPublisher struct {
	cmd UserConfirmedTypeCommand
	err error
}

func (m *mockPublisher) PublishUserConfirmedType(_ context.Context, cmd UserConfirmedTypeCommand) error {
	m.cmd = cmd
	return m.err
}

type kvCall struct {
	Op    string
	Key   string
	Value string
	TTL   time.Duration
}

type mockKV struct {
	store map[string]string
	calls []kvCall
	err   error
}

func newMockKV() *mockKV {
	return &mockKV{store: make(map[string]string)}
}

func (m *mockKV) Get(_ context.Context, key string) (string, error) {
	m.calls = append(m.calls, kvCall{Op: "GET", Key: key})
	if m.err != nil {
		return "", m.err
	}
	v, ok := m.store[key]
	if !ok {
		return "", kvstore.ErrKeyNotFound
	}
	return v, nil
}

func (m *mockKV) Set(_ context.Context, key string, value string, ttl time.Duration) error {
	m.calls = append(m.calls, kvCall{Op: "SET", Key: key, Value: value, TTL: ttl})
	if m.err != nil {
		return m.err
	}
	m.store[key] = value
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func testHandler(tracker StatusTracker, publisher CommandPublisher, kv KVStore) *Handler {
	return NewHandler(tracker, publisher, kv, logger.NewLogger("error"),
		[]string{"услуги", "поставка", "подряд", "аренда", "NDA"}, 60*time.Second)
}

func withAuth(r *http.Request, role auth.Role) *http.Request {
	ctx := auth.WithAuthContext(r.Context(), auth.AuthContext{
		UserID:         "user-123",
		OrganizationID: "org-456",
		Role:           role,
		TokenID:        "token-789",
	})
	ctx = logger.WithRequestContext(ctx, logger.RequestContext{
		CorrelationID:  "corr-001",
		OrganizationID: "org-456",
		UserID:         "user-123",
	})
	return r.WithContext(ctx)
}

func withChi(r *http.Request, contractID, versionID string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("contract_id", contractID)
	rctx.URLParams.Add("version_id", versionID)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func seedMeta(kv *mockKV, versionID string) {
	meta := confirmationMeta{
		OrganizationID: "org-456",
		DocumentID:     "doc-001",
		VersionID:      versionID,
		JobID:          "job-001",
	}
	b, _ := json.Marshal(meta)
	kv.store[confirmationMetaKey(versionID)] = string(b)
}

func validBody() *bytes.Buffer {
	b, _ := json.Marshal(map[string]any{
		"contract_type":     "услуги",
		"confirmed_by_user": true,
	})
	return bytes.NewBuffer(b)
}

func doRequest(h *Handler, contractID, versionID string, body *bytes.Buffer, role auth.Role) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/confirm-type", body)
	r = withAuth(r, role)
	r = withChi(r, contractID, versionID)
	w := httptest.NewRecorder()
	h.Handle()(w, r)
	return w
}

const (
	testContractID = "a0000000-0000-0000-0000-000000000001"
	testVersionID  = "b0000000-0000-0000-0000-000000000002"
)

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestHandle_HappyPath(t *testing.T) {
	kv := newMockKV()
	seedMeta(kv, testVersionID)
	pub := &mockPublisher{}
	h := testHandler(&mockTracker{}, pub, kv)

	w := doRequest(h, testContractID, testVersionID, validBody(), auth.RoleLawyer)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusAccepted)
	}
	var resp confirmTypeResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "ANALYZING" {
		t.Errorf("status = %q, want ANALYZING", resp.Status)
	}
	if resp.ContractID != testContractID {
		t.Errorf("contract_id = %q, want %q", resp.ContractID, testContractID)
	}
	if resp.VersionID != testVersionID {
		t.Errorf("version_id = %q, want %q", resp.VersionID, testVersionID)
	}
	if pub.cmd.ContractType != "услуги" {
		t.Errorf("published contract_type = %q, want услуги", pub.cmd.ContractType)
	}
	if pub.cmd.ConfirmedByUserID != "user-123" {
		t.Errorf("published confirmed_by = %q, want user-123", pub.cmd.ConfirmedByUserID)
	}
	if pub.cmd.JobID != "job-001" {
		t.Errorf("published job_id = %q, want job-001", pub.cmd.JobID)
	}

	// Verify idempotency key was set.
	_, ok := kv.store[idempotencyKey(testVersionID)]
	if !ok {
		t.Error("idempotency key not set")
	}
}

func TestHandle_NoAuth(t *testing.T) {
	h := testHandler(&mockTracker{}, &mockPublisher{}, newMockKV())
	r := httptest.NewRequest(http.MethodPost, "/confirm-type", validBody())
	r = withChi(r, testContractID, testVersionID)
	w := httptest.NewRecorder()
	h.Handle()(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandle_InvalidContractID(t *testing.T) {
	h := testHandler(&mockTracker{}, &mockPublisher{}, newMockKV())
	w := doRequest(h, "not-a-uuid", testVersionID, validBody(), auth.RoleLawyer)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandle_InvalidVersionID(t *testing.T) {
	h := testHandler(&mockTracker{}, &mockPublisher{}, newMockKV())
	w := doRequest(h, testContractID, "not-a-uuid", validBody(), auth.RoleLawyer)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandle_InvalidJSON(t *testing.T) {
	h := testHandler(&mockTracker{}, &mockPublisher{}, newMockKV())

	body := bytes.NewBufferString("{invalid")
	w := doRequest(h, testContractID, testVersionID, body, auth.RoleLawyer)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandle_EmptyContractType(t *testing.T) {
	h := testHandler(&mockTracker{}, &mockPublisher{}, newMockKV())

	body, _ := json.Marshal(map[string]any{"contract_type": "", "confirmed_by_user": true})
	w := doRequest(h, testContractID, testVersionID, bytes.NewBuffer(body), auth.RoleLawyer)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandle_ConfirmedByUserFalse(t *testing.T) {
	h := testHandler(&mockTracker{}, &mockPublisher{}, newMockKV())

	body, _ := json.Marshal(map[string]any{"contract_type": "услуги", "confirmed_by_user": false})
	w := doRequest(h, testContractID, testVersionID, bytes.NewBuffer(body), auth.RoleLawyer)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandle_ContractTypeNotInWhitelist(t *testing.T) {
	h := testHandler(&mockTracker{}, &mockPublisher{}, newMockKV())

	body, _ := json.Marshal(map[string]any{"contract_type": "неизвестный", "confirmed_by_user": true})
	w := doRequest(h, testContractID, testVersionID, bytes.NewBuffer(body), auth.RoleLawyer)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandle_Idempotency(t *testing.T) {
	kv := newMockKV()
	kv.store[idempotencyKey(testVersionID)] = "1"
	pub := &mockPublisher{}
	h := testHandler(&mockTracker{}, pub, kv)

	w := doRequest(h, testContractID, testVersionID, validBody(), auth.RoleLawyer)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusAccepted)
	}
	if pub.cmd.ContractType != "" {
		t.Error("command should NOT have been published on idempotency hit")
	}
}

func TestHandle_MetaNotFound(t *testing.T) {
	kv := newMockKV()
	h := testHandler(&mockTracker{}, &mockPublisher{}, kv)

	w := doRequest(h, testContractID, testVersionID, validBody(), auth.RoleLawyer)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandle_NotAwaitingInput(t *testing.T) {
	kv := newMockKV()
	seedMeta(kv, testVersionID)
	h := testHandler(&mockTracker{err: statustracker.ErrNotAwaitingInput}, &mockPublisher{}, kv)

	w := doRequest(h, testContractID, testVersionID, validBody(), auth.RoleLawyer)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", w.Code, http.StatusConflict)
	}

	var errResp map[string]any
	json.NewDecoder(w.Body).Decode(&errResp)
	if errResp["error_code"] != "VERSION_NOT_AWAITING_INPUT" {
		t.Errorf("error_code = %v, want VERSION_NOT_AWAITING_INPUT", errResp["error_code"])
	}
}

func TestHandle_TrackerInternalError(t *testing.T) {
	kv := newMockKV()
	seedMeta(kv, testVersionID)
	h := testHandler(&mockTracker{err: errors.New("redis down")}, &mockPublisher{}, kv)

	w := doRequest(h, testContractID, testVersionID, validBody(), auth.RoleLawyer)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestHandle_BrokerFailure(t *testing.T) {
	kv := newMockKV()
	seedMeta(kv, testVersionID)
	h := testHandler(&mockTracker{}, &mockPublisher{err: errors.New("broker down")}, kv)

	w := doRequest(h, testContractID, testVersionID, validBody(), auth.RoleLawyer)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
}

func TestHandle_RedisIdempotencyError_Degrade(t *testing.T) {
	kv := newMockKV()
	kv.err = errors.New("redis timeout")
	h := testHandler(&mockTracker{}, &mockPublisher{}, kv)

	w := doRequest(h, testContractID, testVersionID, validBody(), auth.RoleLawyer)

	// Redis error on idempotency GET → 500.
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestHandle_RedisSetIdempotencyFailure_NonCritical(t *testing.T) {
	kv := newMockKV()
	seedMeta(kv, testVersionID)
	// Wrap to fail only on SET for idempotency key.
	setErr := &failOnSetKV{inner: kv}
	h := testHandler(&mockTracker{}, &mockPublisher{}, setErr)

	w := doRequest(h, testContractID, testVersionID, validBody(), auth.RoleLawyer)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d (Redis SET failure is non-critical)", w.Code, http.StatusAccepted)
	}
}

type failOnSetKV struct {
	inner *mockKV
}

func (f *failOnSetKV) Get(ctx context.Context, key string) (string, error) {
	return f.inner.Get(ctx, key)
}

func (f *failOnSetKV) Set(_ context.Context, _ string, _ string, _ time.Duration) error {
	return errors.New("SET failed")
}

func TestHandle_OrgAdminAllowed(t *testing.T) {
	kv := newMockKV()
	seedMeta(kv, testVersionID)
	h := testHandler(&mockTracker{}, &mockPublisher{}, kv)

	w := doRequest(h, testContractID, testVersionID, validBody(), auth.RoleOrgAdmin)

	if w.Code != http.StatusAccepted {
		t.Errorf("status = %d, want %d", w.Code, http.StatusAccepted)
	}
}

func TestHandle_ContractTypeTrimmed(t *testing.T) {
	kv := newMockKV()
	seedMeta(kv, testVersionID)
	pub := &mockPublisher{}
	h := testHandler(&mockTracker{}, pub, kv)

	body, _ := json.Marshal(map[string]any{"contract_type": "  услуги  ", "confirmed_by_user": true})
	w := doRequest(h, testContractID, testVersionID, bytes.NewBuffer(body), auth.RoleLawyer)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusAccepted)
	}
	if pub.cmd.ContractType != "услуги" {
		t.Errorf("published contract_type = %q, want %q", pub.cmd.ContractType, "услуги")
	}
}

func TestHandle_AllWhitelistValues(t *testing.T) {
	for _, ct := range []string{"услуги", "поставка", "подряд", "аренда", "NDA"} {
		t.Run(ct, func(t *testing.T) {
			kv := newMockKV()
			seedMeta(kv, testVersionID)
			h := testHandler(&mockTracker{}, &mockPublisher{}, kv)

			body, _ := json.Marshal(map[string]any{"contract_type": ct, "confirmed_by_user": true})
			w := doRequest(h, testContractID, testVersionID, bytes.NewBuffer(body), auth.RoleLawyer)

			if w.Code != http.StatusAccepted {
				t.Errorf("status = %d, want %d for type %q", w.Code, http.StatusAccepted, ct)
			}
		})
	}
}

func TestHandle_PublishedCommandFields(t *testing.T) {
	kv := newMockKV()
	seedMeta(kv, testVersionID)
	pub := &mockPublisher{}
	h := testHandler(&mockTracker{}, pub, kv)

	w := doRequest(h, testContractID, testVersionID, validBody(), auth.RoleLawyer)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusAccepted)
	}

	if pub.cmd.DocumentID != "doc-001" {
		t.Errorf("DocumentID = %q, want doc-001", pub.cmd.DocumentID)
	}
	if pub.cmd.VersionID != testVersionID {
		t.Errorf("VersionID = %q, want %s", pub.cmd.VersionID, testVersionID)
	}
	if pub.cmd.OrganizationID != "org-456" {
		t.Errorf("OrganizationID = %q, want org-456", pub.cmd.OrganizationID)
	}
}

func TestHandle_CorrelationIDInResponse(t *testing.T) {
	kv := newMockKV()
	seedMeta(kv, testVersionID)
	h := testHandler(&mockTracker{}, &mockPublisher{}, kv)

	w := doRequest(h, testContractID, testVersionID, validBody(), auth.RoleLawyer)

	if w.Header().Get("X-Correlation-Id") != "corr-001" {
		t.Errorf("X-Correlation-Id = %q, want corr-001", w.Header().Get("X-Correlation-Id"))
	}
}

func TestHandle_IdempotencyKeyFormat(t *testing.T) {
	kv := newMockKV()
	seedMeta(kv, testVersionID)
	h := testHandler(&mockTracker{}, &mockPublisher{}, kv)

	doRequest(h, testContractID, testVersionID, validBody(), auth.RoleLawyer)

	expected := "orch-user-confirmed-type:" + testVersionID
	found := false
	for _, c := range kv.calls {
		if c.Op == "SET" && c.Key == expected {
			found = true
			if c.TTL != 60*time.Second {
				t.Errorf("idempotency TTL = %v, want 60s", c.TTL)
			}
		}
	}
	if !found {
		t.Errorf("idempotency key %q not found in KV calls", expected)
	}
}

func TestHandle_MetaCorruptJSON(t *testing.T) {
	kv := newMockKV()
	kv.store[confirmationMetaKey(testVersionID)] = "not-json"
	h := testHandler(&mockTracker{}, &mockPublisher{}, kv)

	w := doRequest(h, testContractID, testVersionID, validBody(), auth.RoleLawyer)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestHandle_ContentType(t *testing.T) {
	kv := newMockKV()
	seedMeta(kv, testVersionID)
	h := testHandler(&mockTracker{}, &mockPublisher{}, kv)

	w := doRequest(h, testContractID, testVersionID, validBody(), auth.RoleLawyer)

	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func TestHandle_WhitelistValues(t *testing.T) {
	h := testHandler(&mockTracker{}, &mockPublisher{}, newMockKV())
	vals := h.WhitelistValues()
	if len(vals) != 5 {
		t.Errorf("whitelist length = %d, want 5", len(vals))
	}
}

func TestIsValidUUID(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"a0000000-0000-0000-0000-000000000001", true},
		{"not-a-uuid", false},
		{"", false},
		{"12345678-1234-1234-1234-123456789012", true},
	}
	for _, tc := range tests {
		if got := isValidUUID(tc.input); got != tc.valid {
			t.Errorf("isValidUUID(%q) = %v, want %v", tc.input, got, tc.valid)
		}
	}
}

func TestIdempotencyKey(t *testing.T) {
	k := idempotencyKey("ver-1")
	if k != "orch-user-confirmed-type:ver-1" {
		t.Errorf("got %q", k)
	}
}

func TestConfirmationMetaKey(t *testing.T) {
	k := confirmationMetaKey("ver-1")
	if k != "confirmation:meta:ver-1" {
		t.Errorf("got %q", k)
	}
}

func TestHandle_NoCommandOnBrokerFailure(t *testing.T) {
	kv := newMockKV()
	seedMeta(kv, testVersionID)
	pub := &mockPublisher{err: errors.New("broker down")}
	h := testHandler(&mockTracker{}, pub, kv)

	w := doRequest(h, testContractID, testVersionID, validBody(), auth.RoleLawyer)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}

	// Idempotency key should NOT be set on broker failure.
	_, ok := kv.store[idempotencyKey(testVersionID)]
	if ok {
		t.Error("idempotency key should not be set when broker fails")
	}
}

func TestHandle_EmptyBody(t *testing.T) {
	h := testHandler(&mockTracker{}, &mockPublisher{}, newMockKV())

	body := bytes.NewBuffer(nil)
	w := doRequest(h, testContractID, testVersionID, body, auth.RoleLawyer)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandle_InterfaceCompliance(t *testing.T) {
	var _ StatusTracker = (*mockTracker)(nil)
	var _ CommandPublisher = (*mockPublisher)(nil)
	var _ KVStore = (*mockKV)(nil)
}
