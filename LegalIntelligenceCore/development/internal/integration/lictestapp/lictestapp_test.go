package lictestapp

import "testing"

// TestNewTestApp_Compiles is a smoke test: build the full production
// stack over the fakes and assert every exposed handle is non-nil. The
// real end-to-end pipeline assertions live in
// internal/integration/happypath / internal/integration/promptinjection
// (LIC-TASK-049 / 053 / forthcoming 050 / 051 / 054).
func TestNewTestApp_Compiles(t *testing.T) {
	app := NewTestApp(t)
	if app == nil {
		t.Fatal("NewTestApp returned nil")
	}
	if app.Broker == nil {
		t.Error("TestApp.Broker is nil")
	}
	if app.KV == nil {
		t.Error("TestApp.KV is nil")
	}
	if app.DM == nil {
		t.Error("TestApp.DM is nil")
	}
	if app.LLM == nil || len(app.LLM) == 0 {
		t.Error("TestApp.LLM is nil/empty")
	}
	if app.Consumer == nil {
		t.Error("TestApp.Consumer is nil")
	}
	if app.Orchestrator == nil {
		t.Error("TestApp.Orchestrator is nil")
	}
	if app.Manager == nil {
		t.Error("TestApp.Manager is nil")
	}
	if app.Router == nil {
		t.Error("TestApp.Router is nil")
	}
}
