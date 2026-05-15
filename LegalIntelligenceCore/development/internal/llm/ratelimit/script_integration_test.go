//go:build redis_integration

// Package-external-free integration test: runs the REAL token-bucket Lua
// against a live Redis through the production kvstore.Client (the actual
// LuaEvaluator implementation) and asserts it agrees with the Go SSOT
// computeBucket — closing the Lua/Go drift gap that the offline unit tests
// cannot (no Lua VM offline). Disabled by default; enable with:
//
//	LIC_TEST_REDIS_URL=redis://localhost:6379/15 \
//	  go test -tags=redis_integration ./internal/llm/ratelimit/...
//
// It also implicitly re-validates that *kvstore.Client satisfies LuaEvaluator
// (the hermetic package never imports kvstore in production code; the
// compile-time assertion lives in app-wiring, LIC-TASK-047).
package ratelimit

import (
	"context"
	"os"
	"testing"
	"time"

	"contractpro/legal-intelligence-core/internal/config"
	"contractpro/legal-intelligence-core/internal/domain/port"
	"contractpro/legal-intelligence-core/internal/infra/kvstore"
)

var _ LuaEvaluator = (*kvstore.Client)(nil)

func TestIntegration_RealLuaAgreesWithComputeBucket(t *testing.T) {
	url := os.Getenv("LIC_TEST_REDIS_URL")
	if url == "" {
		t.Skip("set LIC_TEST_REDIS_URL to run the live-Redis integration test")
	}
	client, err := kvstore.NewClient(config.RedisConfig{URL: url, DialTimeout: 2 * time.Second})
	if err != nil {
		t.Fatalf("kvstore.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	ctx := context.Background()
	rps, burst := 10.0, 5
	b := NewTokenBucket(port.ProviderClaude, rps, burst, client)
	_, _ = client.Delete(ctx, b.key)
	t.Cleanup(func() { _, _ = client.Delete(ctx, b.key) })

	// Cold bucket: the real script must allow exactly `burst` then deny,
	// matching computeBucket's burst behaviour.
	for i := 0; i < burst; i++ {
		out, _, cause := b.allow(ctx)
		if out != outcomeAllowed || cause != nil {
			t.Fatalf("real Lua burst req %d: out=%v cause=%v", i+1, out, cause)
		}
	}
	out, retry, cause := b.allow(ctx)
	if out != outcomeDenied || cause != nil {
		t.Fatalf("real Lua beyond burst: out=%v cause=%v, want denied", out, cause)
	}

	// Numerically tie the real Lua to the Go SSOT: just-drained, the deficit
	// is at most one full request, so retry-after is at most what
	// computeBucket reports for an exactly-empty bucket with zero elapsed
	// (real elapsed between drain and this call only refills, never inflates,
	// the balance — so the real value must lie in (0, max]). This is what
	// closes the Lua/Go arithmetic-drift gap (code-reviewer M3).
	_, maxRes := computeBucket(&bucketState{tokensMicro: 0}, rps, burst, 1, 0)
	maxRetry := msToDuration(maxRes.retryAfterMS)
	if retry <= 0 || retry > maxRetry {
		t.Fatalf("real Lua retry-after = %v, want in (0, %v] (computeBucket SSOT)", retry, maxRetry)
	}
}
