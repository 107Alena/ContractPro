package artifacts

import (
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestHermeticImports pins that non-test files import ONLY stdlib — this
// shared decoder package has the strictest allowlist in the agents layer: it
// must NOT import internal/domain, any sibling agent package, infra/llm, the
// DocumentProcessing module, or any third-party (mirrors
// base.TestHermeticImports / typeclassifier.TestHermeticImports, tightened to
// stdlib-only because a shared leaf with first-party deps would re-introduce
// the very coupling this package exists to remove).
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
			if strings.HasPrefix(path, "contractpro/") {
				t.Errorf("%s imports first-party package %q (this shared leaf must stay stdlib-only)", name, path)
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
// file in the package (same approach as base.TestGofmtClean).
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
