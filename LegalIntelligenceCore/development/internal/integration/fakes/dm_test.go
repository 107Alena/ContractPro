package fakes

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

const (
	tcCorrelationID  = "cid"
	tcJobID          = "job-1"
	tcDocumentID     = "doc-1"
	tcVersionID      = "ver-1"
	tcOrganizationID = "org-1"
)

func newDMTestBroker() *FakeBroker {
	return NewFakeBrokerWithLICTopology()
}

func TestFakeDM_DefaultArtifactsResponse_EchoesTypes(t *testing.T) {
	fb := newDMTestBroker()
	dm := NewFakeDM(fb)
	dm.Start()
	defer dm.Stop()

	req := port.GetArtifactsRequest{
		CorrelationID: tcCorrelationID, JobID: tcJobID,
		DocumentID: tcDocumentID, VersionID: tcVersionID,
		ArtifactTypes: []model.ArtifactType{model.ArtifactSemanticTree, model.ArtifactExtractedText},
	}
	body, _ := json.Marshal(req)
	if err := fb.Publish(context.Background(), "ex", RoutingKeyRequestArtifacts, body); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	msg, err := WaitForPublish(ctx, fb, RoutingKeyArtifactsProvided)
	if err != nil {
		t.Fatal(err)
	}
	var resp port.ArtifactsProvided
	if err := json.Unmarshal(msg.Payload, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.VersionID != tcVersionID || resp.CorrelationID != tcCorrelationID {
		t.Fatalf("ids: %+v", resp)
	}
	if _, ok := resp.Artifacts[model.ArtifactSemanticTree]; !ok {
		t.Fatalf("missing SEMANTIC_TREE")
	}
}

func TestFakeDM_PerVersionResponseOverride(t *testing.T) {
	fb := newDMTestBroker()
	dm := NewFakeDM(fb)
	dm.Start()
	defer dm.Stop()

	dm.SetArtifactsResponse(tcVersionID, ArtifactsResponse{
		ErrorCode:    "DM_NOT_FOUND",
		ErrorMessage: "version not in DM",
	})

	req := port.GetArtifactsRequest{
		CorrelationID: tcCorrelationID, JobID: tcJobID,
		DocumentID: tcDocumentID, VersionID: tcVersionID,
		ArtifactTypes: []model.ArtifactType{model.ArtifactSemanticTree},
	}
	body, _ := json.Marshal(req)
	_ = fb.Publish(context.Background(), "ex", RoutingKeyRequestArtifacts, body)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	msg, err := WaitForPublish(ctx, fb, RoutingKeyArtifactsProvided)
	if err != nil {
		t.Fatal(err)
	}
	var resp port.ArtifactsProvided
	_ = json.Unmarshal(msg.Payload, &resp)
	if resp.ErrorCode != "DM_NOT_FOUND" {
		t.Fatalf("error code: %s", resp.ErrorCode)
	}
}

func TestFakeDM_DropResponse_NoPublish(t *testing.T) {
	fb := newDMTestBroker()
	dm := NewFakeDM(fb)
	dm.Start()
	defer dm.Stop()

	dm.SetArtifactsResponse(tcVersionID, ArtifactsResponse{Drop: true})

	req := port.GetArtifactsRequest{
		CorrelationID: tcCorrelationID, JobID: tcJobID,
		DocumentID: tcDocumentID, VersionID: tcVersionID,
		ArtifactTypes: []model.ArtifactType{model.ArtifactSemanticTree},
	}
	body, _ := json.Marshal(req)
	_ = fb.Publish(context.Background(), "ex", RoutingKeyRequestArtifacts, body)

	// Give the listener goroutine time to enter the Drop branch and exit.
	time.Sleep(30 * time.Millisecond)
	if msgs := fb.PublishedOn(RoutingKeyArtifactsProvided); len(msgs) != 0 {
		t.Fatalf("expected no response, got %d", len(msgs))
	}
}

func TestFakeDM_PersistSuccess_DefaultOutcome(t *testing.T) {
	fb := newDMTestBroker()
	dm := NewFakeDM(fb)
	dm.Start()
	defer dm.Stop()

	body := []byte(`{
	  "correlation_id": "cid", "job_id": "job-1", "document_id": "doc-1",
	  "version_id": "ver-1"
	}`)
	_ = fb.Publish(context.Background(), "ex", RoutingKeyAnalysisReady, body)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	msg, err := WaitForPublish(ctx, fb, RoutingKeyPersisted)
	if err != nil {
		t.Fatal(err)
	}
	var evt port.LegalAnalysisArtifactsPersisted
	_ = json.Unmarshal(msg.Payload, &evt)
	if evt.JobID != "job-1" {
		t.Fatalf("job_id: %s", evt.JobID)
	}
}

func TestFakeDM_PersistFailure_PerVersion(t *testing.T) {
	fb := newDMTestBroker()
	dm := NewFakeDM(fb)
	dm.Start()
	defer dm.Stop()

	dm.SetPersistOutcome("ver-1", PersistOutcome{
		Failed:       true,
		ErrorCode:    "DM_PERSIST_FAILED",
		ErrorMessage: "disk full",
		Retryable:    false,
	})

	body := []byte(`{"correlation_id":"c","job_id":"job-1","document_id":"d","version_id":"ver-1"}`)
	_ = fb.Publish(context.Background(), "ex", RoutingKeyAnalysisReady, body)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	msg, err := WaitForPublish(ctx, fb, RoutingKeyPersistFailed)
	if err != nil {
		t.Fatal(err)
	}
	var evt port.LegalAnalysisArtifactsPersistFailed
	_ = json.Unmarshal(msg.Payload, &evt)
	if evt.ErrorCode != "DM_PERSIST_FAILED" {
		t.Fatalf("error code: %s", evt.ErrorCode)
	}
	if evt.IsRetryable {
		t.Fatal("expected non-retryable")
	}
}

func TestFakeDM_ResponseDelay_DrivesTimeoutPath(t *testing.T) {
	fb := newDMTestBroker()
	dm := NewFakeDM(fb)
	dm.Start()
	defer dm.Stop()

	dm.SetResponseDelay(40 * time.Millisecond)

	req := port.GetArtifactsRequest{
		CorrelationID: "c", JobID: "j", DocumentID: "d", VersionID: "v",
		ArtifactTypes: []model.ArtifactType{model.ArtifactSemanticTree},
	}
	body, _ := json.Marshal(req)
	_ = fb.Publish(context.Background(), "ex", RoutingKeyRequestArtifacts, body)

	// The response is delayed 40ms; a 10ms ctx must time out first.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := WaitForPublish(ctx, fb, RoutingKeyArtifactsProvided); err == nil {
		t.Fatal("expected timeout")
	}

	// Now wait long enough.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel2()
	if _, err := WaitForPublish(ctx2, fb, RoutingKeyArtifactsProvided); err != nil {
		t.Fatalf("delayed response never arrived: %v", err)
	}
}

