package watchdog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"contractpro/document-management/internal/config"
	"contractpro/document-management/internal/domain/model"
	"contractpro/document-management/internal/domain/port"
)

// ---------------------------------------------------------------------------
// Mock types
// ---------------------------------------------------------------------------

type mockTransactor struct {
	mu sync.Mutex
	fn func(ctx context.Context, f func(ctx context.Context) error) error
}

func (m *mockTransactor) WithTransaction(ctx context.Context, f func(ctx context.Context) error) error {
	m.mu.Lock()
	fn := m.fn
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, f)
	}
	return f(ctx)
}

type mockVersionRepo struct {
	mu                   sync.Mutex
	findStaleResult      []*model.DocumentVersion
	findStaleErr         error
	findForUpdateResult  map[string]*model.DocumentVersion
	findForUpdateErr     error
	findForUpdateCalls   int
	updateCalls          int
	updateErr            error
	nextVersionNumber    int
	insertErr            error
	findByIDResult       *model.DocumentVersion
	findByIDErr          error
	listResult           []*model.DocumentVersion
	listTotal            int
	listErr              error
}

func (m *mockVersionRepo) FindStaleInIntermediateStatus(_ context.Context, _ time.Time, _ int) ([]*model.DocumentVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.findStaleResult, m.findStaleErr
}

func (m *mockVersionRepo) FindByIDForUpdate(_ context.Context, orgID, docID, versionID string) (*model.DocumentVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.findForUpdateCalls++
	if m.findForUpdateErr != nil {
		return nil, m.findForUpdateErr
	}
	key := versionID
	if v, ok := m.findForUpdateResult[key]; ok {
		cp := *v
		return &cp, nil
	}
	return nil, port.NewVersionNotFoundError(versionID)
}

func (m *mockVersionRepo) Update(_ context.Context, v *model.DocumentVersion) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateCalls++
	if m.updateErr != nil {
		return m.updateErr
	}
	if m.findForUpdateResult != nil {
		if existing, ok := m.findForUpdateResult[v.VersionID]; ok {
			existing.ArtifactStatus = v.ArtifactStatus
		}
	}
	return nil
}

func (m *mockVersionRepo) Insert(_ context.Context, _ *model.DocumentVersion) error {
	return m.insertErr
}

func (m *mockVersionRepo) FindByID(_ context.Context, _, _, versionID string) (*model.DocumentVersion, error) {
	if m.findByIDResult != nil {
		cp := *m.findByIDResult
		return &cp, nil
	}
	return nil, m.findByIDErr
}

func (m *mockVersionRepo) List(_ context.Context, _, _ string, _, _ int) ([]*model.DocumentVersion, int, error) {
	return m.listResult, m.listTotal, m.listErr
}

func (m *mockVersionRepo) NextVersionNumber(_ context.Context, _, _ string) (int, error) {
	return m.nextVersionNumber, nil
}

func (m *mockVersionRepo) DeleteByDocument(_ context.Context, _ string) error {
	return nil
}

func (m *mockVersionRepo) ListByDocument(_ context.Context, _ string) ([]*model.DocumentVersion, error) {
	return []*model.DocumentVersion{}, nil
}

type mockArtifactRepo struct {
	mu          sync.Mutex
	listResult  map[string][]*model.ArtifactDescriptor
	listErr     error
	listCalls   int
}

func (m *mockArtifactRepo) Insert(_ context.Context, _ *model.ArtifactDescriptor) error { return nil }

func (m *mockArtifactRepo) FindByVersionAndType(_ context.Context, _, _, _ string, _ model.ArtifactType) (*model.ArtifactDescriptor, error) {
	return nil, nil
}

func (m *mockArtifactRepo) ListByVersion(_ context.Context, _, _, versionID string) ([]*model.ArtifactDescriptor, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listCalls++
	if m.listErr != nil {
		return nil, m.listErr
	}
	if m.listResult != nil {
		return m.listResult[versionID], nil
	}
	return []*model.ArtifactDescriptor{}, nil
}

func (m *mockArtifactRepo) ListByVersionAndTypes(_ context.Context, _, _, _ string, _ []model.ArtifactType) ([]*model.ArtifactDescriptor, error) {
	return nil, nil
}

func (m *mockArtifactRepo) DeleteByVersion(_ context.Context, _, _, _ string) error { return nil }

type mockAuditRepo struct {
	mu      sync.Mutex
	records []*model.AuditRecord
	err     error
}

