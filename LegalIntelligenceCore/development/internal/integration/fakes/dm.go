package fakes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// FakeDM simulates Document Management. It hangs a listener off the
// FakeBroker for LIC's two outbound DM wires:
//
//   - lic.requests.artifacts          → respond with ArtifactsProvided
//                                       (or a programmable missing /
//                                       error / timeout outcome).
//   - lic.artifacts.analysis-ready    → respond with
//                                       LegalAnalysisArtifactsPersisted
//                                       (or LegalAnalysisArtifactsPersistFailed).
//
// FakeDM does NOT decode the full LegalAnalysisArtifactsReady payload —
// it only extracts the {job_id, correlation_id, document_id} envelope
// fields the persist confirmation echoes back. Tests asserting payload
// shape do so directly against FakeBroker.PublishedOn().
//
// All responses are published THROUGH FakeBroker.Inject(routingKey,
// headers, body) so the production consumer adapter sees them as a real
// inbound delivery — same code path the real broker takes. Inject is
// synchronous; FakeDM therefore runs the inject from a goroutine spawned
// by the OnPublish callback, so the producing goroutine is not blocked
// by the consumer handler chain.
type FakeDM struct {
	broker *FakeBroker

	mu sync.Mutex

	// Per-version artifact response. Lookup key = versionID.
	artifactsResponses map[string]ArtifactsResponse

	// Default artifact response when no per-version override is set.
	defaultArtifacts ArtifactsResponse
	defaultSet       bool

	// Per-version persist outcome. Lookup key = versionID. If no
	// override is registered, persistDefault is used.
	persistOutcomes map[string]PersistOutcome
	persistDefault  PersistOutcome
	persistSet      bool

	// Per-call response delay. Zero ⇒ respond ASAP.
	responseDelay time.Duration

	// Observed requests (decoded envelopes). Tests assert these for
	// "the orchestrator asked for the right artifact types".
	artifactRequests []ObservedArtifactRequest
	analysisReady    []ObservedAnalysisReady

	// Lifecycle: cancelled by Stop() so the response goroutines exit.
	ctx    context.Context
	cancel context.CancelFunc

	// Counts in-flight response goroutines so Stop can wait for them.
	wg sync.WaitGroup
}

// ArtifactsResponse is the programmable response to a GetArtifactsRequest.
//
// Mutually-exclusive shape:
//   - Success: Artifacts non-empty, MissingTypes empty, ErrorCode "".
//   - Partial: Artifacts plus a non-empty MissingTypes.
//   - Error:   ErrorCode + ErrorMessage; Artifacts may be empty.
//   - Drop:    Drop=true (no response published — drives the
//              ArtifactsAwaiterPort's TTL timeout path).
type ArtifactsResponse struct {
	Artifacts    map[model.ArtifactType]json.RawMessage
	MissingTypes []model.ArtifactType
	ErrorCode    string
	ErrorMessage string
	Drop         bool
}

// PersistOutcome is the programmable response to LegalAnalysisArtifactsReady.
//
// Mutually-exclusive shape:
//   - Success: Failed=false (default) ⇒ Persisted.
//   - Failure: Failed=true + ErrorCode + ErrorMessage + Retryable
//              ⇒ PersistFailed.
//   - Drop:    Drop=true (no response published — drives the
//              PersistConfirmationAwaiterPort's TTL timeout path).
type PersistOutcome struct {
	Failed       bool
	ErrorCode    string
	ErrorMessage string
	Retryable    bool
	Drop         bool
}

// ObservedArtifactRequest is one decoded GetArtifactsRequest envelope.
// Tests assert the orchestrator asked for the right types per version.
type ObservedArtifactRequest struct {
	CorrelationID  string
	JobID          string
	DocumentID     string
	VersionID      string
	OrganizationID string
	ArtifactTypes  []model.ArtifactType
	At             time.Time
}

// ObservedAnalysisReady is one decoded LegalAnalysisArtifactsReady envelope
// (only the IDs — not the eight artifact payloads).
type ObservedAnalysisReady struct {
	CorrelationID  string
	JobID          string
	DocumentID     string
	VersionID      string
	OrganizationID string
	At             time.Time
}

// NewFakeDM wires a FakeDM onto the given FakeBroker. The broker MUST
// be pre-bound with the LIC topology (otherwise the published responses
// wouldn't reach any subscriber). Start() begins listening.
func NewFakeDM(fb *FakeBroker) *FakeDM {
	ctx, cancel := context.WithCancel(context.Background())
	return &FakeDM{
		broker:             fb,
		artifactsResponses: make(map[string]ArtifactsResponse),
		persistOutcomes:    make(map[string]PersistOutcome),
		ctx:                ctx,
		cancel:             cancel,
	}
}

