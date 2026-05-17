package aggregator

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
// non-test source may import. The Result Aggregator is a deterministic,
// no-LLM application component: its ONLY first-party dependency is the domain
// model. Telemetry is inverted behind the Metrics seam and scoring weights
// arrive via the local Config struct, so there is NO internal/config, NO
// internal/infra/observability/metrics, NO internal/agents/* import (the
// universal internal/* hermeticity invariant — schemavalidator CLAUDE.md;
// the agents/* ctor-param + seamed-telemetry precedent).
var allowedInternal = map[string]struct{}{
	"contractpro/legal-intelligence-core/internal/domain/model": {},
}

// TestHermeticImports pins that non-test files import only stdlib + the
// single-entry model allowlist. It ACTIVELY fails if internal/config,
// internal/infra/* or internal/agents/* ever appears (the deliberate
// hermetic divergence; mirrors every agents/* TestHermeticImports).
func TestHermeticImports(t *testing.T) {
	for _, forbidden := range []string{
		"contractpro/legal-intelligence-core/internal/config",
		"contractpro/legal-intelligence-core/internal/infra/observability/metrics",
		"contractpro/legal-intelligence-core/internal/agents/base",
	} {
		if _, present := allowedInternal[forbidden]; present {
			t.Fatalf("allowedInternal must NOT contain %q — the Result Aggregator is hermetic (stdlib + internal/domain/model only); telemetry is seamed and scoring config is a ctor param (D7)", forbidden)
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
				t.Errorf("%s imports third-party package %q (this package must stay third-party-free)", name, path)
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
// file in the package (the schemavalidator/agents TestGofmtClean approach).
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
