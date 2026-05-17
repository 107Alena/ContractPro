package stages

import (
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// allowedInternal is the EXACT set of first-party packages this package's
// non-test source may import. The Stage Executor is a thin orchestrator: its
// ONLY first-party dependencies are the domain model + the agent port it
// drives. Telemetry is inverted behind the StageMetrics/Tracer seams; the
// parallel() helper is stdlib-only (NO golang.org/x/sync — the offline-build
// reconciliation, see CLAUDE.md). So there is NO internal/config, NO
// internal/infra/observability/*, NO internal/agents/*, NO
// internal/application/aggregator import (the universal internal/*
// hermeticity invariant — the aggregator/agents TestHermeticImports
// precedent).
var allowedInternal = map[string]struct{}{
	"contractpro/legal-intelligence-core/internal/domain/model": {},
	"contractpro/legal-intelligence-core/internal/domain/port":  {},
}

// TestHermeticImports pins that non-test files import only stdlib + the
// two-entry model/port allowlist. It ACTIVELY fails if internal/config,
// internal/infra/*, internal/agents/* or internal/application/aggregator ever
// appears, and if ANY third-party package is imported (in particular
// golang.org/x/sync — the offline constraint: parallel() must stay
// stdlib-only; mirrors every aggregator/agents TestHermeticImports +
// schemavalidator's single-exception confinement, INVERSELY: this package is
// fully hermetic, schemavalidator stays the codebase's only exception).
func TestHermeticImports(t *testing.T) {
	for _, forbidden := range []string{
		"contractpro/legal-intelligence-core/internal/config",
		"contractpro/legal-intelligence-core/internal/infra/observability/metrics",
		"contractpro/legal-intelligence-core/internal/infra/observability/tracer",
		"contractpro/legal-intelligence-core/internal/agents/base",
		"contractpro/legal-intelligence-core/internal/application/aggregator",
	} {
		if _, present := allowedInternal[forbidden]; present {
			t.Fatalf("allowedInternal must NOT contain %q — the Stage Executor is hermetic (stdlib + internal/domain/{model,port} only); telemetry is seamed and parallel() is stdlib-only (code-architect D1 Option A / B-1..B-4)", forbidden)
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
				t.Errorf("%s imports third-party package %q (this package must stay third-party-free — in particular NO golang.org/x/sync; parallel() is the stdlib errgroup equivalent, CLAUDE.md)", name, path)
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
// file in the package (the aggregator/schemavalidator TestGofmtClean
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
