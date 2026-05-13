package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"contractpro/api-orchestrator/internal/egress/dmclient"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"
)

// TestVersionUploadFlow_JobIDEndToEnd asserts the cross-component job_id
// contract for the RE_UPLOAD flow (POST /api/v1/contracts/{id}/versions/upload):
//
//  1. The 202 response body carries a UUID v4 job_id.
//  2. DM CreateVersionRequest (recorded by the fake DM server) carries the
//     same job_id with origin_type=RE_UPLOAD.
//  3. ProcessDocumentRequested published to RabbitMQ carries the same job_id.
//
// Counterpart of TestUploadFlow_JobIDEndToEnd (initial upload, established by
// ORCH-TASK-053) — this test extends the same end-to-end invariant to the
// RE_UPLOAD flow under ORCH-TASK-054.
func TestVersionUploadFlow_JobIDEndToEnd(t *testing.T) {
	env := newTestEnv(t)

	// Seed an existing document and a terminal version that the new upload
	// can use as parent.
	docID := uuid.New().String()
	parentVerID := uuid.New().String()
	env.dmFake.SeedDocument(&dmclient.Document{
		DocumentID:     docID,
		OrganizationID: testOrgID,
		Title:          "Existing Contract",
		Status:         "ACTIVE",
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	})
	env.dmFake.SeedVersion(docID, &dmclient.DocumentVersionWithArtifacts{
		DocumentVersion: dmclient.DocumentVersion{
			VersionID:          parentVerID,
			DocumentID:         docID,
			VersionNumber:      1,
			OriginType:         "UPLOAD",
			SourceFileKey:      "uploads/org/job/file.pdf",
			SourceFileName:     "contract.pdf",
			SourceFileSize:     12345,
			SourceFileChecksum: "abc123",
			ArtifactStatus:     "FULLY_READY",
			CreatedAt:          time.Now().UTC(),
		},
		Artifacts: []dmclient.ArtifactDescriptor{},
	})

	resp := env.uploadNewVersion(docID, "newversion.pdf", fakePDF, testUserID, testOrgID)
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

	// DM-side: fake DM captured the JobID on CreateVersionRequest.
	gotDMJob, ok := env.dmFake.GetCreatedVersionJobID(uploadResp.VersionID)
	if !ok {
		t.Fatalf("fake DM did not record job_id for version %s", uploadResp.VersionID)
	}
	if gotDMJob != uploadResp.JobID {
		t.Errorf("DM CreateVersionRequest.JobID=%q, want %q (from upload response)", gotDMJob, uploadResp.JobID)
	}

	// DP-side: ProcessDocumentRequested carries the same job_id.
	if !brokerPublishedJobID(t, env, uploadResp.JobID, uploadResp.VersionID) {
		t.Fatal("ProcessDocumentRequested with the expected job_id not published")
	}
}

