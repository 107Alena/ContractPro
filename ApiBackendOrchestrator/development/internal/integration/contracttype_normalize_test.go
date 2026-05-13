package integration

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"contractpro/api-orchestrator/internal/ingress/middleware/auth"
)

// seedAwaitingForConfirmType primes the test environment so that a
// POST /confirm-type call reaches the publish step without going through the
// full classification-uncertain → AWAITING_USER_INPUT broker path.
func seedAwaitingForConfirmType(t *testing.T, env *testEnv, docID, verID, jobID string) {
	t.Helper()
	env.SeedStatus(testOrgID, docID, verID, "AWAITING_USER_INPUT")
	meta := map[string]string{
		"organization_id": testOrgID,
		"document_id":     docID,
		"version_id":      verID,
		"job_id":          jobID,
	}
	metaJSON, _ := json.Marshal(meta)
	env.kvStore.SetDirect("confirmation:meta:"+verID, string(metaJSON))
}

func confirmTypeBody(t *testing.T, contractType string) []byte {
	t.Helper()
	return mustJSON(t, map[string]any{
		"contract_type":     contractType,
		"confirmed_by_user": true,
	})
}

func publishedConfirmTypePayload(t *testing.T, env *testEnv) (map[string]any, bool) {
	t.Helper()
	for _, m := range env.brokerFake.PublishedMessages() {
		if m.Topic != "orch.commands.user-confirmed-type" {
			continue
		}
		var cmd map[string]any
		if err := json.Unmarshal(m.Payload, &cmd); err != nil {
			t.Fatalf("unmarshal published UserConfirmedType: %v", err)
		}
		return cmd, true
	}
	return nil, false
}

// TestConfirmType_NormalizesAllRussianLabels covers the full RU→EN mapping
// (ASSUMPTION-LIC-16) end-to-end through the HTTP handler and asserts the
// published UserConfirmedType command always carries the EN enum.
func TestConfirmType_NormalizesAllRussianLabels(t *testing.T) {
	cases := map[string]string{
		"услуги":         "SERVICES",
		"поставка":       "SUPPLY",
		"подряд":         "WORK_CONTRACT",
		"аренда":         "LEASE",
		"NDA":            "NDA",
		"купля-продажа":  "SALE",
		"лицензия":       "LICENSE",
		"агентский":      "AGENCY",
		"займ":           "LOAN",
		"страхование":    "INSURANCE",
		"трудовой":       "EMPLOYMENT_CIVIL",
		"иное":           "OTHER",
	}
	for ru, en := range cases {
		ru, en := ru, en
		t.Run(ru, func(t *testing.T) {
			env := newTestEnv(t)
			docID := uuid.New().String()
			verID := uuid.New().String()
			jobID := uuid.New().String()
			seedAwaitingForConfirmType(t, env, docID, verID, jobID)

			resp := env.DoRequest(
				http.MethodPost,
				"/api/v1/contracts/"+docID+"/versions/"+verID+"/confirm-type",
				jsonReader(confirmTypeBody(t, ru)),
				testUserID, testOrgID, auth.RoleLawyer,
			)
			body := readBody(t, resp)
			if resp.StatusCode != http.StatusAccepted {
				t.Fatalf("POST /confirm-type for %q: expected 202, got %d: %s", ru, resp.StatusCode, body)
			}

			cmd, ok := publishedConfirmTypePayload(t, env)
			if !ok {
				t.Fatalf("UserConfirmedType not published for input %q", ru)
			}
			if cmd["contract_type"] != en {
				t.Errorf("published contract_type for input %q = %v, want %q",
					ru, cmd["contract_type"], en)
			}
		})
	}
}

// TestConfirmType_NormalizesCapitalizedRussian verifies the case-insensitive
// RU → EN path end-to-end (not just at the unit level), confirming no HTTP
// layer along the way mutates the input before it reaches NormalizeContractType.
func TestConfirmType_NormalizesCapitalizedRussian(t *testing.T) {
	env := newTestEnv(t)
	docID := uuid.New().String()
	verID := uuid.New().String()
	jobID := uuid.New().String()
	seedAwaitingForConfirmType(t, env, docID, verID, jobID)

	resp := env.DoRequest(
		http.MethodPost,
		"/api/v1/contracts/"+docID+"/versions/"+verID+"/confirm-type",
		jsonReader(confirmTypeBody(t, "Услуги")),
		testUserID, testOrgID, auth.RoleLawyer,
	)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", resp.StatusCode, body)
	}

	cmd, ok := publishedConfirmTypePayload(t, env)
	if !ok {
		t.Fatal("UserConfirmedType not published for capitalized RU input")
	}
	if cmd["contract_type"] != "SERVICES" {
		t.Errorf("published contract_type = %v, want SERVICES (case-insensitive RU 'Услуги')", cmd["contract_type"])
	}
}

// TestConfirmType_PassesThroughEnglishEnum ensures API clients that already
// send the EN enum bypass normalization unchanged (backward compatibility
// for non-UI consumers).
func TestConfirmType_PassesThroughEnglishEnum(t *testing.T) {
	env := newTestEnv(t)
	docID := uuid.New().String()
	verID := uuid.New().String()
	jobID := uuid.New().String()
	seedAwaitingForConfirmType(t, env, docID, verID, jobID)

	resp := env.DoRequest(
		http.MethodPost,
		"/api/v1/contracts/"+docID+"/versions/"+verID+"/confirm-type",
		jsonReader(confirmTypeBody(t, "EMPLOYMENT_CIVIL")),
		testUserID, testOrgID, auth.RoleLawyer,
	)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", resp.StatusCode, body)
	}

	cmd, ok := publishedConfirmTypePayload(t, env)
	if !ok {
		t.Fatal("UserConfirmedType not published for English enum input")
	}
	if cmd["contract_type"] != "EMPLOYMENT_CIVIL" {
		t.Errorf("published contract_type = %v, want EMPLOYMENT_CIVIL (pass-through)", cmd["contract_type"])
	}
}

// TestConfirmType_RejectsInvalid asserts that unknown labels yield a 400
// with error_code INVALID_CONTRACT_TYPE and a Russian message, and that no
// command is published.
func TestConfirmType_RejectsInvalid(t *testing.T) {
	env := newTestEnv(t)
	docID := uuid.New().String()
	verID := uuid.New().String()
	jobID := uuid.New().String()
	seedAwaitingForConfirmType(t, env, docID, verID, jobID)

	resp := env.DoRequest(
		http.MethodPost,
		"/api/v1/contracts/"+docID+"/versions/"+verID+"/confirm-type",
		jsonReader(confirmTypeBody(t, "абракадабра")),
		testUserID, testOrgID, auth.RoleLawyer,
	)
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, body)
	}

	var errResp struct {
		ErrorCode string `json:"error_code"`
		Message   string `json:"message"`
	}
	if err := json.Unmarshal(body, &errResp); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if errResp.ErrorCode != "INVALID_CONTRACT_TYPE" {
		t.Errorf("error_code = %q, want INVALID_CONTRACT_TYPE", errResp.ErrorCode)
	}
	if errResp.Message == "" {
		t.Error("error message is empty; expected Russian text per NFR-5.2")
	}

	if _, ok := publishedConfirmTypePayload(t, env); ok {
		t.Error("UserConfirmedType must NOT be published when contract_type is invalid")
	}
}
