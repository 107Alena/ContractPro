package upload

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"
)

// ---------------------------------------------------------------------------
// Test mocks
// ---------------------------------------------------------------------------

type mockStorage struct {
	putFn    func(ctx context.Context, key string, data io.ReadSeeker, contentType string) error
	deleteFn func(ctx context.Context, key string) error

	putCalls    []putCall
	deleteCalls []string
}

type putCall struct {
	Key         string
	ContentType string
	Data        []byte
}

func (m *mockStorage) PutObject(ctx context.Context, key string, data io.ReadSeeker, contentType string) error {
	// Read all data to simulate S3 consuming the stream.
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, data); err != nil {
		return err
	}
	m.putCalls = append(m.putCalls, putCall{Key: key, ContentType: contentType, Data: buf.Bytes()})

	if m.putFn != nil {
		return m.putFn(ctx, key, data, contentType)
	}
	return nil
}

func (m *mockStorage) DeleteObject(ctx context.Context, key string) error {
	m.deleteCalls = append(m.deleteCalls, key)
	if m.deleteFn != nil {
		return m.deleteFn(ctx, key)
	}
	return nil
}

type mockDMClient struct {
	createDocFn func(ctx context.Context, req CreateDocumentRequest) (*Document, error)
	createVerFn func(ctx context.Context, docID string, req CreateVersionRequest) (*DocumentVersion, error)

	createDocCalls []CreateDocumentRequest
	createVerCalls []createVerCall
}

type createVerCall struct {
	DocumentID string
	Req        CreateVersionRequest
}

func (m *mockDMClient) CreateDocument(ctx context.Context, req CreateDocumentRequest) (*Document, error) {
	m.createDocCalls = append(m.createDocCalls, req)
	if m.createDocFn != nil {
		return m.createDocFn(ctx, req)
	}
	return &Document{DocumentID: "doc-001"}, nil
}

func (m *mockDMClient) CreateVersion(ctx context.Context, docID string, req CreateVersionRequest) (*DocumentVersion, error) {
	m.createVerCalls = append(m.createVerCalls, createVerCall{DocumentID: docID, Req: req})
	if m.createVerFn != nil {
		return m.createVerFn(ctx, docID, req)
	}
	return &DocumentVersion{VersionID: "ver-001", VersionNumber: 1}, nil
}

type mockPublisher struct {
	publishFn func(ctx context.Context, cmd ProcessDocumentCommand) error
	calls     []ProcessDocumentCommand
}

func (m *mockPublisher) PublishProcessDocument(ctx context.Context, cmd ProcessDocumentCommand) error {
	m.calls = append(m.calls, cmd)
	if m.publishFn != nil {
		return m.publishFn(ctx, cmd)
	}
	return nil
}

type mockKVStore struct {
	setFn func(ctx context.Context, key string, value string, ttl time.Duration) error
	calls []kvSetCall
}

type kvSetCall struct {
	Key   string
	Value string
	TTL   time.Duration
}

