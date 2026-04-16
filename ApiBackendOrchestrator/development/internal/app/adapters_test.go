package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"contractpro/api-orchestrator/internal/application/upload"
	"contractpro/api-orchestrator/internal/config"
	"contractpro/api-orchestrator/internal/egress/commandpub"
	"contractpro/api-orchestrator/internal/egress/dmclient"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func newTestDMClient(t *testing.T, serverURL string) *dmclient.Client {
	t.Helper()
	return dmclient.NewClient(config.DMClientConfig{
		BaseURL:      serverURL,
		TimeoutRead:  5e9,
		TimeoutWrite: 5e9,
		RetryMax:     1,
		RetryBackoff: 1e6,
	}, config.CircuitBreakerConfig{
		FailureThreshold: 5,
		Timeout:          30e9,
		MaxRequests:      3,
	}, logger.NewLogger("error"))
}

// ---------------------------------------------------------------------------
// brokerSubscriberAdapter
// ---------------------------------------------------------------------------

func TestBrokerSubscriberAdapter_InterfaceShape(t *testing.T) {
	// Compile-time check is in adapters.go (var _ consumer.BrokerSubscriber = ...).
	// This test verifies the adapter can be constructed.
	adapter := &brokerSubscriberAdapter{client: nil}
	_ = adapter
}

// ---------------------------------------------------------------------------
// uploadDMAdapter
// ---------------------------------------------------------------------------

func TestUploadDMAdapter_CreateDocument_Success(t *testing.T) {
	dm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/documents" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["title"] != "Тестовый договор" {
			t.Errorf("title not forwarded: got %q", body["title"])
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"document_id": "doc-abc"})
	}))
	defer dm.Close()

	adapter := &uploadDMAdapter{client: newTestDMClient(t, dm.URL)}

	doc, err := adapter.CreateDocument(context.Background(), upload.CreateDocumentRequest{
		Title: "Тестовый договор",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if doc.DocumentID != "doc-abc" {
		t.Errorf("got DocumentID %q, want %q", doc.DocumentID, "doc-abc")
	}
}

func TestUploadDMAdapter_CreateDocument_Error(t *testing.T) {
	dm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer dm.Close()

	adapter := &uploadDMAdapter{client: newTestDMClient(t, dm.URL)}
	_, err := adapter.CreateDocument(context.Background(), upload.CreateDocumentRequest{Title: "x"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestUploadDMAdapter_CreateVersion_Success(t *testing.T) {
	dm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/documents/doc-1/versions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)

		if body["source_file_key"] != "key-1" {
			t.Errorf("source_file_key not forwarded")
		}
		if body["source_file_name"] != "name-1" {
			t.Errorf("source_file_name not forwarded")
		}
		if body["source_file_checksum"] != "check-1" {
			t.Errorf("source_file_checksum not forwarded")
		}
		if body["origin_type"] != "UPLOAD" {
			t.Errorf("origin_type not forwarded")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"version_id":     "ver-789",
			"version_number": 3,
		})
	}))
	defer dm.Close()

	adapter := &uploadDMAdapter{client: newTestDMClient(t, dm.URL)}

	ver, err := adapter.CreateVersion(context.Background(), "doc-1", upload.CreateVersionRequest{
		SourceFileKey:      "key-1",
		SourceFileName:     "name-1",
		SourceFileSize:     2048,
		SourceFileChecksum: "check-1",
		OriginType:         "UPLOAD",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ver.VersionID != "ver-789" {
		t.Errorf("got VersionID %q, want %q", ver.VersionID, "ver-789")
	}
	if ver.VersionNumber != 3 {
		t.Errorf("got VersionNumber %d, want %d", ver.VersionNumber, 3)
	}
}

func TestUploadDMAdapter_CreateVersion_Error(t *testing.T) {
	dm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": "conflict"})
	}))
	defer dm.Close()

	adapter := &uploadDMAdapter{client: newTestDMClient(t, dm.URL)}
	_, err := adapter.CreateVersion(context.Background(), "doc-1", upload.CreateVersionRequest{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestUploadDMAdapter_CreateVersion_AllFieldsMapped(t *testing.T) {
	var receivedBody map[string]any

	dm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"version_id": "v", "version_number": 1})
	}))
	defer dm.Close()

	adapter := &uploadDMAdapter{client: newTestDMClient(t, dm.URL)}

	_, _ = adapter.CreateVersion(context.Background(), "d", upload.CreateVersionRequest{
		SourceFileKey:      "key-1",
		SourceFileName:     "name-1",
		SourceFileSize:     999,
		SourceFileChecksum: "check-1",
		OriginType:         "RE_UPLOAD",
	})

	if receivedBody["source_file_key"] != "key-1" {
		t.Error("SourceFileKey not mapped")
	}
	if receivedBody["source_file_name"] != "name-1" {
		t.Error("SourceFileName not mapped")
	}
	if size, _ := receivedBody["source_file_size"].(float64); size != 999 {
		t.Error("SourceFileSize not mapped")
	}
	if receivedBody["source_file_checksum"] != "check-1" {
		t.Error("SourceFileChecksum not mapped")
	}
	if receivedBody["origin_type"] != "RE_UPLOAD" {
		t.Error("OriginType not mapped")
	}
}