func (m *mockAuditRepo) Insert(_ context.Context, record *model.AuditRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.records = append(m.records, record)
	return nil
}

func (m *mockAuditRepo) List(_ context.Context, _ port.AuditListParams) ([]*model.AuditRecord, int, error) {
	return nil, 0, nil
}

func (m *mockAuditRepo) DeleteByDocument(_ context.Context, _ string) error {
	return nil
}

type mockOutboxWriter struct {
	mu     sync.Mutex
	writes []outboxWrite
	err    error
}

type outboxWrite struct {
	AggregateID string
	Topic       string
	Event       any
}

func (m *mockOutboxWriter) Write(_ context.Context, aggregateID, topic string, event any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.writes = append(m.writes, outboxWrite{AggregateID: aggregateID, Topic: topic, Event: event})
	return nil
}

type mockMetrics struct {
	mu               sync.Mutex
	stuckTotal       int
	stuckGaugeValues []float64
}

func (m *mockMetrics) IncStuckVersionsTotal(count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stuckTotal += count
}

func (m *mockMetrics) SetStuckVersionsCount(count float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stuckGaugeValues = append(m.stuckGaugeValues, count)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&discardWriter{}, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

type discardWriter struct{}

func (d *discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func defaultWatchdogCfg() config.WatchdogConfig {
	return config.WatchdogConfig{
		ScanInterval: 100 * time.Millisecond,
		BatchSize:    100,
	}
}

func makeVersion(id, docID, orgID string, status model.ArtifactStatus) *model.DocumentVersion {
	return &model.DocumentVersion{
		VersionID:      id,
		DocumentID:     docID,
		OrganizationID: orgID,
		VersionNumber:  1,
		OriginType:     model.OriginTypeUpload,
		SourceFileKey:  "key",
		SourceFileName: "file.pdf",
		SourceFileSize: 100,
		ArtifactStatus: status,
		CreatedByUserID: "user-1",
		CreatedAt:      time.Now().UTC().Add(-1 * time.Hour),
	}
}

const testTopic = "dm.events.version-partially-available"

func newTestWatchdog(
	transactor port.Transactor,
	versionRepo port.VersionRepository,
	artifactRepo port.ArtifactRepository,
	auditRepo port.AuditRepository,
	outboxWriter OutboxWriter,
	metrics WatchdogMetrics,
) *StaleVersionWatchdog {
	return NewStaleVersionWatchdog(
		transactor, versionRepo, artifactRepo, auditRepo, outboxWriter,
		metrics, testLogger(),
		30*time.Minute, defaultWatchdogCfg(), testTopic,
	)
}

// ---------------------------------------------------------------------------
// Constructor tests
// ---------------------------------------------------------------------------

func TestNewStaleVersionWatchdog_PanicsOnNilDeps(t *testing.T) {
	tr := &mockTransactor{}
	vr := &mockVersionRepo{}
	ar := &mockArtifactRepo{}
	audit := &mockAuditRepo{}
	ow := &mockOutboxWriter{}
	m := &mockMetrics{}
	l := testLogger()

	tests := []struct {
		name string
		fn   func()
	}{
		{"nil transactor", func() { NewStaleVersionWatchdog(nil, vr, ar, audit, ow, m, l, time.Minute, defaultWatchdogCfg(), testTopic) }},
		{"nil versionRepo", func() { NewStaleVersionWatchdog(tr, nil, ar, audit, ow, m, l, time.Minute, defaultWatchdogCfg(), testTopic) }},
		{"nil artifactRepo", func() { NewStaleVersionWatchdog(tr, vr, nil, audit, ow, m, l, time.Minute, defaultWatchdogCfg(), testTopic) }},
		{"nil auditRepo", func() { NewStaleVersionWatchdog(tr, vr, ar, nil, ow, m, l, time.Minute, defaultWatchdogCfg(), testTopic) }},
		{"nil outboxWriter", func() { NewStaleVersionWatchdog(tr, vr, ar, audit, nil, m, l, time.Minute, defaultWatchdogCfg(), testTopic) }},
		{"nil metrics", func() { NewStaleVersionWatchdog(tr, vr, ar, audit, ow, nil, l, time.Minute, defaultWatchdogCfg(), testTopic) }},
		{"nil logger", func() { NewStaleVersionWatchdog(tr, vr, ar, audit, ow, m, nil, time.Minute, defaultWatchdogCfg(), testTopic) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Fatal("expected panic")
				}
			}()
			tt.fn()
		})
	}
}

