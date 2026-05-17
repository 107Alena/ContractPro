package base

import (
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// allowedInternal is the EXACT set of first-party packages base's
// non-test source may import. Hermeticity invariant (CLAUDE.md): stdlib +
// internal/domain/{model,port} + the sibling leaf agents packages base
// composes. NO internal/infra/observability/* (telemetry is seamed), NO
// internal/llm/* , NO third-party.
//
// This test pins base's DIRECT imports only. The transitive-confinement
// guarantee ("schemavalidator re-exposes gojsonschema only via error, so
// base gains no third-party surface") is pinned in schemavalidator itself
// (schemavalidator.TestSingleThirdPartyImport), not here — this test cannot
// and does not prove it; it cross-references it.
var allowedInternal = map[string]struct{}{
	"contractpro/legal-intelligence-core/internal/agents/promptbuilder":   {},
	"contractpro/legal-intelligence-core/internal/agents/schemavalidator": {},
	"contractpro/legal-intelligence-core/internal/domain/model":           {},
	"contractpro/legal-intelligence-core/internal/domain/port":            {},
}

// TestHermeticImports pins that base's NON-test files import only stdlib +
// the allowlist — no concrete infra/llm, no third-party (mirrors
// schemavalidator.TestSingleThirdPartyImport, router seam discipline).
func TestHermeticImports(t *testing.T) {
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
			// Third-party / non-std always carry a dot in the path
			// (github.com/…, go.opentelemetry.io/…). Stdlib never does.
			// The module path "contractpro/legal-intelligence-core/…" also
			// has no dot, so it is handled by the internal allowlist below.
			if strings.HasPrefix(path, "contractpro/legal-intelligence-core/internal/") {
				if _, ok := allowedInternal[path]; !ok {
					t.Errorf("%s imports forbidden internal package %q (hermeticity breach)", name, path)
				}
				continue
			}
			if strings.Contains(path, ".") {
				t.Errorf("%s imports third-party package %q (base must stay third-party-free)", name, path)
			}
			// else: stdlib — allowed.
		}
	}
	if checked == 0 {
		t.Fatal("no non-test .go files found to check")
	}
}

// TestGofmtClean is a self-check: the sandbox blocks gofmt/`go fmt`, so the
// canonical formatting is asserted in-process via go/format over every .go
// file in the package (same approach as schemavalidator.TestGofmtClean).
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
			t.Fatalf("format.Source(%s): %v", name, ferr)
		}
		if string(want) != string(src) {
			t.Errorf("%s is not gofmt-clean", filepath.Base(name))
		}
	}
}

// TestSpecInterface_Shape is a structural sanity pin: a Spec implementation
// is just the two methods (no hidden state contract beyond the godoc), so a
// trivial struct satisfies it — guards an accidental signature change that
// would break the 9 per-agent packages (LIC-TASK-025..033).
func TestSpecInterface_Shape(t *testing.T) {
	var _ Spec = fakeSpec{}
}
