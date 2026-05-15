package ratelimit

import (
	"context"
	"errors"
	"testing"

	"contractpro/legal-intelligence-core/internal/domain/port"
)

func TestTokenBucket_KeyAndArgs(t *testing.T) {
	f := newFakeEvaluator()
	b := NewTokenBucket(port.ProviderClaude, 10, 20, f)

	if b.key != "lic:rate:claude" {
		t.Fatalf("key = %q, want lic:rate:claude (no shard, OQ-F)", b.key)
	}
	if _, _, _ = b.allow(context.Background()); f.callCount() != 1 {
		t.Fatalf("allow must invoke Eval once")
	}
	f.mu.Lock()
	keys, args := f.lastKeys, f.lastArgs
	f.mu.Unlock()
	if len(keys) != 1 || keys[0] != "lic:rate:claude" {
		t.Fatalf("KEYS = %v, want [lic:rate:claude]", keys)
	}
	if len(args) != 3 || args[0] != "10" || args[1] != "20" || args[2] != "1" {
		t.Fatalf("ARGV = %v, want [10 20 1]", args)
	}
}

func TestTokenBucket_AllowedThenDenied(t *testing.T) {
	f := newFakeEvaluator().withVirtualClock(0)
	b := NewTokenBucket(port.ProviderOpenAI, 5, 2, f)

	for i := 0; i < 2; i++ {
		out, _, cause := b.allow(context.Background())
		if out != outcomeAllowed || cause != nil {
			t.Fatalf("burst request %d: out=%v cause=%v, want allowed", i+1, out, cause)
		}
	}
	out, retry, cause := b.allow(context.Background())
	if out != outcomeDenied {
		t.Fatalf("out = %v, want outcomeDenied", out)
	}
	if cause != nil {
		t.Fatalf("denied cause = %v, want nil", cause)
	}
	if retry <= 0 {
		t.Fatalf("denied retryAfter = %v, want > 0", retry)
	}
}

func TestTokenBucket_InfraErrorFailsOpenSignal(t *testing.T) {
	f := newFakeEvaluator()
	f.setForcedErr(errors.New("dial tcp: connection refused"))
	b := NewTokenBucket(port.ProviderGemini, 10, 10, f)

	out, _, cause := b.allow(context.Background())
	if out != outcomeInfraError {
		t.Fatalf("out = %v, want outcomeInfraError", out)
	}
	if cause == nil {
		t.Fatalf("infra outcome must carry the Eval error")
	}
}

func TestTokenBucket_AnomalyOutcomes(t *testing.T) {
	for _, m := range []anomalyMode{anomalyNilReply, anomalyBadShape, anomalyBadElem} {
		f := newFakeEvaluator()
		f.setAnomaly(m)
		b := NewTokenBucket(port.ProviderClaude, 10, 10, f)
		out, _, cause := b.allow(context.Background())
		if out != outcomeScriptAnomaly {
			t.Fatalf("anomaly %v: out = %v, want outcomeScriptAnomaly", m, out)
		}
		if !errors.Is(cause, errScriptAnomaly) {
			t.Fatalf("anomaly %v: cause = %v, want errScriptAnomaly", m, cause)
		}
	}
}

func TestTokenBucket_String(t *testing.T) {
	b := NewTokenBucket(port.ProviderClaude, 10, 20, newFakeEvaluator())
	if b.String() == "" {
		t.Fatalf("String() must be non-empty")
	}
}