func TestNewStaleVersionWatchdog_Success(t *testing.T) {
	w := newTestWatchdog(&mockTransactor{}, &mockVersionRepo{}, &mockArtifactRepo{}, &mockAuditRepo{}, &mockOutboxWriter{}, &mockMetrics{})
	if w == nil {
		t.Fatal("expected non-nil watchdog")
	}
}

// ---------------------------------------------------------------------------
// Scan tests
// ---------------------------------------------------------------------------

func TestScan_NoStaleVersions(t *testing.T) {
	vr := &mockVersionRepo{findStaleResult: []*model.DocumentVersion{}}
	audit := &mockAuditRepo{}
	ow := &mockOutboxWriter{}
	m := &mockMetrics{}

	w := newTestWatchdog(&mockTransactor{}, vr, &mockArtifactRepo{}, audit, ow, m)
	w.scan()

	if len(audit.records) != 0 {
		t.Errorf("expected no audit records, got %d", len(audit.records))
	}
	if len(ow.writes) != 0 {
		t.Errorf("expected no outbox writes, got %d", len(ow.writes))
	}
	if m.stuckTotal != 0 {
		t.Errorf("expected stuckTotal=0, got %d", m.stuckTotal)
	}
}

func TestScan_TransitionsStaleVersion_Pending(t *testing.T) {
	v := makeVersion("v-1", "doc-1", "org-1", model.ArtifactStatusPending)
	vr := &mockVersionRepo{
		findStaleResult: []*model.DocumentVersion{v},
		findForUpdateResult: map[string]*model.DocumentVersion{
			"v-1": makeVersion("v-1", "doc-1", "org-1", model.ArtifactStatusPending),
		},
	}
	artRepo := &mockArtifactRepo{
		listResult: map[string][]*model.ArtifactDescriptor{},
	}
	audit := &mockAuditRepo{}
	ow := &mockOutboxWriter{}
	m := &mockMetrics{}

	w := newTestWatchdog(&mockTransactor{}, vr, artRepo, audit, ow, m)
	w.scan()

	// Version should be transitioned.
	if vr.findForUpdateCalls != 1 {
		t.Errorf("expected 1 FindByIDForUpdate call, got %d", vr.findForUpdateCalls)
	}
	if vr.updateCalls != 1 {
		t.Errorf("expected 1 Update call, got %d", vr.updateCalls)
	}
	if len(audit.records) != 1 {
		t.Fatalf("expected 1 audit record, got %d", len(audit.records))
	}
	if audit.records[0].Action != model.AuditActionArtifactStatusChanged {
		t.Errorf("expected action ARTIFACT_STATUS_CHANGED, got %s", audit.records[0].Action)
	}
	if audit.records[0].ActorType != model.ActorTypeSystem {
		t.Errorf("expected actor type SYSTEM, got %s", audit.records[0].ActorType)
	}
	if audit.records[0].ActorID != "stale-version-watchdog" {
		t.Errorf("expected actor ID stale-version-watchdog, got %s", audit.records[0].ActorID)
	}
	if len(ow.writes) != 1 {
		t.Fatalf("expected 1 outbox write, got %d", len(ow.writes))
	}
	if ow.writes[0].Topic != testTopic {
		t.Errorf("expected topic %s, got %s", testTopic, ow.writes[0].Topic)
	}
	if ow.writes[0].AggregateID != "v-1" {
		t.Errorf("expected aggregate_id v-1, got %s", ow.writes[0].AggregateID)
	}
	if m.stuckTotal != 1 {
		t.Errorf("expected stuckTotal=1, got %d", m.stuckTotal)
	}
}

func TestScan_SkipsAlreadyTerminal(t *testing.T) {
	v := makeVersion("v-1", "doc-1", "org-1", model.ArtifactStatusPending)
	vr := &mockVersionRepo{
		findStaleResult: []*model.DocumentVersion{v},
		findForUpdateResult: map[string]*model.DocumentVersion{
			// By the time we lock, it's already FULLY_READY.
			"v-1": makeVersion("v-1", "doc-1", "org-1", model.ArtifactStatusFullyReady),
		},
	}
	audit := &mockAuditRepo{}
	ow := &mockOutboxWriter{}
	m := &mockMetrics{}

	w := newTestWatchdog(&mockTransactor{}, vr, &mockArtifactRepo{}, audit, ow, m)
	w.scan()

	if vr.updateCalls != 0 {
		t.Errorf("expected 0 Update calls (terminal), got %d", vr.updateCalls)
	}
	if len(audit.records) != 0 {
		t.Errorf("expected 0 audit records (terminal), got %d", len(audit.records))
	}
	if len(ow.writes) != 0 {
		t.Errorf("expected 0 outbox writes (terminal), got %d", len(ow.writes))
	}
}

