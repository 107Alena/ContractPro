package fakes

import (
	"testing"

	"contractpro/legal-intelligence-core/internal/domain/port"
	dlqpub "contractpro/legal-intelligence-core/internal/egress/dlq"
	dmpub "contractpro/legal-intelligence-core/internal/egress/publisher/dm"
	orchpub "contractpro/legal-intelligence-core/internal/egress/publisher/orch"
	"contractpro/legal-intelligence-core/internal/ingress/consumer"
	"contractpro/legal-intelligence-core/internal/ingress/idempotency"
	"contractpro/legal-intelligence-core/internal/llm/ratelimit"
)

// These cross-package interface satisfaction checks pin the rig fakes to
// the production seams they MUST be drop-in replacements for. A method
// signature change in either side surfaces here as a compile error.
//
// Conventional `var _ Iface = (*Impl)(nil)` at file scope would force the
// imports to be load-bearing for production compilation; placing the
// assertions inside a _test.go file keeps them _test-only — production
// hermeticity is preserved (the broker import in broker.go is types-only,
// and these test-only imports do not leak into the binary).

var (
	_ consumer.BrokerSubscriber           = (*FakeBroker)(nil)
	_ dmpub.Publisher                     = (*FakeBroker)(nil)
	_ orchpub.Publisher                   = (*FakeBroker)(nil)
	_ dlqpub.Publisher                    = (*FakeBroker)(nil)

	_ idempotency.RedisSeam               = (*FakeKVStore)(nil)
	_ ratelimit.LuaEvaluator              = (*FakeKVStore)(nil)

	_ port.LLMProviderPort                = (*FakeLLMProvider)(nil)
)

// TestSeamsCompile is a placeholder driving the var-_ block above into
// the binary so `go test` reports a clear failure name on signature
// drift. The var assertions themselves are enough; this test body is
// intentionally empty.
func TestSeamsCompile(t *testing.T) {}
