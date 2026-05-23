package fakes

import (
	"testing"

	"contractpro/legal-intelligence-core/internal/domain/model"
	"contractpro/legal-intelligence-core/internal/domain/port"
)

// TestRig bundles the four collaborators every higher-level integration
// test composes:
//
//   - Broker      — *FakeBroker with the §6.1 LIC topology preset.
//   - KV          — *FakeKVStore (idempotency.RedisSeam + ratelimit.LuaEvaluator).
//   - LLMByID     — *FakeLLMProvider per LLMProviderID (Claude, OpenAI, Gemini).
//                   Tests pick which provider an agent routes to via the LLM
//                   router config; the rig pre-populates all three.
//   - DM          — *FakeDM, already Start()-ed and listening on the broker.
//
// NewTestRig(t) returns a ready-to-use rig and registers t.Cleanup to
// Stop FakeDM and Close FakeKVStore / FakeBroker — no per-test boilerplate.
//
// This is the LIC-TASK-048-scope replacement for "BuildTestApp": instead
// of mirroring app.New()'s 16-step concrete wiring with fakes (which would
// require refactoring app.New into factory-injected form — out of 048's
// scope, see doc.go BuildTestApp deferral), the rig bundles the four
// fakes that the production orchestrator / router / agents / awaiters
// already consume via seams. Each integration test composes the
// production types it needs (orchestrator, consumer, idempotency.Guard,
// etc.) using the rig fakes — see LIC-TASK-049 / 050 / 051 for examples.
type TestRig struct {
	Broker  *FakeBroker
	KV      *FakeKVStore
	LLMByID map[port.LLMProviderID]*FakeLLMProvider
	DM      *FakeDM
}

// NewTestRig builds a TestRig wired with:
//   - FakeBroker with the §6.1 LIC topology preset.
//   - FakeKVStore (empty, real-time TTL).
//   - One FakeLLMProvider for each of {ProviderClaude, ProviderOpenAI,
//     ProviderGemini}.
//   - FakeDM listening on the broker.
//
// Registers cleanup so the rig tears itself down when t finishes.
func NewTestRig(t *testing.T) *TestRig {
	t.Helper()
	fb := NewFakeBrokerWithLICTopology()
	kv := NewFakeKVStore()
	llm := map[port.LLMProviderID]*FakeLLMProvider{
		port.ProviderClaude: NewFakeLLMProvider(port.ProviderClaude),
		port.ProviderOpenAI: NewFakeLLMProvider(port.ProviderOpenAI),
		port.ProviderGemini: NewFakeLLMProvider(port.ProviderGemini),
	}
	dm := NewFakeDM(fb)
	dm.Start()

	rig := &TestRig{Broker: fb, KV: kv, LLMByID: llm, DM: dm}

	t.Cleanup(func() {
		dm.Stop()
		_ = fb.Close()
		_ = kv.Close()
	})
	return rig
}

// InstallCannedAgentResponses installs CannedResponseFor(agent) on the
// provider that the test wires for that agent. providerByAgent picks
// which provider serves which agent; modelByAgent picks the model id.
//
// Pass model.AllAgentIDs() or a subset depending on the test (INITIAL
// pipeline doesn't run AGENT_RISK_DELTA; RE_CHECK does).
func (r *TestRig) InstallCannedAgentResponses(providerByAgent map[model.AgentID]port.LLMProviderID, modelByAgent map[model.AgentID]string, agents []model.AgentID) {
	for _, a := range agents {
		provID, ok := providerByAgent[a]
		if !ok {
			continue
		}
		mod, ok := modelByAgent[a]
		if !ok {
			continue
		}
		p, ok := r.LLMByID[provID]
		if !ok {
			continue
		}
		p.SetResponseJSON(a, mod, CannedResponseFor(a))
	}
}