func TestScan_AllIntermediateStatuses(t *testing.T) {
	statuses := []struct {
		status      model.ArtifactStatus
		failedStage string
	}{
		{model.ArtifactStatusPending, "document_processing"},
		{model.ArtifactStatusProcessingArtifactsReceived, "legal_analysis"},
		{model.ArtifactStatusAnalysisArtifactsReceived, "report_generation"},
		{model.ArtifactStatusReportsReady, "finalization"},
	}

	for _, tc := range statuses {
		t.Run(string(tc.status), func(t *testing.T) {
			id := fmt.Sprintf("v-%s", tc.status)
			v := makeVersion(id, "doc-1", "org-1", tc.status)
			vr := &mockVersionRepo{
				findStaleResult: []*model.DocumentVersion{v},
				findForUpdateResult: map[string]*model.DocumentVersion{
					id: makeVersion(id, "doc-1", "org-1", tc.status),
				},
			}
			artRepo := &mockArtifactRepo{listResult: map[string][]*model.ArtifactDescriptor{}}
			ow := &mockOutboxWriter{}

			w := newTestWatchdog(&mockTransactor{}, vr, artRepo, &mockAuditRepo{}, ow, &mockMetrics{})
			w.scan()

			if len(ow.writes) != 1 {
				t.Fatalf("expected 1 outbox write, got %d", len(ow.writes))
			}

			evt, ok := ow.writes[0].Event.(model.VersionPartiallyAvailable)
			if !ok {
				t.Fatal("expected VersionPartiallyAvailable event")
			}
			if evt.FailedStage != tc.failedStage {
				t.Errorf("expected FailedStage=%s, got %s", tc.failedStage, evt.FailedStage)
			}
			if evt.ArtifactStatus != tc.status {
				t.Errorf("expected ArtifactStatus=%s, got %s", tc.status, evt.ArtifactStatus)
			}
		})
	}
}

func TestScan_PartialFailure_ContinuesProcessing(t *testing.T) {
	v1 := makeVersion("v-1", "doc-1", "org-1", model.ArtifactStatusPending)
	v2 := makeVersion("v-2", "doc-2", "org-1", model.ArtifactStatusPending)

	// Audit repo that fails on the first call, succeeds on the second.
	auditRepo := &conditionalAuditRepo{
		failOnCall: 1,
		failErr:    errors.New("audit insert failed"),
	}

	vr := &mockVersionRepo{
		findStaleResult: []*model.DocumentVersion{v1, v2},
		findForUpdateResult: map[string]*model.DocumentVersion{
			"v-1": makeVersion("v-1", "doc-1", "org-1", model.ArtifactStatusPending),
			"v-2": makeVersion("v-2", "doc-2", "org-1", model.ArtifactStatusPending),
		},
	}
	artRepo := &mockArtifactRepo{listResult: map[string][]*model.ArtifactDescriptor{}}
	ow := &mockOutboxWriter{}
	m := &mockMetrics{}

	w := newTestWatchdog(&mockTransactor{}, vr, artRepo, auditRepo, ow, m)
	w.scan()

	// First version fails (audit), second succeeds.
	if m.stuckTotal != 1 {
		t.Errorf("expected stuckTotal=1 (partial), got %d", m.stuckTotal)
	}
	if len(ow.writes) != 1 {
		t.Errorf("expected 1 outbox write (second version), got %d", len(ow.writes))
	}
}

type conditionalAuditRepo struct {
	mu         sync.Mutex
	failOnCall int
	failErr    error
	callCount  int
	records    []*model.AuditRecord
}

func (m *conditionalAuditRepo) Insert(_ context.Context, record *model.AuditRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCount++
	if m.callCount == m.failOnCall {
		return m.failErr
	}
	m.records = append(m.records, record)
	return nil
}

func (m *conditionalAuditRepo) List(_ context.Context, _ port.AuditListParams) ([]*model.AuditRecord, int, error) {
	return nil, 0, nil
}

func (m *conditionalAuditRepo) DeleteByDocument(_ context.Context, _ string) error {
	return nil
}