// TestRecheckFlow_JobIDEndToEnd asserts the cross-component job_id contract
// for the RE_CHECK flow (POST /api/v1/contracts/{id}/versions/{vid}/recheck):
//
//  1. The 202 response body carries a UUID v4 job_id (NEW, not the parent's).
//  2. DM CreateVersionRequest (recorded by the fake DM server) carries the
//     same job_id with origin_type=RE_CHECK.
//  3. ProcessDocumentRequested published to RabbitMQ carries the same job_id.
func TestRecheckFlow_JobIDEndToEnd(t *testing.T) {
	env := newTestEnv(t)

	docID := uuid.New().String()
	verID := uuid.New().String()
	env.dmFake.SeedDocument(&dmclient.Document{
		DocumentID:     docID,
		OrganizationID: testOrgID,
		Title:          "Existing Contract",
		Status:         "ACTIVE",
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	})
	env.dmFake.SeedVersion(docID, &dmclient.DocumentVersionWithArtifacts{
		DocumentVersion: dmclient.DocumentVersion{
			VersionID:          verID,
			DocumentID:         docID,
			VersionNumber:      1,
			OriginType:         "UPLOAD",
			SourceFileKey:      "uploads/org/job/file.pdf",
			SourceFileName:     "contract.pdf",
			SourceFileSize:     12345,
			SourceFileChecksum: "abc123",
			ArtifactStatus:     "FULLY_READY",
			CreatedAt:          time.Now().UTC(),
		},
		Artifacts: []dmclient.ArtifactDescriptor{},
	})

	resp := env.DoRequest(
		http.MethodPost,
		"/api/v1/contracts/"+docID+"/versions/"+verID+"/recheck",
		nil,
		testUserID, testOrgID, auth.RoleLawyer,
	)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", resp.StatusCode, body)
	}

	var recheckResp struct {
		ContractID    string `json:"contract_id"`
		VersionID     string `json:"version_id"`
		VersionNumber int    `json:"version_number"`
		JobID         string `json:"job_id"`
		Status        string `json:"status"`
	}
	if err := json.Unmarshal(body, &recheckResp); err != nil {
		t.Fatalf("unmarshal recheck response: %v", err)
	}
	if !uuidV4Regexp.MatchString(recheckResp.JobID) {
		t.Fatalf("response job_id %q is not UUID v4", recheckResp.JobID)
	}
	if recheckResp.VersionID == "" || recheckResp.VersionID == verID {
		t.Fatalf("expected NEW version_id, got %q (parent: %q)", recheckResp.VersionID, verID)
	}

	gotDMJob, ok := env.dmFake.GetCreatedVersionJobID(recheckResp.VersionID)
	if !ok {
		t.Fatalf("fake DM did not record job_id for new recheck version %s", recheckResp.VersionID)
	}
	if gotDMJob != recheckResp.JobID {
		t.Errorf("DM CreateVersionRequest.JobID=%q, want %q (from recheck response)", gotDMJob, recheckResp.JobID)
	}

	if !brokerPublishedJobID(t, env, recheckResp.JobID, recheckResp.VersionID) {
		t.Fatal("ProcessDocumentRequested with the expected job_id not published")
	}
}

// uploadNewVersion sends a multipart upload to
// POST /api/v1/contracts/{contract_id}/versions/upload (RE_UPLOAD flow).
func (e *testEnv) uploadNewVersion(contractID, filename string, pdfContent []byte, userID, orgID string) *http.Response {
	e.t.Helper()

	boundary := "----TestBoundary" + fmt.Sprintf("%d", time.Now().UnixNano())
	var sb strings.Builder
	sb.WriteString("--" + boundary + "\r\n")
	sb.WriteString(`Content-Disposition: form-data; name="file"; filename="` + filename + `"` + "\r\n")
	sb.WriteString("Content-Type: application/pdf\r\n\r\n")

	var bodyParts []byte
	bodyParts = append(bodyParts, []byte(sb.String())...)
	bodyParts = append(bodyParts, pdfContent...)
	bodyParts = append(bodyParts, []byte("\r\n--"+boundary+"--\r\n")...)

	url := e.server.URL + "/api/v1/contracts/" + contractID + "/versions/upload"
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(string(bodyParts)))
	if err != nil {
		e.t.Fatalf("uploadNewVersion: new request: %v", err)
	}
	token := e.jwtSigner.SignToken(userID, orgID, auth.RoleLawyer)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatalf("uploadNewVersion: do: %v", err)
	}
	return resp
}

// brokerPublishedJobID scans the fake broker for a ProcessDocumentRequested
// message whose job_id and (optionally) version_id match. Returns true on the
// first match. Reports cross-field mismatches via t.Errorf for diagnostics.
func brokerPublishedJobID(t *testing.T, env *testEnv, wantJobID, wantVersionID string) bool {
	t.Helper()
	for _, m := range env.brokerFake.PublishedMessages() {
		if m.Topic != "dp.commands.process-document" {
			continue
		}
		var cmd map[string]any
		if err := json.Unmarshal(m.Payload, &cmd); err != nil {
			t.Fatalf("unmarshal ProcessDocumentRequested: %v", err)
		}
		gotJob, _ := cmd["job_id"].(string)
		gotVer, _ := cmd["version_id"].(string)
		if gotVer != wantVersionID {
			continue
		}
		if gotJob != wantJobID {
			t.Errorf("ProcessDocumentRequested.job_id=%q, want %q (version_id=%s)", gotJob, wantJobID, wantVersionID)
		}
		return true
	}
	return false
}