// Start wires the OnPublish listener. After Start, every LIC publish on
// lic.requests.artifacts or lic.artifacts.analysis-ready triggers a
// programmable response.
func (d *FakeDM) Start() {
	d.broker.OnPublish(RoutingKeyRequestArtifacts, d.onArtifactRequest)
	d.broker.OnPublish(RoutingKeyAnalysisReady, d.onAnalysisReady)
}

// Stop cancels the context and waits for in-flight responses. Idempotent.
func (d *FakeDM) Stop() {
	d.cancel()
	d.wg.Wait()
}

// SetArtifactsResponse installs the response for versionID. Subsequent
// SetArtifactsResponse for the same versionID replaces.
func (d *FakeDM) SetArtifactsResponse(versionID string, r ArtifactsResponse) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.artifactsResponses[versionID] = r
}

// SetDefaultArtifactsResponse installs the response for versions without
// a per-version override.
func (d *FakeDM) SetDefaultArtifactsResponse(r ArtifactsResponse) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.defaultArtifacts = r
	d.defaultSet = true
}

// SetPersistOutcome installs the persist response for versionID.
func (d *FakeDM) SetPersistOutcome(versionID string, p PersistOutcome) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.persistOutcomes[versionID] = p
}

// SetDefaultPersistOutcome installs the persist response for versions
// without a per-version override.
func (d *FakeDM) SetDefaultPersistOutcome(p PersistOutcome) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.persistDefault = p
	d.persistSet = true
}

// SetResponseDelay configures a delay applied before each response is
// published. Drives the DM-await TTL paths from tests without modifying
// the orchestrator's deadlines.
func (d *FakeDM) SetResponseDelay(delay time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.responseDelay = delay
}

// ArtifactRequests returns the decoded artifact-request log.
func (d *FakeDM) ArtifactRequests() []ObservedArtifactRequest {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]ObservedArtifactRequest, len(d.artifactRequests))
	copy(out, d.artifactRequests)
	return out
}

// AnalysisReady returns the decoded analysis-ready log.
func (d *FakeDM) AnalysisReady() []ObservedAnalysisReady {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]ObservedAnalysisReady, len(d.analysisReady))
	copy(out, d.analysisReady)
	return out
}

// onArtifactRequest is the OnPublish callback for lic.requests.artifacts.
// It decodes the envelope, records it, looks up the programmed response,
// and (in a goroutine) publishes the ArtifactsProvided reply through the
// broker.
func (d *FakeDM) onArtifactRequest(msg PublishedMessage) {
	var req port.GetArtifactsRequest
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		// Decode failures are silently ignored — production publishers
		// always produce valid JSON; an integration test exercising
		// malformed-publish paths would assert the LIC-side outcome via
		// FakeBroker.InjectPublishError, not by polluting the DM mock.
		return
	}

	d.mu.Lock()
	d.artifactRequests = append(d.artifactRequests, ObservedArtifactRequest{
		CorrelationID:  req.CorrelationID,
		JobID:          req.JobID,
		DocumentID:     req.DocumentID,
		VersionID:      req.VersionID,
		OrganizationID: req.OrganizationID,
		ArtifactTypes:  append([]model.ArtifactType(nil), req.ArtifactTypes...),
		At:             time.Now(),
	})

	r, ok := d.artifactsResponses[req.VersionID]
	if !ok {
		if d.defaultSet {
			r = d.defaultArtifacts
		} else {
			// Default: echo back every requested type as a non-empty
			// dummy blob — the integration test's downstream code is
			// expected to override per-version when realistic content
			// matters (use BuildArtifactsResponse from fixtures.go).
			r = autoArtifactsResponse(req.ArtifactTypes)
		}
	}
	delay := d.responseDelay
	d.mu.Unlock()

	if r.Drop {
		return
	}

	d.wg.Add(1)
	go d.publishArtifactsProvided(req, r, delay)
}

// publishArtifactsProvided publishes the typed response back through
// FakeBroker.Inject. Runs in its own goroutine.
func (d *FakeDM) publishArtifactsProvided(req port.GetArtifactsRequest, r ArtifactsResponse, delay time.Duration) {
	defer d.wg.Done()
	if delay > 0 {
		select {
		case <-d.ctx.Done():
			return
		case <-time.After(delay):
		}
	}
	if d.ctx.Err() != nil {
		return
	}

	evt := port.ArtifactsProvided{
		CorrelationID: req.CorrelationID,
		Timestamp:     time.Now().UTC().Format(time.RFC3339Nano),
		JobID:         req.JobID,
		DocumentID:    req.DocumentID,
		VersionID:     req.VersionID,
		Artifacts:     r.Artifacts,
		MissingTypes:  r.MissingTypes,
		ErrorCode:     r.ErrorCode,
		ErrorMessage:  r.ErrorMessage,
	}
	body, err := json.Marshal(evt)
	if err != nil {
		// Defensive — typed envelope cannot fail to marshal.
		return
	}
	_, _ = d.broker.Inject(d.ctx, RoutingKeyArtifactsProvided, nil, body)
}

