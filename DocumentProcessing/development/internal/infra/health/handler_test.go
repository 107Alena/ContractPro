package health

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// helper: issue a GET request to the handler's mux and return the recorder.
func doGet(t *testing.T, h *Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.Mux().ServeHTTP(rec, req)
	return rec
}

// helper: decode the JSON body and return the "status" field value.
func statusField(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode JSON response body: %v", err)
	}
	v, ok := body["status"]
	if !ok {
		t.Fatal("response body missing \"status\" key")
	}
	return v
}

func TestLiveness_AlwaysReturns200(t *testing.T) {
	h := NewHandler()

	rec := doGet(t, h, "/healthz")

	if rec.Code != http.StatusOK {
		t.Fatalf("/healthz: want status 200, got %d", rec.Code)
	}
	if got := statusField(t, rec); got != "ok" {
		t.Fatalf("/healthz: want status field \"ok\", got %q", got)
	}
}

func TestReadiness_NotReadyByDefault(t *testing.T) {
	h := NewHandler()

	rec := doGet(t, h, "/readyz")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz (default): want status 503, got %d", rec.Code)
	}
	if got := statusField(t, rec); got != "not_ready" {
		t.Fatalf("/readyz (default): want status field \"not_ready\", got %q", got)
	}
}

func TestReadiness_ReadyAfterSetReadyTrue(t *testing.T) {
	h := NewHandler()
	h.SetReady(true)

	rec := doGet(t, h, "/readyz")

	if rec.Code != http.StatusOK {
		t.Fatalf("/readyz (ready): want status 200, got %d", rec.Code)
	}
	if got := statusField(t, rec); got != "ready" {
		t.Fatalf("/readyz (ready): want status field \"ready\", got %q", got)
	}
}

func TestReadiness_NotReadyAfterToggleBack(t *testing.T) {
	h := NewHandler()
	h.SetReady(true)
	h.SetReady(false)

	rec := doGet(t, h, "/readyz")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz (toggled back): want status 503, got %d", rec.Code)
	}
	if got := statusField(t, rec); got != "not_ready" {
		t.Fatalf("/readyz (toggled back): want status field \"not_ready\", got %q", got)
	}
}

func TestContentType_IsJSON(t *testing.T) {
	h := NewHandler()

	tests := []struct {
		name string
		path string
	}{
		{"liveness", "/healthz"},
		{"readiness_not_ready", "/readyz"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doGet(t, h, tt.path)
			ct := rec.Header().Get("Content-Type")
			if ct != "application/json" {
				t.Fatalf("%s: want Content-Type \"application/json\", got %q", tt.path, ct)
			}
		})
	}

	// Also verify when ready.
	h.SetReady(true)
	t.Run("readiness_ready", func(t *testing.T) {
		rec := doGet(t, h, "/readyz")
		ct := rec.Header().Get("Content-Type")
		if ct != "application/json" {
			t.Fatalf("/readyz (ready): want Content-Type \"application/json\", got %q", ct)
		}
	})
}