func (m *mockKVStore) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	m.calls = append(m.calls, kvSetCall{Key: key, Value: value, TTL: ttl})
	if m.setFn != nil {
		return m.setFn(ctx, key, value, ttl)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// validPDFContent returns a minimal byte sequence that starts with %PDF-.
func validPDFContent() []byte {
	content := make([]byte, 100)
	copy(content, []byte("%PDF-1.7 test content"))
	return content
}

// newTestHandler creates a Handler with all mock dependencies and a
// deterministic UUID generator.
func newTestHandler(
	storage *mockStorage,
	dm *mockDMClient,
	pub *mockPublisher,
	kv *mockKVStore,
) *Handler {
	h := NewHandler(storage, dm, pub, kv, logger.NewLogger("error"), 20<<20) // 20 MB
	counter := 0
	h.uuidGen = func() string {
		counter++
		return fmt.Sprintf("uuid-%03d", counter)
	}
	return h
}

// createMultipartRequest builds a multipart/form-data POST request with an
// optional file and title field. The file part Content-Type is set to
// application/pdf (matching real browser behavior for PDF uploads).
func createMultipartRequest(t *testing.T, title string, fileName string, fileContent []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)

	if title != "" {
		if err := w.WriteField("title", title); err != nil {
			t.Fatalf("write title field: %v", err)
		}
	}

	if fileContent != nil {
		// Use CreatePart instead of CreateFormFile so we can set the
		// Content-Type to application/pdf (CreateFormFile defaults to
		// application/octet-stream).
		partHeader := make(map[string][]string)
		partHeader["Content-Disposition"] = []string{
			fmt.Sprintf(`form-data; name="file"; filename="%s"`, fileName),
		}
		partHeader["Content-Type"] = []string{"application/pdf"}
		part, err := w.CreatePart(partHeader)
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := part.Write(fileContent); err != nil {
			t.Fatalf("write file content: %v", err)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/contracts/upload", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

// withAuthContext wraps a request with a valid auth context.
func withAuthContext(r *http.Request) *http.Request {
	ctx := auth.WithAuthContext(r.Context(), auth.AuthContext{
		UserID:         "user-123",
		OrganizationID: "org-456",
		Role:           auth.RoleLawyer,
		TokenID:        "token-789",
	})
	return r.WithContext(ctx)
}

// parseErrorResponse decodes a model.ErrorResponse from a response recorder.
func parseErrorResponse(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	return resp
}

// parseUploadResponse decodes an UploadResponse from a response recorder.
func parseUploadResponse(t *testing.T, rr *httptest.ResponseRecorder) UploadResponse {
	t.Helper()
	var resp UploadResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	return resp
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestHandle_Success(t *testing.T) {
	storage := &mockStorage{}
	dm := &mockDMClient{}
	pub := &mockPublisher{}
	kv := &mockKVStore{}
	h := newTestHandler(storage, dm, pub, kv)

	req := createMultipartRequest(t, "Test Contract", "contract.pdf", validPDFContent())
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.Handle().ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rr.Code, rr.Body.String())
	}

	resp := parseUploadResponse(t, rr)

	if resp.ContractID != "doc-001" {
		t.Errorf("expected contract_id=doc-001, got %s", resp.ContractID)
	}
	if resp.VersionID != "ver-001" {
		t.Errorf("expected version_id=ver-001, got %s", resp.VersionID)
	}
	if resp.VersionNumber != 1 {
		t.Errorf("expected version_number=1, got %d", resp.VersionNumber)
	}
	// uuid-001=correlation_id, uuid-002=job_id
	if resp.JobID != "uuid-002" {
		t.Errorf("expected job_id=uuid-002, got %s", resp.JobID)
	}
	if resp.Status != "UPLOADED" {
		t.Errorf("expected status=UPLOADED, got %s", resp.Status)
	}
	if resp.Message == "" {
		t.Error("expected non-empty message")
	}

	// Verify X-Correlation-Id header.
	if got := rr.Header().Get("X-Correlation-Id"); got != "uuid-001" {
		t.Errorf("expected X-Correlation-Id=uuid-001, got %s", got)
	}

	// Verify S3 upload was called.
	if len(storage.putCalls) != 1 {
		t.Fatalf("expected 1 S3 put call, got %d", len(storage.putCalls))
	}
	// S3 key: uploads/{org_id}/{job_id}/{uuid}
	expectedKey := "uploads/org-456/uuid-002/uuid-003"
	if storage.putCalls[0].Key != expectedKey {
		t.Errorf("expected S3 key=%s, got %s", expectedKey, storage.putCalls[0].Key)
	}
	if storage.putCalls[0].ContentType != contentTypePDF {
		t.Errorf("expected content type=%s, got %s", contentTypePDF, storage.putCalls[0].ContentType)
	}

	// Verify DM create document was called with correct title.
	if len(dm.createDocCalls) != 1 {
		t.Fatalf("expected 1 DM CreateDocument call, got %d", len(dm.createDocCalls))
	}
	if dm.createDocCalls[0].Title != "Test Contract" {
		t.Errorf("expected title=Test Contract, got %s", dm.createDocCalls[0].Title)
	}

	// Verify DM create version was called.
	if len(dm.createVerCalls) != 1 {
		t.Fatalf("expected 1 DM CreateVersion call, got %d", len(dm.createVerCalls))
	}
	vc := dm.createVerCalls[0]
	if vc.DocumentID != "doc-001" {
		t.Errorf("expected document_id=doc-001, got %s", vc.DocumentID)
	}
	if vc.Req.SourceFileKey != expectedKey {
		t.Errorf("expected source_file_key=%s, got %s", expectedKey, vc.Req.SourceFileKey)
	}
	if vc.Req.SourceFileName != "contract.pdf" {
		t.Errorf("expected source_file_name=contract.pdf, got %s", vc.Req.SourceFileName)
	}
	if vc.Req.OriginType != "UPLOAD" {
		t.Errorf("expected origin_type=UPLOAD, got %s", vc.Req.OriginType)
	}

	// Verify command was published.
	if len(pub.calls) != 1 {
		t.Fatalf("expected 1 publish call, got %d", len(pub.calls))
	}
	cmd := pub.calls[0]
	if cmd.JobID != "uuid-002" {
		t.Errorf("expected job_id=uuid-002, got %s", cmd.JobID)
	}
	if cmd.DocumentID != "doc-001" {
		t.Errorf("expected document_id=doc-001, got %s", cmd.DocumentID)
	}
	if cmd.VersionID != "ver-001" {
		t.Errorf("expected version_id=ver-001, got %s", cmd.VersionID)
	}
	if cmd.SourceFileMIMEType != contentTypePDF {
		t.Errorf("expected mime=application/pdf, got %s", cmd.SourceFileMIMEType)
	}

	// Verify Redis tracking was saved.
	if len(kv.calls) != 1 {
		t.Fatalf("expected 1 KV set call, got %d", len(kv.calls))
	}
	if kv.calls[0].Key != "upload:org-456:uuid-002" {
		t.Errorf("expected redis key=upload:org-456:uuid-002, got %s", kv.calls[0].Key)
	}
	if kv.calls[0].TTL != uploadTrackingTTL {
		t.Errorf("expected TTL=%v, got %v", uploadTrackingTTL, kv.calls[0].TTL)
	}

	// Verify no S3 cleanup was triggered.
	if len(storage.deleteCalls) != 0 {
		t.Errorf("expected 0 S3 delete calls, got %d", len(storage.deleteCalls))
	}
}

func TestHandle_NoAuthContext(t *testing.T) {
	h := newTestHandler(&mockStorage{}, &mockDMClient{}, &mockPublisher{}, &mockKVStore{})

	req := createMultipartRequest(t, "Test", "contract.pdf", validPDFContent())
	// No auth context set.

	rr := httptest.NewRecorder()
	h.Handle().ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
	resp := parseErrorResponse(t, rr)
	if resp["error_code"] != "AUTH_TOKEN_MISSING" {
		t.Errorf("expected error_code=AUTH_TOKEN_MISSING, got %v", resp["error_code"])
	}
}

func TestHandle_MissingTitle(t *testing.T) {
	h := newTestHandler(&mockStorage{}, &mockDMClient{}, &mockPublisher{}, &mockKVStore{})

	req := createMultipartRequest(t, "", "contract.pdf", validPDFContent())
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.Handle().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	resp := parseErrorResponse(t, rr)
	if resp["error_code"] != "VALIDATION_ERROR" {
		t.Errorf("expected error_code=VALIDATION_ERROR, got %v", resp["error_code"])
	}
}

func TestHandle_WhitespaceOnlyTitle(t *testing.T) {
	h := newTestHandler(&mockStorage{}, &mockDMClient{}, &mockPublisher{}, &mockKVStore{})

	req := createMultipartRequest(t, "   \t  ", "contract.pdf", validPDFContent())
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.Handle().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	resp := parseErrorResponse(t, rr)
	if resp["error_code"] != "VALIDATION_ERROR" {
		t.Errorf("expected error_code=VALIDATION_ERROR, got %v", resp["error_code"])
	}
}

func TestHandle_TitleTooLong(t *testing.T) {
	h := newTestHandler(&mockStorage{}, &mockDMClient{}, &mockPublisher{}, &mockKVStore{})

	longTitle := strings.Repeat("A", 501)
	req := createMultipartRequest(t, longTitle, "contract.pdf", validPDFContent())
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.Handle().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	resp := parseErrorResponse(t, rr)
	if resp["error_code"] != "VALIDATION_ERROR" {
		t.Errorf("expected error_code=VALIDATION_ERROR, got %v", resp["error_code"])
	}
}

func TestHandle_TitleExactly500Chars(t *testing.T) {
	storage := &mockStorage{}
	dm := &mockDMClient{}
	pub := &mockPublisher{}
	kv := &mockKVStore{}
	h := newTestHandler(storage, dm, pub, kv)

	title500 := strings.Repeat("A", 500)
	req := createMultipartRequest(t, title500, "contract.pdf", validPDFContent())
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.Handle().ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202 for 500-char title, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandle_MissingFile(t *testing.T) {
	h := newTestHandler(&mockStorage{}, &mockDMClient{}, &mockPublisher{}, &mockKVStore{})

	// Create request with title but no file.
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.WriteField("title", "Test Contract")
	_ = w.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/contracts/upload", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.Handle().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
	resp := parseErrorResponse(t, rr)
	if resp["error_code"] != "VALIDATION_ERROR" {
		t.Errorf("expected error_code=VALIDATION_ERROR, got %v", resp["error_code"])
	}
}

func TestHandle_FileTooLarge(t *testing.T) {
	storage := &mockStorage{}
	dm := &mockDMClient{}
	pub := &mockPublisher{}
	kv := &mockKVStore{}
	h := newTestHandler(storage, dm, pub, kv)
	h.maxSize = 50 // 50 bytes max for test.

	content := validPDFContent() // 100 bytes > 50.
	req := createMultipartRequest(t, "Test", "contract.pdf", content)
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.Handle().ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", rr.Code, rr.Body.String())
	}
	resp := parseErrorResponse(t, rr)
	if resp["error_code"] != "FILE_TOO_LARGE" {
		t.Errorf("expected error_code=FILE_TOO_LARGE, got %v", resp["error_code"])
	}

	// No S3 upload should have been attempted.
	if len(storage.putCalls) != 0 {
		t.Errorf("expected 0 S3 put calls, got %d", len(storage.putCalls))
	}
}

func TestHandle_UnsupportedMIMEType(t *testing.T) {
	h := newTestHandler(&mockStorage{}, &mockDMClient{}, &mockPublisher{}, &mockKVStore{})

	// Create a multipart request where the file has a non-PDF content type.
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.WriteField("title", "Test Contract")

	// Create part with custom content type header.
	partHeader := make(map[string][]string)
	partHeader["Content-Disposition"] = []string{`form-data; name="file"; filename="doc.docx"`}
	partHeader["Content-Type"] = []string{"application/vnd.openxmlformats-officedocument.wordprocessingml.document"}
	part, _ := w.CreatePart(partHeader)
	part.Write(validPDFContent())
	_ = w.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/contracts/upload", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.Handle().ServeHTTP(rr, req)

	if rr.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415, got %d: %s", rr.Code, rr.Body.String())
	}
	resp := parseErrorResponse(t, rr)
	if resp["error_code"] != "UNSUPPORTED_FORMAT" {
		t.Errorf("expected error_code=UNSUPPORTED_FORMAT, got %v", resp["error_code"])
	}
}

func TestHandle_InvalidPDFMagicBytes(t *testing.T) {
	h := newTestHandler(&mockStorage{}, &mockDMClient{}, &mockPublisher{}, &mockKVStore{})

	// Create content that does NOT start with %PDF-.
	content := []byte("This is not a PDF file at all.")
	req := createMultipartRequest(t, "Test", "contract.pdf", content)
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.Handle().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
	resp := parseErrorResponse(t, rr)
	if resp["error_code"] != "INVALID_FILE" {
		t.Errorf("expected error_code=INVALID_FILE, got %v", resp["error_code"])
	}
}

func TestHandle_FileTooShortForMagic(t *testing.T) {
	h := newTestHandler(&mockStorage{}, &mockDMClient{}, &mockPublisher{}, &mockKVStore{})

	// File with only 3 bytes (less than pdfMagicLen=5).
	content := []byte("%PD")
	req := createMultipartRequest(t, "Test", "contract.pdf", content)
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.Handle().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
	resp := parseErrorResponse(t, rr)
	if resp["error_code"] != "INVALID_FILE" {
		t.Errorf("expected error_code=INVALID_FILE, got %v", resp["error_code"])
	}
}

func TestHandle_S3UploadFailure(t *testing.T) {
	storage := &mockStorage{
		putFn: func(_ context.Context, _ string, _ io.ReadSeeker, _ string) error {
			return errors.New("S3 unavailable")
		},
	}
	h := newTestHandler(storage, &mockDMClient{}, &mockPublisher{}, &mockKVStore{})

	req := createMultipartRequest(t, "Test", "contract.pdf", validPDFContent())
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.Handle().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", rr.Code, rr.Body.String())
	}
	resp := parseErrorResponse(t, rr)
	if resp["error_code"] != "STORAGE_UNAVAILABLE" {
		t.Errorf("expected error_code=STORAGE_UNAVAILABLE, got %v", resp["error_code"])
	}
}

