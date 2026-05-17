package concurrency

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
// non-test source may import. The concurrency Semaphore is a pure
// stdlib-only primitive: it needs neither internal/domain/model (unlike the
// aggregator) nor any infra/config package — observability is inverted behind
// the local Gauge seam (code-architect D3/D9). So the allowlist is EMPTY.
var allowedInternal = map[string]struct{}{}

// TestHermeticImports pins that non-test files import ONLY the standard
// library. It ACTIVELY fails if internal/config, the metrics package,
// internal/domain/model, internal/agents/* or ANY third-party (notably
// github.com/prometheus/...) ever appears. This test is the enforcement
// mechanism for D3 ("no direct prometheus import — the Gauge seam instead"):
// unlike sibling infra/broker & infra/kvstore (which import amqp/go-redis and
// ship no hermetic test), infra/concurrency is hermetic. Recorded so this
// test is neither "consistency-deleted" to match broker/kvstore nor weakened
// to admit prometheus.
func TestHermeticImports(t *testing.T) {
	for _, forbidden := range []string{
		"contractpro/legal-intelligence-core/internal/config",
		"contractpro/legal-intelligence-core/internal/infra/observability/metrics",
		"contractpro/legal-intelligence-core/internal/domain/model",
		"contractpro/legal-intelligence-core/internal/agents/base",
	} {
		if _, present := allowedInternal[forbidden]; present {
			t.Fatalf("allowedInternal must NOT contain %q — concurrency is hermetic (stdlib only); the gauge is seamed (D3)", forbidden)
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
				t.Errorf("%s imports third-party package %q (this package must stay third-party-free — the gauge is seamed, D3)", name, path)
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
// file in the package (the aggregator/schemavalidator TestGofmtClean approach).
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
