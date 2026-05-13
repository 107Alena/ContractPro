package integration

import (
	"encoding/json"
	"net/http"
	"regexp"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// uuidV4Regexp matches the lowercase canonical UUID v4 shape.
var uuidV4Regexp = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// TestUploadFlow_JobIDEndToEnd asserts the cross-component job_id contract
// established by ORCH-TASK-053:
//
//  1. The 202 response body carries a UUID v4 job_id.
//  2. DM CreateVersionRequest (recorded by the fake DM server) was called with
//     the same job_id.
//  3. ProcessDocumentRequested published to RabbitMQ carries the same job_id.
//
// This is the wire-level guarantee that DP can later trust DM to have
// persisted the same job_id under the same version_id (DM-TASK-056).
func TestUploadFlow_JobIDEndToEnd(t *testing.T) {
	env := newTestEnv(t)

	resp := env.UploadContract("Контракт под job_id", fakePDF, testUserID, testOrgID)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", resp.StatusCode, body)
	}

	var uploadResp struct {
		ContractID    string `json:"contract_id"`
		VersionID     string `json:"version_id"`
		VersionNumber int    `json:"version_number"`
		JobID         string `json:"job_id"`
		Status        string `json:"status"`
	}
	if err := json.Unmarshal(body, &uploadResp); err != nil {
		t.Fatalf("unmarshal upload response: %v", err)
	}

	if !uuidV4Regexp.MatchString(uploadResp.JobID) {
		t.Fatalf("response job_id %q is not UUID v4", uploadResp.JobID)
	}

	// 1. DM-side: fake DM captured the JobID on CreateVersionRequest.
	gotDMJob, ok := env.dmFake.GetCreatedVersionJobID(uploadResp.VersionID)
	if !ok {
		t.Fatalf("fake DM did not record job_id for version %s", uploadResp.VersionID)
	}
	if gotDMJob != uploadResp.JobID {
		t.Errorf("DM CreateVersionRequest.JobID=%q, want %q (from upload response)", gotDMJob, uploadResp.JobID)
	}

	// 2. DP-side: ProcessDocumentRequested in the broker carries the same job_id.
	msgs := env.brokerFake.PublishedMessages()
	var found bool
	for _, m := range msgs {
		if m.Topic != "dp.commands.process-document" {
			continue
		}
		var cmd map[string]any
		if err := json.Unmarshal(m.Payload, &cmd); err != nil {
			t.Fatalf("unmarshal ProcessDocumentRequested: %v", err)
		}
		gotJob, _ := cmd["job_id"].(string)
		if gotJob != uploadResp.JobID {
			t.Errorf("ProcessDocumentRequested.job_id=%q, want %q", gotJob, uploadResp.JobID)
		}
		found = true
		break
	}
	if !found {
		t.Fatal("ProcessDocumentRequested not published")
	}
}

// TestUploadFlow_JobIDUnique_Concurrent runs 5 parallel uploads through a
// single Orchestrator instance and asserts every upload produced a distinct
// UUID v4 job_id. Detects accidental sharing of state and confirms that
// jobid.NewJobID() is safe for concurrent use from the upload handler.
func TestUploadFlow_JobIDUnique_Concurrent(t *testing.T) {
	const N = 5

	env := newTestEnv(t)

	type result struct {
		jobID  string
		status int
		body   []byte
	}
	results := make([]result, N)

	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(idx int) {
			defer wg.Done()
			// Distinct user IDs per goroutine — orgID shared by design (tenant).
			uid := uuid.NewString()
			resp := env.UploadContract("Концурент-аплоад", fakePDF, uid, testOrgID)
			body := readBody(t, resp)
			results[idx] = result{status: resp.StatusCode, body: body}
			if resp.StatusCode != http.StatusAccepted {
				return
			}
			var uploadResp struct {
				JobID string `json:"job_id"`
			}
			if err := json.Unmarshal(body, &uploadResp); err != nil {
				t.Errorf("upload %d: decode: %v", idx, err)
				return
			}
			results[idx].jobID = uploadResp.JobID
		}(i)
	}
	wg.Wait()

	seen := make(map[string]struct{}, N)
	for i, r := range results {
		if r.status != http.StatusAccepted {
			t.Fatalf("upload %d: expected 202, got %d: %s", i, r.status, r.body)
		}
		if !uuidV4Regexp.MatchString(r.jobID) {
			t.Fatalf("upload %d: job_id %q is not UUID v4", i, r.jobID)
		}
		if _, dup := seen[r.jobID]; dup {
			t.Fatalf("upload %d: duplicate job_id %q across concurrent uploads", i, r.jobID)
		}
		seen[r.jobID] = struct{}{}
	}
	if len(seen) != N {
		t.Fatalf("expected %d unique job_ids, got %d", N, len(seen))
	}

	// All N ProcessDocumentRequested commands must also carry the matching
	// job_ids. Build the published-set and verify subset equality.
	publishedJobs := make(map[string]struct{})
	for _, m := range env.brokerFake.PublishedMessages() {
		if m.Topic != "dp.commands.process-document" {
			continue
		}
		var cmd map[string]any
		if err := json.Unmarshal(m.Payload, &cmd); err != nil {
			t.Fatalf("unmarshal ProcessDocumentRequested: %v", err)
		}
		if jid, ok := cmd["job_id"].(string); ok {
			publishedJobs[jid] = struct{}{}
		}
	}
	for jid := range seen {
		if _, ok := publishedJobs[jid]; !ok {
			t.Errorf("job_id %q from upload response not found in published commands", jid)
		}
	}
}