func TestHandle_DMCreateDocumentFailure_CleansUpS3(t *testing.T) {
	storage := &mockStorage{}
	dm := &mockDMClient{
		createDocFn: func(_ context.Context, _ CreateDocumentRequest) (*Document, error) {
			return nil, errors.New("DM unavailable")
		},
	}
	h := newTestHandler(storage, dm, &mockPublisher{}, &mockKVStore{})

	req := createMultipartRequest(t, "Test", "contract.pdf", validPDFContent())
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.Handle().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", rr.Code, rr.Body.String())
	}
	resp := parseErrorResponse(t, rr)
	if resp["error_code"] != "DM_UNAVAILABLE" {
		t.Errorf("expected error_code=DM_UNAVAILABLE, got %v", resp["error_code"])
	}

	// Verify S3 was uploaded then cleaned up.
	if len(storage.putCalls) != 1 {
		t.Fatalf("expected 1 S3 put call, got %d", len(storage.putCalls))
	}
	if len(storage.deleteCalls) != 1 {
		t.Fatalf("expected 1 S3 delete call, got %d", len(storage.deleteCalls))
	}
	if storage.deleteCalls[0] != storage.putCalls[0].Key {
		t.Errorf("expected delete key=%s to match put key=%s",
			storage.deleteCalls[0], storage.putCalls[0].Key)
	}
}

