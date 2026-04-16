package watchdog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
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
	mu                  sync.Mutex
	findStaleResult     []*model.DocumentVersion
	findStaleErr        error
	findStaleCutoffs    map[model.ArtifactStatus]time.Time
	findForUpdateResult map[string]*model.DocumentVersion
	findForUpdateErr    error
	findForUpdateCalls  int
	updateCalls         int
	updateErr           error
	nextVersionNumber   int
	insertErr           error
	findByIDResult      *model.DocumentVersion
	findByIDErr         error
	listResult          []*model.DocumentVersion
	listTotal           int
	listErr             error
}

func (m *mockVersionRepo) FindStaleInIntermediateStatus(_ context.Context, cutoffs map[model.ArtifactStatus]time.Time, _ int) ([]*model.DocumentVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Snapshot the cutoffs the watchdog passed so tests can assert on them.
	m.findStaleCutoffs = make(map[model.ArtifactStatus]time.Time, len(cutoffs))
	for k, v := range cutoffs {
		m.findStaleCutoffs[k] = v
	}
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
	mu         sync.Mutex
	listResult map[string][]*model.ArtifactDescriptor
	listErr    error
	listCalls  int
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

type stuckGaugeSample struct {
	stage string
	count float64
}

type stuckTotalInc struct {
	stage string
	count int
}

type mockMetrics struct {
	mu               sync.Mutex
	totalByStage     map[string]int
	gaugeByStage     map[string]float64
	totalIncs        []stuckTotalInc
	gaugeSamples     []stuckGaugeSample
}

func newMockMetrics() *mockMetrics {
	return &mockMetrics{
		totalByStage: make(map[string]int),
		gaugeByStage: make(map[string]float64),
	}
}

func (m *mockMetrics) IncStuckVersionsTotal(stage string, count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.totalByStage == nil {
		m.totalByStage = make(map[string]int)
	}
	m.totalByStage[stage] += count
	m.totalIncs = append(m.totalIncs, stuckTotalInc{stage: stage, count: count})
}

func (m *mockMetrics) SetStuckVersionsCount(stage string, count float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.gaugeByStage == nil {
		m.gaugeByStage = make(map[string]float64)
	}
	m.gaugeByStage[stage] = count
	m.gaugeSamples = append(m.gaugeSamples, stuckGaugeSample{stage: stage, count: count})
}

// totalAcrossStages sums total counter increments across all stages.
func (m *mockMetrics) totalAcrossStages() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	sum := 0
	for _, v := range m.totalByStage {
		sum += v
	}
	return sum
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&discardWriter{}, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

type discardWriter struct{}

func (d *discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// defaultWatchdogCfg returns a WatchdogConfig with explicit per-stage timeouts
// so tests exercise the DM-TASK-053 code path by default.
func defaultWatchdogCfg() config.WatchdogConfig {
	return config.WatchdogConfig{
		ScanInterval:             100 * time.Millisecond,
		BatchSize:                100,
		StaleTimeoutProcessing:   5 * time.Minute,
		StaleTimeoutAnalysis:     10 * time.Minute,
		StaleTimeoutReports:      5 * time.Minute,
		StaleTimeoutFinalization: 5 * time.Minute,
	}
}

func makeVersion(id, docID, orgID string, status model.ArtifactStatus) *model.DocumentVersion {
	return &model.DocumentVersion{
		VersionID:       id,
		DocumentID:      docID,
		OrganizationID:  orgID,
		VersionNumber:   1,
		OriginType:      model.OriginTypeUpload,
		SourceFileKey:   "key",
		SourceFileName:  "file.pdf",
		SourceFileSize:  100,
		ArtifactStatus:  status,
		CreatedByUserID: "user-1",
		CreatedAt:       time.Now().UTC().Add(-1 * time.Hour),
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
		defaultWatchdogCfg(), testTopic,
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
	m := newMockMetrics()
	l := testLogger()

	tests := []struct {
		name string
		fn   func()
	}{
		{"nil transactor", func() { NewStaleVersionWatchdog(nil, vr, ar, audit, ow, m, l, defaultWatchdogCfg(), testTopic) }},
		{"nil versionRepo", func() { NewStaleVersionWatchdog(tr, nil, ar, audit, ow, m, l, defaultWatchdogCfg(), testTopic) }},
		{"nil artifactRepo", func() { NewStaleVersionWatchdog(tr, vr, nil, audit, ow, m, l, defaultWatchdogCfg(), testTopic) }},
		{"nil auditRepo", func() { NewStaleVersionWatchdog(tr, vr, ar, nil, ow, m, l, defaultWatchdogCfg(), testTopic) }},
		{"nil outboxWriter", func() { NewStaleVersionWatchdog(tr, vr, ar, audit, nil, m, l, defaultWatchdogCfg(), testTopic) }},
		{"nil metrics", func() { NewStaleVersionWatchdog(tr, vr, ar, audit, ow, nil, l, defaultWatchdogCfg(), testTopic) }},
		{"nil logger", func() { NewStaleVersionWatchdog(tr, vr, ar, audit, ow, m, nil, defaultWatchdogCfg(), testTopic) }},
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
	w := newTestWatchdog(&mockTransactor{}, &mockVersionRepo{}, &mockArtifactRepo{}, &mockAuditRepo{}, &mockOutboxWriter{}, newMockMetrics())
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
	m := newMockMetrics()

	w := newTestWatchdog(&mockTransactor{}, vr, &mockArtifactRepo{}, audit, ow, m)
	w.scan()

	if len(audit.records) != 0 {
		t.Errorf("expected no audit records, got %d", len(audit.records))
	}
	if len(ow.writes) != 0 {
		t.Errorf("expected no outbox writes, got %d", len(ow.writes))
	}
	if m.totalAcrossStages() != 0 {
		t.Errorf("expected stuckTotal=0, got %d", m.totalAcrossStages())
	}

	// All four stage gauges should be reset to zero even when no versions are stale.
	for _, stage := range []string{"processing", "analysis", "reports", "finalization"} {
		if got := m.gaugeByStage[stage]; got != 0 {
			t.Errorf("expected gauge[%s]=0, got %v", stage, got)
		}
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
	m := newMockMetrics()

	w := newTestWatchdog(&mockTransactor{}, vr, artRepo, audit, ow, m)
	w.scan()

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
	if m.totalByStage["processing"] != 1 {
		t.Errorf("expected stuckTotal[processing]=1, got %d", m.totalByStage["processing"])
	}
}

func TestScan_SkipsAlreadyTerminal(t *testing.T) {
	v := makeVersion("v-1", "doc-1", "org-1", model.ArtifactStatusPending)
	vr := &mockVersionRepo{
		findStaleResult: []*model.DocumentVersion{v},
		findForUpdateResult: map[string]*model.DocumentVersion{
			"v-1": makeVersion("v-1", "doc-1", "org-1", model.ArtifactStatusFullyReady),
		},
	}
	audit := &mockAuditRepo{}
	ow := &mockOutboxWriter{}
	m := newMockMetrics()

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
	cases := []struct {
		status         model.ArtifactStatus
		failedStage    string
		metricStage    string
	}{
		{model.ArtifactStatusPending, "document_processing", "processing"},
		{model.ArtifactStatusProcessingArtifactsReceived, "legal_analysis", "analysis"},
		{model.ArtifactStatusAnalysisArtifactsReceived, "report_generation", "reports"},
		{model.ArtifactStatusReportsReady, "finalization", "finalization"},
	}

	for _, tc := range cases {
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
			m := newMockMetrics()

			w := newTestWatchdog(&mockTransactor{}, vr, artRepo, &mockAuditRepo{}, ow, m)
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

			// Per-stage metric assertions.
			if m.totalByStage[tc.metricStage] != 1 {
				t.Errorf("expected stuckTotal[%s]=1, got %d", tc.metricStage, m.totalByStage[tc.metricStage])
			}
			if m.gaugeByStage[tc.metricStage] != 1 {
				t.Errorf("expected gauge[%s]=1, got %v", tc.metricStage, m.gaugeByStage[tc.metricStage])
			}
		})
	}
}

func TestScan_PartialFailure_ContinuesProcessing(t *testing.T) {
	v1 := makeVersion("v-1", "doc-1", "org-1", model.ArtifactStatusPending)
	v2 := makeVersion("v-2", "doc-2", "org-1", model.ArtifactStatusPending)

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
	m := newMockMetrics()

	w := newTestWatchdog(&mockTransactor{}, vr, artRepo, auditRepo, ow, m)
	w.scan()

	if m.totalAcrossStages() != 1 {
		t.Errorf("expected stuckTotal=1 (partial), got %d", m.totalAcrossStages())
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

	w := newTestWatchdog(&mockTransactor{}, vr, artRepo, &mockAuditRepo{}, ow, newMockMetrics())
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
	v2 := makeVersion("v-2", "doc-2", "org-1", model.ArtifactStatusProcessingArtifactsReceived)
	vr := &mockVersionRepo{
		findStaleResult: []*model.DocumentVersion{v1, v2},
		findForUpdateResult: map[string]*model.DocumentVersion{
			"v-1": makeVersion("v-1", "doc-1", "org-1", model.ArtifactStatusPending),
			"v-2": makeVersion("v-2", "doc-2", "org-1", model.ArtifactStatusProcessingArtifactsReceived),
		},
	}
	artRepo := &mockArtifactRepo{listResult: map[string][]*model.ArtifactDescriptor{}}
	m := newMockMetrics()

	w := newTestWatchdog(&mockTransactor{}, vr, artRepo, &mockAuditRepo{}, &mockOutboxWriter{}, m)
	w.scan()

	if m.gaugeByStage["processing"] != 1 {
		t.Errorf("expected gauge[processing]=1, got %v", m.gaugeByStage["processing"])
	}
	if m.gaugeByStage["analysis"] != 1 {
		t.Errorf("expected gauge[analysis]=1, got %v", m.gaugeByStage["analysis"])
	}
	if m.gaugeByStage["reports"] != 0 {
		t.Errorf("expected gauge[reports]=0, got %v", m.gaugeByStage["reports"])
	}
	if m.gaugeByStage["finalization"] != 0 {
		t.Errorf("expected gauge[finalization]=0, got %v", m.gaugeByStage["finalization"])
	}
}

func TestScan_GaugeZeroWhenNoStale(t *testing.T) {
	vr := &mockVersionRepo{findStaleResult: []*model.DocumentVersion{}}
	m := newMockMetrics()

	w := newTestWatchdog(&mockTransactor{}, vr, &mockArtifactRepo{}, &mockAuditRepo{}, &mockOutboxWriter{}, m)
	w.scan()

	// All four stage labels must have been set to zero.
	expected := []string{"processing", "analysis", "reports", "finalization"}
	for _, stage := range expected {
		if _, ok := m.gaugeByStage[stage]; !ok {
			t.Errorf("expected SetStuckVersionsCount called for stage %s", stage)
		}
		if m.gaugeByStage[stage] != 0 {
			t.Errorf("expected gauge[%s]=0, got %v", stage, m.gaugeByStage[stage])
		}
	}
}

func TestScan_DBErrorOnFind(t *testing.T) {
	vr := &mockVersionRepo{
		findStaleErr: errors.New("db connection lost"),
	}
	m := newMockMetrics()

	w := newTestWatchdog(&mockTransactor{}, vr, &mockArtifactRepo{}, &mockAuditRepo{}, &mockOutboxWriter{}, m)
	w.scan()

	if m.totalAcrossStages() != 0 {
		t.Errorf("expected stuckTotal=0 on DB error, got %d", m.totalAcrossStages())
	}
	// No gauges should have been set when the query itself failed early.
	if len(m.gaugeSamples) != 0 {
		t.Errorf("expected no gauge samples on DB error, got %d", len(m.gaugeSamples))
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

	w := newTestWatchdog(&mockTransactor{}, vr, artRepo, audit, &mockOutboxWriter{}, newMockMetrics())
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
	if details["stage"] != "reports" {
		t.Errorf("expected stage=reports, got %s", details["stage"])
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

	w := newTestWatchdog(&mockTransactor{}, vr, artRepo, &mockAuditRepo{}, ow, newMockMetrics())
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
	m := newMockMetrics()

	w := newTestWatchdog(&mockTransactor{}, vr, &mockArtifactRepo{}, &mockAuditRepo{}, &mockOutboxWriter{}, m)
	w.scan()

	if m.totalAcrossStages() != 0 {
		t.Errorf("expected stuckTotal=0 on lock error, got %d", m.totalAcrossStages())
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
	m := newMockMetrics()

	w := newTestWatchdog(&mockTransactor{}, vr, &mockArtifactRepo{}, &mockAuditRepo{}, &mockOutboxWriter{}, m)
	w.scan()

	if m.totalAcrossStages() != 0 {
		t.Errorf("expected stuckTotal=0 on update error, got %d", m.totalAcrossStages())
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
	m := newMockMetrics()

	w := newTestWatchdog(&mockTransactor{}, vr, artRepo, &mockAuditRepo{}, ow, m)
	w.scan()

	if m.totalAcrossStages() != 0 {
		t.Errorf("expected stuckTotal=0 on outbox error, got %d", m.totalAcrossStages())
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
	m := newMockMetrics()

	w := newTestWatchdog(&mockTransactor{}, vr, artRepo, &mockAuditRepo{}, &mockOutboxWriter{}, m)
	w.scan()

	if m.totalAcrossStages() != 0 {
		t.Errorf("expected stuckTotal=0 on artifact list error, got %d", m.totalAcrossStages())
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
	m := newMockMetrics()

	w := newTestWatchdog(&mockTransactor{}, vr, artRepo, audit, ow, m)
	w.scan()

	if m.totalAcrossStages() != 3 {
		t.Errorf("expected stuckTotal=3, got %d", m.totalAcrossStages())
	}
	if len(audit.records) != 3 {
		t.Errorf("expected 3 audit records, got %d", len(audit.records))
	}
	if len(ow.writes) != 3 {
		t.Errorf("expected 3 outbox writes, got %d", len(ow.writes))
	}

	// Per-stage totals should reflect one each.
	for _, stage := range []string{"processing", "analysis", "finalization"} {
		if m.totalByStage[stage] != 1 {
			t.Errorf("expected totalByStage[%s]=1, got %d", stage, m.totalByStage[stage])
		}
	}
}

// ---------------------------------------------------------------------------
// Per-stage cutoffs tests (DM-TASK-053)
// ---------------------------------------------------------------------------

func TestScan_PerStageCutoffsPassedToRepo(t *testing.T) {
	vr := &mockVersionRepo{findStaleResult: []*model.DocumentVersion{}}
	cfg := config.WatchdogConfig{
		ScanInterval:             time.Second,
		BatchSize:                100,
		StaleTimeoutProcessing:   5 * time.Minute,
		StaleTimeoutAnalysis:     10 * time.Minute,
		StaleTimeoutReports:      5 * time.Minute,
		StaleTimeoutFinalization: 5 * time.Minute,
	}
	w := NewStaleVersionWatchdog(
		&mockTransactor{}, vr, &mockArtifactRepo{}, &mockAuditRepo{}, &mockOutboxWriter{},
		newMockMetrics(), testLogger(), cfg, testTopic,
	)

	before := time.Now().UTC()
	w.scan()
	after := time.Now().UTC()

	if len(vr.findStaleCutoffs) != 4 {
		t.Fatalf("expected 4 per-stage cutoffs, got %d", len(vr.findStaleCutoffs))
	}

	// The watchdog subtracts the per-stage timeout from "now" to derive each
	// cutoff; verify bounds per status.
	checks := []struct {
		status  model.ArtifactStatus
		timeout time.Duration
	}{
		{model.ArtifactStatusPending, 5 * time.Minute},
		{model.ArtifactStatusProcessingArtifactsReceived, 10 * time.Minute},
		{model.ArtifactStatusAnalysisArtifactsReceived, 5 * time.Minute},
		{model.ArtifactStatusReportsReady, 5 * time.Minute},
	}
	for _, c := range checks {
		got, ok := vr.findStaleCutoffs[c.status]
		if !ok {
			t.Errorf("missing cutoff for status %s", c.status)
			continue
		}
		minCutoff := before.Add(-c.timeout - time.Second)
		maxCutoff := after.Add(-c.timeout + time.Second)
		if got.Before(minCutoff) || got.After(maxCutoff) {
			t.Errorf("cutoff for %s = %v, expected in [%v, %v]", c.status, got, minCutoff, maxCutoff)
		}
	}
}

func TestScan_ZeroTimeout_DisablesStage(t *testing.T) {
	// Only PENDING has a positive timeout; other stages are disabled.
	cfg := config.WatchdogConfig{
		ScanInterval:           time.Second,
		BatchSize:              100,
		StaleTimeoutProcessing: 5 * time.Minute,
	}

	vr := &mockVersionRepo{findStaleResult: []*model.DocumentVersion{}}
	w := NewStaleVersionWatchdog(
		&mockTransactor{}, vr, &mockArtifactRepo{}, &mockAuditRepo{}, &mockOutboxWriter{},
		newMockMetrics(), testLogger(), cfg, testTopic,
	)

	w.scan()

	if len(vr.findStaleCutoffs) != 1 {
		t.Errorf("expected only 1 cutoff (PENDING), got %d", len(vr.findStaleCutoffs))
	}
	if _, ok := vr.findStaleCutoffs[model.ArtifactStatusPending]; !ok {
		t.Error("expected cutoff for PENDING status")
	}
}

func TestScan_PerStageLogDiagnostics(t *testing.T) {
	// Ensures that at least the transition log path is exercised — we do not
	// snapshot log content here (slog output is mocked to discard), but the
	// test verifies the happy path runs without panicking when a transition
	// succeeds. The actual "stage" key is present in the watchdog code
	// and is verified indirectly via the audit details (see
	// TestScan_AuditRecordFields) and per-stage metric labels.
	v := makeVersion("v-1", "doc-1", "org-1", model.ArtifactStatusReportsReady)
	vr := &mockVersionRepo{
		findStaleResult: []*model.DocumentVersion{v},
		findForUpdateResult: map[string]*model.DocumentVersion{
			"v-1": makeVersion("v-1", "doc-1", "org-1", model.ArtifactStatusReportsReady),
		},
	}
	w := newTestWatchdog(&mockTransactor{}, vr, &mockArtifactRepo{}, &mockAuditRepo{}, &mockOutboxWriter{}, newMockMetrics())
	w.scan()
}

// ---------------------------------------------------------------------------
// Start/Stop tests
// ---------------------------------------------------------------------------

func TestStartStop_GracefulShutdown(t *testing.T) {
	vr := &mockVersionRepo{findStaleResult: []*model.DocumentVersion{}}
	w := newTestWatchdog(&mockTransactor{}, vr, &mockArtifactRepo{}, &mockAuditRepo{}, &mockOutboxWriter{}, newMockMetrics())

	w.Start()
	time.Sleep(150 * time.Millisecond)
	w.Stop()

	select {
	case <-w.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog goroutine did not exit within timeout")
	}
}

func TestStop_SafeToCallMultipleTimes(t *testing.T) {
	vr := &mockVersionRepo{findStaleResult: []*model.DocumentVersion{}}
	w := newTestWatchdog(&mockTransactor{}, vr, &mockArtifactRepo{}, &mockAuditRepo{}, &mockOutboxWriter{}, newMockMetrics())

	w.Start()
	w.Stop()
	w.Stop()
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

func TestStageFromStatus(t *testing.T) {
	tests := []struct {
		status model.ArtifactStatus
		want   string
		ok     bool
	}{
		{model.ArtifactStatusPending, "processing", true},
		{model.ArtifactStatusProcessingArtifactsReceived, "analysis", true},
		{model.ArtifactStatusAnalysisArtifactsReceived, "reports", true},
		{model.ArtifactStatusReportsReady, "finalization", true},
		{model.ArtifactStatusFullyReady, "unknown", false},
		{model.ArtifactStatusPartiallyAvailable, "unknown", false},
	}
	for _, tc := range tests {
		t.Run(string(tc.status), func(t *testing.T) {
			got, ok := stageFromStatus(tc.status)
			if got != tc.want || ok != tc.ok {
				t.Errorf("stageFromStatus(%s) = (%s, %v), want (%s, %v)", tc.status, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestAllStagesCoversKnownStatuses(t *testing.T) {
	want := []string{"analysis", "finalization", "processing", "reports"}
	got := append([]string(nil), allStages...)
	sort.Strings(got)
	if len(got) != len(want) {
		t.Fatalf("expected %d stages, got %d", len(want), len(got))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("stage[%d] = %q, want %q", i, got[i], want[i])
		}
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
	m := newMockMetrics()

	w := newTestWatchdog(failingTransactor, vr, artRepo, &mockAuditRepo{}, ow, m)
	w.scan()

	if m.totalAcrossStages() != 1 {
		t.Errorf("expected stuckTotal=1, got %d", m.totalAcrossStages())
	}
}