func TestScan_AvailableTypesPopulated(t *testing.T) {
	v := makeVersion("v-1", "doc-1", "org-1", model.ArtifactStatusProcessingArtifactsReceived)
	vr := &mockVersionRepo{
		findStaleResult: []*model.DocumentVersion{v},
		findForUpdateResult: map[string]*model.DocumentVersion{
			"v-1": makeVersion("v-1", "doc-1", "org-1", model.ArtifactStatusProcessingArtifactsReceived),
		},
	}
	artRepo := &mockArtifactRepo{
		listResult: map[string][]*model.ArtifactDescriptor{
			"v-1": {
				{ArtifactType: model.ArtifactTypeOCRRaw},
				{ArtifactType: model.ArtifactTypeExtractedText},
				{ArtifactType: model.ArtifactTypeDocumentStructure},
			},
		},
	}
	ow := &mockOutboxWriter{}

	w := newTestWatchdog(&mockTransactor{}, vr, artRepo, &mockAuditRepo{}, ow, &mockMetrics{})
	w.scan()

	if len(ow.writes) != 1 {
		t.Fatalf("expected 1 outbox write, got %d", len(ow.writes))
	}
	evt := ow.writes[0].Event.(model.VersionPartiallyAvailable)
	if len(evt.AvailableTypes) != 3 {
		t.Errorf("expected 3 available types, got %d", len(evt.AvailableTypes))
	}
}

func TestScan_GaugeUpdatedWithStaleCount(t *testing.T) {
	v1 := makeVersion("v-1", "doc-1", "org-1", model.ArtifactStatusPending)
	v2 := makeVersion("v-2", "doc-2", "org-1", model.ArtifactStatusPending)
	vr := &mockVersionRepo{
		findStaleResult: []*model.DocumentVersion{v1, v2},
		findForUpdateResult: map[string]*model.DocumentVersion{
			"v-1": makeVersion("v-1", "doc-1", "org-1", model.ArtifactStatusPending),
			"v-2": makeVersion("v-2", "doc-2", "org-1", model.ArtifactStatusPending),
		},
	}
	artRepo := &mockArtifactRepo{listResult: map[string][]*model.ArtifactDescriptor{}}
	m := &mockMetrics{}

	w := newTestWatchdog(&mockTransactor{}, vr, artRepo, &mockAuditRepo{}, &mockOutboxWriter{}, m)
	w.scan()

	if len(m.stuckGaugeValues) == 0 {
		t.Fatal("expected SetStuckVersionsCount to be called")
	}
	if m.stuckGaugeValues[0] != 2 {
		t.Errorf("expected gauge=2, got %v", m.stuckGaugeValues[0])
	}
}

func TestScan_GaugeZeroWhenNoStale(t *testing.T) {
	vr := &mockVersionRepo{findStaleResult: []*model.DocumentVersion{}}
	m := &mockMetrics{}

	w := newTestWatchdog(&mockTransactor{}, vr, &mockArtifactRepo{}, &mockAuditRepo{}, &mockOutboxWriter{}, m)
	w.scan()

	if len(m.stuckGaugeValues) == 0 {
		t.Fatal("expected SetStuckVersionsCount to be called")
	}
	if m.stuckGaugeValues[0] != 0 {
		t.Errorf("expected gauge=0, got %v", m.stuckGaugeValues[0])
	}
}

func TestScan_DBErrorOnFind(t *testing.T) {
	vr := &mockVersionRepo{
		findStaleErr: errors.New("db connection lost"),
	}
	m := &mockMetrics{}

	w := newTestWatchdog(&mockTransactor{}, vr, &mockArtifactRepo{}, &mockAuditRepo{}, &mockOutboxWriter{}, m)
	// Should not panic, should log error.
	w.scan()

	if m.stuckTotal != 0 {
		t.Errorf("expected stuckTotal=0 on DB error, got %d", m.stuckTotal)
	}
}

