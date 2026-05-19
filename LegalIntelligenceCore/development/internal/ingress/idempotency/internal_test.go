package idempotency

import (
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// allowedInternal is the EXACT set of first-party packages the Idempotency
// Guard's non-test source may import (build-spec D10). It is an inbound
// infrastructure ADAPTER (it implements a domain port over Redis), exactly
// like internal/ingress/consumer is the broker adapter, so it MAY import
// internal/infra/kvstore — but ONLY for its exported error sentinels/types
// (kvstore.ErrKeyNotFound, *kvstore.RedisError, kvstore.IsRetryable); the
// primitive itself is injected behind the RedisSeam (D7), never
// kvstore.NewClient. The frozen port (IdempotencyStorePort,
// IdempotencyStatus, ErrIdempotencyKeyExists) is the other entry. Config is a
// ctor param (no internal/config — D9/D10); logger/metrics/clock are seams
// (D7); the Guard returns NO model.ErrorCode (R4).
var allowedInternal = map[string]struct{}{
	"contractpro/legal-intelligence-core/internal/domain/port":   {},
	"contractpro/legal-intelligence-core/internal/infra/kvstore": {},
}

// forbiddenInternal is the build-spec D10 active-fail set: packages whose
// presence in allowedInternal (a future "consistency" edit) would silently
// break the hermeticity contract. TestHermeticImports asserts they are NOT in
// the allowlist BEFORE scanning, so a regression fails loudly here rather
// than passing the import scan because someone widened the allowlist. Notably
// internal/domain/model (the Guard returns kvstore/local-typed errors, NEVER
// a model.ErrorCode — R4), internal/config (Config is a ctor param — D9),
// internal/application/* (the Guard's own CONSUMERS — the dependency is
// INVERTED) and internal/ingress/{consumer,router}.
var forbiddenInternal = []string{
	"contractpro/legal-intelligence-core/internal/config",
	"contractpro/legal-intelligence-core/internal/domain/model",
	"contractpro/legal-intelligence-core/internal/infra/observability/logger",
	"contractpro/legal-intelligence-core/internal/infra/observability/metrics",
	"contractpro/legal-intelligence-core/internal/infra/observability/tracer",
	"contractpro/legal-intelligence-core/internal/ingress/consumer",
	"contractpro/legal-intelligence-core/internal/ingress/router",
	"contractpro/legal-intelligence-core/internal/application/pipeline",
	"contractpro/legal-intelligence-core/internal/application/pendingconfirmation",
	"contractpro/legal-intelligence-core/internal/application/aggregator",
	"contractpro/legal-intelligence-core/internal/agents/base",
}

// forbiddenThirdParty is the build-spec D10 active-fail third-party set: the
// Guard permits ZERO third-party (unlike consumer's single google/uuid). The
// generic "contains a dot ⇒ third-party" rule below already rejects any
// third-party; this list pins the specific notable offenders so a reviewer
// sees the intent (go-redis — the kvstore shields it behind RedisSeam;
// prometheus/otel — seamed; miniredis — absent offline, replaced by the
// in-package fakeRedis, D11).
var forbiddenThirdParty = []string{
	"github.com/redis/go-redis/v9",
	"github.com/redis/go-redis",
	"github.com/prometheus/client_golang",
	"go.opentelemetry.io/otel",
	"github.com/alicebob/miniredis",
	"github.com/google/uuid",
}

// TestHermeticImports pins that non-test files import only stdlib + the
// 2-entry {domain/port, infra/kvstore} allowlist with ZERO permitted
// third-party. It ACTIVELY fails if any forbidden internal (notably
// internal/domain/model, internal/config, the concrete logger/metrics, or
// internal/application/* — the Guard's own consumers) ever lands in the
// allowlist, and flags any third-party import (notably
// github.com/redis/go-redis/v9 — the kvstore shields it behind RedisSeam,
// prometheus/otel/miniredis).
func TestHermeticImports(t *testing.T) {
	for _, forbidden := range forbiddenInternal {
		if _, present := allowedInternal[forbidden]; present {
			t.Fatalf("allowedInternal must NOT contain %q — the Idempotency Guard is hermetic (stdlib + domain/port + infra/kvstore error-helpers only); the Redis primitive is the RedisSeam, logger/metrics/clock are seamed, config is a ctor param and it returns NO model.ErrorCode (build-spec D10/R4)", forbidden)
		}
	}
	for _, forbidden := range forbiddenThirdParty {
		if _, present := allowedInternal[forbidden]; present {
			t.Fatalf("allowedInternal must NOT contain third-party %q — the Idempotency Guard permits ZERO third-party imports (build-spec D10)", forbidden)
		}
	}

	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read pkg dir: %v", err)
	}
	checked := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		checked++
		f, perr := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if perr != nil {
			t.Fatalf("parse %s: %v", name, perr)
		}
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if strings.HasPrefix(path, "contractpro/") {
				if _, ok := allowedInternal[path]; !ok {
					t.Errorf("%s imports forbidden internal package %q (hermeticity breach)", name, path)
				}
				continue
			}
			if strings.Contains(path, ".") {
				t.Errorf("%s imports third-party package %q (this package must stay third-party-free — go-redis/prometheus/otel/miniredis included; the kvstore shields go-redis behind RedisSeam)", name, path)
			}
			// else: stdlib — allowed.
		}
	}
	if checked == 0 {
		t.Fatal("no non-test .go files found to check")
	}
}

// TestGofmtClean is a self-check: the sandbox blocks gofmt/`go fmt`, so
// canonical formatting is asserted in-process via go/format over every .go
// file in the package (the consumer/pendingconfirmation TestGofmtClean
// approach — build-spec D14).
func TestGofmtClean(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read pkg dir: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		src, rerr := os.ReadFile(name)
		if rerr != nil {
			t.Fatalf("read %s: %v", name, rerr)
		}
		want, ferr := format.Source(src)
		if ferr != nil {
			t.Fatalf("format %s: %v", name, ferr)
		}
		if string(want) != string(src) {
			t.Errorf("%s is not gofmt-clean", name)
		}
	}
}