// onAnalysisReady is the OnPublish callback for
// lic.artifacts.analysis-ready. It decodes only the envelope IDs, records
// the event, looks up the programmed outcome, and (in a goroutine)
// publishes either Persisted or PersistFailed back through the broker.
func (d *FakeDM) onAnalysisReady(msg PublishedMessage) {
	var probe struct {
		CorrelationID  string `json:"correlation_id"`
		JobID          string `json:"job_id"`
		DocumentID     string `json:"document_id"`
		VersionID      string `json:"version_id"`
		OrganizationID string `json:"organization_id"`
	}
	if err := json.Unmarshal(msg.Payload, &probe); err != nil {
		return
	}

	d.mu.Lock()
	d.analysisReady = append(d.analysisReady, ObservedAnalysisReady{
		CorrelationID:  probe.CorrelationID,
		JobID:          probe.JobID,
		DocumentID:     probe.DocumentID,
		VersionID:      probe.VersionID,
		OrganizationID: probe.OrganizationID,
		At:             time.Now(),
	})

	p, ok := d.persistOutcomes[probe.VersionID]
	if !ok {
		if d.persistSet {
			p = d.persistDefault
		} else {
			// Default: success.
			p = PersistOutcome{}
		}
	}
	delay := d.responseDelay
	d.mu.Unlock()

	if p.Drop {
		return
	}

	d.wg.Add(1)
	go d.publishPersistOutcome(probe.CorrelationID, probe.JobID, probe.DocumentID, p, delay)
}

func (d *FakeDM) publishPersistOutcome(correlationID, jobID, documentID string, p PersistOutcome, delay time.Duration) {
	defer d.wg.Done()
	if delay > 0 {
		select {
		case <-d.ctx.Done():
			return
		case <-time.After(delay):
		}
	}
	if d.ctx.Err() != nil {
		return
	}
	if p.Failed {
		evt := port.LegalAnalysisArtifactsPersistFailed{
			CorrelationID: correlationID,
			Timestamp:     time.Now().UTC().Format(time.RFC3339Nano),
			JobID:         jobID,
			DocumentID:    documentID,
			ErrorCode:     p.ErrorCode,
			ErrorMessage:  p.ErrorMessage,
			IsRetryable:   p.Retryable,
		}
		body, err := json.Marshal(evt)
		if err != nil {
			return
		}
		_, _ = d.broker.Inject(d.ctx, RoutingKeyPersistFailed, nil, body)
		return
	}
	evt := port.LegalAnalysisArtifactsPersisted{
		CorrelationID: correlationID,
		Timestamp:     time.Now().UTC().Format(time.RFC3339Nano),
		JobID:         jobID,
		DocumentID:    documentID,
	}
	body, err := json.Marshal(evt)
	if err != nil {
		return
	}
	_, _ = d.broker.Inject(d.ctx, RoutingKeyPersisted, nil, body)
}

// autoArtifactsResponse builds a placeholder response that echoes back
// the requested types with minimal valid blobs. Tests that need realistic
// content install per-version overrides via SetArtifactsResponse.
func autoArtifactsResponse(types []model.ArtifactType) ArtifactsResponse {
	out := ArtifactsResponse{Artifacts: make(map[model.ArtifactType]json.RawMessage, len(types))}
	for _, t := range types {
		out.Artifacts[t] = json.RawMessage(autoBlobFor(t))
	}
	return out
}

func autoBlobFor(t model.ArtifactType) string {
	switch t {
	case model.ArtifactSemanticTree:
		return `{"root":{"id":"root","node_type":"DOCUMENT","children":[]}}`
	case model.ArtifactExtractedText:
		return `{"text":""}`
	case model.ArtifactDocumentStructure:
		return `{"sections":[],"clauses":[],"appendices":[],"party_details":[]}`
	case model.ArtifactProcessingWarnings:
		return `[]`
	case model.ArtifactRiskAnalysis:
		return `{"risks":[]}`
	default:
		return `{}`
	}
}

// ErrFakeDMNotStarted is returned if a test tries to inspect responses
// before calling Start. Reserved for future use.
var ErrFakeDMNotStarted = errors.New("fakes: FakeDM not started")

// String returns an identifier for tests.
func (d *FakeDM) String() string {
	return fmt.Sprintf("FakeDM{broker=%v}", d.broker)
}