func TestScan_AuditRecordFields(t *testing.T) {
	v := makeVersion("v-1", "doc-1", "org-1", model.ArtifactStatusAnalysisArtifactsReceived)
	vr := &mockVersionRepo{
		findStaleResult: []*model.DocumentVersion{v},
		findForUpdateResult: map[string]*model.DocumentVersion{
			"v-1": makeVersion("v-1", "doc-1", "org-1", model.ArtifactStatusAnalysisArtifactsReceived),
		},
	}
	artRepo := &mockArtifactRepo{listResult: map[string][]*model.ArtifactDescriptor{}}
	audit := &mockAuditRepo{}

	w := newTestWatchdog(&mockTransactor{}, vr, artRepo, audit, &mockOutboxWriter{}, &mockMetrics{})
	w.scan()

	if len(audit.records) != 1 {
		t.Fatalf("expected 1 audit record, got %d", len(audit.records))
	}

	r := audit.records[0]
	if r.OrganizationID != "org-1" {
		t.Errorf("expected org org-1, got %s", r.OrganizationID)
	}
	if r.DocumentID != "doc-1" {
		t.Errorf("expected doc doc-1, got %s", r.DocumentID)
	}
	if r.VersionID != "v-1" {
		t.Errorf("expected version v-1, got %s", r.VersionID)
	}
	if r.Action != model.AuditActionArtifactStatusChanged {
		t.Errorf("expected action ARTIFACT_STATUS_CHANGED, got %s", r.Action)
	}
	if r.ActorType != model.ActorTypeSystem {
		t.Errorf("expected actor type SYSTEM, got %s", r.ActorType)
	}
	if r.ActorID != "stale-version-watchdog" {
		t.Errorf("expected actor ID stale-version-watchdog, got %s", r.ActorID)
	}

	// Check details JSON.
	var details map[string]string
	if err := json.Unmarshal(r.Details, &details); err != nil {
		t.Fatalf("failed to unmarshal details: %v", err)
	}
	if details["from"] != string(model.ArtifactStatusAnalysisArtifactsReceived) {
		t.Errorf("expected from=ANALYSIS_ARTIFACTS_RECEIVED, got %s", details["from"])
	}
	if details["to"] != string(model.ArtifactStatusPartiallyAvailable) {
		t.Errorf("expected to=PARTIALLY_AVAILABLE, got %s", details["to"])
	}
	if details["reason"] != "stale_version_timeout" {
		t.Errorf("expected reason=stale_version_timeout, got %s", details["reason"])
	}
}

func TestScan_EventFields(t *testing.T) {
	v := makeVersion("v-1", "doc-1", "org-1", model.ArtifactStatusReportsReady)
	vr := &mockVersionRepo{
		findStaleResult: []*model.DocumentVersion{v},
		findForUpdateResult: map[string]*model.DocumentVersion{
			"v-1": makeVersion("v-1", "doc-1", "org-1", model.ArtifactStatusReportsReady),
		},
	}
	artRepo := &mockArtifactRepo{listResult: map[string][]*model.ArtifactDescriptor{}}
	ow := &mockOutboxWriter{}

	w := newTestWatchdog(&mockTransactor{}, vr, artRepo, &mockAuditRepo{}, ow, &mockMetrics{})
	w.scan()

	if len(ow.writes) != 1 {
		t.Fatalf("expected 1 outbox write, got %d", len(ow.writes))
	}

	evt := ow.writes[0].Event.(model.VersionPartiallyAvailable)
	if evt.DocumentID != "doc-1" {
		t.Errorf("expected DocumentID=doc-1, got %s", evt.DocumentID)
	}
	if evt.VersionID != "v-1" {
		t.Errorf("expected VersionID=v-1, got %s", evt.VersionID)
	}
	if evt.OrgID != "org-1" {
		t.Errorf("expected OrgID=org-1, got %s", evt.OrgID)
	}
	if evt.CorrelationID == "" {
		t.Error("expected non-empty CorrelationID")
	}
	if evt.Timestamp.IsZero() {
		t.Error("expected non-zero Timestamp")
	}
	if evt.ErrorMessage == "" {
		t.Error("expected non-empty ErrorMessage")
	}
}

func TestScan_FindByIDForUpdateError(t *testing.T) {
	v := makeVersion("v-1", "doc-1", "org-1", model.ArtifactStatusPending)
	vr := &mockVersionRepo{
		findStaleResult:  []*model.DocumentVersion{v},
		findForUpdateErr: errors.New("lock timeout"),
	}
	m := &mockMetrics{}

	w := newTestWatchdog(&mockTransactor{}, vr, &mockArtifactRepo{}, &mockAuditRepo{}, &mockOutboxWriter{}, m)
	w.scan()

	if m.stuckTotal != 0 {
		t.Errorf("expected stuckTotal=0 on lock error, got %d", m.stuckTotal)
	}
}

