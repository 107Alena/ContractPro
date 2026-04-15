package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"contractpro/api-orchestrator/internal/application/comparison"
	"contractpro/api-orchestrator/internal/application/contracts"
	"contractpro/api-orchestrator/internal/application/export"
	"contractpro/api-orchestrator/internal/application/results"
	"contractpro/api-orchestrator/internal/application/statustracker"
	"contractpro/api-orchestrator/internal/application/upload"
	"contractpro/api-orchestrator/internal/application/versions"
	"contractpro/api-orchestrator/internal/config"
	"contractpro/api-orchestrator/internal/egress/commandpub"
	"contractpro/api-orchestrator/internal/egress/dmclient"
	"contractpro/api-orchestrator/internal/egress/ssebroadcast"
	"contractpro/api-orchestrator/internal/infra/health"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/api"
	"contractpro/api-orchestrator/internal/ingress/consumer"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"
	"contractpro/api-orchestrator/internal/ingress/middleware/rbac"
	"contractpro/api-orchestrator/internal/ingress/sse"
)

// testEnv holds all wired components for integration testing.
type testEnv struct {
	t          *testing.T
	server     *httptest.Server
	kvStore    *fakeKVStore
	dmFake     *fakeDMServer
	brokerFake *fakeBroker
	s3Fake     *fakeObjectStorage
	sseHandler *sse.Handler
	consumer   *consumer.Consumer
	tracker    *statustracker.Tracker
	jwtSigner  *testJWTSigner
}