func TestFakeDM_ObservedLogs(t *testing.T) {
	fb := newDMTestBroker()
	dm := NewFakeDM(fb)
	dm.Start()
	defer dm.Stop()

	req := port.GetArtifactsRequest{
		CorrelationID: "c", JobID: "j", DocumentID: "d", VersionID: "v",
		ArtifactTypes: []model.ArtifactType{model.ArtifactSemanticTree, model.ArtifactExtractedText},
	}
	body, _ := json.Marshal(req)
	_ = fb.Publish(context.Background(), "ex", RoutingKeyRequestArtifacts, body)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, _ = WaitForPublish(ctx, fb, RoutingKeyArtifactsProvided)

	reqs := dm.ArtifactRequests()
	if len(reqs) != 1 {
		t.Fatalf("requests=%d", len(reqs))
	}
	if reqs[0].VersionID != "v" || len(reqs[0].ArtifactTypes) != 2 {
		t.Fatalf("captured: %+v", reqs[0])
	}
}

func TestFakeDM_Stop_DrainsInFlight(t *testing.T) {
	fb := newDMTestBroker()
	dm := NewFakeDM(fb)
	dm.Start()
	dm.SetResponseDelay(50 * time.Millisecond)

	req := port.GetArtifactsRequest{
		CorrelationID: "c", JobID: "j", DocumentID: "d", VersionID: "v",
		ArtifactTypes: []model.ArtifactType{model.ArtifactSemanticTree},
	}
	body, _ := json.Marshal(req)
	_ = fb.Publish(context.Background(), "ex", RoutingKeyRequestArtifacts, body)

	dm.Stop()
	// After Stop returns, no further publishes are produced.
	count := len(fb.PublishedOn(RoutingKeyArtifactsProvided))
	time.Sleep(80 * time.Millisecond)
	if got := len(fb.PublishedOn(RoutingKeyArtifactsProvided)); got != count {
		t.Fatalf("publishes increased after Stop: was %d, now %d", count, got)
	}
}