func TestScan_UpdateError(t *testing.T) {
	v := makeVersion("v-1", "doc-1", "org-1", model.ArtifactStatusPending)
	vr := &mockVersionRepo{
		findStaleResult: []*model.DocumentVersion{v},
		findForUpdateResult: map[string]*model.DocumentVersion{
			"v-1": makeVersion("v-1", "doc-1", "org-1", model.ArtifactStatusPending),
		},
		updateErr: errors.New("update failed"),
	}
	m := &mockMetrics{}

	w := newTestWatchdog(&mockTransactor{}, vr, &mockArtifactRepo{}, &mockAuditRepo{}, &mockOutboxWriter{}, m)
	w.scan()

	if m.stuckTotal != 0 {
		t.Errorf("expected stuckTotal=0 on update error, got %d", m.stuckTotal)
	}
}

func TestScan_OutboxWriteError(t *testing.T) {
	v := makeVersion("v-1", "doc-1", "org-1", model.ArtifactStatusPending)
	vr := &mockVersionRepo{
		findStaleResult: []*model.DocumentVersion{v},
		findForUpdateResult: map[string]*model.DocumentVersion{
			"v-1": makeVersion("v-1", "doc-1", "org-1", model.ArtifactStatusPending),
		},
	}
	artRepo := &mockArtifactRepo{listResult: map[string][]*model.ArtifactDescriptor{}}
	ow := &mockOutboxWriter{err: errors.New("outbox insert failed")}
	m := &mockMetrics{}

	w := newTestWatchdog(&mockTransactor{}, vr, artRepo, &mockAuditRepo{}, ow, m)
	w.scan()

	if m.stuckTotal != 0 {
		t.Errorf("expected stuckTotal=0 on outbox error, got %d", m.stuckTotal)
	}
}

func TestScan_ListArtifactsError(t *testing.T) {
	v := makeVersion("v-1", "doc-1", "org-1", model.ArtifactStatusPending)
	vr := &mockVersionRepo{
		findStaleResult: []*model.DocumentVersion{v},
		findForUpdateResult: map[string]*model.DocumentVersion{
			"v-1": makeVersion("v-1", "doc-1", "org-1", model.ArtifactStatusPending),
		},
	}
	artRepo := &mockArtifactRepo{listErr: errors.New("artifact list failed")}
	m := &mockMetrics{}

	w := newTestWatchdog(&mockTransactor{}, vr, artRepo, &mockAuditRepo{}, &mockOutboxWriter{}, m)
	w.scan()

	if m.stuckTotal != 0 {
		t.Errorf("expected stuckTotal=0 on artifact list error, got %d", m.stuckTotal)
	}
}

func TestScan_MultipleVersions_AllTransitioned(t *testing.T) {
	v1 := makeVersion("v-1", "doc-1", "org-1", model.ArtifactStatusPending)
	v2 := makeVersion("v-2", "doc-2", "org-1", model.ArtifactStatusProcessingArtifactsReceived)
	v3 := makeVersion("v-3", "doc-3", "org-1", model.ArtifactStatusReportsReady)

	vr := &mockVersionRepo{
		findStaleResult: []*model.DocumentVersion{v1, v2, v3},
		findForUpdateResult: map[string]*model.DocumentVersion{
			"v-1": makeVersion("v-1", "doc-1", "org-1", model.ArtifactStatusPending),
			"v-2": makeVersion("v-2", "doc-2", "org-1", model.ArtifactStatusProcessingArtifactsReceived),
			"v-3": makeVersion("v-3", "doc-3", "org-1", model.ArtifactStatusReportsReady),
		},
	}
	artRepo := &mockArtifactRepo{listResult: map[string][]*model.ArtifactDescriptor{}}
	audit := &mockAuditRepo{}
	ow := &mockOutboxWriter{}
	m := &mockMetrics{}

	w := newTestWatchdog(&mockTransactor{}, vr, artRepo, audit, ow, m)
	w.scan()

	if m.stuckTotal != 3 {
		t.Errorf("expected stuckTotal=3, got %d", m.stuckTotal)
	}
	if len(audit.records) != 3 {
		t.Errorf("expected 3 audit records, got %d", len(audit.records))
	}
	if len(ow.writes) != 3 {
		t.Errorf("expected 3 outbox writes, got %d", len(ow.writes))
	}
}

// ---------------------------------------------------------------------------
// Start/Stop tests
// ---------------------------------------------------------------------------

