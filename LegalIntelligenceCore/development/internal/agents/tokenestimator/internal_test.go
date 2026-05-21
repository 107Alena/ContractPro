package tokenestimator

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

// allowedInternal is the EXACT set of first-party packages tokenestimator's
// non-test source may import. Hermeticity invariant (CLAUDE.md): stdlib +
// internal/domain/{model,port}. NO internal/config (Config is ctor-injected),
// NO internal/agents/base (the LIC-TASK-047 wiring goes the other way — base
// imports tokenestimator via the TokenEstimator seam), NO internal/infra/*,
// NO third-party.
//
// Mirrors internal/agents/base/internal_test.go's allowlist mechanism.
var allowedInternal = map[string]struct{}{
	"contractpro/legal-intelligence-core/internal/domain/model": {},
	"contractpro/legal-intelligence-core/internal/domain/port":  {},
}

// TestHermeticImports pins that tokenestimator's NON-test files import only
// stdlib + the allowlist — no internal/agents/base (inverted wiring), no
// concrete infra/llm, no third-party.
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
				t.Errorf("%s imports third-party package %q (tokenestimator must stay third-party-free)", name, path)
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
// file in the package (same approach as base.TestGofmtClean /
// schemavalidator.TestGofmtClean).
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