func TestHandle_DMCreateVersionFailure_CleansUpS3(t *testing.T) {
	storage := &mockStorage{}
	dm := &mockDMClient{
		createVerFn: func(_ context.Context, _ string, _ CreateVersionRequest) (*DocumentVersion, error) {
			return nil, errors.New("DM version create failed")
		},
	}
	h := newTestHandler(storage, dm, &mockPublisher{}, &mockKVStore{})

	req := createMultipartRequest(t, "Test", "contract.pdf", validPDFContent())
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.Handle().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rr.Code)
	}

	// S3 cleanup should have been triggered.
	if len(storage.deleteCalls) != 1 {
		t.Fatalf("expected 1 S3 delete call, got %d", len(storage.deleteCalls))
	}
}

func TestHandle_BrokerPublishFailure_NoS3Cleanup(t *testing.T) {
	storage := &mockStorage{}
	pub := &mockPublisher{
		publishFn: func(_ context.Context, _ ProcessDocumentCommand) error {
			return errors.New("broker unavailable")
		},
	}
	h := newTestHandler(storage, &mockDMClient{}, pub, &mockKVStore{})

	req := createMultipartRequest(t, "Test", "contract.pdf", validPDFContent())
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.Handle().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", rr.Code)
	}
	resp := parseErrorResponse(t, rr)
	if resp["error_code"] != "BROKER_UNAVAILABLE" {
		t.Errorf("expected error_code=BROKER_UNAVAILABLE, got %v", resp["error_code"])
	}

	// NO S3 cleanup - version already exists in DM.
	if len(storage.deleteCalls) != 0 {
		t.Errorf("expected 0 S3 delete calls on broker failure, got %d", len(storage.deleteCalls))
	}
}