// newTestEnv wires everything together using fakes instead of real
// infrastructure, mirroring the production app.go NewApp wiring.
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	// 1. Logger (debug level to capture all log output).
	log := logger.NewLogger("debug")

	// 2. Fakes.
	kvFake := newFakeKVStore()
	brokerFk := newFakeBroker()
	s3Fake := newFakeObjectStorage()
	dmFake := newFakeDMServer()

	// 3. SSE broadcaster — real implementation backed by fakeKVStore.Publish.
	broadcaster := ssebroadcast.NewBroadcaster(kvFake, log)

	// 4. Status tracker — real implementation backed by fakeKVStore.
	tracker := statustracker.NewTracker(kvFake, broadcaster, log)

	// 5. Broker config with default test topic names.
	brokerCfg := config.BrokerConfig{
		TopicProcessDocument:        "dp.commands.process-document",
		TopicCompareVersions:        "dp.commands.compare-versions",
		TopicDPStatusChanged:        "dp.events.status-changed",
		TopicDPProcessingCompleted:  "dp.events.processing-completed",
		TopicDPProcessingFailed:     "dp.events.processing-failed",
		TopicDPComparisonCompleted:  "dp.events.comparison-completed",
		TopicDPComparisonFailed:     "dp.events.comparison-failed",
		TopicLICStatusChanged:           "lic.events.status-changed",
		TopicLICClassificationUncertain: "lic.events.classification-uncertain",
		TopicREStatusChanged:            "re.events.status-changed",
		TopicDMVersionArtifactsReady: "dm.events.version-artifacts-ready",
		TopicDMVersionAnalysisReady:  "dm.events.version-analysis-ready",
		TopicDMVersionReportsReady:   "dm.events.version-reports-ready",
		TopicDMVersionPartiallyAvail: "dm.events.version-partially-available",
		TopicDMVersionCreated:        "dm.events.version-created",
	}

	// 6. Event consumer — real consumer with fake broker.
	eventConsumer := consumer.NewConsumer(brokerFk, tracker, log, brokerCfg)

	// 7. JWT signer — generates ECDSA P-256 key pair for test tokens.
	jwtSigner := newTestJWTSigner()

	// 8. Auth middleware — real auth with test public key.
	authMW, err := auth.NewMiddleware(jwtSigner.PublicKey, log)
	if err != nil {
		t.Fatalf("newTestEnv: auth middleware: %v", err)
	}

	// 9. RBAC middleware — real RBAC.
	rbacMW := rbac.NewMiddleware(log)

	// 10. DM client — real HTTP client pointed at fake DM server.
	dmClientCfg := config.DMClientConfig{
		BaseURL:      dmFake.URL(),
		TimeoutRead:  5 * time.Second,
		TimeoutWrite: 5 * time.Second,
		RetryMax:     1,
		RetryBackoff: 10 * time.Millisecond,
	}
	cbCfg := config.CircuitBreakerConfig{
		FailureThreshold: 10,
		Timeout:          30 * time.Second,
		MaxRequests:      5,
	}
	dmClient := dmclient.NewClient(dmClientCfg, cbCfg, log)

	// 11. Command publisher — real publisher backed by fakeBroker.
	cmdPub := commandpub.NewPublisher(
		brokerFk,
		brokerCfg.TopicProcessDocument,
		brokerCfg.TopicCompareVersions,
		log,
	)

	// 12. Upload handler — uses adapters to bridge upload.DMClient
	//     and upload.CommandPublisher interfaces.
	uploadDM := &testUploadDMAdapter{client: dmClient}
	uploadCmd := &testUploadCmdPubAdapter{pub: cmdPub}
	uploadHandler := upload.NewHandler(s3Fake, uploadDM, uploadCmd, kvFake, log, 20<<20)

	// 13. Application handlers.
	contractHandler := contracts.NewHandler(dmClient, log)
	versionHandler := versions.NewHandler(dmClient, s3Fake, cmdPub, kvFake, log, 20<<20)
	resultsHandler := results.NewHandler(dmClient, log)
	comparisonHandler := comparison.NewHandler(dmClient, cmdPub, log)
	exportHandler := export.NewHandler(dmClient, log)

	// 14. SSE handler — uses auth middleware as token validator and
	//     fakeKVStore directly (fakeKVStore implements sse.KVStore).
	sseCfg := config.SSEConfig{
		HeartbeatInterval: 15 * time.Second,
		MaxConnectionAge:  1 * time.Hour,
	}
	sseHandler := sse.NewHandler(authMW, kvFake, sseCfg, log)

	// 15. Health handler — uses stubs since we have no real Redis/broker
	//     in integration tests. The health endpoint still routes correctly.
	healthHandler := health.NewHandler(&noopRedisPinger{}, &noopBrokerPinger{}, dmFake.URL())

	// 16. HTTP server — assembles all handlers and middleware.
	srv := api.NewServer(api.Deps{
		Config: config.HTTPConfig{
			Port:            0, // unused, we use httptest
			MetricsPort:     0, // unused
			RequestTimeout:  30 * time.Second,
			UploadTimeout:   60 * time.Second,
			ShutdownTimeout: 5 * time.Second,
		},
		CORSConfig:          config.CORSConfig{},
		Health:              healthHandler,
		Logger:              log,
		AuthMiddleware:      authMW.Handler(),
		RBACMiddleware:      rbacMW.Handler(),
		RateLimitMiddleware: nil, // no rate limiting in tests
		UploadHandler:       uploadHandler.Handle(),
		ContractHandler:     contractHandler,
		VersionHandler:      versionHandler,
		ResultsHandler:      resultsHandler,
		ComparisonHandler:   comparisonHandler,
		ExportHandler:       exportHandler,
		SSEHandler:          sseHandler,
	})

	// 17. Start the event consumer (registers topic handlers on fakeBroker).
	if err := eventConsumer.Start(); err != nil {
		dmFake.Close()
		t.Fatalf("newTestEnv: consumer start: %v", err)
	}

	// 18. Create httptest.Server from the chi router.
	testServer := httptest.NewServer(srv.Router())

	env := &testEnv{
		t:          t,
		server:     testServer,
		kvStore:    kvFake,
		dmFake:     dmFake,
		brokerFake: brokerFk,
		s3Fake:     s3Fake,
		sseHandler: sseHandler,
		consumer:   eventConsumer,
		tracker:    tracker,
		jwtSigner:  jwtSigner,
	}

	t.Cleanup(env.Close)
	return env
}

// Close tears down the test environment.
func (e *testEnv) Close() {
	if e.sseHandler != nil {
		e.sseHandler.Shutdown()
	}
	if e.server != nil {
		e.server.Close()
	}
	if e.dmFake != nil {
		e.dmFake.Close()
	}
}

// DoRequest sends an authenticated HTTP request to the test server.
func (e *testEnv) DoRequest(method, path string, body io.Reader, userID, orgID string, role auth.Role) *http.Response {
	e.t.Helper()

	url := e.server.URL + path
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		e.t.Fatalf("DoRequest: new request: %v", err)
	}

	token := e.jwtSigner.SignToken(userID, orgID, role)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatalf("DoRequest: do: %v", err)
	}
	return resp
}

