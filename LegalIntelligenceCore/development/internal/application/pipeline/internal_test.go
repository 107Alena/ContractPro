package pipeline

import (
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// allowedInternal is the EXACT set of first-party packages the Pipeline
// Orchestrator's non-test source may import (build-spec §7 allowlist). The
// Orchestrator depends on the domain model + ports (the cross-domain wire
// contracts) and the two concrete engines (stages.Executor /
// aggregator.Aggregator — explicitly sanctioned by the build spec). Every
// other collaborator is inverted behind a seam (seams.go) so there is NO
// internal/config, NO internal/infra/* (observability/concurrency/kvstore/
// broker), NO internal/egress/*, NO internal/agents/* import — the universal
// internal/application/* hermeticity invariant (stages / aggregator CLAUDE.md;
// the seamed-telemetry + ctor-param precedent).
var allowedInternal = map[string]struct{}{
	"contractpro/legal-intelligence-core/internal/domain/model":                {},
	"contractpro/legal-intelligence-core/internal/domain/port":                 {},
	"contractpro/legal-intelligence-core/internal/application/pipeline/stages": {},
	"contractpro/legal-intelligence-core/internal/application/aggregator":      {},
}

// forbiddenInternal is the build-spec §7 active-fail set: packages whose
// presence in allowedInternal (a future "consistency" edit) would silently
// break the hermeticity contract. TestHermeticImports asserts they are NOT
// in the allowlist BEFORE scanning, so a regression fails loudly here rather
// than passing the import scan because someone widened the allowlist.
var forbiddenInternal = []string{
	"contractpro/legal-intelligence-core/internal/config",
	"contractpro/legal-intelligence-core/internal/infra/observability/metrics",
	"contractpro/legal-intelligence-core/internal/infra/observability/tracer",
	"contractpro/legal-intelligence-core/internal/infra/observability/logger",
	"contractpro/legal-intelligence-core/internal/infra/concurrency",
	"contractpro/legal-intelligence-core/internal/infra/kvstore",
	"contractpro/legal-intelligence-core/internal/infra/broker",
	"contractpro/legal-intelligence-core/internal/egress/publisher",
	"contractpro/legal-intelligence-core/internal/agents/base",
}

// TestHermeticImports pins that non-test files import only stdlib + the
// 4-entry allowlist. It ACTIVELY fails if any forbidden internal ever lands
// in the allowlist, and flags any third-party import (notably
// golang.org/x/sync — verified absent: the Orchestrator awaits sequentially
// and delegates parallelism to stages.parallel(), so it needs no errgroup).
// Stdlib allowed includes encoding/json (parent RISK_ANALYSIS unmarshal).
func TestHermeticImports(t *testing.T) {
	for _, forbidden := range forbiddenInternal {
		if _, present := allowedInternal[forbidden]; present {
			t.Fatalf("allowedInternal must NOT contain %q — the Pipeline Orchestrator is hermetic (stdlib + model/port + pipeline/stages + aggregator only); telemetry/clock/logger/cache/limiter/pause are seamed and config is a ctor param (build-spec §7)", forbidden)
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
				t.Errorf("%s imports third-party package %q (this package must stay third-party-free — golang.org/x/sync included)", name, path)
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
// file in the package (the aggregator/stages TestGofmtClean approach).
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