func TestHandle_RedisFailure_StillReturns202(t *testing.T) {
	kv := &mockKVStore{
		setFn: func(_ context.Context, _ string, _ string, _ time.Duration) error {
			return errors.New("redis unavailable")
		},
	}
	h := newTestHandler(&mockStorage{}, &mockDMClient{}, &mockPublisher{}, kv)

	req := createMultipartRequest(t, "Test", "contract.pdf", validPDFContent())
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.Handle().ServeHTTP(rr, req)

	// Redis failure is non-critical; upload should still succeed.
	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202 even with Redis failure, got %d", rr.Code)
	}
}

func TestHandle_ChecksumIsCorrect(t *testing.T) {
	storage := &mockStorage{}
	dm := &mockDMClient{}
	h := newTestHandler(storage, dm, &mockPublisher{}, &mockKVStore{})

	content := validPDFContent()
	req := createMultipartRequest(t, "Test", "contract.pdf", content)
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.Handle().ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rr.Code)
	}

	// Verify the checksum passed to DM is a valid hex-encoded SHA-256.
	if len(dm.createVerCalls) != 1 {
		t.Fatalf("expected 1 create version call, got %d", len(dm.createVerCalls))
	}
	checksum := dm.createVerCalls[0].Req.SourceFileChecksum
	if len(checksum) != 64 {
		t.Errorf("expected 64-char hex checksum, got %d chars: %s", len(checksum), checksum)
	}
}

func TestHandle_EmptyFile(t *testing.T) {
	h := newTestHandler(&mockStorage{}, &mockDMClient{}, &mockPublisher{}, &mockKVStore{})

	req := createMultipartRequest(t, "Test", "contract.pdf", []byte{})
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.Handle().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty file, got %d: %s", rr.Code, rr.Body.String())
	}
	resp := parseErrorResponse(t, rr)
	if resp["error_code"] != "INVALID_FILE" {
		t.Errorf("expected error_code=INVALID_FILE, got %v", resp["error_code"])
	}
}

