package schemavalidator

import (
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestGofmtClean asserts every .go file in this package (source AND tests) is
// gofmt-formatted. The house style requires byte-clean gofmt; this enforces
// it via the build toolchain since the sandboxed `gofmt`/`go fmt` are not
// runnable here.
func TestGofmtClean(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		want, err := format.Source(src)
		if err != nil {
			t.Fatalf("%s: format.Source: %v", name, err)
		}
		if string(want) != string(src) {
			t.Errorf("%s is not gofmt-clean", filepath.Base(name))
		}
	}
}

// TestRepairPrompt_VerbatimSSOT pins repairPromptTemplate byte-exact against
// error-handling.md §5.2 (the verbatim SSOT). The mid-sentence hard line
// break inside "без объяснений и\npreamble" is intentional and present in
// §5.2 — deviating from a frozen prompt SSOT is the larger risk
// (code-architect item 7). %s is the {validation_errors_pretty_printed} slot.
func TestRepairPrompt_VerbatimSSOT(t *testing.T) {
	const want = "Твой предыдущий ответ не прошёл валидацию по схеме.\n\n" +
		"Ошибки валидации:\n%s\n\n" +
		"Исправь ответ. Возвращай ТОЛЬКО валидный JSON по исходной схеме, без объяснений и\n" +
		"preamble. Не добавляй markdown. Не цитируй ошибки в ответе."
	if repairPromptTemplate != want {
		t.Fatalf("repairPromptTemplate drifted from error-handling.md §5.2:\n got=%q\nwant=%q",
			repairPromptTemplate, want)
	}
	const placeholder = "%s"
	if !strings.Contains(repairPromptTemplate, placeholder) {
		t.Fatalf("repairPromptTemplate must keep the %q validation-errors placeholder", placeholder)
	}
}

func TestNewSchemaViolation_SortedDedupedNonEmpty(t *testing.T) {
	v := newSchemaViolation([]string{"  b ", "a", "b", "", "   ", "a"})
	if got := v.Pretty(); got != "a\nb" {
		t.Fatalf("Pretty() = %q, want %q (sorted, de-duped, trimmed)", got, "a\nb")
	}
	empty := newSchemaViolation([]string{"", "   "})
	if len(empty.Errors) != 1 || empty.Errors[0] == "" {
		t.Fatalf("an all-empty input must yield a single sentinel message, got %#v", empty.Errors)
	}
}

// TestSingleThirdPartyImport confines the deliberate hermeticity exception:
// the ONLY third-party import allowed anywhere in this package's non-test
// source is github.com/xeipuuv/gojsonschema. Everything else must be stdlib
// or internal/domain (code-architect Q4). This prevents the exception from
// silently spreading.
func TestSingleThirdPartyImport(t *testing.T) {
	const (
		modulePrefix = "contractpro/legal-intelligence-core/"
		allowed      = "github.com/xeipuuv/gojsonschema"
	)
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse package dir: %v", err)
	}
	if len(pkgs) == 0 {
		t.Fatal("no non-test package source found")
	}
	for _, pkg := range pkgs {
		for fname, file := range pkg.Files {
			for _, imp := range file.Imports {
				path, _ := strconv.Unquote(imp.Path.Value)
				switch {
				case strings.HasPrefix(path, modulePrefix):
					// internal/domain etc. — fine.
				case !strings.Contains(strings.SplitN(path, "/", 2)[0], "."):
					// stdlib (first segment has no dot) — fine.
				case path == allowed:
					// the single documented exception.
				default:
					t.Fatalf("%s imports forbidden third-party package %q; "+
						"only %q is allowed in this package (see CLAUDE.md)", fname, path, allowed)
				}
			}
		}
	}
}
