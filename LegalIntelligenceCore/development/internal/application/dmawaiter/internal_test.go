package dmawaiter

import (
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// allowedInternal is the EXACT set of first-party packages the DM Awaiter's
// non-test source may import (build-spec D17). It depends ONLY on the domain
// model + ports (the cross-domain wire contracts). There is NO
// internal/application/pipeline import (the orchestrator is the CALLER of
// Register/Await/Cancel; the reverse edge would be circular — build-spec D18),
// NO internal/application/pendingconfirmation / internal/application/
// aggregator (orthogonal application-layer siblings), NO internal/ingress/*
// (the Router-side deliverer seam is satisfied structurally and asserted in
// the LIC-TASK-047 wiring package, build-spec D19), NO internal/infra/*
// (Metrics/Clock/Logger seamed away), NO internal/config (local
// ArtifactConfig + ConfirmationConfig, 047-injected — build-spec D8) — the
// universal internal/application/* hermeticity invariant (the
// pendingconfirmation / pipeline / aggregator CLAUDE.md precedent, reviewer
// gate G15: size==2 EXACTLY).
var allowedInternal = map[string]struct{}{
	"contractpro/legal-intelligence-core/internal/domain/model": {},
	"contractpro/legal-intelligence-core/internal/domain/port":  {},
}

// forbiddenInternal is the build-spec D17 active-fail set: packages whose
// presence in allowedInternal (a future "consistency" edit) would silently
// break the hermeticity contract. TestHermeticImports asserts they are NOT
// in the allowlist BEFORE scanning, so a regression fails loudly here rather
// than passing the import scan because someone widened the allowlist.
// Notably internal/application/pipeline: dmawaiter must NEVER import it (the
// reverse edge would be circular — the orchestrator IS the caller).
var forbiddenInternal = []string{
	"contractpro/legal-intelligence-core/internal/config",
	"contractpro/legal-intelligence-core/internal/infra/observability/metrics",
	"contractpro/legal-intelligence-core/internal/infra/observability/tracer",
	"contractpro/legal-intelligence-core/internal/infra/observability/logger",
	"contractpro/legal-intelligence-core/internal/infra/kvstore",
	"contractpro/legal-intelligence-core/internal/infra/broker",
	"contractpro/legal-intelligence-core/internal/application/pipeline",
	"contractpro/legal-intelligence-core/internal/application/pendingconfirmation",
	"contractpro/legal-intelligence-core/internal/application/aggregator",
	"contractpro/legal-intelligence-core/internal/application/pipeline/stages",
	"contractpro/legal-intelligence-core/internal/ingress/consumer",
	"contractpro/legal-intelligence-core/internal/ingress/idempotency",
	"contractpro/legal-intelligence-core/internal/ingress/router",
	"contractpro/legal-intelligence-core/internal/egress/publisher",
	"contractpro/legal-intelligence-core/internal/egress/dlq",
	"contractpro/legal-intelligence-core/internal/agents/base",
	"contractpro/legal-intelligence-core/internal/llm",
}

// TestHermeticImports pins that non-test files import only stdlib + the
// 2-entry {model,port} allowlist (reviewer gate G15: size==2 EXACTLY). It
// ACTIVELY fails if any forbidden internal (notably
// internal/application/pipeline / internal/ingress/router) ever lands in the
// allowlist, and flags any third-party import (notably
// github.com/prometheus/*, go.opentelemetry.io/*, github.com/redis/* — all
// seamed away).
func TestHermeticImports(t *testing.T) {
	if len(allowedInternal) != 2 {
		t.Fatalf("allowedInternal MUST have exactly 2 entries (reviewer gate G15); got %d", len(allowedInternal))
	}
	for _, forbidden := range forbiddenInternal {
		if _, present := allowedInternal[forbidden]; present {
			t.Fatalf("allowedInternal must NOT contain %q — the DM Awaiter is hermetic (stdlib + model/port only); telemetry/clock/logger are seamed, Config is a ctor param, and the router deliverer seam is satisfied structurally (build-spec D17)", forbidden)
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
				t.Errorf("%s imports third-party package %q (this package must stay third-party-free — prometheus/otel/redis included)", name, path)
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
// file in the package (the pipeline/aggregator/stages/pendingconfirmation
// TestGofmtClean approach).
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