// ---------------------------------------------------------------------------
// uploadCmdPubAdapter
// ---------------------------------------------------------------------------

type mockBrokerPublisher struct {
	topic   string
	payload []byte
	err     error
}

func (m *mockBrokerPublisher) Publish(ctx context.Context, topic string, payload []byte) error {
	m.topic = topic
	m.payload = payload
	return m.err
}

func TestUploadCmdPubAdapter_PublishProcessDocument_AllFields(t *testing.T) {
	mock := &mockBrokerPublisher{}
	pub := commandpub.NewPublisher(mock, "dp.commands.process-document", "dp.commands.compare-versions", "orch.commands.user-confirmed-type", logger.NewLogger("error"))
	adapter := &uploadCmdPubAdapter{pub: pub}

	cmd := upload.ProcessDocumentCommand{
		JobID:              "j-1",
		DocumentID:         "d-1",
		VersionID:          "v-1",
		OrganizationID:     "o-1",
		RequestedByUserID:  "u-1",
		SourceFileKey:      "k-1",
		SourceFileName:     "n-1",
		SourceFileSize:     42,
		SourceFileChecksum: "c-1",
		SourceFileMIMEType: "application/pdf",
	}

	err := adapter.PublishProcessDocument(context.Background(), cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mock.topic != "dp.commands.process-document" {
		t.Errorf("got topic %q, want %q", mock.topic, "dp.commands.process-document")
	}

	var envelope map[string]any
	if err := json.Unmarshal(mock.payload, &envelope); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	checks := map[string]any{
		"job_id":               "j-1",
		"document_id":         "d-1",
		"version_id":          "v-1",
		"organization_id":     "o-1",
		"requested_by_user_id": "u-1",
		"source_file_key":     "k-1",
		"source_file_name":    "n-1",
		"source_file_checksum": "c-1",
		"source_file_mime_type": "application/pdf",
	}
	for k, want := range checks {
		if envelope[k] != want {
			t.Errorf("%s: got %v, want %v", k, envelope[k], want)
		}
	}
	if size, ok := envelope["source_file_size"].(float64); !ok || size != 42 {
		t.Errorf("source_file_size: got %v, want 42", envelope["source_file_size"])
	}
}

func TestUploadCmdPubAdapter_PublishProcessDocument_BrokerError(t *testing.T) {
	mock := &mockBrokerPublisher{err: errors.New("broker down")}
	pub := commandpub.NewPublisher(mock, "dp.commands.process-document", "dp.commands.compare-versions", "orch.commands.user-confirmed-type", logger.NewLogger("error"))
	adapter := &uploadCmdPubAdapter{pub: pub}

	err := adapter.PublishProcessDocument(context.Background(), upload.ProcessDocumentCommand{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