func TestStartStop_GracefulShutdown(t *testing.T) {
	vr := &mockVersionRepo{findStaleResult: []*model.DocumentVersion{}}
	w := newTestWatchdog(&mockTransactor{}, vr, &mockArtifactRepo{}, &mockAuditRepo{}, &mockOutboxWriter{}, &mockMetrics{})

	w.Start()

	// Let it run a cycle or two.
	time.Sleep(150 * time.Millisecond)

	w.Stop()

	select {
	case <-w.Done():
		// OK — goroutine exited.
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog goroutine did not exit within timeout")
	}
}

func TestStop_SafeToCallMultipleTimes(t *testing.T) {
	vr := &mockVersionRepo{findStaleResult: []*model.DocumentVersion{}}
	w := newTestWatchdog(&mockTransactor{}, vr, &mockArtifactRepo{}, &mockAuditRepo{}, &mockOutboxWriter{}, &mockMetrics{})

	w.Start()
	w.Stop()
	w.Stop() // Should not panic.
	<-w.Done()
}

// ---------------------------------------------------------------------------
// Helper tests
// ---------------------------------------------------------------------------

func TestFailedStageFromStatus(t *testing.T) {
	tests := []struct {
		status model.ArtifactStatus
		want   string
	}{
		{model.ArtifactStatusPending, "document_processing"},
		{model.ArtifactStatusProcessingArtifactsReceived, "legal_analysis"},
		{model.ArtifactStatusAnalysisArtifactsReceived, "report_generation"},
		{model.ArtifactStatusReportsReady, "finalization"},
		{model.ArtifactStatusFullyReady, "unknown"},
	}

	for _, tc := range tests {
		t.Run(string(tc.status), func(t *testing.T) {
			got := failedStageFromStatus(tc.status)
			if got != tc.want {
				t.Errorf("expected %s, got %s", tc.want, got)
			}
		})
	}
}

func TestExtractArtifactTypes(t *testing.T) {
	artifacts := []*model.ArtifactDescriptor{
		{ArtifactType: model.ArtifactTypeOCRRaw},
		{ArtifactType: model.ArtifactTypeExtractedText},
	}
	types := extractArtifactTypes(artifacts)
	if len(types) != 2 {
		t.Fatalf("expected 2 types, got %d", len(types))
	}
	if types[0] != model.ArtifactTypeOCRRaw {
		t.Errorf("expected OCR_RAW, got %s", types[0])
	}
	if types[1] != model.ArtifactTypeExtractedText {
		t.Errorf("expected EXTRACTED_TEXT, got %s", types[1])
	}
}

func TestExtractArtifactTypes_Empty(t *testing.T) {
	types := extractArtifactTypes(nil)
	if len(types) != 0 {
		t.Errorf("expected 0 types for nil, got %d", len(types))
	}
}

func TestGenerateUUID(t *testing.T) {
	id := generateUUID()
	if len(id) != 36 {
		t.Errorf("expected UUID length 36, got %d", len(id))
	}
	// Should be unique.
	id2 := generateUUID()
	if id == id2 {
		t.Error("expected unique UUIDs")
	}
}

func TestScan_TransactorErrorOnSecondVersion(t *testing.T) {
	v1 := makeVersion("v-1", "doc-1", "org-1", model.ArtifactStatusPending)
	v2 := makeVersion("v-2", "doc-2", "org-1", model.ArtifactStatusPending)

	vr := &mockVersionRepo{
		findStaleResult: []*model.DocumentVersion{v1, v2},
		findForUpdateResult: map[string]*model.DocumentVersion{
			"v-1": makeVersion("v-1", "doc-1", "org-1", model.ArtifactStatusPending),
			"v-2": makeVersion("v-2", "doc-2", "org-1", model.ArtifactStatusPending),
		},
	}

	// Transactor that succeeds on first call and fails on second.
	callCount := 0
	failingTransactor := &mockTransactor{
		fn: func(ctx context.Context, f func(ctx context.Context) error) error {
			callCount++
			if callCount == 1 {
				return f(ctx)
			}
			return context.Canceled
		},
	}

	artRepo := &mockArtifactRepo{listResult: map[string][]*model.ArtifactDescriptor{}}
	ow := &mockOutboxWriter{}
	m := &mockMetrics{}

	w := newTestWatchdog(failingTransactor, vr, artRepo, &mockAuditRepo{}, ow, m)
	w.scan()

	// First version succeeds, second fails.
	if m.stuckTotal != 1 {
		t.Errorf("expected stuckTotal=1, got %d", m.stuckTotal)
	}
}