func TestHandle_TitleTrimmedBeforeValidation(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(&mockStorage{}, dm, &mockPublisher{}, &mockKVStore{})

	req := createMultipartRequest(t, "  Trimmed Title  ", "contract.pdf", validPDFContent())
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.Handle().ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rr.Code)
	}

	// DM should receive the trimmed title.
	if len(dm.createDocCalls) != 1 {
		t.Fatalf("expected 1 DM call")
	}
	if dm.createDocCalls[0].Title != "Trimmed Title" {
		t.Errorf("expected title='Trimmed Title', got '%s'", dm.createDocCalls[0].Title)
	}
}

func TestHandle_UnicodeTitle(t *testing.T) {
	dm := &mockDMClient{}
	h := newTestHandler(&mockStorage{}, dm, &mockPublisher{}, &mockKVStore{})

	unicodeTitle := strings.Repeat("\u0414", 500) // 500 Cyrillic characters
	req := createMultipartRequest(t, unicodeTitle, "contract.pdf", validPDFContent())
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.Handle().ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202 for 500-char unicode title, got %d", rr.Code)
	}
}

func TestHandle_UnicodeTitleTooLong(t *testing.T) {
	h := newTestHandler(&mockStorage{}, &mockDMClient{}, &mockPublisher{}, &mockKVStore{})

	unicodeTitle := strings.Repeat("\u0414", 501) // 501 Cyrillic characters
	req := createMultipartRequest(t, unicodeTitle, "contract.pdf", validPDFContent())
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.Handle().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestHandle_S3KeyFormat(t *testing.T) {
	storage := &mockStorage{}
	h := newTestHandler(storage, &mockDMClient{}, &mockPublisher{}, &mockKVStore{})

	req := createMultipartRequest(t, "Test", "contract.pdf", validPDFContent())
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.Handle().ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rr.Code)
	}

	// uuid-001=correlation, uuid-002=job, uuid-003=file uuid.
	// S3 key: uploads/{org_id}/{job_id}/{uuid}
	expected := "uploads/org-456/uuid-002/uuid-003"
	if len(storage.putCalls) != 1 || storage.putCalls[0].Key != expected {
		t.Errorf("expected S3 key=%s, got %s", expected, storage.putCalls[0].Key)
	}
}

func TestHandle_RedisTrackingKeyAndPayload(t *testing.T) {
	kv := &mockKVStore{}
	h := newTestHandler(&mockStorage{}, &mockDMClient{}, &mockPublisher{}, kv)

	req := createMultipartRequest(t, "Test", "contract.pdf", validPDFContent())
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.Handle().ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rr.Code)
	}

	if len(kv.calls) != 1 {
		t.Fatalf("expected 1 KV call, got %d", len(kv.calls))
	}

	call := kv.calls[0]
	if call.Key != "upload:org-456:uuid-002" {
		t.Errorf("expected key=upload:org-456:uuid-002, got %s", call.Key)
	}
	if call.TTL != uploadTrackingTTL {
		t.Errorf("expected TTL=%v, got %v", uploadTrackingTTL, call.TTL)
	}

	// Verify the JSON payload has expected fields.
	var tracking uploadTracking
	if err := json.Unmarshal([]byte(call.Value), &tracking); err != nil {
		t.Fatalf("failed to unmarshal tracking: %v", err)
	}
	if tracking.JobID != "uuid-002" {
		t.Errorf("expected job_id=uuid-002, got %s", tracking.JobID)
	}
	if tracking.DocumentID != "doc-001" {
		t.Errorf("expected document_id=doc-001, got %s", tracking.DocumentID)
	}
	if tracking.VersionID != "ver-001" {
		t.Errorf("expected version_id=ver-001, got %s", tracking.VersionID)
	}
	if tracking.Status != "UPLOADED" {
		t.Errorf("expected status=UPLOADED, got %s", tracking.Status)
	}
	if tracking.OrganizationID != "org-456" {
		t.Errorf("expected org_id=org-456, got %s", tracking.OrganizationID)
	}
	if tracking.UserID != "user-123" {
		t.Errorf("expected user_id=user-123, got %s", tracking.UserID)
	}
}

