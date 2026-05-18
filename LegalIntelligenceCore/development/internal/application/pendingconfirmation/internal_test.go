package pendingconfirmation

import (
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// allowedInternal is the EXACT set of first-party packages the Pending Type
// Confirmation Manager's non-test source may import (build-spec D17). It
// depends ONLY on the domain model + ports (the cross-domain wire contracts).
// There is NO internal/application/pipeline import (the resumer is the local
// PipelineResumer seam, the paused sentinel flows as Config.PausedSentinel
// error — build-spec D5/D11), NO internal/config (local Config, 047-injected
// — D12), NO internal/infra/* (Metrics/Clock/Logger/TraceRestorer seamed) —
// the universal internal/application/* hermeticity invariant (the pipeline /
// aggregator CLAUDE.md precedent).
var allowedInternal = map[string]struct{}{
	"contractpro/legal-intelligence-core/internal/domain/model": {},
	"contractpro/legal-intelligence-core/internal/domain/port":  {},
}

// forbiddenInternal is the build-spec D17 active-fail set: packages whose
// presence in allowedInternal (a future "consistency" edit) would silently
// break the hermeticity contract. TestHermeticImports asserts they are NOT
// in the allowlist BEFORE scanning, so a regression fails loudly here rather
// than passing the import scan because someone widened the allowlist. Notably
// internal/application/pipeline: pendingconfirmation must NEVER import it.
var forbiddenInternal = []string{
	"contractpro/legal-intelligence-core/internal/config",
	"contractpro/legal-intelligence-core/internal/infra/observability/metrics",
	"contractpro/legal-intelligence-core/internal/infra/observability/tracer",
	"contractpro/legal-intelligence-core/internal/infra/observability/logger",
	"contractpro/legal-intelligence-core/internal/infra/kvstore",
	"contractpro/legal-intelligence-core/internal/infra/broker",
	"contractpro/legal-intelligence-core/internal/application/pipeline",
	"contractpro/legal-intelligence-core/internal/application/aggregator",
	"contractpro/legal-intelligence-core/internal/application/pipeline/stages",
	"contractpro/legal-intelligence-core/internal/egress/publisher",
	"contractpro/legal-intelligence-core/internal/agents/base",
}

// TestHermeticImports pins that non-test files import only stdlib + the
// 2-entry {model,port} allowlist. It ACTIVELY fails if any forbidden internal
// (notably internal/application/pipeline) ever lands in the allowlist, and
// flags any third-party import (notably github.com/prometheus/*,
// go.opentelemetry.io/*, github.com/redis/* — all seamed/ported away).
func TestHermeticImports(t *testing.T) {
	for _, forbidden := range forbiddenInternal {
		if _, present := allowedInternal[forbidden]; present {
			t.Fatalf("allowedInternal must NOT contain %q — the Pending Type Confirmation Manager is hermetic (stdlib + model/port only); the resumer is the local PipelineResumer seam, the paused sentinel is Config.PausedSentinel, telemetry/clock/logger/trace are seamed and config is a ctor param (build-spec D17)", forbidden)
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
// file in the package (the pipeline/aggregator/stages TestGofmtClean
// approach).
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