// UploadContract sends a multipart upload request with a minimal PDF payload.
func (e *testEnv) UploadContract(title string, pdfContent []byte, userID, orgID string) *http.Response {
	e.t.Helper()

	// Build multipart body manually using a simple boundary.
	boundary := "----TestBoundary" + fmt.Sprintf("%d", time.Now().UnixNano())
	var sb strings.Builder
	sb.WriteString("--" + boundary + "\r\n")
	sb.WriteString(`Content-Disposition: form-data; name="title"` + "\r\n\r\n")
	sb.WriteString(title + "\r\n")
	sb.WriteString("--" + boundary + "\r\n")
	sb.WriteString(`Content-Disposition: form-data; name="file"; filename="test.pdf"` + "\r\n")
	sb.WriteString("Content-Type: application/pdf\r\n\r\n")

	var bodyParts []byte
	bodyParts = append(bodyParts, []byte(sb.String())...)
	bodyParts = append(bodyParts, pdfContent...)
	bodyParts = append(bodyParts, []byte("\r\n--"+boundary+"--\r\n")...)

	url := e.server.URL + "/api/v1/contracts/upload"
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(string(bodyParts)))
	if err != nil {
		e.t.Fatalf("UploadContract: new request: %v", err)
	}

	token := e.jwtSigner.SignToken(userID, orgID, auth.RoleLawyer)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatalf("UploadContract: do: %v", err)
	}
	return resp
}

// InjectEvent injects an event into the fake broker, triggering the
// consumer's registered handler.
func (e *testEnv) InjectEvent(topic string, payload []byte) error {
	return e.brokerFake.InjectEvent(topic, payload)
}

// SeedStatus writes a status record directly into the fake KV store,
// matching the statustracker key format.
func (e *testEnv) SeedStatus(orgID, docID, verID, status string) {
	e.t.Helper()
	key := "status:" + orgID + ":" + docID + ":" + verID
	record := struct {
		Status    string `json:"status"`
		UpdatedAt string `json:"updated_at"`
	}{
		Status:    status,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	data, _ := json.Marshal(record)
	e.kvStore.SetDirect(key, string(data))
}

// ---------------------------------------------------------------------------
// Adapters for the upload handler
// ---------------------------------------------------------------------------

// testUploadDMAdapter bridges *dmclient.Client to upload.DMClient.
// Mirrors the production uploadDMAdapter in app/adapters.go.
type testUploadDMAdapter struct {
	client *dmclient.Client
}

func (a *testUploadDMAdapter) CreateDocument(ctx context.Context, req upload.CreateDocumentRequest) (*upload.Document, error) {
	doc, err := a.client.CreateDocument(ctx, dmclient.CreateDocumentRequest{
		Title: req.Title,
	})
	if err != nil {
		return nil, err
	}
	return &upload.Document{DocumentID: doc.DocumentID}, nil
}

func (a *testUploadDMAdapter) CreateVersion(ctx context.Context, documentID string, req upload.CreateVersionRequest) (*upload.DocumentVersion, error) {
	ver, err := a.client.CreateVersion(ctx, documentID, dmclient.CreateVersionRequest{
		SourceFileKey:      req.SourceFileKey,
		SourceFileName:     req.SourceFileName,
		SourceFileSize:     req.SourceFileSize,
		SourceFileChecksum: req.SourceFileChecksum,
		OriginType:         req.OriginType,
	})
	if err != nil {
		return nil, err
	}
	return &upload.DocumentVersion{
		VersionID:     ver.VersionID,
		VersionNumber: ver.VersionNumber,
	}, nil
}

// testUploadCmdPubAdapter bridges *commandpub.Publisher to upload.CommandPublisher.
type testUploadCmdPubAdapter struct {
	pub *commandpub.Publisher
}

func (a *testUploadCmdPubAdapter) PublishProcessDocument(ctx context.Context, cmd upload.ProcessDocumentCommand) error {
	return a.pub.PublishProcessDocument(ctx, commandpub.ProcessDocumentCommand{
		JobID:              cmd.JobID,
		DocumentID:         cmd.DocumentID,
		VersionID:          cmd.VersionID,
		OrganizationID:     cmd.OrganizationID,
		RequestedByUserID:  cmd.RequestedByUserID,
		SourceFileKey:      cmd.SourceFileKey,
		SourceFileName:     cmd.SourceFileName,
		SourceFileSize:     cmd.SourceFileSize,
		SourceFileChecksum: cmd.SourceFileChecksum,
		SourceFileMIMEType: cmd.SourceFileMIMEType,
	})
}

// ---------------------------------------------------------------------------
// Stubs for health handler dependencies
// ---------------------------------------------------------------------------

// noopRedisPinger satisfies health.RedisPinger without a real Redis.
type noopRedisPinger struct{}

func (p *noopRedisPinger) Ping(_ context.Context) error { return nil }

// noopBrokerPinger satisfies health.BrokerPinger without a real RabbitMQ.
type noopBrokerPinger struct{}

func (p *noopBrokerPinger) Ping() error { return nil }