func TestHandle_PublishedCommandFields(t *testing.T) {
	pub := &mockPublisher{}
	h := newTestHandler(&mockStorage{}, &mockDMClient{}, pub, &mockKVStore{})

	req := createMultipartRequest(t, "Test", "contract.pdf", validPDFContent())
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.Handle().ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rr.Code)
	}

	if len(pub.calls) != 1 {
		t.Fatalf("expected 1 publish call, got %d", len(pub.calls))
	}

	cmd := pub.calls[0]
	if cmd.JobID != "uuid-002" {
		t.Errorf("expected job_id=uuid-002, got %s", cmd.JobID)
	}
	if cmd.DocumentID != "doc-001" {
		t.Errorf("expected document_id=doc-001, got %s", cmd.DocumentID)
	}
	if cmd.VersionID != "ver-001" {
		t.Errorf("expected version_id=ver-001, got %s", cmd.VersionID)
	}
	if cmd.OrganizationID != "org-456" {
		t.Errorf("expected org_id=org-456, got %s", cmd.OrganizationID)
	}
	if cmd.RequestedByUserID != "user-123" {
		t.Errorf("expected user_id=user-123, got %s", cmd.RequestedByUserID)
	}
	if cmd.SourceFileMIMEType != "application/pdf" {
		t.Errorf("expected mime=application/pdf, got %s", cmd.SourceFileMIMEType)
	}
	if cmd.SourceFileKey != "uploads/org-456/uuid-002/uuid-003" {
		t.Errorf("expected s3 key, got %s", cmd.SourceFileKey)
	}
	if cmd.SourceFileName != "contract.pdf" {
		t.Errorf("expected file name=contract.pdf, got %s", cmd.SourceFileName)
	}
}

