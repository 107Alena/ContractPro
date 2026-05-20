package health

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
// non-test source may import. The health Handler is a pure stdlib-only
// primitive: dependency checks are inverted behind the local Checker seam
// (architect D1) and the metrics handler is injected as a plain
// http.Handler (architect D6). So the allowlist is EMPTY — the package
// imports zero first-party packages and zero third-party packages.
var allowedInternal = map[string]struct{}{}

// TestHermeticImports pins that non-test files import ONLY the standard
// library. It ACTIVELY fails if internal/config, internal/infra/broker,
// internal/infra/kvstore, internal/llm/router, OR ANY third-party (notably
// github.com/prometheus/client_golang/prometheus/promhttp,
// github.com/prometheus/client_golang) ever appears. This test is the
// enforcement mechanism for D1 ("hermetic seam; concrete adapters live in
// app-wiring") and D6 ("no promhttp import — metricsHandler is injected").
// Recorded so this test is neither "consistency-deleted" to match broker/
// kvstore (which import amqp/go-redis and ship no hermetic test) nor
// weakened to admit prometheus.
func TestHermeticImports(t *testing.T) {
	for _, forbidden := range []string{
		"contractpro/legal-intelligence-core/internal/config",
		"contractpro/legal-intelligence-core/internal/infra/broker",
		"contractpro/legal-intelligence-core/internal/infra/kvstore",
		"contractpro/legal-intelligence-core/internal/llm/router",
		"github.com/prometheus/client_golang/prometheus/promhttp",
		"github.com/prometheus/client_golang",
	} {
		if _, present := allowedInternal[forbidden]; present {
			t.Fatalf("allowedInternal must NOT contain %q — health is hermetic (stdlib only); dependency probes are seamed via Checker (D1) and the metrics handler is injected (D6)", forbidden)
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
				t.Errorf("%s imports third-party package %q (this package must stay third-party-free — Checker seam D1, metricsHandler injection D6)", name, path)
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
// approach; concurrency package precedent).
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