func TestHandle_NotMultipart(t *testing.T) {
	h := newTestHandler(&mockStorage{}, &mockDMClient{}, &mockPublisher{}, &mockKVStore{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/contracts/upload",
		strings.NewReader(`{"title":"test"}`))
	req.Header.Set("Content-Type", "application/json")
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.Handle().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-multipart request, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// sanitizeFilename tests
// ---------------------------------------------------------------------------

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "normal filename",
			input:    "contract.pdf",
			expected: "contract.pdf",
		},
		{
			name:     "path traversal with forward slashes",
			input:    "../../../etc/passwd",
			expected: "passwd",
		},
		{
			name:     "path traversal with backslashes",
			input:    "..\\..\\..\\etc\\passwd",
			expected: "passwd",
		},
		{
			name:     "embedded path traversal",
			input:    "foo/../bar.pdf",
			expected: "bar.pdf",
		},
		{
			name:     "null bytes",
			input:    "contract\x00.pdf",
			expected: "contract.pdf",
		},
		{
			name:     "control characters",
			input:    "contract\x01\x02\x1F.pdf",
			expected: "contract.pdf",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "upload.pdf",
		},
		{
			name:     "dot only",
			input:    ".",
			expected: "upload.pdf",
		},
		{
			name:     "dot-dot only",
			input:    "..",
			expected: "upload.pdf",
		},
		{
			name:     "long filename truncated",
			input:    strings.Repeat("a", 300) + ".pdf",
			expected: strings.Repeat("a", 255),
		},
		{
			name:     "unicode filename preserved",
			input:    "договор.pdf",
			expected: "договор.pdf",
		},
		{
			name:     "spaces preserved",
			input:    "my contract.pdf",
			expected: "my contract.pdf",
		},
		{
			name:     "absolute path stripped",
			input:    "/var/tmp/contract.pdf",
			expected: "contract.pdf",
		},
		{
			name:     "windows path stripped",
			input:    "C:\\Users\\test\\contract.pdf",
			expected: "contract.pdf",
		},
		{
			name:     "DEL character removed",
			input:    "contract\x7F.pdf",
			expected: "contract.pdf",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeFilename(tc.input)
			if got != tc.expected {
				t.Errorf("sanitizeFilename(%q) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// checksumReadSeeker tests
// ---------------------------------------------------------------------------

func TestChecksumReadSeeker_ResetOnSeek(t *testing.T) {
	content := []byte("hello world")
	rs := bytes.NewReader(content)
	hasher := &trackingHasher{resetCount: 0}

	crs := &checksumReadSeeker{rs: rs, hasher: hasher}

	// Read all data.
	buf := make([]byte, len(content))
	n, err := io.ReadFull(crs, buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if n != len(content) {
		t.Fatalf("read %d, expected %d", n, len(content))
	}

	// Seek to start.
	_, err = crs.Seek(0, io.SeekStart)
	if err != nil {
		t.Fatalf("seek: %v", err)
	}

	// Hash should have been reset.
	if hasher.resetCount != 1 {
		t.Errorf("expected 1 reset, got %d", hasher.resetCount)
	}
}

// trackingHasher tracks Reset() calls.
type trackingHasher struct {
	buf        bytes.Buffer
	resetCount int
}

func (h *trackingHasher) Write(p []byte) (int, error) { return h.buf.Write(p) }
func (h *trackingHasher) Sum(b []byte) []byte          { return append(b, h.buf.Bytes()...) }
func (h *trackingHasher) Reset()                       { h.resetCount++; h.buf.Reset() }

// ---------------------------------------------------------------------------
// Title control character validation tests
// ---------------------------------------------------------------------------

func TestHandle_TitleWithControlChars(t *testing.T) {
	h := newTestHandler(&mockStorage{}, &mockDMClient{}, &mockPublisher{}, &mockKVStore{})

	req := createMultipartRequest(t, "Contract\x01Name", "contract.pdf", validPDFContent())
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.Handle().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for title with control chars, got %d", rr.Code)
	}
	resp := parseErrorResponse(t, rr)
	if resp["error_code"] != "VALIDATION_ERROR" {
		t.Errorf("expected error_code=VALIDATION_ERROR, got %v", resp["error_code"])
	}
}

func TestHandle_TitleWithTab(t *testing.T) {
	h := newTestHandler(&mockStorage{}, &mockDMClient{}, &mockPublisher{}, &mockKVStore{})

	req := createMultipartRequest(t, "Contract\tName", "contract.pdf", validPDFContent())
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.Handle().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for title with tab, got %d", rr.Code)
	}
}

func TestHandle_TitleWithNullByte(t *testing.T) {
	h := newTestHandler(&mockStorage{}, &mockDMClient{}, &mockPublisher{}, &mockKVStore{})

	req := createMultipartRequest(t, "Contract\x00Name", "contract.pdf", validPDFContent())
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.Handle().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for title with null byte, got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// UTF-8 filename truncation test
// ---------------------------------------------------------------------------

func TestSanitizeFilename_UTF8Truncation(t *testing.T) {
	// 130 Cyrillic characters = 260 bytes, exceeds maxFilenameLen=255.
	// Truncation must NOT break a multi-byte character.
	input := strings.Repeat("Д", 130) + ".pdf"
	got := sanitizeFilename(input)

	if len(got) > 255 {
		t.Errorf("expected ≤255 bytes, got %d", len(got))
	}
	// Verify valid UTF-8 (no broken sequences).
	if !isValidUTF8(got) {
		t.Errorf("truncated filename is not valid UTF-8: %q", got)
	}
}

// isValidUTF8 checks that all bytes form valid UTF-8 sequences.
func isValidUTF8(s string) bool {
	for i := 0; i < len(s); {
		_, size := utf8.DecodeRuneInString(s[i:])
		if size == 0 {
			return false
		}
		i += size
	}
	return true
}

// ---------------------------------------------------------------------------
// Body size limit test (MaxBytesReader)
// ---------------------------------------------------------------------------

func TestHandle_OversizedRequestBody(t *testing.T) {
	h := newTestHandler(&mockStorage{}, &mockDMClient{}, &mockPublisher{}, &mockKVStore{})
	h.maxSize = 100 // 100 bytes max

	// Create a request body larger than maxSize + 1MB overhead.
	// The MaxBytesReader should reject it before full parsing.
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.WriteField("title", "Test")

	partHeader := make(map[string][]string)
	partHeader["Content-Disposition"] = []string{`form-data; name="file"; filename="huge.pdf"`}
	partHeader["Content-Type"] = []string{"application/pdf"}
	part, _ := w.CreatePart(partHeader)

	// Write PDF magic + large padding (larger than maxSize + 1MB).
	part.Write([]byte("%PDF-"))
	part.Write(bytes.Repeat([]byte("X"), 2<<20)) // 2 MB, exceeds 100 + 1MB
	_ = w.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/contracts/upload", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	h.Handle().ServeHTTP(rr, req)

	// Should fail with 400 (parse error from MaxBytesReader) or 413.
	if rr.Code == http.StatusAccepted {
		t.Fatalf("expected error for oversized body, got 202")
	}
}

// ---------------------------------------------------------------------------
// containsControlChars tests
// ---------------------------------------------------------------------------

func TestContainsControlChars(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"normal text", false},
		{"Кириллица 123", false},
		{"with\x00null", true},
		{"with\x01control", true},
		{"with\ttab", true},
		{"with\nnewline", true},
		{"with\x7Fdel", true},
		{"clean text 日本語", false},
	}
	for _, tc := range tests {
		got := containsControlChars(tc.input)
		if got != tc.expected {
			t.Errorf("containsControlChars(%q) = %v, want %v", tc.input, got, tc.expected)
		}
	}
}
